package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v82"
)

// StripeInvoiceSyncService handles synchronization of FlexPrice invoices with Stripe
type StripeInvoiceSyncService struct {
	ServiceParams
	stripeService *StripeService
}

// NewStripeInvoiceSyncService creates a new Stripe invoice sync service
func NewStripeInvoiceSyncService(params ServiceParams) *StripeInvoiceSyncService {
	return &StripeInvoiceSyncService{
		ServiceParams: params,
		stripeService: NewStripeService(params),
	}
}

// SyncInvoiceToStripe syncs a FlexPrice invoice to Stripe following the complete flow
func (s *StripeInvoiceSyncService) SyncInvoiceToStripe(ctx context.Context, req dto.StripeInvoiceSyncRequest) (*dto.StripeInvoiceSyncResponse, error) {
	s.Logger.Infow("starting Stripe invoice sync",
		"invoice_id", req.InvoiceID,
		"collection_method", req.CollectionMethod)

	// Step 1: Check if Stripe connection exists
	if !s.hasStripeConnection(ctx) {
		return nil, ierr.NewError("Stripe connection not available").
			WithHint("Stripe integration must be configured for invoice sync").
			Mark(ierr.ErrNotFound)
	}

	// Step 2: Get FlexPrice invoice
	flexInvoice, err := s.InvoiceRepo.Get(ctx, req.InvoiceID)
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
		s.Logger.Infow("invoice already synced to Stripe",
			"invoice_id", req.InvoiceID,
			"stripe_invoice_id", stripeInvoiceID)
	} else {
		// Step 4: Create draft invoice in Stripe
		stripeInvoiceID, err = s.createDraftInvoiceInStripe(ctx, flexInvoice, req.CollectionMethod)
		if err != nil {
			return nil, err
		}

		// Step 5: Create entity integration mapping
		if err := s.createInvoiceMapping(ctx, req.InvoiceID, stripeInvoiceID); err != nil {
			s.Logger.Errorw("failed to create invoice mapping", "error", err)
			// Continue with sync even if mapping fails
		}
	}

	// Step 6: Sync line items to Stripe
	if err := s.syncLineItemsToStripe(ctx, flexInvoice, stripeInvoiceID); err != nil {
		return nil, err
	}

	// Step 7: Finalize invoice in Stripe
	finalizedInvoice, err := s.finalizeStripeInvoice(ctx, stripeInvoiceID, req.CollectionMethod)
	if err != nil {
		return nil, err
	}

	// Step 8: Update FlexPrice invoice with Stripe data
	if err := s.updateFlexPriceInvoiceFromStripe(ctx, flexInvoice, finalizedInvoice); err != nil {
		s.Logger.Errorw("failed to update FlexPrice invoice from Stripe", "error", err)
		// Don't fail the entire sync for this
	}

	response := &dto.StripeInvoiceSyncResponse{
		StripeInvoiceID:  finalizedInvoice.ID,
		Status:           string(finalizedInvoice.Status),
		HostedInvoiceURL: finalizedInvoice.HostedInvoiceURL,
		InvoicePDF:       finalizedInvoice.InvoicePDF,
		Metadata: map[string]interface{}{
			"flexprice_invoice_id": req.InvoiceID,
			"sync_timestamp":       finalizedInvoice.Created,
		},
	}

	s.Logger.Infow("Stripe invoice sync completed successfully",
		"invoice_id", req.InvoiceID,
		"stripe_invoice_id", finalizedInvoice.ID,
		"status", finalizedInvoice.Status)

	return response, nil
}

// createDraftInvoiceInStripe creates a draft invoice in Stripe
func (s *StripeInvoiceSyncService) createDraftInvoiceInStripe(ctx context.Context, flexInvoice *invoice.Invoice, collectionMethod types.CollectionMethod) (string, error) {
	// Get Stripe connection and client
	stripeClient, err := s.getStripeClient(ctx)
	if err != nil {
		return "", err
	}

	// Get customer's Stripe ID
	stripeCustomerID, err := s.getStripeCustomerID(ctx, flexInvoice.CustomerID)
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
		AutoAdvance: stripe.Bool(false), // We control finalization
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
		s.Logger.Errorw("failed to create draft invoice in Stripe",
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

	s.Logger.Infow("created draft invoice in Stripe",
		"invoice_id", flexInvoice.ID,
		"stripe_invoice_id", stripeInvoice.ID)

	return stripeInvoice.ID, nil
}

// syncLineItemsToStripe adds all line items to the Stripe invoice
func (s *StripeInvoiceSyncService) syncLineItemsToStripe(ctx context.Context, flexInvoice *invoice.Invoice, stripeInvoiceID string) error {
	if len(flexInvoice.LineItems) == 0 {
		s.Logger.Infow("no line items to sync", "invoice_id", flexInvoice.ID)
		return nil
	}

	stripeClient, err := s.getStripeClient(ctx)
	if err != nil {
		return err
	}

	s.Logger.Infow("syncing line items to Stripe",
		"invoice_id", flexInvoice.ID,
		"stripe_invoice_id", stripeInvoiceID,
		"line_items_count", len(flexInvoice.LineItems))

	// Add each line item to Stripe invoice
	for _, lineItem := range flexInvoice.LineItems {
		if err := s.addLineItemToStripeInvoice(ctx, stripeClient, stripeInvoiceID, lineItem, flexInvoice); err != nil {
			return err
		}
	}

	s.Logger.Infow("successfully synced all line items to Stripe",
		"invoice_id", flexInvoice.ID,
		"stripe_invoice_id", stripeInvoiceID)

	return nil
}

// addLineItemToStripeInvoice adds a single line item to Stripe invoice
func (s *StripeInvoiceSyncService) addLineItemToStripeInvoice(ctx context.Context, stripeClient *stripe.Client, stripeInvoiceID string, lineItem *invoice.InvoiceLineItem, flexInvoice *invoice.Invoice) error {
	// Convert amount to cents (Stripe uses cents)
	amountCents := lineItem.Amount.Mul(decimal.NewFromInt(100)).IntPart()

	// Get customer ID from the invoice
	customerID, err := s.getStripeCustomerID(ctx, flexInvoice.CustomerID)
	if err != nil {
		s.Logger.Errorw("failed to get Stripe customer ID",
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
		s.Logger.Errorw("failed to add line item to Stripe invoice",
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

	s.Logger.Debugw("added line item to Stripe invoice",
		"line_item_id", lineItem.ID,
		"stripe_invoice_id", stripeInvoiceID,
		"stripe_item_id", invoiceItem.ID)

	return nil
}

// finalizeStripeInvoice finalizes the invoice in Stripe
func (s *StripeInvoiceSyncService) finalizeStripeInvoice(ctx context.Context, stripeInvoiceID string, collectionMethod types.CollectionMethod) (*stripe.Invoice, error) {
	stripeClient, err := s.getStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	s.Logger.Infow("finalizing Stripe invoice",
		"stripe_invoice_id", stripeInvoiceID,
		"collection_method", collectionMethod)

	// Finalize the invoice
	params := &stripe.InvoiceFinalizeInvoiceParams{
		AutoAdvance: stripe.Bool(false), // Let Stripe handle payment intent creation and sending
	}

	finalizedInvoice, err := stripeClient.V1Invoices.FinalizeInvoice(ctx, stripeInvoiceID, params)
	if err != nil {
		s.Logger.Errorw("failed to finalize Stripe invoice",
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

	s.Logger.Infow("successfully finalized Stripe invoice",
		"stripe_invoice_id", stripeInvoiceID,
		"status", finalizedInvoice.Status,
		"total", finalizedInvoice.Total)

	// Send invoice if collection method is send_invoice
	if collectionMethod == types.CollectionMethodSendInvoice {
		s.Logger.Infow("sending invoice to customer via Stripe",
			"stripe_invoice_id", stripeInvoiceID,
			"collection_method", collectionMethod)

		_, err = stripeClient.V1Invoices.SendInvoice(ctx, stripeInvoiceID, &stripe.InvoiceSendInvoiceParams{})
		if err != nil {
			s.Logger.Errorw("failed to send Stripe invoice",
				"error", err,
				"stripe_invoice_id", stripeInvoiceID)
			// Don't fail the entire sync if sending fails, just log the error
		} else {
			s.Logger.Infow("successfully sent Stripe invoice to customer",
				"stripe_invoice_id", stripeInvoiceID)
		}
	}

	return finalizedInvoice, nil
}

// SyncPaymentToStripe syncs a FlexPrice payment to Stripe as an external payment
func (s *StripeInvoiceSyncService) SyncPaymentToStripe(ctx context.Context, invoiceID string, paymentAmount decimal.Decimal, paymentSource string, metadata map[string]string) error {
	// Get Stripe invoice ID from mapping
	mapping, err := s.getExistingStripeMapping(ctx, invoiceID)
	if err != nil {
		return ierr.WithError(err).WithHint("Invoice not synced to Stripe").Mark(ierr.ErrNotFound)
	}

	stripeClient, err := s.getStripeClient(ctx)
	if err != nil {
		return err
	}

	stripeInvoiceID := mapping.ProviderEntityID
	amountCents := paymentAmount.Mul(decimal.NewFromInt(100)).IntPart()

	s.Logger.Infow("syncing external payment to Stripe",
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
		s.Logger.Infow("Stripe invoice not in open status, skipping payment sync",
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

	s.Logger.Infow("marking external payment as paid out of band in Stripe",
		"stripe_invoice_id", stripeInvoiceID,
		"amount_cents", amountCents,
		"payment_source", paymentSource)

	updatedInvoice, err := stripeClient.V1Invoices.Pay(ctx, stripeInvoiceID, payParams)
	if err != nil {
		s.Logger.Errorw("failed to mark payment as paid out of band",
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
		s.Logger.Errorw("failed to update Stripe invoice metadata",
			"error", err,
			"stripe_invoice_id", stripeInvoiceID,
			"amount", paymentAmount)
		// Don't fail the whole operation, just log the error
	}

	s.Logger.Infow("successfully synced payment to Stripe",
		"invoice_id", invoiceID,
		"stripe_invoice_id", stripeInvoiceID,
		"amount", paymentAmount,
		"new_status", updatedInvoice.Status)

	return nil
}

// Helper methods

// hasStripeConnection checks if Stripe connection is available
func (s *StripeInvoiceSyncService) hasStripeConnection(ctx context.Context) bool {
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	return err == nil && conn != nil
}

// getStripeClient gets an authenticated Stripe client
func (s *StripeInvoiceSyncService) getStripeClient(ctx context.Context) (*stripe.Client, error) {
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured").
			Mark(ierr.ErrNotFound)
	}

	stripeConfig, err := s.stripeService.GetDecryptedStripeConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	return stripe.NewClient(stripeConfig.SecretKey, nil), nil
}

// getStripeCustomerID gets the Stripe customer ID for a FlexPrice customer
func (s *StripeInvoiceSyncService) getStripeCustomerID(ctx context.Context, customerID string) (string, error) {
	customerService := NewCustomerService(s.ServiceParams)
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

// getExistingStripeMapping gets existing Stripe mapping for an invoice
func (s *StripeInvoiceSyncService) getExistingStripeMapping(ctx context.Context, invoiceID string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	mappingService := NewEntityIntegrationMappingService(s.ServiceParams)

	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      invoiceID,
		EntityType:    types.IntegrationEntityTypeInvoice,
		ProviderTypes: []string{"stripe"},
		QueryFilter:   types.NewDefaultQueryFilter(),
	}

	mappings, err := mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(mappings.Items) == 0 {
		return nil, ierr.NewError("mapping not found").Mark(ierr.ErrNotFound)
	}

	// Convert response to domain model
	mappingResp := mappings.Items[0]
	return &entityintegrationmapping.EntityIntegrationMapping{
		ID:               mappingResp.ID,
		EntityID:         mappingResp.EntityID,
		EntityType:       mappingResp.EntityType,
		ProviderType:     mappingResp.ProviderType,
		ProviderEntityID: mappingResp.ProviderEntityID,
		EnvironmentID:    mappingResp.EnvironmentID,
	}, nil
}

// createInvoiceMapping creates a new entity integration mapping for the invoice
func (s *StripeInvoiceSyncService) createInvoiceMapping(ctx context.Context, invoiceID, stripeInvoiceID string) error {
	mappingService := NewEntityIntegrationMappingService(s.ServiceParams)

	createReq := dto.CreateEntityIntegrationMappingRequest{
		EntityID:         invoiceID,
		EntityType:       types.IntegrationEntityTypeInvoice,
		ProviderType:     "stripe",
		ProviderEntityID: stripeInvoiceID,
		Metadata: map[string]interface{}{
			"sync_timestamp": time.Now().Unix(),
			"sync_source":    "flexprice",
		},
	}

	_, err := mappingService.CreateEntityIntegrationMapping(ctx, createReq)
	return err
}

// updateFlexPriceInvoiceFromStripe updates FlexPrice invoice with data from Stripe
func (s *StripeInvoiceSyncService) updateFlexPriceInvoiceFromStripe(ctx context.Context, flexInvoice *invoice.Invoice, stripeInvoice *stripe.Invoice) error {
	// Update invoice with Stripe data if needed
	updated := false

	// Update total if Stripe calculated taxes
	if stripeInvoice.Total > 0 {
		stripeTotal := decimal.NewFromInt(stripeInvoice.Total).Div(decimal.NewFromInt(100))
		if !flexInvoice.Total.Equal(stripeTotal) {
			flexInvoice.Total = stripeTotal
			flexInvoice.AmountDue = stripeTotal
			updated = true
		}
	}

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
		return s.InvoiceRepo.Update(ctx, flexInvoice)
	}

	return nil
}

// updateStripeInvoiceMetadata updates the Stripe invoice metadata to track FlexPrice credit payments
func (s *StripeInvoiceSyncService) updateStripeInvoiceMetadata(ctx context.Context, stripeClient *stripe.Client, stripeInvoiceID string, paymentAmount decimal.Decimal, paymentSource string, paymentMetadata map[string]string) error {
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

	s.Logger.Infow("updating Stripe invoice metadata with total credit amount",
		"stripe_invoice_id", stripeInvoiceID,
		"payment_amount_cents", paymentAmountCents.String(),
		"new_total_credits_cents", newTotalCredits.String())

	_, err = stripeClient.V1Invoices.Update(ctx, stripeInvoiceID, updateParams)
	if err != nil {
		return err
	}

	s.Logger.Infow("successfully updated Stripe invoice metadata",
		"stripe_invoice_id", stripeInvoiceID,
		"total_credits_paid_cents", newTotalCredits.String())

	return nil
}
