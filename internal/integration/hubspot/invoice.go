package hubspot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// InvoiceSyncService handles synchronization of FlexPrice invoices with HubSpot
type InvoiceSyncService struct {
	client                       HubSpotClient
	invoiceRepo                  invoice.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
}

// NewInvoiceSyncService creates a new HubSpot invoice sync service
func NewInvoiceSyncService(
	client HubSpotClient,
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

// SyncInvoiceToHubSpot syncs a FlexPrice invoice to HubSpot following the 6-step flow
func (s *InvoiceSyncService) SyncInvoiceToHubSpot(ctx context.Context, invoiceID string, hubspotContactID string) error {
	// Check if invoice is already synced to avoid duplicates
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:    types.IntegrationEntityTypeInvoice,
		EntityID:      invoiceID,
		ProviderTypes: []string{string(types.SecretProviderHubSpot)},
	}
	existingMappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err == nil && len(existingMappings) > 0 {
		s.logger.Debugw("invoice already synced to HubSpot",
			"invoice_id", invoiceID,
			"hubspot_invoice_id", existingMappings[0].ProviderEntityID)
		return nil
	}

	// Fetch the invoice
	inv, err := s.invoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch invoice").
			Mark(ierr.ErrInternal)
	}

	// Step 1: Create draft invoice with currency and due date
	properties := InvoiceProperties{
		Currency: strings.ToUpper(string(inv.Currency)), // HubSpot requires UPPERCASE (e.g., "USD")
	}

	// Set due date (Unix timestamp in milliseconds)
	if inv.DueDate != nil {
		dueDate := inv.DueDate.UnixMilli()
		properties.DueDate = strconv.FormatInt(dueDate, 10)
	}

	hubspotInvoice, err := s.client.CreateInvoice(ctx, &InvoiceCreateRequest{
		Properties: properties,
	})
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create invoice in HubSpot").
			Mark(ierr.ErrHTTPClient)
	}

	hubspotInvoiceID := hubspotInvoice.ID

	// Step 2 & 3: Create line items and associate them to the invoice
	if err := s.syncLineItems(ctx, inv, hubspotInvoiceID); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to sync line items").
			Mark(ierr.ErrHTTPClient)
	}

	// Step 4: Associate invoice to contact
	if err := s.client.AssociateInvoiceToContact(ctx, hubspotInvoiceID, hubspotContactID); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to associate invoice to contact").
			Mark(ierr.ErrHTTPClient)
	}

	// Step 5: Update invoice properties (dates, amounts, PO number)
	if err := s.updateInvoiceProperties(ctx, inv, hubspotInvoiceID); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update invoice properties").
			Mark(ierr.ErrHTTPClient)
	}

	// Step 6: Set invoice status to "open"
	_, err = s.client.UpdateInvoice(ctx, hubspotInvoiceID, InvoiceProperties{
		Currency:      strings.ToUpper(string(inv.Currency)), // UPPERCASE required
		InvoiceStatus: string(InvoiceStatusOpen),
	})
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to set invoice status to open").
			Mark(ierr.ErrHTTPClient)
	}

	// Create entity integration mapping to track the sync
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityType:       types.IntegrationEntityTypeInvoice,
		EntityID:         invoiceID,
		ProviderType:     string(types.SecretProviderHubSpot),
		ProviderEntityID: hubspotInvoiceID,
		Metadata:         make(map[string]interface{}),
		EnvironmentID:    inv.EnvironmentID,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}

	if err := s.entityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		// If duplicate key (race condition), that's fine - invoice is already tracked
		s.logger.Warnw("failed to create entity integration mapping (may already exist)",
			"error", err,
			"invoice_id", invoiceID,
			"hubspot_invoice_id", hubspotInvoiceID)
		// Don't return error - the invoice was successfully synced to HubSpot
	}

	return nil
}

// syncLineItems creates line items in HubSpot and associates them with the invoice
func (s *InvoiceSyncService) syncLineItems(ctx context.Context, inv *invoice.Invoice, hubspotInvoiceID string) error {
	if len(inv.LineItems) == 0 {
		return nil
	}

	for _, lineItem := range inv.LineItems {
		// Get quantity and amount (HubSpot uses decimal format, not cents)
		quantity := lineItem.Quantity
		amount := lineItem.Amount

		// Calculate price per unit from amount and quantity
		var price decimal.Decimal
		if !quantity.IsZero() {
			price = amount.Div(quantity)
		} else {
			price = amount
		}

		// Get description from DisplayName or fallback
		description := "Line Item"
		if lineItem.DisplayName != nil && *lineItem.DisplayName != "" {
			description = *lineItem.DisplayName
		}

		// Create line item in HubSpot
		hubspotLineItem, err := s.client.CreateLineItem(ctx, &LineItemCreateRequest{
			Properties: LineItemProperties{
				Name:        description,
				Quantity:    quantity.String(),
				Price:       price.String(),  // Decimal format (e.g., "10.00")
				Amount:      amount.String(), // Decimal format (e.g., "10.00")
				Description: description,
			},
		})
		if err != nil {
			return ierr.WithError(err).
				WithHint(fmt.Sprintf("Failed to create line item: %s", description)).
				Mark(ierr.ErrHTTPClient)
		}

		// Associate line item to invoice
		if err := s.client.AssociateLineItemToInvoice(ctx, hubspotLineItem.ID, hubspotInvoiceID); err != nil {
			return ierr.WithError(err).
				WithHint(fmt.Sprintf("Failed to associate line item %s to invoice", hubspotLineItem.ID)).
				Mark(ierr.ErrHTTPClient)
		}
	}

	return nil
}

// updateInvoiceProperties updates invoice properties like amounts and PO number
func (s *InvoiceSyncService) updateInvoiceProperties(ctx context.Context, inv *invoice.Invoice, hubspotInvoiceID string) error {
	properties := InvoiceProperties{
		Currency: strings.ToUpper(string(inv.Currency)), // HubSpot requires UPPERCASE
	}

	// Set amount billed (total amount as decimal, e.g., "10.00")
	if !inv.Total.IsZero() {
		properties.AmountBilled = inv.Total.String()
	}

	// Note: hs_balance_due is READ-ONLY and calculated by HubSpot automatically - don't set it

	// Set tax amount if applicable (as decimal)
	if !inv.TotalTax.IsZero() {
		properties.Tax = inv.TotalTax.String()
	}

	// Set invoice number using PO number field (hs_invoice_number doesn't exist)
	if inv.InvoiceNumber != nil && *inv.InvoiceNumber != "" {
		properties.PurchaseOrderNumber = *inv.InvoiceNumber
	}

	_, err := s.client.UpdateInvoice(ctx, hubspotInvoiceID, properties)
	return err
}

// GetHubSpotContactID retrieves the HubSpot contact ID for a FlexPrice customer
func (s *InvoiceSyncService) GetHubSpotContactID(ctx context.Context, customerID string) (string, error) {
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:    types.IntegrationEntityTypeCustomer,
		EntityID:      customerID,
		ProviderTypes: []string{string(types.SecretProviderHubSpot)},
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Customer not synced to HubSpot").
			Mark(ierr.ErrNotFound)
	}

	if len(mappings) == 0 || mappings[0].ProviderEntityID == "" {
		return "", ierr.NewError("HubSpot contact ID not found for customer").
			WithHint("Customer mapping not found or external ID is empty").
			Mark(ierr.ErrNotFound)
	}

	return mappings[0].ProviderEntityID, nil
}
