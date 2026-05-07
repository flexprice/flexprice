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
	customerRepo customer.Repository
	invoiceRepo  invoice.Repository
	mappingRepo  entityintegrationmapping.Repository
	logger       *logger.Logger
}

func NewInvoiceService(
	client ZohoClient,
	customerSvc ZohoCustomerService,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	mappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
) ZohoInvoiceService {
	return &InvoiceService{
		client:       client,
		customerSvc:  customerSvc,
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

	lineItems := flexLineItemsToZoho(flexInvoice.LineItems)
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

// flexLineItemsToZoho maps FlexPrice invoice lines to Zoho line_items.
// Like Stripe (amount-only invoice items) and Razorpay (quantity 1 + line total), we send
// quantity 1 and rate = full line Amount so tiered usage and FlexPrice rounding stay exact.
func flexLineItemsToZoho(items []*invoice.InvoiceLineItem) []InvoiceLineItem {
	out := make([]InvoiceLineItem, 0, len(items))
	for _, li := range items {
		if li == nil || li.Amount.IsZero() {
			continue
		}
		name := "Charge"
		if li.DisplayName != nil && *li.DisplayName != "" {
			name = *li.DisplayName
		}
		out = append(out, InvoiceLineItem{
			Name:        name,
			Description: name,
			Quantity:    decimal.NewFromInt(1),
			Rate:        li.Amount,
		})
	}
	return out
}
