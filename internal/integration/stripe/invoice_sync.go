package stripe

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v82"
)

// InvoiceSyncService handles synchronization of FlexPrice invoices with Stripe
type InvoiceSyncService struct {
	client                       *Client
	customerSvc                  *CustomerService
	invoiceRepo                  invoice.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewInvoiceSyncService creates a new Stripe invoice sync service
func NewInvoiceSyncService(
	client *Client,
	customerSvc *CustomerService,
	invoiceRepo invoice.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) *InvoiceSyncService {
	return &InvoiceSyncService{
		client:                       client,
		customerSvc:                  customerSvc,
		invoiceRepo:                  invoiceRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
	}
}

// SyncInvoiceToStripe syncs a FlexPrice invoice to Stripe following the complete flow
func (s *InvoiceSyncService) SyncInvoiceToStripe(ctx context.Context, req StripeInvoiceSyncRequest, customerService interfaces.CustomerService) (*StripeInvoiceSyncResponse, error) {
	s.logger.Infow("starting Stripe invoice sync",
		"invoice_id", req.InvoiceID,
		"collection_method", req.CollectionMethod)

	// Step 1: Check if Stripe connection exists
	if !s.client.HasStripeConnection(ctx) {
		return nil, ierr.NewError("Stripe connection not available").
			WithHint("Stripe integration must be configured for invoice sync").
			Mark(ierr.ErrNotFound)
	}

	// Step 2: Get FlexPrice invoice
	flexInvoice, err := s.invoiceRepo.Get(ctx, req.InvoiceID)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to get FlexPrice invoice").Mark(ierr.ErrDatabase)
	}

	// Step 3: Check if invoice is already synced
	existingMapping, err := s.getExistingStripeMapping(ctx, req.InvoiceID)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}

	var stripeInvoiceID string
	if existingMapping != nil {
		stripeInvoiceID = existingMapping.ProviderEntityID
		s.logger.Infow("invoice already synced to Stripe",
			"invoice_id", req.InvoiceID,
			"stripe_invoice_id", stripeInvoiceID)
	} else {
		// Step 4: Create draft invoice in Stripe
		collectionMethod := types.CollectionMethod(req.CollectionMethod)
		stripeInvoiceID, err = s.createDraftInvoiceInStripe(ctx, flexInvoice, collectionMethod, customerService)
		if err != nil {
			return nil, err
		}

		// Step 5: Create entity integration mapping
		if err := s.createInvoiceMapping(ctx, req.InvoiceID, stripeInvoiceID); err != nil {
			s.logger.Errorw("failed to create invoice mapping", "error", err)
			// Continue with sync even if mapping fails
		}
	}

	// Step 6: Sync line items to Stripe
	if err := s.syncLineItemsToStripe(ctx, flexInvoice, stripeInvoiceID, customerService); err != nil {
		return nil, err
	}

	// Step 7: Finalize invoice in Stripe
	collectionMethod := types.CollectionMethod(req.CollectionMethod)
	finalizedInvoice, err := s.finalizeStripeInvoice(ctx, stripeInvoiceID, collectionMethod)
	if err != nil {
		return nil, err
	}

	// Step 8: Update FlexPrice invoice with Stripe data
	if err := s.updateFlexPriceInvoiceFromStripe(ctx, flexInvoice, finalizedInvoice); err != nil {
		s.logger.Errorw("failed to update FlexPrice invoice from Stripe", "error", err)
		// Don't fail the entire sync for this
	}

	response := &StripeInvoiceSyncResponse{
		InvoiceID:       req.InvoiceID,
		StripeInvoiceID: finalizedInvoice.ID,
		Status:          string(finalizedInvoice.Status),
		Amount:          decimal.NewFromInt(finalizedInvoice.Total).Div(decimal.NewFromInt(100)),
		Currency:        string(finalizedInvoice.Currency),
		InvoiceURL:      finalizedInvoice.HostedInvoiceURL,
		PaymentURL:      finalizedInvoice.HostedInvoiceURL, // Same as invoice URL for Stripe
		CreatedAt:       time.Unix(finalizedInvoice.Created, 0),
		UpdatedAt:       time.Now(),
	}

	s.logger.Infow("Stripe invoice sync completed successfully",
		"invoice_id", req.InvoiceID,
		"stripe_invoice_id", finalizedInvoice.ID,
		"status", finalizedInvoice.Status)

	return response, nil
}

// createDraftInvoiceInStripe creates a draft invoice in Stripe
func (s *InvoiceSyncService) createDraftInvoiceInStripe(ctx context.Context, flexInvoice *invoice.Invoice, collectionMethod types.CollectionMethod, customerService interfaces.CustomerService) (string, error) {
	// Get Stripe client
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return "", err
	}

	// Get customer's Stripe ID
	stripeCustomerID, err := s.getStripeCustomerID(ctx, flexInvoice.CustomerID, customerService)
	if err != nil {
		return "", err
	}

	// Prepare invoice metadata
	metadata := map[string]string{
		"flexprice_invoice_id":     flexInvoice.ID,
		"flexprice_customer_id":    flexInvoice.CustomerID,
		"flexprice_invoice_number": lo.FromPtr(flexInvoice.InvoiceNumber),
		"sync_source":              "flexprice",
	}

	if flexInvoice.SubscriptionID != nil {
		metadata["flexprice_subscription_id"] = *flexInvoice.SubscriptionID
	}

	// Create invoice parameters
	params := &stripe.InvoiceCreateParams{
		Customer:    stripe.String(stripeCustomerID),
		Currency:    stripe.String(strings.ToLower(flexInvoice.Currency)),
		AutoAdvance: stripe.Bool(true),
		Description: stripe.String(flexInvoice.Description),
		Metadata:    metadata,
	}

	// Set collection method
	switch collectionMethod {
	case types.CollectionMethodChargeAutomatically:
		params.CollectionMethod = stripe.String(string(stripe.InvoiceCollectionMethodChargeAutomatically))
	case types.CollectionMethodSendInvoice:
		params.CollectionMethod = stripe.String(string(stripe.InvoiceCollectionMethodSendInvoice))
	default:
		params.CollectionMethod = stripe.String(string(stripe.InvoiceCollectionMethodChargeAutomatically))
	}

	// Set due date only for send_invoice collection method
	if flexInvoice.DueDate != nil && collectionMethod == types.CollectionMethodSendInvoice {
		params.DueDate = stripe.Int64(flexInvoice.DueDate.Unix())
	}

	// Create the invoice
	stripeInvoice, err := stripeClient.V1Invoices.Create(ctx, params)
	if err != nil {
		s.logger.Errorw("failed to create draft invoice in Stripe",
			"error", err,
			"invoice_id", flexInvoice.ID)
		return "", ierr.NewError("failed to create Stripe invoice").
			WithHint("Unable to create draft invoice in Stripe").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": flexInvoice.ID,
				"error":      err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	s.logger.Infow("created draft invoice in Stripe",
		"invoice_id", flexInvoice.ID,
		"stripe_invoice_id", stripeInvoice.ID)

	return stripeInvoice.ID, nil
}

// syncLineItemsToStripe adds all line items to the Stripe invoice
func (s *InvoiceSyncService) syncLineItemsToStripe(ctx context.Context, flexInvoice *invoice.Invoice, stripeInvoiceID string, customerService interfaces.CustomerService) error {
	if len(flexInvoice.LineItems) == 0 {
		s.logger.Infow("no line items to sync", "invoice_id", flexInvoice.ID)
		return nil
	}

	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return err
	}

	s.logger.Infow("syncing line items to Stripe",
		"invoice_id", flexInvoice.ID,
		"stripe_invoice_id", stripeInvoiceID,
		"line_items_count", len(flexInvoice.LineItems))

	// Add each line item to Stripe invoice
	for _, lineItem := range flexInvoice.LineItems {
		if err := s.addLineItemToStripeInvoice(ctx, stripeClient, stripeInvoiceID, lineItem, flexInvoice, customerService); err != nil {
			return err
		}
	}

	s.logger.Infow("successfully synced all line items to Stripe",
		"invoice_id", flexInvoice.ID,
		"stripe_invoice_id", stripeInvoiceID)

	return nil
}

// addLineItemToStripeInvoice adds a single line item to Stripe invoice
func (s *InvoiceSyncService) addLineItemToStripeInvoice(ctx context.Context, stripeClient *stripe.Client, stripeInvoiceID string, lineItem *invoice.InvoiceLineItem, flexInvoice *invoice.Invoice, customerService interfaces.CustomerService) error {
	// Convert amount to cents (Stripe uses cents)
	amountCents := lineItem.Amount.Mul(decimal.NewFromInt(100)).IntPart()

	// Get customer ID from the invoice
	customerID, err := s.getStripeCustomerID(ctx, flexInvoice.CustomerID, customerService)
	if err != nil {
		s.logger.Errorw("failed to get Stripe customer ID",
			"error", err,
			"customer_id", flexInvoice.CustomerID,
			"line_item_id", lineItem.ID)
		return ierr.NewError("failed to get Stripe customer ID").
			WithHint("Unable to find Stripe customer mapping").
			WithReportableDetails(map[string]interface{}{
				"customer_id":  flexInvoice.CustomerID,
				"line_item_id": lineItem.ID,
				"error":        err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	params := &stripe.InvoiceItemCreateParams{
		Customer:    stripe.String(customerID),
		Invoice:     stripe.String(stripeInvoiceID),
		Currency:    stripe.String(strings.ToLower(lineItem.Currency)),
		Description: stripe.String(lo.FromPtr(lineItem.DisplayName)),
		Metadata: map[string]string{
			"flexprice_line_item_id": lineItem.ID,
			"flexprice_price_id":     lo.FromPtr(lineItem.PriceID),
			"sync_source":            "flexprice",
		},
	}

	// Stripe only allows either Amount OR Quantity, not both
	// For now, we'll always use Amount since it's simpler and works for all cases
	params.Amount = stripe.Int64(amountCents)

	invoiceItem, err := stripeClient.V1InvoiceItems.Create(ctx, params)
	if err != nil {
		s.logger.Errorw("failed to add line item to Stripe invoice",
			"error", err,
			"line_item_id", lineItem.ID,
			"stripe_invoice_id", stripeInvoiceID)
		return ierr.NewError("failed to add line item to Stripe").
			WithHint("Unable to add line item to Stripe invoice").
			WithReportableDetails(map[string]interface{}{
				"line_item_id":      lineItem.ID,
				"stripe_invoice_id": stripeInvoiceID,
				"error":             err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	s.logger.Debugw("added line item to Stripe invoice",
		"line_item_id", lineItem.ID,
		"stripe_invoice_id", stripeInvoiceID,
		"stripe_item_id", invoiceItem.ID)

	return nil
}

// finalizeStripeInvoice finalizes the invoice in Stripe
func (s *InvoiceSyncService) finalizeStripeInvoice(ctx context.Context, stripeInvoiceID string, collectionMethod types.CollectionMethod) (*stripe.Invoice, error) {
	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	s.logger.Infow("finalizing Stripe invoice",
		"stripe_invoice_id", stripeInvoiceID,
		"collection_method", collectionMethod)

	// Finalize the invoice
	params := &stripe.InvoiceFinalizeInvoiceParams{
		AutoAdvance: stripe.Bool(true), // Let Stripe handle payment intent creation and sending
	}

	finalizedInvoice, err := stripeClient.V1Invoices.FinalizeInvoice(ctx, stripeInvoiceID, params)
	if err != nil {
		s.logger.Errorw("failed to finalize Stripe invoice",
			"error", err,
			"stripe_invoice_id", stripeInvoiceID)
		return nil, ierr.NewError("failed to finalize Stripe invoice").
			WithHint("Unable to finalize invoice in Stripe").
			WithReportableDetails(map[string]interface{}{
				"stripe_invoice_id": stripeInvoiceID,
				"error":             err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	s.logger.Infow("successfully finalized Stripe invoice",
		"stripe_invoice_id", stripeInvoiceID,
		"status", finalizedInvoice.Status,
		"total", finalizedInvoice.Total)

	if collectionMethod == types.CollectionMethodSendInvoice {
		s.logger.Infow("sending invoice to customer via Stripe",
			"stripe_invoice_id", stripeInvoiceID,
			"collection_method", collectionMethod)

		_, err = stripeClient.V1Invoices.SendInvoice(ctx, stripeInvoiceID, &stripe.InvoiceSendInvoiceParams{})
		if err != nil {
			s.logger.Errorw("failed to send Stripe invoice",
				"error", err,
				"stripe_invoice_id", stripeInvoiceID)
			// Don't fail the entire sync if sending fails, just log the error
		} else {
			s.logger.Infow("successfully sent Stripe invoice to customer",
				"stripe_invoice_id", stripeInvoiceID)
		}
	}

	return finalizedInvoice, nil
}

// SyncPaymentToStripe syncs a FlexPrice payment to Stripe as an external payment
func (s *InvoiceSyncService) SyncPaymentToStripe(ctx context.Context, invoiceID string, paymentAmount decimal.Decimal, paymentSource string, metadata map[string]string) error {
	// Get Stripe invoice ID from mapping
	mapping, err := s.getExistingStripeMapping(ctx, invoiceID)
	if err != nil {
		return ierr.WithError(err).WithHint("Invoice not synced to Stripe").Mark(ierr.ErrNotFound)
	}

	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return err
	}

	stripeInvoiceID := mapping.ProviderEntityID
	amountCents := paymentAmount.Mul(decimal.NewFromInt(100)).IntPart()

	s.logger.Infow("syncing external payment to Stripe",
		"invoice_id", invoiceID,
		"stripe_invoice_id", stripeInvoiceID,
		"amount", paymentAmount,
		"source", paymentSource)

	// Get the invoice to check current status
	stripeInvoice, err := stripeClient.V1Invoices.Retrieve(ctx, stripeInvoiceID, nil)
	if err != nil {
		return ierr.NewError("failed to retrieve Stripe invoice").
			WithHint("Unable to get invoice from Stripe").
			Mark(ierr.ErrSystem)
	}

	// Only proceed if invoice is finalized and not fully paid
	if stripeInvoice.Status != stripe.InvoiceStatusOpen {
		s.logger.Infow("Stripe invoice not in open status, skipping payment sync",
			"stripe_invoice_id", stripeInvoiceID,
			"status", stripeInvoice.Status)
		return nil
	}

	// For external payments (like FlexPrice credits), mark as paid out of band
	// This indicates the payment was processed outside of Stripe

	// Create simple params without metadata to avoid Stripe SDK conflicts
	payParams := &stripe.InvoicePayParams{
		PaidOutOfBand: stripe.Bool(true),
	}

	s.logger.Infow("marking external payment as paid out of band in Stripe",
		"stripe_invoice_id", stripeInvoiceID,
		"amount_cents", amountCents,
		"payment_source", paymentSource)

	updatedInvoice, err := stripeClient.V1Invoices.Pay(ctx, stripeInvoiceID, payParams)
	if err != nil {
		s.logger.Errorw("failed to mark payment as paid out of band",
			"error", err,
			"stripe_invoice_id", stripeInvoiceID,
			"amount_cents", amountCents)
		return ierr.NewError("failed to mark payment as paid out of band").
			WithHint("Unable to record external payment in Stripe").
			WithReportableDetails(map[string]interface{}{
				"stripe_invoice_id": stripeInvoiceID,
				"payment_amount":    paymentAmount.String(),
				"error":             err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	// Update Stripe invoice metadata to track FlexPrice credit payments
	err = s.updateStripeInvoiceMetadata(ctx, stripeClient, stripeInvoiceID, paymentAmount, paymentSource, metadata)
	if err != nil {
		s.logger.Errorw("failed to update Stripe invoice metadata",
			"error", err,
			"stripe_invoice_id", stripeInvoiceID,
			"amount", paymentAmount)
		// Don't fail the whole operation, just log the error
	}

	s.logger.Infow("successfully synced payment to Stripe",
		"invoice_id", invoiceID,
		"stripe_invoice_id", stripeInvoiceID,
		"amount", paymentAmount,
		"new_status", updatedInvoice.Status)

	return nil
}

// getStripeCustomerID gets the Stripe customer ID for a FlexPrice customer
func (s *InvoiceSyncService) getStripeCustomerID(ctx context.Context, customerID string, customerService interfaces.CustomerService) (string, error) {
	customerResp, err := customerService.GetCustomer(ctx, customerID)
	if err != nil {
		return "", err
	}

	stripeCustomerID, exists := customerResp.Customer.Metadata["stripe_customer_id"]
	if !exists || stripeCustomerID == "" {
		return "", ierr.NewError("customer not synced to Stripe").
			WithHint("Customer must be synced to Stripe before invoice sync").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrNotFound)
	}

	return stripeCustomerID, nil
}

// GetExistingStripeMapping gets existing Stripe mapping for an invoice
func (s *InvoiceSyncService) GetExistingStripeMapping(ctx context.Context, invoiceID string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	return s.getExistingStripeMapping(ctx, invoiceID)
}

// GetStripeInvoiceID gets the Stripe invoice ID for a FlexPrice invoice
func (s *InvoiceSyncService) GetStripeInvoiceID(ctx context.Context, flexpriceInvoiceID string) (string, error) {
	mapping, err := s.getExistingStripeMapping(ctx, flexpriceInvoiceID)
	if err != nil {
		s.logger.Debugw("no Stripe invoice mapping found",
			"invoice_id", flexpriceInvoiceID)
		return "", ierr.WithError(err).Mark(ierr.ErrNotFound)
	}

	stripeInvoiceID := mapping.ProviderEntityID
	s.logger.Debugw("found Stripe invoice mapping",
		"invoice_id", flexpriceInvoiceID,
		"stripe_invoice_id", stripeInvoiceID)

	return stripeInvoiceID, nil
}

// GetFlexPriceInvoiceID gets the FlexPrice invoice ID from a Stripe invoice ID
func (s *InvoiceSyncService) GetFlexPriceInvoiceID(ctx context.Context, stripeInvoiceID string) (string, error) {
	if s.entityIntegrationMappingRepo == nil {
		return "", ierr.NewError("entity integration mapping repository not available").Mark(ierr.ErrNotFound)
	}

	filter := &types.EntityIntegrationMappingFilter{
		ProviderEntityIDs: []string{stripeInvoiceID},
		EntityType:        types.IntegrationEntityTypeInvoice,
		ProviderTypes:     []string{"stripe"},
		QueryFilter:       types.NewDefaultQueryFilter(),
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		s.logger.Debugw("failed to query entity integration mapping",
			"error", err,
			"stripe_invoice_id", stripeInvoiceID)
		return "", ierr.WithError(err).Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		s.logger.Debugw("no FlexPrice invoice mapping found",
			"stripe_invoice_id", stripeInvoiceID)
		return "", ierr.NewError("flexprice invoice mapping not found").Mark(ierr.ErrNotFound)
	}

	flexpriceInvoiceID := mappings[0].EntityID
	s.logger.Debugw("found FlexPrice invoice mapping",
		"stripe_invoice_id", stripeInvoiceID,
		"flexprice_invoice_id", flexpriceInvoiceID)

	return flexpriceInvoiceID, nil
}

// getExistingStripeMapping gets existing Stripe mapping for an invoice
func (s *InvoiceSyncService) getExistingStripeMapping(ctx context.Context, invoiceID string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	if s.entityIntegrationMappingRepo == nil {
		return nil, ierr.NewError("entity integration mapping repository not available").Mark(ierr.ErrNotFound)
	}

	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      invoiceID,
		EntityType:    types.IntegrationEntityTypeInvoice,
		ProviderTypes: []string{"stripe"},
		QueryFilter:   types.NewDefaultQueryFilter(),
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(mappings) == 0 {
		return nil, ierr.NewError("mapping not found").Mark(ierr.ErrNotFound)
	}

	return mappings[0], nil
}

// createInvoiceMapping creates a new entity integration mapping for the invoice
func (s *InvoiceSyncService) createInvoiceMapping(ctx context.Context, invoiceID, stripeInvoiceID string) error {
	if s.entityIntegrationMappingRepo == nil {
		s.logger.Warnw("entity integration mapping repository not available, skipping mapping creation")
		return nil
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         invoiceID,
		EntityType:       types.IntegrationEntityTypeInvoice,
		ProviderType:     "stripe",
		ProviderEntityID: stripeInvoiceID,
		Metadata: map[string]interface{}{
			"sync_timestamp": time.Now().Unix(),
			"sync_source":    "flexprice",
		},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	return s.entityIntegrationMappingRepo.Create(ctx, mapping)
}

// updateFlexPriceInvoiceFromStripe updates FlexPrice invoice with data from Stripe
func (s *InvoiceSyncService) updateFlexPriceInvoiceFromStripe(ctx context.Context, flexInvoice *invoice.Invoice, stripeInvoice *stripe.Invoice) error {
	// Update invoice with Stripe data if needed
	updated := false

	// Update hosted invoice URL
	if stripeInvoice.HostedInvoiceURL != "" {
		if flexInvoice.Metadata == nil {
			flexInvoice.Metadata = make(types.Metadata)
		}
		flexInvoice.Metadata["stripe_hosted_invoice_url"] = stripeInvoice.HostedInvoiceURL
		updated = true
	}

	// Update PDF URL
	if stripeInvoice.InvoicePDF != "" {
		flexInvoice.InvoicePDFURL = &stripeInvoice.InvoicePDF
		updated = true
	}

	if updated {
		return s.invoiceRepo.Update(ctx, flexInvoice)
	}

	return nil
}

// updateStripeInvoiceMetadata updates the Stripe invoice metadata to track FlexPrice credit payments
func (s *InvoiceSyncService) updateStripeInvoiceMetadata(ctx context.Context, stripeClient *stripe.Client, stripeInvoiceID string, paymentAmount decimal.Decimal, paymentSource string, paymentMetadata map[string]string) error {
	// Get current invoice to read existing metadata
	currentInvoice, err := stripeClient.V1Invoices.Retrieve(ctx, stripeInvoiceID, nil)
	if err != nil {
		return err
	}

	// Track total FlexPrice credit amount paid (only this field needed)
	totalCreditsKey := "flexprice_credits_paid_cents"
	currentCreditsStr := ""
	if currentInvoice.Metadata != nil {
		currentCreditsStr = currentInvoice.Metadata[totalCreditsKey]
	}

	// Parse existing credits amount (keep everything in decimal for accuracy)
	var currentCredits decimal.Decimal
	if currentCreditsStr != "" {
		if parsed, err := decimal.NewFromString(currentCreditsStr); err == nil {
			currentCredits = parsed
		}
	}

	// Add new payment amount (convert to cents as decimal for precision)
	paymentAmountCents := paymentAmount.Mul(decimal.NewFromInt(100))
	newTotalCredits := currentCredits.Add(paymentAmountCents)

	// Update the invoice metadata with only the credits total
	updateParams := &stripe.InvoiceUpdateParams{}
	updateParams.AddMetadata(totalCreditsKey, newTotalCredits.String())

	s.logger.Infow("updating Stripe invoice metadata with total credit amount",
		"stripe_invoice_id", stripeInvoiceID,
		"payment_amount_cents", paymentAmountCents.String(),
		"new_total_credits_cents", newTotalCredits.String())

	_, err = stripeClient.V1Invoices.Update(ctx, stripeInvoiceID, updateParams)
	if err != nil {
		return err
	}

	s.logger.Infow("successfully updated Stripe invoice metadata",
		"stripe_invoice_id", stripeInvoiceID,
		"total_credits_paid_cents", newTotalCredits.String())

	return nil
}

// IsInvoiceSyncedToStripe checks if an invoice is already synced to Stripe
func (s *InvoiceSyncService) IsInvoiceSyncedToStripe(ctx context.Context, invoiceID string) bool {
	_, err := s.getExistingStripeMapping(ctx, invoiceID)
	return err == nil
}
