package razorpay

import (
	"context"
	"fmt"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// InvoiceSyncService handles synchronization of FlexPrice invoices with Razorpay
type InvoiceSyncService struct {
	client                       RazorpayClient
	customerSvc                  *CustomerService
	invoiceRepo                  invoice.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewInvoiceSyncService creates a new Razorpay invoice sync service
func NewInvoiceSyncService(
	client RazorpayClient,
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

// SyncInvoiceToRazorpay syncs a FlexPrice invoice to Razorpay
// This creates an invoice in Razorpay with all line items in a single API call
func (s *InvoiceSyncService) SyncInvoiceToRazorpay(
	ctx context.Context,
	req RazorpayInvoiceSyncRequest,
	customerService interfaces.CustomerService,
) (*RazorpayInvoiceSyncResponse, error) {
	s.logger.Infow("starting Razorpay invoice sync",
		"invoice_id", req.InvoiceID)

	// Step 1: Check if Razorpay connection exists
	if !s.client.HasRazorpayConnection(ctx) {
		return nil, ierr.NewError("Razorpay connection not available").
			WithHint("Razorpay integration must be configured for invoice sync").
			Mark(ierr.ErrNotFound)
	}

	// Step 2: Get FlexPrice invoice
	flexInvoice, err := s.invoiceRepo.Get(ctx, req.InvoiceID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get FlexPrice invoice").
			Mark(ierr.ErrDatabase)
	}

	// Step 3: Check if invoice is already synced to avoid duplicates
	existingMapping, err := s.GetExistingRazorpayMapping(ctx, req.InvoiceID)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}

	if existingMapping != nil {
		razorpayInvoiceID := existingMapping.ProviderEntityID
		s.logger.Infow("invoice already synced to Razorpay",
			"invoice_id", req.InvoiceID,
			"razorpay_invoice_id", razorpayInvoiceID)

		// Fetch existing invoice details and return
		return s.fetchInvoiceResponse(ctx, razorpayInvoiceID)
	}

	// Step 4: Ensure customer is synced to Razorpay
	flexpriceCustomer, err := s.customerSvc.EnsureCustomerSyncedToRazorpay(ctx, flexInvoice.CustomerID, customerService)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to sync customer to Razorpay").
			Mark(ierr.ErrInternal)
	}

	razorpayCustomerID := flexpriceCustomer.Metadata["razorpay_customer_id"]
	s.logger.Infow("customer synced to Razorpay",
		"customer_id", flexInvoice.CustomerID,
		"razorpay_customer_id", razorpayCustomerID)

	// Step 5: Build invoice data with inline line items
	invoiceData, err := s.buildInvoiceData(ctx, flexInvoice, razorpayCustomerID)
	if err != nil {
		return nil, err
	}

	// Step 6: Create invoice in Razorpay (single API call)
	razorpayInvoice, err := s.client.CreateInvoice(ctx, invoiceData)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create invoice in Razorpay").
			Mark(ierr.ErrInternal)
	}

	razorpayInvoiceID := razorpayInvoice["id"].(string)
	s.logger.Infow("successfully created invoice in Razorpay",
		"invoice_id", req.InvoiceID,
		"razorpay_invoice_id", razorpayInvoiceID)

	// Extract short URL for storage in mapping
	razorpayShortURL := ""
	if shortURL, ok := razorpayInvoice["short_url"].(string); ok {
		razorpayShortURL = shortURL
	}

	// Step 7: Create entity integration mapping with short URL
	if err := s.createInvoiceMapping(ctx, req.InvoiceID, razorpayInvoiceID, razorpayShortURL, flexInvoice.EnvironmentID); err != nil {
		s.logger.Errorw("failed to create invoice mapping",
			"error", err,
			"invoice_id", req.InvoiceID,
			"razorpay_invoice_id", razorpayInvoiceID)
		// Don't fail the sync, just log the error
	}

	// Step 8: Build and return response
	return s.buildSyncResponse(razorpayInvoice), nil
}

// buildInvoiceData constructs the Razorpay invoice creation payload
func (s *InvoiceSyncService) buildInvoiceData(
	ctx context.Context,
	flexInvoice *invoice.Invoice,
	razorpayCustomerID string,
) (map[string]interface{}, error) {
	// Build line items array
	lineItems, err := s.buildLineItems(flexInvoice)
	if err != nil {
		return nil, err
	}

	if len(lineItems) == 0 {
		return nil, ierr.NewError("invoice has no line items").
			WithHint("Cannot create Razorpay invoice without line items").
			Mark(ierr.ErrValidation)
	}

	// Build description
	description := s.buildInvoiceDescription(flexInvoice)

	// Build notes with metadata
	notes := map[string]interface{}{
		"flexprice_invoice_id":     flexInvoice.ID,
		"flexprice_customer_id":    flexInvoice.CustomerID,
		"flexprice_environment_id": flexInvoice.EnvironmentID,
		"sync_source":              "flexprice",
	}

	// Add invoice number to notes if available
	if flexInvoice.InvoiceNumber != nil {
		notes["invoice_number"] = *flexInvoice.InvoiceNumber
	}

	// Construct invoice data according to Razorpay API format
	// Use invoice currency (convert to uppercase as Razorpay expects uppercase currency codes)
	invoiceCurrency := strings.ToUpper(flexInvoice.Currency)
	invoiceData := map[string]interface{}{
		"type":         "invoice",
		"customer_id":  razorpayCustomerID,
		"line_items":   lineItems,
		"currency":     invoiceCurrency,
		"description":  description,
		"email_notify": true,  // Enable email notifications
		"sms_notify":   false, // Disable SMS notifications
		"notes":        notes,
	}

	// Add due date if available (Unix timestamp in seconds)
	if flexInvoice.DueDate != nil {
		invoiceData["expire_by"] = flexInvoice.DueDate.Unix()
	}

	s.logger.Infow("built invoice data for Razorpay",
		"invoice_id", flexInvoice.ID,
		"line_items_count", len(lineItems),
		"currency", flexInvoice.Currency,
		"has_due_date", flexInvoice.DueDate != nil)

	return invoiceData, nil
}

// buildLineItems converts FlexPrice line items to Razorpay format
func (s *InvoiceSyncService) buildLineItems(flexInvoice *invoice.Invoice) (map[string]interface{}, error) {
	lineItems := make(map[string]interface{})
	lineItemIndex := 0

	for _, item := range flexInvoice.LineItems {
		// Skip zero-amount items
		if item.Amount.IsZero() {
			s.logger.Debugw("skipping zero-amount line item",
				"invoice_id", flexInvoice.ID,
				"line_item_index", lineItemIndex)
			continue
		}

		// Get item name with fallback
		itemName := s.getLineItemName(item)

		// Get item description (entity type for clarity)
		itemDescription := s.getLineItemDescription(item)

		// Keep quantity as 1 and use total line item amount
		quantity := 1

		// Convert total line item amount to smallest currency unit (paise/cents)
		// Razorpay expects integer amount in smallest unit
		amountInSmallestUnit := item.Amount.Mul(decimal.NewFromInt(100)).IntPart()

		// Build Razorpay line item
		// Use line item currency (convert to uppercase as Razorpay expects uppercase currency codes)
		// Fallback to invoice currency if line item currency is empty
		lineItemCurrency := strings.ToUpper(item.Currency)
		if lineItemCurrency == "" {
			lineItemCurrency = strings.ToUpper(flexInvoice.Currency)
		}
		razorpayLineItem := RazorpayLineItem{
			Name:        itemName,
			Description: itemDescription,
			Amount:      amountInSmallestUnit,
			Currency:    lineItemCurrency,
			Quantity:    quantity,
		}

		// Add to line items map (Razorpay expects sequential indexed map)
		// Use separate counter to ensure sequential indices even when items are skipped
		lineItems[fmt.Sprintf("%d", lineItemIndex)] = razorpayLineItem
		lineItemIndex++
	}

	return lineItems, nil
}

// getLineItemName extracts the display name for a line item
func (s *InvoiceSyncService) getLineItemName(item *invoice.InvoiceLineItem) string {
	// Priority: DisplayName > PlanDisplayName > Default
	if item.DisplayName != nil && *item.DisplayName != "" {
		return *item.DisplayName
	}

	if item.PlanDisplayName != nil && *item.PlanDisplayName != "" {
		return *item.PlanDisplayName
	}

	return DefaultItemName
}

// getLineItemDescription builds a description based on entity type
func (s *InvoiceSyncService) getLineItemDescription(item *invoice.InvoiceLineItem) string {
	if item.EntityType == nil {
		return "Service"
	}

	switch *item.EntityType {
	case string(types.InvoiceLineItemEntityTypePlan):
		return "Subscription Plan"
	case string(types.InvoiceLineItemEntityTypeAddon):
		return "Add-on"
	default:
		return "Service"
	}
}

// buildInvoiceDescription creates a concise description for the invoice
func (s *InvoiceSyncService) buildInvoiceDescription(flexInvoice *invoice.Invoice) string {
	// Use invoice number if available
	if flexInvoice.InvoiceNumber != nil && *flexInvoice.InvoiceNumber != "" {
		return fmt.Sprintf("Invoice %s", *flexInvoice.InvoiceNumber)
	}

	// Fallback to generic description with item count
	itemCount := len(flexInvoice.LineItems)
	if itemCount == 1 {
		return "Invoice for 1 item"
	}

	return fmt.Sprintf("Invoice for %d items", itemCount)
}

// buildSyncResponse constructs the sync response from Razorpay invoice data
func (s *InvoiceSyncService) buildSyncResponse(razorpayInvoice map[string]interface{}) *RazorpayInvoiceSyncResponse {
	response := &RazorpayInvoiceSyncResponse{
		RazorpayInvoiceID: lo.FromPtrOr(extractString(razorpayInvoice, "id"), ""),
		InvoiceNumber:     lo.FromPtrOr(extractString(razorpayInvoice, "invoice_number"), ""),
		ShortURL:          lo.FromPtrOr(extractString(razorpayInvoice, "short_url"), ""),
		Status:            lo.FromPtrOr(extractString(razorpayInvoice, "status"), ""),
		Currency:          lo.FromPtrOr(extractString(razorpayInvoice, "currency"), ""),
	}

	// Extract amounts (Razorpay returns in smallest unit)
	if amount, ok := razorpayInvoice["amount"].(float64); ok {
		response.Amount = decimal.NewFromFloat(amount).Div(decimal.NewFromInt(100))
	}

	if amountDue, ok := razorpayInvoice["amount_due"].(float64); ok {
		response.AmountDue = decimal.NewFromFloat(amountDue).Div(decimal.NewFromInt(100))
	}

	// Extract timestamp
	if createdAt, ok := razorpayInvoice["created_at"].(float64); ok {
		response.CreatedAt = int64(createdAt)
	}

	return response
}

// fetchInvoiceResponse fetches existing invoice and builds response
func (s *InvoiceSyncService) fetchInvoiceResponse(ctx context.Context, razorpayInvoiceID string) (*RazorpayInvoiceSyncResponse, error) {
	razorpayInvoice, err := s.client.GetInvoice(ctx, razorpayInvoiceID)
	if err != nil {
		return nil, err
	}

	return s.buildSyncResponse(razorpayInvoice), nil
}

// createInvoiceMapping creates entity integration mapping to track the sync
func (s *InvoiceSyncService) createInvoiceMapping(
	ctx context.Context,
	flexInvoiceID string,
	razorpayInvoiceID string,
	razorpayShortURL string,
	environmentID string,
) error {
	metadata := make(map[string]interface{})
	if razorpayShortURL != "" {
		metadata["razorpay_payment_url"] = razorpayShortURL
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityType:       types.IntegrationEntityTypeInvoice,
		EntityID:         flexInvoiceID,
		ProviderType:     string(types.SecretProviderRazorpay),
		ProviderEntityID: razorpayInvoiceID,
		Metadata:         metadata,
		EnvironmentID:    environmentID,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}

	if err := s.entityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		// If duplicate key error, invoice is already tracked (race condition)
		s.logger.Warnw("failed to create entity integration mapping (may already exist)",
			"error", err,
			"invoice_id", flexInvoiceID,
			"razorpay_invoice_id", razorpayInvoiceID)
		return err
	}

	s.logger.Infow("created invoice mapping",
		"invoice_id", flexInvoiceID,
		"razorpay_invoice_id", razorpayInvoiceID)

	return nil
}

// GetExistingRazorpayMapping checks if invoice is already synced to Razorpay
func (s *InvoiceSyncService) GetExistingRazorpayMapping(
	ctx context.Context,
	flexInvoiceID string,
) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:    types.IntegrationEntityTypeInvoice,
		EntityID:      flexInvoiceID,
		ProviderTypes: []string{string(types.SecretProviderRazorpay)},
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to check existing invoice mapping").
			Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		return nil, ierr.NewError("invoice not synced to Razorpay").
			Mark(ierr.ErrNotFound)
	}

	return mappings[0], nil
}

// GetFlexPriceInvoiceID retrieves the FlexPrice invoice ID from a Razorpay invoice ID (reverse lookup)
// This is used when processing external Razorpay payments to find the corresponding FlexPrice invoice
func (s *InvoiceSyncService) GetFlexPriceInvoiceID(ctx context.Context, razorpayInvoiceID string) (string, error) {
	if s.entityIntegrationMappingRepo == nil {
		return "", ierr.NewError("entity integration mapping repository not available").
			Mark(ierr.ErrNotFound)
	}

	s.logger.Debugw("looking up FlexPrice invoice ID from Razorpay invoice ID",
		"razorpay_invoice_id", razorpayInvoiceID)

	filter := &types.EntityIntegrationMappingFilter{
		ProviderEntityIDs: []string{razorpayInvoiceID},
		EntityType:        types.IntegrationEntityTypeInvoice,
		ProviderTypes:     []string{string(types.SecretProviderRazorpay)},
		QueryFilter:       types.NewDefaultQueryFilter(),
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		s.logger.Debugw("failed to query entity integration mapping",
			"error", err,
			"razorpay_invoice_id", razorpayInvoiceID)
		return "", ierr.WithError(err).
			WithHint("Failed to look up invoice mapping").
			Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		s.logger.Debugw("no FlexPrice invoice mapping found for Razorpay invoice",
			"razorpay_invoice_id", razorpayInvoiceID)
		return "", ierr.NewError("flexprice invoice mapping not found").
			Mark(ierr.ErrNotFound)
	}

	flexpriceInvoiceID := mappings[0].EntityID
	s.logger.Infow("found FlexPrice invoice mapping",
		"razorpay_invoice_id", razorpayInvoiceID,
		"flexprice_invoice_id", flexpriceInvoiceID)

	return flexpriceInvoiceID, nil
}

// extractString safely extracts a string value from map
func extractString(data map[string]interface{}, key string) *string {
	if val, ok := data[key].(string); ok {
		return &val
	}
	return nil
}
