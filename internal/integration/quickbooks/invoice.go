package quickbooks

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// QuickBooksInvoiceService defines the interface for QuickBooks invoice operations
type QuickBooksInvoiceService interface {
	SyncInvoiceToQuickBooks(ctx context.Context, req QuickBooksInvoiceSyncRequest) (*QuickBooksInvoiceSyncResponse, error)
}

// InvoiceServiceParams holds dependencies for InvoiceService
type InvoiceServiceParams struct {
	Client                       QuickBooksClient
	CustomerSvc                  QuickBooksCustomerService
	CustomerRepo                 customer.Repository
	InvoiceRepo                  invoice.Repository
	EntityIntegrationMappingRepo entityintegrationmapping.Repository
	Logger                       *logger.Logger
}

// InvoiceService handles QuickBooks invoice operations
type InvoiceService struct {
	InvoiceServiceParams
}

// NewInvoiceService creates a new QuickBooks invoice service
func NewInvoiceService(params InvoiceServiceParams) QuickBooksInvoiceService {
	return &InvoiceService{
		InvoiceServiceParams: params,
	}
}

// SyncInvoiceToQuickBooks syncs a Flexprice invoice to QuickBooks.
// Simple workflow:
// 1. Check if invoice mapping already exists - if yes, return it
// 2. Get or create customer in QuickBooks
// 3. Create invoice in QuickBooks
// 4. Create mapping
func (s *InvoiceService) SyncInvoiceToQuickBooks(
	ctx context.Context,
	req QuickBooksInvoiceSyncRequest,
) (*QuickBooksInvoiceSyncResponse, error) {
	s.Logger.Debugw("starting QuickBooks invoice sync",
		"invoice_id", req.InvoiceID)

	flexInvoice, err := s.InvoiceRepo.Get(ctx, req.InvoiceID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get Flexprice invoice").
			Mark(ierr.ErrDatabase)
	}

	// Step 1: Check if mapping exists
	existingMapping, err := s.getExistingQuickBooksMapping(ctx, req.InvoiceID)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}

	if existingMapping != nil {
		s.Logger.Infow("invoice already synced to QuickBooks",
			"invoice_id", req.InvoiceID,
			"quickbooks_invoice_id", existingMapping.ProviderEntityID)
		return &QuickBooksInvoiceSyncResponse{
			QuickBooksInvoiceID: existingMapping.ProviderEntityID,
			Total:               flexInvoice.Total,
			Currency:            flexInvoice.Currency,
		}, nil
	}

	// Step 2: Get or create customer
	flexpriceCustomer, err := s.CustomerRepo.Get(ctx, flexInvoice.CustomerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get Flexprice customer").
			Mark(ierr.ErrDatabase)
	}

	quickBooksCustomerID, err := s.CustomerSvc.GetOrCreateQuickBooksCustomer(ctx, flexpriceCustomer)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get or create QuickBooks customer").
			Mark(ierr.ErrInternal)
	}

	// Step 3: Build line items and create invoice
	lineItems, err := s.buildLineItems(ctx, flexInvoice)
	if err != nil {
		return nil, err
	}

	if len(lineItems) == 0 {
		return nil, ierr.NewError("invoice has no line items").
			WithHint("Cannot create QuickBooks invoice without line items").
			Mark(ierr.ErrValidation)
	}

	invoiceReq := &InvoiceCreateRequest{
		CustomerRef: AccountRef{
			Value: quickBooksCustomerID,
		},
		Line: lineItems,
	}

	// Set due date if available (format: YYYY-MM-DD)
	if flexInvoice.DueDate != nil {
		dueDateStr := flexInvoice.DueDate.Format("2006-01-02")
		invoiceReq.DueDate = &dueDateStr
	}

	quickBooksInvoice, err := s.Client.CreateInvoice(ctx, invoiceReq)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create invoice in QuickBooks").
			Mark(ierr.ErrInternal)
	}

	s.Logger.Infow("created invoice in QuickBooks",
		"invoice_id", req.InvoiceID,
		"quickbooks_invoice_id", quickBooksInvoice.ID)

	// Step 4: Create mapping
	if err := s.createInvoiceMapping(ctx, req.InvoiceID, quickBooksInvoice.ID, flexInvoice.EnvironmentID, flexInvoice.TenantID); err != nil {
		s.Logger.Errorw("failed to create invoice mapping",
			"invoice_id", req.InvoiceID,
			"quickbooks_invoice_id", quickBooksInvoice.ID,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Invoice created in QuickBooks but mapping failed").
			Mark(ierr.ErrDatabase)
	}

	s.Logger.Infow("successfully synced invoice to QuickBooks",
		"invoice_id", req.InvoiceID,
		"quickbooks_invoice_id", quickBooksInvoice.ID)

	return &QuickBooksInvoiceSyncResponse{
		QuickBooksInvoiceID: quickBooksInvoice.ID,
		Total:               quickBooksInvoice.TotalAmt,
		Currency:            flexInvoice.Currency,
	}, nil
}

// buildLineItems converts Flexprice line items to QuickBooks format.
// Each Flexprice line item must have a corresponding QuickBooks item (created from plan prices).
// The method:
// 1. Filters out zero-amount items and items without price IDs
// 2. Looks up QuickBooks item ID from entity_integration_mapping using price_id
// 3. Maps Flexprice line item fields to QuickBooks invoice line item format
// 4. Includes Amount, UnitPrice, Description, and ItemRef as per requirements
func (s *InvoiceService) buildLineItems(ctx context.Context, flexInvoice *invoice.Invoice) ([]InvoiceLineItem, error) {
	lineItems := make([]InvoiceLineItem, 0)

	for _, item := range flexInvoice.LineItems {
		// Skip zero-amount items - QuickBooks doesn't need them
		if item.Amount.IsZero() {
			continue
		}

		// Price ID is required to find the corresponding QuickBooks item
		// Items without price ID cannot be mapped to QuickBooks items
		if item.PriceID == nil || *item.PriceID == "" {
			continue
		}

		// Get QuickBooks item ID from entity mapping
		// EntityType: "price", EntityID: {price_id}, ProviderType: "quickbooks"
		quickBooksItemID, itemName, err := s.getQuickBooksItemID(ctx, *item.PriceID)
		if err != nil {
			// Log error but continue with other line items
			// This allows partial invoice creation if some items can't be mapped
			s.Logger.Errorw("failed to get QuickBooks item ID for line item",
				"invoice_id", flexInvoice.ID,
				"line_item_id", item.ID,
				"price_id", *item.PriceID,
				"error", err)
			continue
		}

		// Send Amount only (no UnitPrice, no Rate) - QuickBooks will display Amount directly
		lineItem := InvoiceLineItem{
			Amount:     item.Amount,
			DetailType: "SalesItemLineDetail",
			SalesItemLineDetail: &SalesItemLineDetail{
				ItemRef: AccountRef{
					Value: quickBooksItemID,
				},
			},
		}

		// Item name is optional but helpful for QuickBooks display
		if itemName != "" {
			lineItem.SalesItemLineDetail.ItemRef.Name = itemName
		}

		// Description maps to Flexprice line item display name
		if item.DisplayName != nil && *item.DisplayName != "" {
			lineItem.Description = *item.DisplayName
		}

		s.Logger.Debugw("built QuickBooks invoice line item",
			"invoice_id", flexInvoice.ID,
			"line_item_id", item.ID,
			"price_id", *item.PriceID,
			"qb_item_id", quickBooksItemID,
			"item_name", itemName,
			"amount", item.Amount.String())

		lineItems = append(lineItems, lineItem)
	}

	return lineItems, nil
}

// getQuickBooksItemID retrieves the QuickBooks item ID from entity mapping.
// Looks up the mapping using price_id as EntityID, "price" as EntityType, and "quickbooks" as ProviderType.
// Returns both the QuickBooks item ID and item name (from metadata) for use in invoice line items.
func (s *InvoiceService) getQuickBooksItemID(ctx context.Context, priceID string) (string, string, error) {
	if priceID == "" {
		return "", "", ierr.NewError("price ID is required").
			WithHint("Line item must have a price ID").
			Mark(ierr.ErrValidation)
	}

	// Query entity mapping table to find QuickBooks item for this Flexprice price
	// EntityID: {price_id}, EntityType: "price", ProviderType: "quickbooks"
	filter := types.NewEntityIntegrationMappingFilter()
	filter.EntityID = priceID
	filter.EntityType = types.IntegrationEntityTypePrice
	filter.ProviderTypes = []string{string(types.SecretProviderQuickBooks)}

	mappings, err := s.EntityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return "", "", ierr.WithError(err).
			WithHint("Failed to get QuickBooks item mapping").
			Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		return "", "", ierr.NewError("QuickBooks item not found for Flexprice price").
			WithHint("Price must be synced to QuickBooks before creating invoice").
			WithReportableDetails(map[string]interface{}{
				"flexprice_price_id": priceID,
			}).
			Mark(ierr.ErrNotFound)
	}

	quickBooksItemID := mappings[0].ProviderEntityID

	// Get item name from metadata - stored during item creation for reference
	// Item name is optional in ItemRef but helpful for QuickBooks display
	itemName := ""
	if mappings[0].Metadata != nil {
		if name, ok := mappings[0].Metadata["quickbooks_item_name"].(string); ok {
			itemName = name
		}
	}

	return quickBooksItemID, itemName, nil
}

// getExistingQuickBooksMapping checks if invoice is already synced to QuickBooks.
// This prevents duplicate invoice creation if sync is triggered multiple times.
// Returns the existing mapping if found, or ErrNotFound if invoice hasn't been synced yet.
func (s *InvoiceService) getExistingQuickBooksMapping(ctx context.Context, invoiceID string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	filter := types.NewEntityIntegrationMappingFilter()
	filter.EntityID = invoiceID
	filter.EntityType = types.IntegrationEntityTypeInvoice
	filter.ProviderTypes = []string{string(types.SecretProviderQuickBooks)}

	mappings, err := s.EntityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to check existing invoice mapping").
			Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		return nil, ierr.NewError("invoice mapping not found").
			Mark(ierr.ErrNotFound)
	}

	return mappings[0], nil
}

// createInvoiceMapping creates an entity integration mapping for the invoice.
// Stores the relationship between Flexprice invoice and QuickBooks invoice.
// Metadata includes synced_at timestamp and flexprice_invoice_id for reference.
func (s *InvoiceService) createInvoiceMapping(ctx context.Context, invoiceID, quickBooksInvoiceID, environmentID, tenantID string) error {
	baseModel := types.GetDefaultBaseModel(ctx)
	baseModel.TenantID = tenantID

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityType:       types.IntegrationEntityTypeInvoice,
		EntityID:         invoiceID,
		ProviderType:     string(types.SecretProviderQuickBooks),
		ProviderEntityID: quickBooksInvoiceID,
		EnvironmentID:    environmentID,
		BaseModel:        baseModel,
		Metadata: map[string]interface{}{
			"synced_at":            time.Now().UTC().Format(time.RFC3339),
			"flexprice_invoice_id": invoiceID,
		},
	}

	err := s.EntityIntegrationMappingRepo.Create(ctx, mapping)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create invoice mapping").
			Mark(ierr.ErrDatabase)
	}

	return nil
}
