package zoho

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type ZohoInvoiceService interface {
	SyncInvoiceToZoho(ctx context.Context, req ZohoInvoiceSyncRequest) (*ZohoInvoiceSyncResponse, error)
}

type InvoiceService struct {
	client       ZohoClient
	customerSvc  ZohoCustomerService
	itemSyncSvc  ZohoItemSyncService
	taxSvc       ZohoTaxService
	customerRepo customer.Repository
	invoiceRepo  invoice.Repository
	mappingRepo  entityintegrationmapping.Repository
	logger       *logger.Logger
}

func NewInvoiceService(
	client ZohoClient,
	customerSvc ZohoCustomerService,
	itemSyncSvc ZohoItemSyncService,
	taxSvc ZohoTaxService,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	mappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) ZohoInvoiceService {
	return &InvoiceService{
		client:       client,
		customerSvc:  customerSvc,
		itemSyncSvc:  itemSyncSvc,
		taxSvc:       taxSvc,
		customerRepo: customerRepo,
		invoiceRepo:  invoiceRepo,
		mappingRepo:  mappingRepo,
		logger:       logger,
	}
}

func (s *InvoiceService) SyncInvoiceToZoho(ctx context.Context, req ZohoInvoiceSyncRequest) (*ZohoInvoiceSyncResponse, error) {
	flexInvoice, err := s.invoiceRepo.Get(ctx, req.InvoiceID)
	if err != nil {
		return nil, err
	}

	filter := types.NewEntityIntegrationMappingFilter()
	filter.EntityType = types.IntegrationEntityTypeInvoice
	filter.EntityID = req.InvoiceID
	filter.ProviderTypes = []string{string(types.SecretProviderZohoBooks)}
	mappings, err := s.mappingRepo.List(ctx, filter)
	if err == nil && len(mappings) > 0 {
		zohoID := mappings[0].ProviderEntityID
		status := "draft"
		if mappings[0].Metadata != nil {
			if s, ok := mappings[0].Metadata["zoho_status"].(string); ok && s != "" {
				status = s
			}
		}
		if werr := s.writeZohoInvoiceMetadata(ctx, flexInvoice, zohoID); werr != nil {
			s.logger.Warnw("failed to update FlexPrice invoice metadata from existing Zoho mapping",
				"error", werr,
				"invoice_id", req.InvoiceID,
				"zoho_invoice_id", zohoID)
		}
		return &ZohoInvoiceSyncResponse{
			ZohoInvoiceID: zohoID,
			Status:        status,
			Total:         flexInvoice.Total,
			Currency:      flexInvoice.Currency,
		}, nil
	}

	flexCustomer, err := s.customerRepo.Get(ctx, flexInvoice.CustomerID)
	if err != nil {
		return nil, err
	}
	zohoCustomerID, err := s.customerSvc.GetOrCreateZohoCustomer(ctx, flexCustomer)
	if err != nil {
		return nil, err
	}

	lineItems, err := s.buildLineItems(ctx, flexInvoice)
	if err != nil {
		return nil, err
	}
	if len(lineItems) == 0 {
		return nil, ierr.NewError("invoice has no syncable line items").Mark(ierr.ErrValidation)
	}

	reqPayload := &InvoiceCreateRequest{
		CustomerID: zohoCustomerID,
		LineItems:  lineItems,
		Notes:      "Synced from FlexPrice",
	}
	if flexInvoice.FinalizedAt != nil {
		reqPayload.Date = flexInvoice.FinalizedAt.Format("2006-01-02")
	} else {
		reqPayload.Date = time.Now().UTC().Format("2006-01-02")
	}
	if flexInvoice.DueDate != nil {
		reqPayload.DueDate = flexInvoice.DueDate.Format("2006-01-02")
	}
	if flexInvoice.InvoiceNumber != nil {
		reqPayload.ReferenceNumber = *flexInvoice.InvoiceNumber
	}

	curCode, exchRate, err := s.client.ResolveInvoiceCurrency(ctx, flexInvoice.Currency)
	if err != nil {
		return nil, err
	}
	reqPayload.CurrencyCode = curCode
	reqPayload.ExchangeRate = exchRate

	zohoInv, err := s.client.CreateInvoice(ctx, reqPayload)
	if err != nil {
		return nil, err
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityType:       types.IntegrationEntityTypeInvoice,
		EntityID:         req.InvoiceID,
		ProviderType:     string(types.SecretProviderZohoBooks),
		ProviderEntityID: zohoInv.InvoiceID,
		EnvironmentID:    flexInvoice.EnvironmentID,
		BaseModel:        types.GetDefaultBaseModel(ctx),
		Metadata: map[string]interface{}{
			"synced_at":         time.Now().UTC().Format(time.RFC3339),
			"zoho_status":       zohoInv.Status,
			"flexprice_invoice": req.InvoiceID,
		},
	}
	mapping.TenantID = flexInvoice.TenantID
	if err := s.mappingRepo.Create(ctx, mapping); err != nil {
		return nil, err
	}

	if werr := s.writeZohoInvoiceMetadata(ctx, flexInvoice, zohoInv.InvoiceID); werr != nil {
		s.logger.Warnw("failed to update FlexPrice invoice metadata from Zoho sync",
			"error", werr,
			"invoice_id", req.InvoiceID,
			"zoho_invoice_id", zohoInv.InvoiceID)
	}

	return &ZohoInvoiceSyncResponse{
		ZohoInvoiceID: zohoInv.InvoiceID,
		Status:        zohoInv.Status,
		Total:         zohoInv.Total,
		Currency:      flexInvoice.Currency,
	}, nil
}

// writeZohoInvoiceMetadata stores the Zoho Books invoice id on the FlexPrice invoice metadata.
func (s *InvoiceService) writeZohoInvoiceMetadata(ctx context.Context, flex *invoice.Invoice, zohoInvoiceID string) error {
	if flex == nil || zohoInvoiceID == "" {
		return nil
	}
	if flex.Metadata == nil {
		flex.Metadata = make(types.Metadata)
	}
	flex.Metadata["zoho_invoice_id"] = zohoInvoiceID
	return s.invoiceRepo.Update(ctx, flex)
}

// buildLineItems converts FlexPrice invoice line items to Zoho line items.
// It bulk-queries Zoho item mappings for all price IDs and creates missing items.
// If item creation fails for any price, that line item is still sent using name+rate.
func (s *InvoiceService) buildLineItems(ctx context.Context, flexInvoice *invoice.Invoice) ([]InvoiceLineItem, error) {
	out := make([]InvoiceLineItem, 0, len(flexInvoice.LineItems))
	inputs := make([]ItemSyncInput, 0, len(flexInvoice.LineItems))

	// Single pass: filter, extract name, build sync inputs
	type lineItemMeta struct {
		name    string
		priceID string
		amount  decimal.Decimal
	}
	metas := make([]lineItemMeta, 0, len(flexInvoice.LineItems))

	for _, li := range flexInvoice.LineItems {
		if li == nil || li.Amount.IsZero() {
			continue
			//todo: check handling
		}
		name := "Charge"
		if li.DisplayName != nil && *li.DisplayName != "" {
			name = *li.DisplayName
		}
		priceID := ""
		if li.PriceID != nil {
			priceID = *li.PriceID
		} else {
			continue
			//todo: check handling
		}
		metas = append(metas, lineItemMeta{name: name, priceID: priceID, amount: li.Amount})
		if priceID != "" {
			inputs = append(inputs, ItemSyncInput{PriceID: priceID, Name: name, Rate: li.Amount})
		}
	}

	// Resolve tax once — shared by item creation and line item fields
	var taxRes *ItemTaxResolution
	if s.taxSvc != nil {
		resolved, taxErr := s.taxSvc.ResolveItemTax(ctx)
		if taxErr != nil {
			s.logger.Warnw("failed to resolve tax for invoice, building without tax info",
				"invoice_id", flexInvoice.ID, "error", taxErr)
		} else {
			taxRes = resolved
		}
	}

	// Bulk resolve Zoho item mappings — pass resolved tax so newly created items are tagged correctly
	priceToItemID := map[string]string{}
	if len(inputs) > 0 && s.itemSyncSvc != nil {
		mapped, err := s.itemSyncSvc.EnsureItemsMapped(ctx, inputs, flexInvoice.EnvironmentID, flexInvoice.TenantID, taxRes)
		if err != nil {
			s.logger.Warnw("failed to ensure Zoho item mappings, sending line items without item_id",
				"invoice_id", flexInvoice.ID,
				"error", err)
			return nil, err
		}
		priceToItemID = mapped
	}

	// Build output — apply the same tax resolution to each line item
	for _, m := range metas {
		li := InvoiceLineItem{
			ItemID:      priceToItemID[m.priceID],
			Name:        m.name,
			Description: m.name,
			Quantity:    decimal.NewFromInt(1),
			Rate:        m.amount,
		}
		if taxRes != nil {
			if taxRes.IsTaxable {
				li.TaxID = taxRes.TaxID
			} else {
				li.TaxExemptionID = taxRes.TaxExemptionID
			}
		}
		out = append(out, li)
	}
	return out, nil
}
