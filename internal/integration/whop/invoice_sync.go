package whop

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// InvoiceSyncService handles synchronisation of Flexprice invoices with Whop
type InvoiceSyncService struct {
	client                       WhopClient
	invoiceRepo                  invoice.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewInvoiceSyncService creates a new Whop invoice sync service
func NewInvoiceSyncService(
	client WhopClient,
	invoiceRepo invoice.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) *InvoiceSyncService {
	return &InvoiceSyncService{
		client:                       client,
		invoiceRepo:                  invoiceRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// SyncInvoiceToWhop syncs a Flexprice invoice to Whop.
// Flow:
//  1. Verify Whop connection exists
//  2. Idempotency: if already synced, return mapping
//  3. Ensure product exists (GET; create if missing and persist product_id back)
//  4. Create one-time Whop invoice with plan.initial_price = flexprice invoice total
//  5. Persist entity integration mapping
func (s *InvoiceSyncService) SyncInvoiceToWhop(
	ctx context.Context,
	req WhopInvoiceSyncRequest,
	customerService interfaces.CustomerService,
) (*WhopInvoiceSyncResponse, error) {
	s.logger.Infow("starting Whop invoice sync", "invoice_id", req.InvoiceID)

	if !s.client.HasWhopConnection(ctx) {
		return nil, ierr.NewError("Whop connection not available").
			WithHint("Whop integration must be configured for invoice sync").
			Mark(ierr.ErrNotFound)
	}

	flexInvoice, err := s.invoiceRepo.Get(ctx, req.InvoiceID)
	if err != nil {
		return nil, err
	}

	existingMapping, err := s.getExistingWhopMapping(ctx, req.InvoiceID)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}
	if existingMapping != nil {
		s.logger.Infow("invoice already synced to Whop",
			"invoice_id", req.InvoiceID,
			"whop_invoice_id", existingMapping.ProviderEntityID)
		return &WhopInvoiceSyncResponse{
			WhopInvoiceID: existingMapping.ProviderEntityID,
		}, nil
	}

	productID, err := s.ensureProduct(ctx)
	if err != nil {
		return nil, err
	}

	cfg, err := s.client.GetWhopConfig(ctx)
	if err != nil {
		return nil, err
	}

	customerName, customerEmail, err := s.resolveCustomer(ctx, flexInvoice, customerService)
	if err != nil {
		return nil, err
	}

	defaultDueDate := time.Now().AddDate(0, 0, DefaultInvoiceDueDays).UTC()
	dueDate := defaultDueDate
	if flexInvoice.DueDate != nil && flexInvoice.DueDate.After(time.Now()) {
		dueDate = flexInvoice.DueDate.UTC()
	}

	// Determine collection method based on whether we have a saved member mapping.
	// TODO: configurable collection method
	collectionMethod := WhopCollectionMethodSendInvoice
	paymentMethodID := ""
	if flexInvoice.CustomerID != "" {
		if savedPaymentMethodID, err := s.resolvePaymentMethod(ctx, flexInvoice.CustomerID); err != nil {
			s.logger.Infow("could not resolve Whop payment method, falling back to send_invoice",
				"customer_id", flexInvoice.CustomerID, "error", err)
		} else if savedPaymentMethodID != "" {
			collectionMethod = WhopCollectionMethodChargeAutomatically
			paymentMethodID = savedPaymentMethodID
		}
	}

	createReq := CreateInvoiceRequest{
		CompanyID: cfg.CompanyID,
		ProductID: productID,
		Plan: CreateInvoicePlan{
			InitialPrice:  flexInvoice.AmountDue.Round(2).InexactFloat64(),
			PlanType:      WhopPlanTypeOneTime,
			InternalNotes: flexInvoice.CustomerID, // always store customer_id so payment.succeeded can resolve it
		},
		CollectionMethod: collectionMethod,
		PaymentMethodID:  paymentMethodID,
		DueDate:          dueDate.Format(time.RFC3339),
		CustomerName:     customerName,
		EmailAddress:     customerEmail,
	}

	whopInvoice, err := s.client.CreateInvoice(ctx, createReq)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create invoice in Whop").
			Mark(ierr.ErrHTTPClient)
	}
	s.logger.Infow("created Whop invoice",
		"amount_due", flexInvoice.AmountDue.String(),
		"flexprice_invoice_id", flexInvoice.ID,
		"whop_invoice_id", whopInvoice.ID)

	if whopInvoice.CurrentPlan.ID != "" {
		plan, planErr := s.client.GetPlan(ctx, whopInvoice.CurrentPlan.ID)
		if planErr != nil {
			s.logger.Warnw("failed to fetch Whop plan for purchase_url",
				"plan_id", whopInvoice.CurrentPlan.ID, "error", planErr)
		} else if plan.PurchaseURL != "" {
			if flexInvoice.Metadata == nil {
				flexInvoice.Metadata = make(types.Metadata)
			}
			flexInvoice.Metadata["whop_checkout_url"] = plan.PurchaseURL
			if updateErr := s.invoiceRepo.Update(ctx, flexInvoice); updateErr != nil {
				s.logger.Warnw("failed to store whop_checkout_url on invoice",
					"invoice_id", req.InvoiceID, "error", updateErr)
			}
		}
	}

	if err := s.createInvoiceMapping(ctx, req.InvoiceID, whopInvoice.ID); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create Whop invoice mapping").
			Mark(ierr.ErrDatabase)
	}

	s.logger.Infow("successfully synced invoice to Whop",
		"invoice_id", req.InvoiceID,
		"whop_invoice_id", whopInvoice.ID)

	return &WhopInvoiceSyncResponse{
		WhopInvoiceID: whopInvoice.ID,
		Status:        whopInvoice.Status,
	}, nil
}

// MarkInvoicePaidInWhop marks the corresponding Whop invoice as paid.
func (s *InvoiceSyncService) MarkInvoicePaidInWhop(ctx context.Context, flexpriceInvoiceID string) error {
	if !s.client.HasWhopConnection(ctx) {
		return ierr.NewError("Whop connection not available").
			WithHint("Whop integration must be configured").
			Mark(ierr.ErrNotFound)
	}

	flexInvoice, err := s.invoiceRepo.Get(ctx, flexpriceInvoiceID)
	if err != nil {
		return err
	}
	if flexInvoice.PaymentStatus != types.PaymentStatusSucceeded {
		s.logger.Infow("invoice not in succeeded payment state, skipping Whop mark_paid",
			"invoice_id", flexpriceInvoiceID,
			"payment_status", flexInvoice.PaymentStatus)
		return nil
	}

	mapping, err := s.getExistingWhopMapping(ctx, flexpriceInvoiceID)
	if ierr.IsNotFound(err) {
		s.logger.Infow("no Whop mapping for invoice, skipping mark_paid",
			"invoice_id", flexpriceInvoiceID)
		return nil
	}
	if err != nil {
		return err
	}

	if err := s.client.MarkInvoicePaid(ctx, mapping.ProviderEntityID); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to mark Whop invoice as paid").
			Mark(ierr.ErrHTTPClient)
	}

	s.logger.Infow("marked Whop invoice as paid",
		"invoice_id", flexpriceInvoiceID,
		"whop_invoice_id", mapping.ProviderEntityID)
	return nil
}

// ensureProduct verifies the Whop product exists; creates one if not and persists ID back to connection
func (s *InvoiceSyncService) ensureProduct(ctx context.Context) (string, error) {
	cfg, err := s.client.GetWhopConfig(ctx)
	if err != nil {
		return "", err
	}

	if cfg.ProductID != "" {
		if _, err := s.client.GetProduct(ctx, cfg.ProductID); err == nil {
			return cfg.ProductID, nil
		}
		s.logger.Warnw("configured Whop product not found, creating a new one",
			"product_id", cfg.ProductID)
	}

	product, err := s.client.CreateProduct(ctx, CreateProductRequest{
		CompanyID:  cfg.CompanyID,
		Title:      DefaultProductTitle,
		Visibility: WhopVisibilityQuickLink,
	})
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to create Whop product").
			Mark(ierr.ErrHTTPClient)
	}

	if err := s.client.UpdateProductID(ctx, product.ID); err != nil {
		s.logger.Errorw("failed to persist Whop product ID to connection", "error", err,
			"product_id", product.ID)
	}

	return product.ID, nil
}

func (s *InvoiceSyncService) resolveCustomer(
	ctx context.Context,
	flexInvoice *invoice.Invoice,
	customerService interfaces.CustomerService,
) (name, email string, err error) {
	name = "Customer"
	if flexInvoice.CustomerID == "" {
		return "", "", ierr.NewError("invoice has no customer_id; Whop invoice requires customer email").
			WithHint("Attach a customer with an email address to the invoice before syncing to Whop").
			Mark(ierr.ErrValidation)
	}

	cust, custErr := customerService.GetCustomer(ctx, flexInvoice.CustomerID)
	if custErr != nil {
		s.logger.Warnw("failed to fetch customer for Whop invoice sync",
			"customer_id", flexInvoice.CustomerID, "error", custErr)
		return "", "", ierr.WithError(custErr).
			WithHint("Failed to fetch customer for Whop invoice sync").
			Mark(ierr.ErrDatabase)
	}

	if cust.Name != "" {
		name = cust.Name
	}
	if cust.Email == "" {
		return "", "", ierr.NewError("customer has no email address; Whop invoice requires email").
			WithHint("Add an email address to the customer before syncing to Whop").
			Mark(ierr.ErrValidation)
	}
	email = cust.Email
	return name, email, nil
}

func (s *InvoiceSyncService) getExistingWhopMapping(ctx context.Context, invoiceID string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	filter := &types.EntityIntegrationMappingFilter{
		EntityIDs:     []string{invoiceID},
		EntityType:    types.IntegrationEntityTypeInvoice,
		ProviderTypes: []string{string(types.SecretProviderWhop)},
	}
	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(mappings) == 0 {
		return nil, ierr.NewError("no Whop mapping found").Mark(ierr.ErrNotFound)
	}
	return mappings[0], nil
}

// resolvePaymentMethod looks up the customer→whop member mapping and fetches their first payment method.
// Returns (paymentMethodID, nil) if found, ("", nil) if no mapping exists, or ("", err) on API failure.
func (s *InvoiceSyncService) resolvePaymentMethod(ctx context.Context, customerID string) (string, error) {
	filter := &types.EntityIntegrationMappingFilter{
		EntityIDs:     []string{customerID},
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{string(types.SecretProviderWhop)},
	}
	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return "", err
	}
	if len(mappings) == 0 {
		return "", nil // no mapping — caller falls back to send_invoice
	}

	memberID := mappings[0].ProviderEntityID
	paymentMethods, err := s.client.GetPaymentMethods(ctx, memberID)
	if err != nil {
		return "", err
	}
	if len(paymentMethods.Data) == 0 {
		s.logger.Infow("no payment methods on Whop member, falling back to send_invoice",
			"customer_id", customerID, "member_id", memberID)
		return "", nil
	}

	// first payment method from the fetched list is returned
	// TODO: allow users to select their default payment method
	paymentMethodID := paymentMethods.Data[0].ID
	s.logger.Infow("resolved Whop payment method for customer",
		"customer_id", customerID, "member_id", memberID, "payment_method_id", paymentMethodID)
	return paymentMethodID, nil
}

// CreateCustomerMapping creates an entity_integration_mapping for customer→Whop member.
// Called from the webhook handler when a payment.succeeded event is received.
// Treats ErrAlreadyExists as success — concurrent webhook deliveries of the same event are safe.
func (s *InvoiceSyncService) CreateCustomerMapping(ctx context.Context, customerID, memberID string) error {
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         customerID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderWhop),
		ProviderEntityID: memberID,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
		Metadata: map[string]interface{}{
			"synced_via": "whop_payment_succeeded_webhook",
		},
	}
	if err := s.entityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		if ierr.IsAlreadyExists(err) {
			s.logger.Infow("customer→Whop mapping already exists, skipping",
				"customer_id", customerID)
			return nil
		}
		return err
	}
	return nil
}

func (s *InvoiceSyncService) createInvoiceMapping(ctx context.Context, flexpriceInvoiceID, whopInvoiceID string) error {
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         flexpriceInvoiceID,
		EntityType:       types.IntegrationEntityTypeInvoice,
		ProviderType:     string(types.SecretProviderWhop),
		ProviderEntityID: whopInvoiceID,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
		Metadata: map[string]interface{}{
			"synced_via": "whop_invoice_sync",
		},
	}
	return s.entityIntegrationMappingRepo.Create(ctx, mapping)
}
