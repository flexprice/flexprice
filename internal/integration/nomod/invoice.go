package nomod

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// InvoiceSyncService handles synchronization of FlexPrice invoices with Nomod
type InvoiceSyncService struct {
	client                       NomodClient
	customerSvc                  *CustomerService
	invoiceRepo                  invoice.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewInvoiceSyncService creates a new Nomod invoice sync service
func NewInvoiceSyncService(
	client NomodClient,
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

// SyncInvoiceToNomod syncs a FlexPrice invoice to Nomod
// This creates an invoice in Nomod with all line items in a single API call
func (s *InvoiceSyncService) SyncInvoiceToNomod(
	ctx context.Context,
	req NomodInvoiceSyncRequest,
	customerService interfaces.CustomerService,
) (*NomodInvoiceSyncResponse, error) {
	s.logger.Infow("starting Nomod invoice sync",
		"invoice_id", req.InvoiceID)

	// Step 1: Check if Nomod connection exists
	if !s.client.HasNomodConnection(ctx) {
		return nil, ierr.NewError("Nomod connection not available").
			WithHint("Nomod integration must be configured for invoice sync").
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
	existingMapping, err := s.GetExistingNomodMapping(ctx, req.InvoiceID)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}

	if existingMapping != nil {
		nomodInvoiceID := existingMapping.ProviderEntityID
		s.logger.Infow("invoice already synced to Nomod",
			"invoice_id", req.InvoiceID,
			"nomod_invoice_id", nomodInvoiceID)

		// Return existing invoice details from mapping metadata
		return s.buildResponseFromMapping(existingMapping), nil
	}

	// Step 4: Ensure customer is synced to Nomod
	flexpriceCustomer, err := s.customerSvc.EnsureCustomerSyncedToNomod(ctx, flexInvoice.CustomerID, customerService)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to sync customer to Nomod").
			Mark(ierr.ErrInternal)
	}

	nomodCustomerID := flexpriceCustomer.Metadata["nomod_customer_id"]
	s.logger.Infow("customer synced to Nomod",
		"customer_id", flexInvoice.CustomerID,
		"nomod_customer_id", nomodCustomerID)

	// Step 5: Build invoice request
	invoiceReq, err := s.buildInvoiceRequest(ctx, flexInvoice, nomodCustomerID)
	if err != nil {
		return nil, err
	}

	// Step 6: Create invoice in Nomod (single API call)
	nomodInvoice, err := s.client.CreateInvoice(ctx, *invoiceReq)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create invoice in Nomod").
			Mark(ierr.ErrInternal)
	}

	nomodInvoiceID := nomodInvoice.ID
	s.logger.Infow("successfully created invoice in Nomod",
		"invoice_id", req.InvoiceID,
		"nomod_invoice_id", nomodInvoiceID)

	// Step 7: Create entity integration mapping
	if err := s.createInvoiceMapping(ctx, req.InvoiceID, nomodInvoice, flexInvoice.EnvironmentID); err != nil {
		s.logger.Errorw("failed to create invoice mapping",
			"error", err,
			"invoice_id", req.InvoiceID,
			"nomod_invoice_id", nomodInvoiceID)
		// Don't fail the sync, just log the error
	}

	// Step 8: Update FlexPrice invoice metadata with Nomod details
	if err := s.updateFlexPriceInvoiceFromNomod(ctx, flexInvoice, nomodInvoice); err != nil {
		s.logger.Errorw("failed to update FlexPrice invoice metadata from Nomod", "error", err)
		// Don't fail the entire sync for this
	}

	// Step 9: Build and return response
	return s.buildSyncResponse(nomodInvoice), nil
}

// buildInvoiceRequest constructs the Nomod invoice creation request
func (s *InvoiceSyncService) buildInvoiceRequest(
	ctx context.Context,
	flexInvoice *invoice.Invoice,
	nomodCustomerID string,
) (*CreateInvoiceRequest, error) {
	// Build line items
	items, err := s.buildLineItems(flexInvoice)
	if err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return nil, ierr.NewError("invoice has no line items").
			WithHint("Cannot create Nomod invoice without line items").
			Mark(ierr.ErrValidation)
	}

	// Format due date
	var dueDateStr string
	if flexInvoice.DueDate != nil {
		dueDateStr = flexInvoice.DueDate.Format("2006-01-02")
	} else {
		// Default to 30 days from now if no due date
		dueDateStr = time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	}

	// Build request
	req := &CreateInvoiceRequest{
		Currency: strings.ToUpper(flexInvoice.Currency),
		Items:    items,
		Customer: nomodCustomerID,
		DueDate:  dueDateStr,
	}

	// Add optional fields
	if flexInvoice.InvoiceNumber != nil && *flexInvoice.InvoiceNumber != "" {
		req.InvoiceNumber = flexInvoice.InvoiceNumber
	}

	// Add title if available
	title := s.buildInvoiceTitle(flexInvoice)
	if title != "" {
		req.Title = &title
	}

	// Add note/description
	note := s.buildInvoiceNote(flexInvoice)
	if note != "" {
		req.Note = &note
	}

	// Add starts_at if available (using PeriodStart)
	if flexInvoice.PeriodStart != nil {
		startsAt := flexInvoice.PeriodStart.Format("2006-01-02")
		req.StartsAt = &startsAt
	}

	s.logger.Infow("built invoice request for Nomod",
		"invoice_id", flexInvoice.ID,
		"line_items_count", len(items),
		"currency", flexInvoice.Currency,
		"has_due_date", flexInvoice.DueDate != nil)

	return req, nil
}

// buildLineItems converts FlexPrice line items to Nomod format
func (s *InvoiceSyncService) buildLineItems(flexInvoice *invoice.Invoice) ([]LineItem, error) {
	var items []LineItem

	for _, item := range flexInvoice.LineItems {
		// Skip zero-amount items
		if item.Amount.IsZero() {
			s.logger.Debugw("skipping zero-amount line item",
				"invoice_id", flexInvoice.ID)
			continue
		}

		// Get item name with fallback
		itemName := s.getLineItemName(item)

		// Nomod has quantity limits (typically 1-999)
		// For large quantities or usage-based billing, we set quantity to 1
		// and pass the total amount directly
		quantity := 1
		if !item.Quantity.IsZero() && item.Quantity.GreaterThan(decimal.Zero) {
			qty := int(item.Quantity.IntPart())
			// Only use actual quantity if it's reasonable (1-999)
			if qty > 0 && qty <= 999 {
				quantity = qty
			}
			// For quantities > 999, we default to 1 and use the total amount
		}

		// Build Nomod line item
		nomodItem := LineItem{
			Name:     itemName,
			Amount:   item.Amount.StringFixed(2), // Total amount for the line item
			Quantity: quantity,
		}

		items = append(items, nomodItem)
	}

	return items, nil
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

// buildInvoiceTitle creates a title for the invoice
func (s *InvoiceSyncService) buildInvoiceTitle(flexInvoice *invoice.Invoice) string {
	// Use invoice number if available
	if flexInvoice.InvoiceNumber != nil && *flexInvoice.InvoiceNumber != "" {
		return fmt.Sprintf("Invoice %s", *flexInvoice.InvoiceNumber)
	}

	// Fallback to generic title
	return "Invoice"
}

// buildInvoiceNote creates a note/description for the invoice
func (s *InvoiceSyncService) buildInvoiceNote(flexInvoice *invoice.Invoice) string {
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

// buildSyncResponse constructs the sync response from Nomod invoice data
func (s *InvoiceSyncService) buildSyncResponse(nomodInvoice *InvoiceResponse) *NomodInvoiceSyncResponse {
	// Parse amount with error handling
	amount, err := decimal.NewFromString(nomodInvoice.Amount)
	if err != nil {
		s.logger.Errorw("failed to parse Nomod invoice amount",
			"raw_amount", nomodInvoice.Amount,
			"invoice_id", nomodInvoice.ID,
			"error", err)
		// Use zero as fallback but log the error for visibility
		amount = decimal.Zero
	}

	return &NomodInvoiceSyncResponse{
		NomodInvoiceID: nomodInvoice.ID,
		ReferenceID:    nomodInvoice.ReferenceID,
		InvoiceNumber:  nomodInvoice.InvoiceNumber,
		Code:           nomodInvoice.Code,
		URL:            nomodInvoice.URL,
		Status:         nomodInvoice.Status,
		Amount:         amount,
		Currency:       nomodInvoice.Currency,
		CreatedAt:      nomodInvoice.Created,
	}
}

// buildResponseFromMapping builds response from existing mapping metadata
func (s *InvoiceSyncService) buildResponseFromMapping(mapping *entityintegrationmapping.EntityIntegrationMapping) *NomodInvoiceSyncResponse {
	response := &NomodInvoiceSyncResponse{
		NomodInvoiceID: mapping.ProviderEntityID,
	}

	// Extract metadata if available
	if mapping.Metadata != nil {
		if referenceID, ok := mapping.Metadata["nomod_reference_id"].(string); ok {
			response.ReferenceID = referenceID
		}
		if url, ok := mapping.Metadata["nomod_payment_url"].(string); ok {
			response.URL = url
		}
		if code, ok := mapping.Metadata["nomod_code"].(string); ok {
			response.Code = code
		}
		if status, ok := mapping.Metadata["nomod_status"].(string); ok {
			response.Status = status
		}
	}

	return response
}

// createInvoiceMapping creates entity integration mapping to track the sync
func (s *InvoiceSyncService) createInvoiceMapping(
	ctx context.Context,
	flexInvoiceID string,
	nomodInvoice *InvoiceResponse,
	environmentID string,
) error {
	metadata := map[string]interface{}{
		"nomod_payment_url":  nomodInvoice.URL,
		"nomod_reference_id": nomodInvoice.ReferenceID,
		"nomod_code":         nomodInvoice.Code,
		"nomod_status":       nomodInvoice.Status,
		"sync_source":        "flexprice",
		"synced_at":          time.Now().UTC().Format(time.RFC3339),
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityType:       types.IntegrationEntityTypeInvoice,
		EntityID:         flexInvoiceID,
		ProviderType:     string(types.SecretProviderNomod),
		ProviderEntityID: nomodInvoice.ID,
		Metadata:         metadata,
		EnvironmentID:    environmentID,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}

	if err := s.entityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		// If duplicate key error, invoice is already tracked (race condition)
		s.logger.Warnw("failed to create entity integration mapping (may already exist)",
			"error", err,
			"invoice_id", flexInvoiceID,
			"nomod_invoice_id", nomodInvoice.ID)
		return err
	}

	s.logger.Infow("created invoice mapping",
		"invoice_id", flexInvoiceID,
		"nomod_invoice_id", nomodInvoice.ID)

	return nil
}

// GetExistingNomodMapping checks if invoice is already synced to Nomod
func (s *InvoiceSyncService) GetExistingNomodMapping(
	ctx context.Context,
	flexInvoiceID string,
) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:    types.IntegrationEntityTypeInvoice,
		EntityID:      flexInvoiceID,
		ProviderTypes: []string{string(types.SecretProviderNomod)},
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to check existing invoice mapping").
			Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		return nil, ierr.NewError("invoice not synced to Nomod").
			Mark(ierr.ErrNotFound)
	}

	return mappings[0], nil
}

// GetNomodInvoiceID retrieves the Nomod invoice ID from a FlexPrice invoice ID
func (s *InvoiceSyncService) GetNomodInvoiceID(ctx context.Context, flexInvoiceID string) (string, error) {
	mapping, err := s.GetExistingNomodMapping(ctx, flexInvoiceID)
	if err != nil {
		return "", err
	}

	return mapping.ProviderEntityID, nil
}

// GetFlexPriceInvoiceID retrieves the FlexPrice invoice ID from a Nomod invoice ID (reverse lookup)
// This is used when processing external Nomod payments to find the corresponding FlexPrice invoice
func (s *InvoiceSyncService) GetFlexPriceInvoiceID(ctx context.Context, nomodInvoiceID string) (string, error) {
	if s.entityIntegrationMappingRepo == nil {
		return "", ierr.NewError("entity integration mapping repository not available").
			Mark(ierr.ErrNotFound)
	}

	s.logger.Debugw("looking up FlexPrice invoice ID from Nomod invoice ID",
		"nomod_invoice_id", nomodInvoiceID)

	filter := &types.EntityIntegrationMappingFilter{
		ProviderEntityIDs: []string{nomodInvoiceID},
		EntityType:        types.IntegrationEntityTypeInvoice,
		ProviderTypes:     []string{string(types.SecretProviderNomod)},
		QueryFilter:       types.NewDefaultQueryFilter(),
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		s.logger.Debugw("failed to query entity integration mapping",
			"error", err,
			"nomod_invoice_id", nomodInvoiceID)
		return "", ierr.WithError(err).
			WithHint("Failed to look up invoice mapping").
			Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		s.logger.Debugw("no FlexPrice invoice mapping found for Nomod invoice",
			"nomod_invoice_id", nomodInvoiceID)
		return "", ierr.NewError("flexprice invoice mapping not found").
			Mark(ierr.ErrNotFound)
	}

	flexpriceInvoiceID := mappings[0].EntityID
	s.logger.Infow("found FlexPrice invoice mapping",
		"nomod_invoice_id", nomodInvoiceID,
		"flexprice_invoice_id", flexpriceInvoiceID)

	return flexpriceInvoiceID, nil
}

// updateFlexPriceInvoiceFromNomod updates FlexPrice invoice with data from Nomod
func (s *InvoiceSyncService) updateFlexPriceInvoiceFromNomod(ctx context.Context, flexInvoice *invoice.Invoice, nomodInvoice *InvoiceResponse) error {
	// Initialize metadata if not exists
	if flexInvoice.Metadata == nil {
		flexInvoice.Metadata = make(types.Metadata)
	}

	// Update invoice metadata with Nomod details
	updated := false

	// Store Nomod invoice URL
	if nomodInvoice.URL != "" {
		flexInvoice.Metadata["nomod_invoice_url"] = nomodInvoice.URL
		updated = true
	}

	// Store Nomod invoice ID
	if nomodInvoice.ID != "" {
		flexInvoice.Metadata["nomod_invoice_id"] = nomodInvoice.ID
		updated = true
	}

	// Store Nomod reference ID
	if nomodInvoice.ReferenceID != "" {
		flexInvoice.Metadata["nomod_reference_id"] = nomodInvoice.ReferenceID
		updated = true
	}

	// Store Nomod invoice status
	if nomodInvoice.Status != "" {
		flexInvoice.Metadata["nomod_invoice_status"] = nomodInvoice.Status
		updated = true
	}

	// Store Nomod invoice code
	if nomodInvoice.Code != "" {
		flexInvoice.Metadata["nomod_invoice_code"] = nomodInvoice.Code
		updated = true
	}

	if updated {
		s.logger.Infow("updating FlexPrice invoice with Nomod details",
			"invoice_id", flexInvoice.ID,
			"nomod_invoice_id", nomodInvoice.ID,
			"nomod_invoice_url", nomodInvoice.URL)

		return s.invoiceRepo.Update(ctx, flexInvoice)
	}

	return nil
}
