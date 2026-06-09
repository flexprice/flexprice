package paddle

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	paddlesdk "github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/PaddleHQ/paddle-go-sdk/v4/pkg/paddlenotification"
	apidto "github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	temporalmodels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/samber/lo"
)

// PaddleSyncService orchestrates syncing FlexPrice entities to Paddle.
type PaddleSyncService struct {
	client           PaddleClient
	customerRepo     customer.Repository
	invoiceRepo      invoice.Repository
	subscriptionRepo subscription.Repository
	mappingService   interfaces.EntityIntegrationMappingService
	connectionRepo   connection.Repository
	logger           *logger.Logger
	authSecret       string
	temporalSvc      temporalservice.TemporalService
	paymentService   interfaces.PaymentService
	invoiceService   interfaces.InvoiceService
}

// NewPaddleSyncService creates a new PaddleSyncService.
func NewPaddleSyncService(
	client PaddleClient,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	subscriptionRepo subscription.Repository,
	mappingService interfaces.EntityIntegrationMappingService,
	connectionRepo connection.Repository,
	log *logger.Logger,
	authSecret string,
	temporalSvc temporalservice.TemporalService,
) *PaddleSyncService {
	return &PaddleSyncService{
		client:           client,
		customerRepo:     customerRepo,
		invoiceRepo:      invoiceRepo,
		subscriptionRepo: subscriptionRepo,
		mappingService:   mappingService,
		connectionRepo:   connectionRepo,
		logger:           log,
		authSecret:       authSecret,
		temporalSvc:      temporalSvc,
	}
}

// SetServices sets the payment and invoice services needed by PullAndUpdateInvoice.
func (s *PaddleSyncService) SetServices(paymentService interfaces.PaymentService, invoiceService interfaces.InvoiceService) {
	s.paymentService = paymentService
	s.invoiceService = invoiceService
}

// EnsureCustomerSynced ensures the given FlexPrice customer exists in Paddle
// and that a corresponding EntityIntegrationMapping row is present.
// It is idempotent: if the customer is already synced it returns the existing
// Paddle IDs and Created=false.
// upsertCustomerPaddleMetadata merges paddle_customer_id into the FlexPrice customer's
// metadata so downstream services can read the Paddle ID without querying the mapping table.
// Errors are logged and swallowed — the sync itself succeeded; metadata update is best-effort.
func (s *PaddleSyncService) upsertCustomerPaddleMetadata(ctx context.Context, c *customer.Customer, paddleCustomerID string) {
	if c.Metadata == nil {
		c.Metadata = make(types.Metadata)
	}
	if c.Metadata[MetaKeyPaddleCustomerID] == paddleCustomerID {
		return // already up-to-date, skip the write
	}
	c.Metadata[MetaKeyPaddleCustomerID] = paddleCustomerID
	if err := s.customerRepo.Update(ctx, c); err != nil {
		s.logger.Warnw("failed to update customer metadata with paddle_customer_id",
			"customer_id", c.ID, "paddle_customer_id", paddleCustomerID, "error", err)
	}
}

func (s *PaddleSyncService) EnsureCustomerSynced(ctx context.Context, req EnsureCustomerSyncedRequest) (*EnsureCustomerSyncedResponse, error) {
	flexCustomer, err := s.customerRepo.Get(ctx, req.CustomerID)
	if err != nil {
		return nil, err
	}
	if flexCustomer.Email == "" {
		return nil, ierr.NewError("customer email is required for Paddle sync").
			WithHint("Add email to the customer before syncing to Paddle").
			WithReportableDetails(map[string]interface{}{"customer_id": req.CustomerID}).
			Mark(ierr.ErrValidation)
	}

	// Check for an existing mapping.
	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      req.CustomerID,
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}
	resp, err := s.mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(resp.Items) > 0 {
		m := resp.Items[0]
		paddleCustomerID := m.ProviderEntityID
		paddleAddressID, _ := m.Metadata[MetaKeyPaddleAddressID].(string)

		paddleAddressID, err = s.syncAddressForMapping(ctx, flexCustomer, paddleCustomerID, paddleAddressID, m.ID)
		if err != nil {
			return nil, err
		}
		s.upsertCustomerPaddleMetadata(ctx, flexCustomer, paddleCustomerID)
		return &EnsureCustomerSyncedResponse{
			PaddleCustomerID: paddleCustomerID,
			PaddleAddressID:  paddleAddressID,
			Created:          false,
		}, nil
	}

	// Check if a Paddle customer with the same email already exists before attempting to create.
	// Paddle rejects CreateCustomer with customer_already_exists when the email is taken.
	paddleCustomerID, err := s.lookupPaddleCustomerByEmail(ctx, flexCustomer.Email)
	if err != nil {
		return nil, err
	}

	if paddleCustomerID == "" {
		// Create customer in Paddle.
		createReq := &paddlesdk.CreateCustomerRequest{
			Email: flexCustomer.Email,
			CustomData: map[string]interface{}{
				"flexprice_customer_id": flexCustomer.ID,
				"environment_id":        types.GetEnvironmentID(ctx),
			},
		}
		if flexCustomer.Name != "" {
			createReq.Name = paddlesdk.PtrTo(flexCustomer.Name)
		}
		paddleCustomer, createErr := s.client.CreateCustomer(ctx, createReq)
		if createErr != nil {
			return nil, ierr.WithError(createErr).WithHint("Failed to create customer in Paddle").Mark(ierr.ErrInternal)
		}
		paddleCustomerID = paddleCustomer.ID
	}

	var paddleAddressID string
	if flexCustomer.AddressCountry != "" {
		addr, addrErr := s.client.CreateAddress(ctx, paddleCustomerID, buildCreateAddressRequest(flexCustomer))
		if addrErr != nil {
			s.logger.Warnw("failed to create Paddle address after customer creation — proceeding",
				"customer_id", req.CustomerID, "error", addrErr)
		} else {
			paddleAddressID = addr.ID
		}
	}

	meta := map[string]interface{}{
		MetaKeyCreatedVia:       CreatedViaFlexpriceToProvider,
		MetaKeyPaddleCustomerID: paddleCustomerID,
		MetaKeySyncedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	if paddleAddressID != "" {
		meta[MetaKeyPaddleAddressID] = paddleAddressID
	}
	_, createErr := s.mappingService.CreateEntityIntegrationMapping(ctx, apidto.CreateEntityIntegrationMappingRequest{
		EntityID:         flexCustomer.ID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: paddleCustomerID,
		Metadata:         meta,
	})
	if createErr != nil {
		return nil, createErr
	}

	s.upsertCustomerPaddleMetadata(ctx, flexCustomer, paddleCustomerID)

	return &EnsureCustomerSyncedResponse{
		PaddleCustomerID: paddleCustomerID,
		PaddleAddressID:  paddleAddressID,
		Created:          true,
	}, nil
}

// lookupPaddleCustomerByEmail returns the Paddle customer ID for the given email, or "" if none exists.
func (s *PaddleSyncService) lookupPaddleCustomerByEmail(ctx context.Context, email string) (string, error) {
	result, err := s.client.ListCustomers(ctx, &paddlesdk.ListCustomersRequest{
		Email: []string{email},
	})
	if err != nil {
		return "", fmt.Errorf("looking up Paddle customer by email: %w", err)
	}
	if result == nil {
		return "", nil
	}
	first := result.Next(ctx)
	if first == nil || !first.Ok() {
		return "", nil
	}
	customer := first.Value()
	s.logger.Info(ctx, "found existing Paddle customer by email", "paddle_customer_id", customer.ID, "email", email)
	return customer.ID, nil
}

// EnsureBulkProductSynced ensures Paddle catalog products exist for all given FlexPrice prices.
// It issues a single mapping query for all price IDs, then creates only the unmapped ones.
func (s *PaddleSyncService) EnsureBulkProductSynced(ctx context.Context, req EnsureBulkProductSyncedRequest) (*EnsureBulkProductSyncedResponse, error) {
	if len(req.Items) == 0 {
		return &EnsureBulkProductSyncedResponse{PriceIDToPaddleProductID: map[string]string{}}, nil
	}

	priceIDs := make([]string, 0, len(req.Items))
	for _, item := range req.Items {
		if item.PriceID != "" {
			priceIDs = append(priceIDs, item.PriceID)
		}
	}

	bulkResp, err := s.mappingService.GetEntityIntegrationMappings(ctx, &types.EntityIntegrationMappingFilter{
		EntityIDs:     priceIDs,
		EntityType:    types.IntegrationEntityTypePrice,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
		QueryFilter:   types.NewDefaultQueryFilter(),
	})
	if err != nil {
		return nil, fmt.Errorf("fetching existing product mappings: %w", err)
	}

	result := make(map[string]string, len(req.Items))
	for _, m := range bulkResp.Items {
		result[m.EntityID] = m.ProviderEntityID
	}

	for _, item := range req.Items {
		if item.PriceID == "" || result[item.PriceID] != "" {
			continue
		}
		name := item.Name
		if name == "" {
			name = item.PriceID
		}
		product, err := s.client.CreateProduct(ctx, &paddlesdk.CreateProductRequest{
			Name:        name,
			TaxCategory: paddlesdk.TaxCategoryStandard,
			CustomData: map[string]interface{}{
				"flexprice_price_id": item.PriceID,
				"environment_id":     types.GetEnvironmentID(ctx),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("creating Paddle product for price %s: %w", item.PriceID, err)
		}
		_, err = s.mappingService.CreateEntityIntegrationMapping(ctx, apidto.CreateEntityIntegrationMappingRequest{
			EntityID:         item.PriceID,
			EntityType:       types.IntegrationEntityTypePrice,
			ProviderType:     string(types.SecretProviderPaddle),
			ProviderEntityID: product.ID,
			Metadata: map[string]interface{}{
				MetaKeyPaddleProductID: product.ID,
				MetaKeySyncedAt:        time.Now().UTC().Format(time.RFC3339),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("persisting product mapping for price %s: %w", item.PriceID, err)
		}
		result[item.PriceID] = product.ID
	}

	return &EnsureBulkProductSyncedResponse{PriceIDToPaddleProductID: result}, nil
}

// EnsureSubscriptionSynced bootstraps a Paddle subscription for a FlexPrice subscription.
//
// State machine:
//   - Mapping exists → already activated (return sub ID, no-op)
//   - Metadata has paddle_transaction_id → checkout in-progress (return stored checkout URL, no-op)
//   - Neither → create $0 bootstrap transaction, store txn ID + checkout URL in metadata
//
// The entity_integration_mapping is NOT created here; it is created by the subscription.activated webhook.
func (s *PaddleSyncService) EnsureSubscriptionSynced(ctx context.Context, req EnsureSubscriptionSyncedRequest) (*EnsureSubscriptionSyncedResponse, error) {
	sub := req.Subscription
	if sub == nil || sub.ID == "" {
		return nil, ierr.NewError("subscription is required").Mark(ierr.ErrValidation)
	}

	// Guard 1: fully activated — mapping exists.
	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      sub.ID,
		EntityType:    types.IntegrationEntityTypeSubscription,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}
	mappingResp, err := s.mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("checking subscription mapping: %w", err)
	}
	if len(mappingResp.Items) > 0 {
		return &EnsureSubscriptionSyncedResponse{
			PaddleSubscriptionID: mappingResp.Items[0].ProviderEntityID,
		}, nil
	}

	// Guard 2: bootstrap transaction already created — customer has not completed checkout yet.
	// The stored URL already includes the auth token (appended at creation time).
	if txnID := sub.Metadata[MetaKeyPaddleTransactionID]; txnID != "" {
		return &EnsureSubscriptionSyncedResponse{
			CheckoutURL: sub.Metadata[MetaKeyPaddleCheckoutURL],
		}, nil
	}

	if len(req.PriceIDToProductID) == 0 {
		return nil, ierr.NewError("no products to bootstrap subscription with").Mark(ierr.ErrValidation)
	}

	// EnsureCustomerSynced guarantees customer_id and address_id are present.
	customerResp, err := s.EnsureCustomerSynced(ctx, EnsureCustomerSyncedRequest{CustomerID: sub.GetInvoicingCustomerID()})
	if err != nil {
		return nil, fmt.Errorf("ensuring customer synced: %w", err)
	}
	if customerResp.PaddleAddressID == "" {
		return nil, ierr.NewError("Paddle address ID not found after customer sync").
			WithHint("Customer must have an address (country required) for Paddle subscription creation").
			WithReportableDetails(map[string]interface{}{"customer_id": sub.CustomerID}).
			Mark(ierr.ErrValidation)
	}

	// Create $0 catalog prices with billing_cycle so Paddle recognises this as a recurring subscription.
	billingCycle := paddleBillingCycle(sub.BillingPeriod, sub.BillingPeriodCount)
	currency := strings.ToUpper(sub.Currency)
	type bootstrapPair struct{ paddlePriceID string }
	pairs := make([]bootstrapPair, 0, len(req.PriceIDToProductID))
	for priceID, productID := range req.PriceIDToProductID {
		name := priceID
		for _, li := range sub.LineItems {
			if li != nil && li.PriceID == priceID && li.DisplayName != "" {
				name = li.DisplayName
				break
			}
		}
		catalogPrice, priceErr := s.client.CreatePrice(ctx, &paddlesdk.CreatePriceRequest{
			ProductID:    productID,
			Description:  name,
			Name:         paddlesdk.PtrTo(name),
			UnitPrice:    paddlesdk.Money{Amount: "0", CurrencyCode: paddlesdk.CurrencyCode(currency)},
			BillingCycle: billingCycle,
			TaxMode:      paddlesdk.PtrTo(paddlesdk.TaxModeAccountSetting),
			Quantity:     &paddlesdk.PriceQuantity{Minimum: 1, Maximum: 1},
		})
		if priceErr != nil {
			return nil, fmt.Errorf("creating bootstrap catalog price for product %s: %w", productID, priceErr)
		}
		pairs = append(pairs, bootstrapPair{catalogPrice.ID})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].paddlePriceID < pairs[j].paddlePriceID })

	items := make([]paddlesdk.CreateTransactionItems, 0, len(pairs))
	for _, p := range pairs {
		items = append(items, *paddlesdk.NewCreateTransactionItemsTransactionItemFromCatalog(
			&paddlesdk.TransactionItemFromCatalog{PriceID: p.paddlePriceID, Quantity: 1},
		))
	}

	txn, err := s.client.CreateTransaction(ctx, &paddlesdk.CreateTransactionRequest{
		CustomerID:     paddlesdk.PtrTo(customerResp.PaddleCustomerID),
		AddressID:      paddlesdk.PtrTo(customerResp.PaddleAddressID),
		CollectionMode: paddlesdk.PtrTo(paddlesdk.CollectionModeAutomatic),
		Items:          items,
		CustomData: map[string]interface{}{
			"flexprice_subscription_id": sub.ID,
			"environment_id":            types.GetEnvironmentID(ctx),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("creating bootstrap transaction: %w", err)
	}

	// Extract checkout URL from transaction response;
	checkoutURL := ""
	if txn.Checkout != nil && txn.Checkout.URL != nil {
		checkoutURL = lo.FromPtr(txn.Checkout.URL)
	}
	if checkoutURL == "" {
		if conn, connErr := s.client.GetConnection(ctx); connErr == nil && conn != nil && conn.Metadata != nil {
			if base, ok := conn.Metadata[ConnKeyCheckoutURL].(string); ok && base != "" {
				checkoutURL = base + "?_ptxn=" + txn.ID
			}
		}
	}

	// Append the auth token before persisting so any downstream reader of the metadata
	// gets the fully-formed URL without needing to call appendCheckoutToken separately.
	checkoutURLWithToken := s.appendCheckoutToken(ctx, checkoutURL)

	// Persist txn ID + checkout URL (with token) in subscription metadata so future calls hit Guard 2.
	if sub.Metadata == nil {
		sub.Metadata = make(types.Metadata)
	}
	sub.Metadata[MetaKeyPaddleTransactionID] = txn.ID
	if checkoutURLWithToken != "" {
		sub.Metadata[MetaKeyPaddleCheckoutURL] = checkoutURLWithToken
	}
	if err := s.subscriptionRepo.Update(ctx, sub); err != nil {
		return nil, fmt.Errorf("updating subscription metadata after bootstrap: %w", err)
	}

	return &EnsureSubscriptionSyncedResponse{
		CheckoutURL: checkoutURLWithToken,
		Created:     true,
	}, nil
}

// paddleBillingCycle maps a FlexPrice BillingPeriod + count to a Paddle Duration.
func paddleBillingCycle(period types.BillingPeriod, count int) *paddlesdk.Duration {
	if count <= 0 {
		count = 1
	}
	switch period {
	case types.BILLING_PERIOD_DAILY:
		return &paddlesdk.Duration{Interval: paddlesdk.IntervalDay, Frequency: count}
	case types.BILLING_PERIOD_WEEKLY:
		return &paddlesdk.Duration{Interval: paddlesdk.IntervalWeek, Frequency: count}
	case types.BILLING_PERIOD_ANNUAL:
		return &paddlesdk.Duration{Interval: paddlesdk.IntervalYear, Frequency: count}
	case types.BILLING_PERIOD_QUARTER:
		return &paddlesdk.Duration{Interval: paddlesdk.IntervalMonth, Frequency: 3 * count}
	default:
		return &paddlesdk.Duration{Interval: paddlesdk.IntervalMonth, Frequency: count}
	}
}

// syncAddressForMapping ensures the Paddle address for an already-mapped customer is up to date.
// paddleAddressID is the value currently stored in the mapping metadata (may be empty).
// mappingID is the EntityIntegrationMapping ID (used to update metadata if a new address is created).
// It returns the final paddleAddressID (possibly newly created).
func (s *PaddleSyncService) syncAddressForMapping(
	ctx context.Context,
	c *customer.Customer,
	paddleCustomerID, paddleAddressID string,
	mappingID string,
) (string, error) {
	if c.AddressCountry == "" {
		return paddleAddressID, nil
	}
	if paddleAddressID != "" {
		updateReq := &paddlesdk.UpdateAddressRequest{
			CountryCode: paddlesdk.NewPatchField(toCountryCode(c.AddressCountry)),
		}
		if c.AddressLine1 != "" {
			updateReq.FirstLine = paddlesdk.NewPtrPatchField(c.AddressLine1)
		}
		if c.AddressLine2 != "" {
			updateReq.SecondLine = paddlesdk.NewPtrPatchField(c.AddressLine2)
		}
		if c.AddressCity != "" {
			updateReq.City = paddlesdk.NewPtrPatchField(c.AddressCity)
		}
		if c.AddressPostalCode != "" {
			updateReq.PostalCode = paddlesdk.NewPtrPatchField(c.AddressPostalCode)
		}
		if c.AddressState != "" {
			updateReq.Region = paddlesdk.NewPtrPatchField(c.AddressState)
		}
		if _, err := s.client.UpdateAddress(ctx, paddleCustomerID, paddleAddressID, updateReq); err != nil {
			s.logger.Warnw("failed to update Paddle address — using existing",
				"error", err, "customer_id", c.ID, "paddle_address_id", paddleAddressID)
		}
		return paddleAddressID, nil
	}

	// No address ID yet — create one.
	addr, err := s.client.CreateAddress(ctx, paddleCustomerID, buildCreateAddressRequest(c))
	if err != nil {
		return "", err
	}
	if mappingID != "" {
		_, err = s.mappingService.UpdateEntityIntegrationMapping(ctx, mappingID, apidto.UpdateEntityIntegrationMappingRequest{
			Metadata: map[string]interface{}{
				MetaKeyPaddleAddressID: addr.ID,
				MetaKeySyncedAt:        time.Now().UTC().Format(time.RFC3339),
			},
		})
		if err != nil {
			s.logger.Warnw("failed to update mapping with new Paddle address ID", "error", err)
		}
	}
	return addr.ID, nil
}

// getExistingInvoiceMapping checks entity_integration_mapping for a prior Paddle sync of this invoice.
func (s *PaddleSyncService) getExistingInvoiceMapping(ctx context.Context, invoiceID string) (*apidto.EntityIntegrationMappingResponse, error) {
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:    types.IntegrationEntityTypeInvoice,
		EntityID:      invoiceID,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}
	resp, err := s.mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(resp.Items) == 0 {
		return nil, nil
	}
	return resp.Items[0], nil
}

// appendCheckoutToken appends a signed JWT containing client_side_token + success_url to the
// checkout URL so the frontend can initialize Paddle.js overlay checkout.
func (s *PaddleSyncService) appendCheckoutToken(ctx context.Context, checkoutURL string) string {
	if checkoutURL == "" {
		return checkoutURL
	}

	paddleConfig, err := s.client.GetPaddleConfig(ctx)
	if err != nil || paddleConfig == nil || paddleConfig.ClientSideToken == "" {
		s.logger.Debug(ctx, "skipping checkout token: client_side_token not configured")
		return checkoutURL
	}

	conn, err := s.client.GetConnection(ctx)
	if err != nil || conn == nil || conn.Metadata == nil {
		return checkoutURL
	}
	successURL, _ := conn.Metadata[ConnKeyRedirectURL].(string)

	claims := jwt.MapClaims{
		"client_side_token": paddleConfig.ClientSideToken,
		"success_url":       successURL,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte(s.authSecret))
	if err != nil {
		s.logger.Warnw("failed to sign Paddle checkout token", "error", err)
		return checkoutURL
	}

	parsed, err := url.Parse(checkoutURL)
	if err != nil {
		s.logger.Warnw("failed to parse Paddle checkout URL for token append",
			"error", err, "checkout_url", checkoutURL)
		return checkoutURL
	}
	q := parsed.Query()
	q.Set("token", signedToken)
	parsed.RawQuery = q.Encode()
	s.logger.Debug(ctx, "appended checkout token to Paddle checkout URL")
	return parsed.String()
}

// SyncInvoice is the main invoice sync orchestrator.
// Idempotent: returns early if the invoice is already mapped to a Paddle transaction.
// Fetches the FlexPrice subscription, builds real product-backed charge items at the
// invoice amount, and creates a Paddle subscription charge.
func (s *PaddleSyncService) SyncInvoice(ctx context.Context, req SyncInvoiceRequest) (*SyncInvoiceResponse, error) {
	if !s.client.HasPaddleConnection(ctx) {
		return nil, ierr.NewError("Paddle connection not available").
			WithHint("Paddle integration must be configured for invoice sync").
			Mark(ierr.ErrNotFound)
	}

	flexInvoice, err := s.invoiceRepo.Get(ctx, req.InvoiceID)
	if err != nil {
		return nil, fmt.Errorf("fetching invoice: %w", err)
	}

	// Step 1: Idempotency guard.
	existingMapping, err := s.getExistingInvoiceMapping(ctx, req.InvoiceID)
	if err != nil {
		return nil, fmt.Errorf("checking invoice mapping: %w", err)
	}
	if existingMapping != nil {
		checkoutURL, _ := existingMapping.Metadata[MetaKeyPaddleCheckoutURL].(string)
		paddleSubID, _ := existingMapping.Metadata[MetaKeyPaddleSubscriptionID].(string)
		return &SyncInvoiceResponse{
			PaddleTransactionID:  existingMapping.ProviderEntityID,
			PaddleSubscriptionID: paddleSubID,
			CheckoutURL:          s.appendCheckoutToken(ctx, checkoutURL),
			AlreadySynced:        true,
		}, nil
	}

	if flexInvoice.SubscriptionID == nil || *flexInvoice.SubscriptionID == "" {
		return nil, ierr.NewError("invoice has no subscription_id").
			WithHint("Paddle subscription-charge sync requires the invoice to be linked to a FlexPrice subscription").
			Mark(ierr.ErrValidation)
	}

	// Step 2: Fetch FlexPrice subscription.
	flexSub, err := s.subscriptionRepo.Get(ctx, *flexInvoice.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("fetching subscription: %w", err)
	}

	// Step 4: Ensure products synced.
	syncable := syncableInvoiceLineItems(flexInvoice.LineItems)
	productItems := make([]EnsureBulkProductSyncedItem, len(syncable))
	for i, li := range syncable {
		priceID := lo.FromPtr(li.PriceID)
		productItems[i] = EnsureBulkProductSyncedItem{
			PriceID: priceID,
			Name:    lo.FromPtrOr(li.DisplayName, priceID),
		}
	}
	productsResp, err := s.EnsureBulkProductSynced(ctx, EnsureBulkProductSyncedRequest{Items: productItems})
	if err != nil {
		return nil, fmt.Errorf("ensuring products synced: %w", err)
	}

	// Step 5: Ensure subscription synced.
	// By the time we reach here (via PaddleInvoiceSyncWorkflow step 2.5), the subscription
	// must already be activated — EnsureSubscriptionSynced hits Guard 1 and returns the sub ID.
	subResp, err := s.EnsureSubscriptionSynced(ctx, EnsureSubscriptionSyncedRequest{
		Subscription:       flexSub,
		PriceIDToProductID: productsResp.PriceIDToPaddleProductID,
	})
	if err != nil {
		return nil, fmt.Errorf("ensuring subscription synced: %w", err)
	}
	if subResp.PaddleSubscriptionID == "" {
		return nil, ierr.NewError("Paddle subscription not yet activated; customer must complete checkout first").
			WithHint("Re-run invoice sync after the customer completes the Paddle checkout flow").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": flexSub.ID,
				"checkout_url":    subResp.CheckoutURL,
			}).
			Mark(ierr.ErrValidation)
	}

	// Step 6: Build charge items — create an ephemeral one-time catalog price per line item,
	// then reference it by ID so Paddle treats this as a catalog-price charge.
	chargeItems := make([]paddlesdk.CreateSubscriptionChargeItems, 0, len(syncable))
	for _, li := range syncable {
		priceID := lo.FromPtr(li.PriceID)
		paddleProductID := productsResp.PriceIDToPaddleProductID[priceID]
		if paddleProductID == "" {
			return nil, ierr.NewError(fmt.Sprintf("no Paddle product ID for FlexPrice price %s", priceID)).
				WithHint("Ensure all invoice line item prices are synced to Paddle").
				Mark(ierr.ErrValidation)
		}
		amountSmallest := types.ToSmallestUnit(li.Amount, li.Currency)
		displayName := lo.FromPtrOr(li.DisplayName, priceID)

		catalogPrice, priceErr := s.client.CreatePrice(ctx, &paddlesdk.CreatePriceRequest{
			ProductID:   paddleProductID,
			Description: displayName,
			Name:        paddlesdk.PtrTo(displayName),
			UnitPrice: paddlesdk.Money{
				Amount:       fmt.Sprintf("%d", amountSmallest),
				CurrencyCode: paddlesdk.CurrencyCode(strings.ToUpper(li.Currency)),
			},
			TaxMode:  paddlesdk.PtrTo(paddlesdk.TaxModeAccountSetting),
			Quantity: &paddlesdk.PriceQuantity{Minimum: 1, Maximum: 100000},
			// No BillingCycle = one-time price.
		})
		if priceErr != nil {
			return nil, fmt.Errorf("creating catalog price for line item %s: %w", priceID, priceErr)
		}

		chargeItems = append(chargeItems, *paddlesdk.NewCreateSubscriptionChargeItemsSubscriptionChargeItemFromCatalog(
			&paddlesdk.SubscriptionChargeItemFromCatalog{
				PriceID:  catalogPrice.ID,
				Quantity: 1,
			},
		))
	}
	if len(chargeItems) == 0 {
		return nil, ierr.NewError("invoice has no syncable line items").Mark(ierr.ErrValidation)
	}

	// Step 7: Create subscription charge.
	_, err = s.client.CreateSubscriptionCharge(ctx, &paddlesdk.CreateSubscriptionChargeRequest{
		SubscriptionID: subResp.PaddleSubscriptionID,
		EffectiveFrom:  paddlesdk.EffectiveFromImmediately,
		Items:          chargeItems,
	})
	if err != nil {
		return nil, fmt.Errorf("creating subscription charge: %w", err)
	}

	// Filter by origin=subscription_charge to skip the $0 bootstrap transaction (origin=api)
	// and always retrieve the actual invoice charge regardless of creation order.
	orderBy := "created_at[DESC]"
	perPage := 1
	originFilter := string(paddlesdk.TransactionOriginSubscriptionCharge)
	txnCollection, err := s.client.ListTransactions(ctx, &paddlesdk.ListTransactionsRequest{
		SubscriptionID: []string{subResp.PaddleSubscriptionID},
		Origin:         []string{originFilter},
		OrderBy:        &orderBy,
		PerPage:        &perPage,
	})
	if err != nil {
		return nil, fmt.Errorf("listing transactions after charge: %w", err)
	}
	var txnID string
	if txnCollection != nil {
		if res := txnCollection.Next(ctx); res != nil && res.Ok() {
			if v := res.Value(); v != nil {
				txnID = v.ID
			}
		}
	}
	if txnID == "" {
		return nil, ierr.NewError("no subscription_charge transaction found after charge").
			WithReportableDetails(map[string]interface{}{"paddle_subscription_id": subResp.PaddleSubscriptionID}).
			Mark(ierr.ErrInternal)
	}

	// Fetch the full transaction — ListTransactions omits Checkout.URL in its payload.
	txn, err := s.client.GetTransaction(ctx, txnID)
	if err != nil {
		return nil, fmt.Errorf("fetching charge transaction: %w", err)
	}

	checkoutURL := ""
	if txn.Checkout != nil {
		checkoutURL = lo.FromPtrOr(txn.Checkout.URL, "")
	}
	s.logger.Debug(ctx, "paddle transaction checkout",
		"transaction_id", txn.ID,
		"checkout_nil", txn.Checkout == nil,
		"checkout_url_from_paddle", checkoutURL,
		"collection_mode", txn.CollectionMode,
		"status", txn.Status,
	)
	checkoutURL = s.appendCheckoutToken(ctx, checkoutURL)

	// Step 9: Persist invoice metadata + mapping.
	if flexInvoice.Metadata == nil {
		flexInvoice.Metadata = make(types.Metadata)
	}
	flexInvoice.Metadata[MetaKeyPaddleTransactionID] = txn.ID
	if checkoutURL != "" {
		flexInvoice.Metadata[MetaKeyPaddleCheckoutURL] = checkoutURL
	}
	if err := s.invoiceRepo.Update(ctx, flexInvoice); err != nil {
		s.logger.Warnw("failed to write Paddle transaction ID to invoice metadata", "error", err, "invoice_id", req.InvoiceID)
	}

	invoiceMeta := map[string]interface{}{
		MetaKeyPaddleTransactionID:  txn.ID,
		MetaKeyPaddleSubscriptionID: subResp.PaddleSubscriptionID,
		MetaKeySyncedAt:             time.Now().UTC().Format(time.RFC3339),
	}
	if checkoutURL != "" {
		invoiceMeta[MetaKeyPaddleCheckoutURL] = checkoutURL
	}
	if txn.InvoiceNumber != nil {
		invoiceMeta[MetaKeyInvoiceNumber] = *txn.InvoiceNumber
	}
	_, err = s.mappingService.CreateEntityIntegrationMapping(ctx, apidto.CreateEntityIntegrationMappingRequest{
		EntityType:       types.IntegrationEntityTypeInvoice,
		EntityID:         req.InvoiceID,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: txn.ID,
		Metadata:         invoiceMeta,
	})
	if err != nil {
		return nil, fmt.Errorf("persisting invoice mapping: %w", err)
	}

	// Step 10: Return.
	return &SyncInvoiceResponse{
		PaddleTransactionID:  txn.ID,
		PaddleSubscriptionID: subResp.PaddleSubscriptionID,
		CheckoutURL:          checkoutURL,
		AlreadySynced:        false,
	}, nil
}

// GetSubscriptionMappingStatus returns true if a Paddle entity_integration_mapping exists for the subscription.
func (s *PaddleSyncService) GetSubscriptionMappingStatus(ctx context.Context, subscriptionID string) (bool, error) {
	resp, err := s.mappingService.GetEntityIntegrationMappings(ctx, &types.EntityIntegrationMappingFilter{
		EntityID:      subscriptionID,
		EntityType:    types.IntegrationEntityTypeSubscription,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	})
	if err != nil {
		return false, fmt.Errorf("checking subscription mapping: %w", err)
	}
	return len(resp.Items) > 0, nil
}

// GetSubscriptionWithLineItems fetches a subscription and its line items.
func (s *PaddleSyncService) GetSubscriptionWithLineItems(ctx context.Context, subscriptionID string) (*subscription.Subscription, []*subscription.SubscriptionLineItem, error) {
	return s.subscriptionRepo.GetWithLineItems(ctx, subscriptionID)
}

// GetInvoiceByID fetches a FlexPrice invoice by ID.
func (s *PaddleSyncService) GetInvoiceByID(ctx context.Context, invoiceID string) (*invoice.Invoice, error) {
	return s.invoiceRepo.Get(ctx, invoiceID)
}

// GetFlexPriceInvoiceIDByTransaction looks up the FlexPrice invoice ID for a Paddle transaction ID.
// Used by the webhook handler to find the invoice associated with a completed payment.
func (s *PaddleSyncService) GetFlexPriceInvoiceIDByTransaction(ctx context.Context, paddleTransactionID string) (string, error) {
	filter := &types.EntityIntegrationMappingFilter{
		ProviderEntityIDs: []string{paddleTransactionID},
		EntityType:        types.IntegrationEntityTypeInvoice,
		ProviderTypes:     []string{string(types.SecretProviderPaddle)},
		QueryFilter:       types.NewDefaultQueryFilter(),
	}
	resp, err := s.mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return "", err
	}
	if len(resp.Items) == 0 {
		return "", ierr.NewError("invoice mapping not found for Paddle transaction").
			WithReportableDetails(map[string]interface{}{"paddle_transaction_id": paddleTransactionID}).
			Mark(ierr.ErrNotFound)
	}
	return resp.Items[0].EntityID, nil
}

// buildCreateAddressRequest builds a Paddle CreateAddressRequest from a FlexPrice customer.
// Caller must ensure AddressCountry is non-empty before calling.
func buildCreateAddressRequest(c *customer.Customer) *paddlesdk.CreateAddressRequest {
	req := &paddlesdk.CreateAddressRequest{
		CountryCode: toCountryCode(c.AddressCountry),
	}
	if c.AddressLine1 != "" {
		req.FirstLine = paddlesdk.PtrTo(c.AddressLine1)
	}
	if c.AddressLine2 != "" {
		req.SecondLine = paddlesdk.PtrTo(c.AddressLine2)
	}
	if c.AddressCity != "" {
		req.City = paddlesdk.PtrTo(c.AddressCity)
	}
	if c.AddressPostalCode != "" {
		req.PostalCode = paddlesdk.PtrTo(c.AddressPostalCode)
	}
	if c.AddressState != "" {
		req.Region = paddlesdk.PtrTo(c.AddressState)
	}
	return req
}

// ProcessCustomerCreatedWebhook creates a FlexPrice customer from Paddle webhook data (customer.created).
func (s *PaddleSyncService) ProcessCustomerCreatedWebhook(ctx context.Context, paddleCustomer *paddlenotification.CustomerNotification, customerService interfaces.CustomerService) error {
	paddleCustomerID := paddleCustomer.ID

	// Idempotency: check if mapping already exists
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderPaddle)},
		ProviderEntityIDs: []string{paddleCustomerID},
		EntityType:        types.IntegrationEntityTypeCustomer,
	}
	resp, err := s.mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		s.logger.Error(ctx, "failed to check Paddle customer mapping",
			"error", err,
			MetaKeyPaddleCustomerID, paddleCustomerID)
		return err
	}
	if len(resp.Items) > 0 {
		return nil
	}

	// Deduplication by email: if customer exists by email, create mapping and skip creation
	if paddleCustomer.Email != "" {
		emailFilter := &types.CustomerFilter{
			Email:       paddleCustomer.Email,
			QueryFilter: types.NewDefaultQueryFilter(),
		}
		existingCustomers, err := customerService.GetCustomers(ctx, emailFilter)
		if err == nil && existingCustomers != nil && len(existingCustomers.Items) > 0 {
			existingCustomer := existingCustomers.Items[0]

			_, err = s.mappingService.CreateEntityIntegrationMapping(ctx, apidto.CreateEntityIntegrationMappingRequest{
				EntityID:         existingCustomer.ID,
				EntityType:       types.IntegrationEntityTypeCustomer,
				ProviderType:     string(types.SecretProviderPaddle),
				ProviderEntityID: paddleCustomerID,
				Metadata: map[string]interface{}{
					MetaKeyCreatedVia:          CreatedViaProviderToFlexprice,
					MetaKeyPaddleCustomerEmail: paddleCustomer.Email,
					MetaKeySyncedAt:            time.Now().UTC().Format(time.RFC3339),
				},
			})
			if err != nil {
				s.logger.Warnw("failed to create mapping for existing customer",
					"error", err,
					"customer_id", existingCustomer.ID,
					MetaKeyPaddleCustomerID, paddleCustomerID)
			}
			return nil
		}
	}

	// Create new customer
	name := paddleCustomerID
	if paddleCustomer.Name != nil && *paddleCustomer.Name != "" {
		name = *paddleCustomer.Name
	} else if paddleCustomer.Email != "" {
		name = paddleCustomer.Email
	}

	createReq := apidto.CreateCustomerRequest{
		ExternalID:             paddleCustomerID,
		Name:                   name,
		Email:                  paddleCustomer.Email,
		SkipOnboardingWorkflow: true,
		Metadata: map[string]string{
			MetaKeyPaddleCustomerID: paddleCustomerID,
		},
	}

	customerResp, err := customerService.CreateCustomer(ctx, createReq)
	if err != nil {
		return err
	}

	// Create entity mapping
	_, err = s.mappingService.CreateEntityIntegrationMapping(ctx, apidto.CreateEntityIntegrationMappingRequest{
		EntityID:         customerResp.ID,
		EntityType:       types.IntegrationEntityTypeCustomer,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: paddleCustomerID,
		Metadata: map[string]interface{}{
			MetaKeyCreatedVia:          CreatedViaProviderToFlexprice,
			MetaKeyPaddleCustomerEmail: paddleCustomer.Email,
			MetaKeySyncedAt:            time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		s.logger.Warnw("failed to create mapping for new customer",
			"error", err,
			"customer_id", customerResp.ID,
			MetaKeyPaddleCustomerID, paddleCustomerID)
	}

	return nil
}

// ProcessAddressCreatedWebhook updates a FlexPrice customer's address from a Paddle address.created webhook.
func (s *PaddleSyncService) ProcessAddressCreatedWebhook(
	ctx context.Context,
	paddleCustomerID string,
	addr *paddlenotification.AddressNotification,
	customerService interfaces.CustomerService,
) error {
	// Look up FlexPrice customer by Paddle customer ID.
	filter := &types.EntityIntegrationMappingFilter{
		ProviderTypes:     []string{string(types.SecretProviderPaddle)},
		ProviderEntityIDs: []string{paddleCustomerID},
		EntityType:        types.IntegrationEntityTypeCustomer,
		QueryFilter:       types.NewDefaultQueryFilter(),
	}
	resp, err := s.mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return err
	}
	if len(resp.Items) == 0 {
		// No FlexPrice customer mapped — skip silently.
		return nil
	}
	flexCustomerID := resp.Items[0].EntityID
	mappingID := resp.Items[0].ID

	// Update FlexPrice customer address fields.
	updateReq := mapToUpdateCustomerAddressRequest(addr)
	_, err = customerService.UpdateCustomer(ctx, flexCustomerID, updateReq)
	if err != nil {
		return err
	}

	// Update mapping metadata with the new Paddle address ID.
	_, err = s.mappingService.UpdateEntityIntegrationMapping(ctx, mappingID, apidto.UpdateEntityIntegrationMappingRequest{
		Metadata: map[string]interface{}{MetaKeyPaddleAddressID: addr.ID},
	})
	if err != nil {
		s.logger.Warnw("failed to update customer mapping with paddle_address_id",
			"error", err, "flexprice_customer_id", flexCustomerID, "paddle_address_id", addr.ID)
	}
	return nil
}

// ProcessTransactionCompletedWebhook processes a transaction.completed Paddle webhook event.
// It finds the FlexPrice invoice via entity_integration_mapping and delegates payment processing.
func (s *PaddleSyncService) ProcessTransactionCompletedWebhook(
	ctx context.Context,
	txnID string,
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
) error {
	flexpriceInvoiceID, err := s.GetFlexPriceInvoiceIDByTransaction(ctx, txnID)
	if err != nil {
		if ierr.IsNotFound(err) {
			// No mapping — this transaction may not be one we created, skip.
			s.logger.Warnw("no FlexPrice invoice found for Paddle transaction, skipping",
				"paddle_transaction_id", txnID)
		}
		return err
	}
	txnCollection, err := s.client.ListTransactions(ctx, &paddlesdk.ListTransactionsRequest{
		ID: []string{txnID},
	})
	if err != nil {
		return fmt.Errorf("listing Paddle transaction %s: %w", txnID, err)
	}

	var txn *paddlesdk.Transaction
	if txnCollection != nil {
		if res := txnCollection.Next(ctx); res != nil && res.Ok() {
			txn = res.Value()
		}
	}
	if txn == nil {
		return ierr.NewError("Paddle transaction not found").
			WithReportableDetails(map[string]interface{}{"paddle_transaction_id": txnID}).
			Mark(ierr.ErrNotFound)
	}

	if txn.Status != paddlesdk.TransactionStatusCompleted {
		s.logger.Debug(ctx, "Paddle transaction not yet completed",
			"paddle_transaction_id", txnID,
			"status", txn.Status)
		return nil
	}

	// Process the payment (idempotent — checks if payment already exists).
	paymentSvc := NewPaymentService(s.logger)
	return paymentSvc.ProcessExternalPaddleTransaction(ctx, txn, flexpriceInvoiceID, paymentService, invoiceService)
}

// extractFlexSubIDFromCustomData reads flexprice_subscription_id from a Paddle custom_data map.
// Checks both snake_case ("flexprice_subscription_id") and camelCase ("flexpriceSubscriptionId")
// because Paddle may preserve or convert key casing depending on the API version / context.
func extractFlexSubIDFromCustomData(customData map[string]any) string {
	if id, ok := customData["flexprice_subscription_id"].(string); ok && id != "" {
		return id
	}
	if id, ok := customData["flexpriceSubscriptionId"].(string); ok && id != "" {
		return id
	}
	return ""
}

// ProcessSubscriptionActivatedWebhook handles the Paddle subscription.activated event.
// It creates the entity_integration_mapping with the real Paddle subscription ID and
// transitions the FlexPrice subscription from incomplete to active (or trialing).
func (s *PaddleSyncService) ProcessSubscriptionActivatedWebhook(
	ctx context.Context,
	data *paddlenotification.SubscriptionNotification,
	subscriptionService interfaces.SubscriptionService,
) error {
	paddleSubID := data.ID

	flexSubID := extractFlexSubIDFromCustomData(data.CustomData)
	if flexSubID == "" {
		s.logger.Warnw("subscription.activated: no flexprice_subscription_id in custom_data — skipping",
			"paddle_sub_id", paddleSubID)
		return nil
	}

	// Idempotent: create mapping only if one does not already exist.
	activated, err := s.GetSubscriptionMappingStatus(ctx, flexSubID)
	if err != nil {
		return fmt.Errorf("checking existing subscription mapping: %w", err)
	}
	if !activated {
		_, err = s.mappingService.CreateEntityIntegrationMapping(ctx, apidto.CreateEntityIntegrationMappingRequest{
			EntityID:         flexSubID,
			EntityType:       types.IntegrationEntityTypeSubscription,
			ProviderType:     string(types.SecretProviderPaddle),
			ProviderEntityID: paddleSubID,
			Metadata: map[string]interface{}{
				MetaKeyPaddleSubscriptionID: paddleSubID,
				MetaKeySyncedAt:             time.Now().UTC().Format(time.RFC3339),
			},
		})
		if err != nil {
			return fmt.Errorf("creating subscription mapping: %w", err)
		}
	}

	// Persist paddle_subscription_id in subscription metadata.
	sub, err := s.subscriptionRepo.Get(ctx, flexSubID)
	if err != nil {
		return fmt.Errorf("fetching subscription: %w", err)
	}
	if sub.Metadata == nil {
		sub.Metadata = make(types.Metadata)
	}
	sub.Metadata[MetaKeyPaddleSubscriptionID] = paddleSubID
	if err := s.subscriptionRepo.Update(ctx, sub); err != nil {
		s.logger.Warnw("failed to update sub metadata with paddle_subscription_id",
			"sub_id", flexSubID, "error", err)
	}

	// Activate the FlexPrice subscription.
	switch sub.SubscriptionStatus {
	//case types.SubscriptionStatusIncomplete:
	//	if sub.TrialEnd != nil && sub.TrialEnd.After(time.Now()) {
	//		sub.SubscriptionStatus = types.SubscriptionStatusTrialing
	//		if err := s.subscriptionRepo.Update(ctx, sub); err != nil {
	//			return fmt.Errorf("setting subscription to trialing: %w", err)
	//		}
	//		s.logger.Infow("subscription.activated: set incomplete→trialing",
	//			"sub_id", flexSubID, "paddle_sub_id", paddleSubID)
	//	} else {
	//		if subscriptionService == nil {
	//			return ierr.NewError("subscriptionService is required to activate subscription").Mark(ierr.ErrInternal)
	//		}
	//		if err := subscriptionService.ActivateIncompleteSubscription(ctx, flexSubID); err != nil {
	//			return fmt.Errorf("activating incomplete subscription: %w", err)
	//		}
	//		s.logger.Infow("subscription.activated: set incomplete→active",
	//			"sub_id", flexSubID, "paddle_sub_id", paddleSubID)
	//	}
	case types.SubscriptionStatusDraft:
		startDate := time.Now().UTC()
		if data.StartedAt != nil {
			if parsed, parseErr := time.Parse(time.RFC3339, *data.StartedAt); parseErr == nil {
				startDate = parsed
			}
		}
		if _, err := subscriptionService.ActivateDraftSubscription(ctx, flexSubID, apidto.ActivateDraftSubscriptionRequest{
			StartDate: &startDate,
		}); err != nil {
			return fmt.Errorf("activating draft subscription: %w", err)
		}
		s.logger.Info(ctx, "subscription.activated",
			"sub_id", flexSubID, "paddle_sub_id", paddleSubID)
	default:
		s.logger.Info(ctx, "subscription not in draft state — no-op",
			"sub_id", flexSubID, "status", sub.SubscriptionStatus, "paddle_sub_id", paddleSubID)
	}

	s.resyncPendingInvoicesForSubscription(ctx, flexSubID)
	return nil
}

// resyncPendingInvoicesForSubscription re-triggers the PaddleInvoiceSyncWorkflow for every
// finalized+pending invoice that belongs to the given subscription.
//
// Called from ProcessSubscriptionActivatedWebhook after the subscription mapping is confirmed.
// Invoices created during subscription setup failed their original sync because the mapping
// didn't exist yet; now that it does, we schedule a fresh workflow run for each one.
//
// Each workflow is delayed 30 seconds so Paddle has time to fully settle the subscription
// before the charge is attempted. The workflow's own retry policy handles transient failures.
// If TemporalService is nil (e.g. in unit-test environments), the method returns silently.
func (s *PaddleSyncService) resyncPendingInvoicesForSubscription(ctx context.Context, subscriptionID string) {
	if s.temporalSvc == nil {
		s.logger.Error(ctx, "temporal service is nil, skipping resync pending invoices for subscription",
			"subscription_id", subscriptionID)
		return
	}

	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = subscriptionID
	filter.InvoiceStatus = []types.InvoiceStatus{
		types.InvoiceStatusFinalized,
	}
	filter.PaymentStatus = []types.PaymentStatus{
		types.PaymentStatusPending,
	}
	filter.SkipLineItems = true

	invoices, err := s.invoiceRepo.List(ctx, filter)
	if err != nil {
		s.logger.Warnw("failed to list invoices for post-activation resync",
			"subscription_id", subscriptionID, "error", err)
		return
	}

	if len(invoices) == 0 {
		s.logger.Info(ctx, "no pending invoices to resync after subscription.activated",
			"subscription_id", subscriptionID)
		return
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	s.logger.Info(ctx, "triggering post-activation invoice resync workflows",
		"subscription_id", subscriptionID, "count", len(invoices))

	triggered := 0
	for _, inv := range invoices {
		input := temporalmodels.PaddleInvoiceSyncWorkflowInput{
			InvoiceID:      inv.ID,
			CustomerID:     inv.CustomerID,
			SubscriptionID: subscriptionID,
			TenantID:       tenantID,
			EnvironmentID:  environmentID,
		}
		_, wErr := s.temporalSvc.ExecuteWorkflow(ctx, types.TemporalPaddleInvoiceSyncWorkflow, input)
		if wErr != nil {
			s.logger.Error(ctx, "failed to trigger invoice resync workflow after subscription.activated",
				"subscription_id", subscriptionID, "invoice_id", inv.ID, "error", wErr)
			continue
		}
		triggered++
	}

	s.logger.Info(ctx, "completed post-activation invoice resync workflow dispatch",
		"subscription_id", subscriptionID,
		"attempted", len(invoices),
		"triggered", triggered,
		"failed", len(invoices)-triggered)
}

func syncableInvoiceLineItems(items []*invoice.InvoiceLineItem) []*invoice.InvoiceLineItem {
	out := make([]*invoice.InvoiceLineItem, 0, len(items))
	for _, li := range items {
		if li != nil && lo.FromPtr(li.PriceID) != "" {
			out = append(out, li)
		}
	}
	return out
}

// mapToUpdateCustomerAddressRequest maps Paddle AddressNotification to Flexprice UpdateCustomerRequest.
// Flexprice has no separate Address entity; address is embedded on Customer.
func mapToUpdateCustomerAddressRequest(addr *paddlenotification.AddressNotification) apidto.UpdateCustomerRequest {
	req := apidto.UpdateCustomerRequest{}
	if addr.FirstLine != nil && *addr.FirstLine != "" {
		req.AddressLine1 = addr.FirstLine
	}
	if addr.SecondLine != nil && *addr.SecondLine != "" {
		req.AddressLine2 = addr.SecondLine
	}
	if addr.City != nil && *addr.City != "" {
		req.AddressCity = addr.City
	}
	if addr.Region != nil && *addr.Region != "" {
		req.AddressState = addr.Region
	}
	if addr.PostalCode != nil && *addr.PostalCode != "" {
		req.AddressPostalCode = addr.PostalCode
	}
	if addr.CountryCode != "" {
		req.AddressCountry = lo.ToPtr(strings.ToUpper(string(addr.CountryCode)))
	}
	return req
}

// PullAndUpdateInvoice polls Paddle for the payment status of an already-synced invoice.
// If the invoice mapping exists and the invoice is finalized+paid, it returns early.
// If the invoice is unpaid, it fetches the Paddle transaction by ID and, if the
// transaction is completed, processes the payment (same flow as handleTransactionCompleted).
func (s *PaddleSyncService) PullAndUpdateInvoice(ctx context.Context, invoiceID string) error {
	flexInvoice, err := s.invoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return fmt.Errorf("fetching invoice: %w", err)
	}

	existingMapping, err := s.getExistingInvoiceMapping(ctx, invoiceID)
	if err != nil {
		return fmt.Errorf("checking invoice mapping: %w", err)
	}
	if existingMapping == nil {
		return ierr.NewError("no Paddle mapping found for invoice").
			WithReportableDetails(map[string]interface{}{"invoice_id": invoiceID}).
			Mark(ierr.ErrNotFound)
	}

	if flexInvoice.InvoiceStatus == types.InvoiceStatusFinalized &&
		flexInvoice.PaymentStatus == types.PaymentStatusSucceeded {
		s.logger.Debug(ctx, "invoice already finalized and paid, skipping reconciliation",
			"invoice_id", invoiceID)
		return nil
	}

	paddleTransactionID := existingMapping.ProviderEntityID
	return s.ProcessTransactionCompletedWebhook(ctx, paddleTransactionID, s.paymentService, s.invoiceService)
}
