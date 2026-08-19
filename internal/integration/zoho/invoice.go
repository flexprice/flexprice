package zoho

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const zohoDiscountTypeItemLevel = "item_level"

type ZohoInvoiceService interface {
	SyncInvoiceToZoho(ctx context.Context, req ZohoInvoiceSyncRequest) (*ZohoInvoiceSyncResponse, error)
	// MarkInvoicePaidInZoho records a Zoho customer payment for the invoice's current
	// outstanding balance, bringing it to Paid. No-op if the invoice was never synced to
	// Zoho, or if Zoho's balance is already zero.
	MarkInvoicePaidInZoho(ctx context.Context, flexpriceInvoiceID string) error
}

type InvoiceService struct {
	client       ZohoClient
	customerSvc  ZohoCustomerService
	itemSyncSvc  ZohoItemSyncService
	taxSvc       ZohoTaxService
	customerRepo customer.Repository
	invoiceRepo  invoice.Repository
	priceRepo    price.Repository
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
	priceRepo price.Repository,
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
		priceRepo:    priceRepo,
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
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	mappings, err := s.mappingRepo.List(ctx, filter)
	if err == nil && len(mappings) > 0 {
		zohoID := mappings[0].ProviderEntityID
		status := "draft"
		zohoInvNumber := ""
		if mappings[0].Metadata != nil {
			if s, ok := mappings[0].Metadata["zoho_status"].(string); ok && s != "" {
				status = s
			}
			if invNumber, ok := mappings[0].Metadata["zoho_invoice_number"].(string); ok {
				zohoInvNumber = invNumber
			}
		}
		if werr := s.writeZohoInvoiceMetadata(ctx, flexInvoice, zohoID, zohoInvNumber); werr != nil {
			s.logger.Info(ctx, "failed to update FlexPrice invoice metadata from existing Zoho mapping",
				"error", werr,
				"invoice_id", req.InvoiceID,
				"zoho_invoice_id", zohoID)
		}

		if err := s.markPaidIfFlexpricePaid(ctx, req.InvoiceID); err != nil {
			return nil, err
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
		Adjustment: flexInvoice.TotalPrepaidCreditsApplied.Mul(decimal.NewFromInt(-1)),
	}

	if s.totalLineItemDiscount(lineItems).IsPositive() {
		reqPayload.DiscountType = zohoDiscountTypeItemLevel
		reqPayload.IsDiscountBeforeTax = true
	}

	if flexInvoice.FinalizedAt != nil {
		reqPayload.Date = flexInvoice.FinalizedAt.Format("2006-01-02")
	} else {
		reqPayload.Date = time.Now().UTC().Format("2006-01-02")
	}

	settings, err := s.getInvoiceSyncSettings(ctx)
	if err != nil {
		return nil, err
	}

	reqPayload.PlaceOfSupply = types.TaxMetadataFromMap(flexCustomer.Metadata).PlaceOfSupply()
	reqPayload.CustomFields = servicePeriodCustomFields(settings, flexInvoice.PeriodStart, flexInvoice.PeriodEnd)

	curCode, exchRate, err := s.client.ResolveInvoiceCurrency(ctx, flexInvoice.Currency)
	if err != nil {
		return nil, err
	}
	s.logger.Info(ctx, "resolved Zoho invoice currency for sync",
		"invoice_id", req.InvoiceID,
		"flexprice_currency", flexInvoice.Currency,
		"zoho_currency_code", curCode,
		"zoho_exchange_rate", exchRate)
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
			"synced_at":           time.Now().UTC().Format(time.RFC3339),
			"zoho_status":         zohoInv.Status,
			"flexprice_invoice":   req.InvoiceID,
			"zoho_invoice_number": zohoInv.InvoiceNumber,
		},
	}
	mapping.TenantID = flexInvoice.TenantID
	if err := s.mappingRepo.Create(ctx, mapping); err != nil {
		return nil, err
	}

	if werr := s.writeZohoInvoiceMetadata(ctx, flexInvoice, zohoInv.InvoiceID, zohoInv.InvoiceNumber); werr != nil {
		s.logger.Info(ctx, "failed to update FlexPrice invoice metadata from Zoho sync",
			"error", werr,
			"invoice_id", req.InvoiceID,
			"zoho_invoice_id", zohoInv.InvoiceID)
	}

	if err := s.markPaidIfFlexpricePaid(ctx, req.InvoiceID); err != nil {
		return nil, err
	}

	return &ZohoInvoiceSyncResponse{
		ZohoInvoiceID: zohoInv.InvoiceID,
		Status:        zohoInv.Status,
		Total:         zohoInv.Total,
		Currency:      flexInvoice.Currency,
	}, nil
}

// markPaidIfFlexpricePaid re-reads the FlexPrice invoice and records a Zoho customer
// payment when it is already succeeded or overpaid. Re-fetch is required because
// checkout can mark the invoice paid while CreateInvoice is still in flight.
func (s *InvoiceService) markPaidIfFlexpricePaid(ctx context.Context, flexpriceInvoiceID string) error {
	flex, err := s.invoiceRepo.Get(ctx, flexpriceInvoiceID)
	if err != nil {
		return err
	}
	if flex == nil {
		return nil
	}

	switch flex.PaymentStatus {
	case types.PaymentStatusSucceeded, types.PaymentStatusOverpaid:
		return s.MarkInvoicePaidInZoho(ctx, flex.ID)
	default:
		return nil
	}
}

// MarkInvoicePaidInZoho records a Zoho customer payment for the invoice's current
// outstanding balance, bringing it to Paid.
//
// Zoho's invoice total is intentionally tax-inclusive (items are synced as taxable —
// see buildLineItems/EnsureItemsMapped) while FlexPrice's invoice.Total is not, so the
// two totals routinely differ. Rather than track how much FlexPrice has separately
// pushed to Zoho, this reads Zoho's own live balance and pays it off in full: a zero
// balance means Zoho already considers the invoice settled (whether from a prior call
// here, or from the existing inbound "Zoho invoice paid" webhook), so this is a no-op.
func (s *InvoiceService) MarkInvoicePaidInZoho(ctx context.Context, flexpriceInvoiceID string) error {
	filter := types.NewEntityIntegrationMappingFilter()
	filter.EntityType = types.IntegrationEntityTypeInvoice
	filter.EntityID = flexpriceInvoiceID
	filter.ProviderTypes = []string{string(types.SecretProviderZohoBooks)}
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	mappings, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return err
	}
	if len(mappings) == 0 {
		s.logger.Info(ctx, "no Zoho mapping for invoice, skipping mark-paid",
			"invoice_id", flexpriceInvoiceID)
		return nil
	}
	zohoInvoiceID := mappings[0].ProviderEntityID

	zohoInv, err := s.client.GetInvoice(ctx, zohoInvoiceID)
	if err != nil {
		return err
	}
	if zohoInv == nil || !zohoInv.Balance.IsPositive() {
		s.logger.Info(ctx, "Zoho invoice already has zero balance, skipping mark-paid",
			"invoice_id", flexpriceInvoiceID,
			"zoho_invoice_id", zohoInvoiceID)
		return nil
	}

	// Zoho's total is tax-inclusive while Flexprice's is tax-exclusive, so log both sides of the
	// amount being settled to explain any mismatch between the two systems' figures.
	s.logger.Info(ctx, "recording Zoho customer payment for synced invoice",
		"invoice_id", flexpriceInvoiceID,
		"zoho_invoice_id", zohoInvoiceID,
		"zoho_customer_id", zohoInv.CustomerID,
		"zoho_balance", zohoInv.Balance.String(),
	)

	_, err = s.client.CreateCustomerPayment(ctx, NewCustomerPaymentCreateRequest(
		zohoInv.CustomerID,
		"others",
		zohoInv.Balance,
		time.Now().UTC().Format("2006-01-02"),
		[]CustomerPaymentInvoiceApply{
			NewCustomerPaymentInvoiceApply(zohoInvoiceID, zohoInv.Balance),
		},
	))
	if err != nil {
		return err
	}

	s.logger.Info(ctx, "marked Zoho invoice as paid",
		"invoice_id", flexpriceInvoiceID,
		"zoho_invoice_id", zohoInvoiceID,
		"amount", zohoInv.Balance.String())
	return nil
}

// writeZohoInvoiceMetadata stores the Zoho Books invoice id on the FlexPrice invoice metadata.
func (s *InvoiceService) writeZohoInvoiceMetadata(ctx context.Context, flex *invoice.Invoice, zohoInvoiceID, zohoInvoiceNumber string) error {
	if flex == nil || zohoInvoiceID == "" {
		return nil
	}
	if zohoInvoiceNumber != "" {
		flex.InvoiceNumber = lo.ToPtr(zohoInvoiceNumber)
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
	inputs := make([]ItemSyncInput, 0, len(flexInvoice.LineItems))
	childCustomerIDs := make([]string, 0)
	lineItemIDToChildCustomer := make(map[string]string)
	for _, li := range flexInvoice.LineItems {
		// skip line items that are not syncable
		if li == nil || li.Amount.IsZero() || li.PriceID == nil {
			continue
		}

		inputs = append(inputs, ItemSyncInput{
			PriceID: lo.FromPtr(li.PriceID),
			Name:    lo.FromPtrOr(li.DisplayName, "Charge"),
		})
		if customerID, ok := li.Metadata[types.InvoiceLineItemMetadataKeyChildCustomerID]; ok {
			lineItemIDToChildCustomer[li.ID] = customerID
			childCustomerIDs = append(childCustomerIDs, customerID)
		}
	}
	childCustomerIDToName := make(map[string]string)
	if len(childCustomerIDs) > 0 {
		custFilter := types.NewNoLimitCustomerFilter()
		custFilter.CustomerIDs = childCustomerIDs
		custFilter.Status = lo.ToPtr(types.StatusPublished)
		customers, custErr := s.customerRepo.List(ctx, custFilter)
		if custErr != nil {
			return nil, custErr
		}
		for _, c := range customers {
			childCustomerIDToName[c.ID] = c.Name
		}
	}
	taxRes, err := s.taxSvc.ResolveItemTax(ctx)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("failed to resolve tax for invoice").Mark(ierr.ErrInternal)
	}
	s.logger.Info(ctx, "resolved Zoho tax for invoice sync",
		"invoice_id", flexInvoice.ID,
		"is_taxable", taxRes.IsTaxable,
		"tax_id", taxRes.TaxID,
		"tax_exemption_id", taxRes.TaxExemptionID)

	hsnByPriceID := s.resolveHSNSAC(ctx, inputs)
	for i := range inputs {
		inputs[i].HSNOrSAC = hsnByPriceID[inputs[i].PriceID]
	}

	priceToItemID := map[string]string{}
	if len(inputs) > 0 {
		mapped, err := s.itemSyncSvc.EnsureItemsMapped(ctx, inputs, taxRes)
		if err != nil {
			s.logger.Error(ctx, "failed to ensure Zoho item mappings, sending line items without item_id",
				"invoice_id", flexInvoice.ID,
				"error", err)
			return nil, err
		}
		priceToItemID = mapped
	}

	settings, err := s.getInvoiceSyncSettings(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]InvoiceLineItem, 0, len(inputs))
	for _, li := range flexInvoice.LineItems {
		if li == nil || li.Amount.IsZero() {
			continue
		}
		qty, rate := s.normalizeRateAndQuantity(li, settings, flexInvoice.BillingPeriod)
		name := lo.FromPtrOr(li.DisplayName, "Charge")
		childName := childCustomerIDToName[lineItemIDToChildCustomer[li.ID]]
		out = append(out, InvoiceLineItem{
			Name:        name,
			Description: formatPeriodDescription(childName, li.PeriodStart, li.PeriodEnd),
			Quantity:    qty,
			Rate:        rate,
			Discount:    li.LineItemDiscount.Add(li.InvoiceLevelDiscount),
			ItemID:      priceToItemID[lo.FromPtr(li.PriceID)],
			HSNOrSAC:    hsnByPriceID[lo.FromPtr(li.PriceID)],
			//TaxID:          taxRes.TaxID,
			//TaxExemptionID: taxRes.TaxExemptionID,
		})
	}
	return out, nil
}

func (s *InvoiceService) totalLineItemDiscount(lineItems []InvoiceLineItem) decimal.Decimal {
	total := decimal.Zero
	for _, li := range lineItems {
		total = total.Add(li.Discount)
	}

	return total
}

func (s *InvoiceService) getInvoiceSyncSettings(ctx context.Context) (*types.InvoiceSyncSettings, error) {
	syncConfig, err := s.client.GetZohoBooksSyncConfig(ctx)
	if err != nil {
		return nil, err
	}
	if syncConfig != nil && syncConfig.InvoiceSyncSettings != nil {
		return syncConfig.InvoiceSyncSettings, nil
	}
	return nil, nil
}

func (s *InvoiceService) normalizeRateAndQuantity(li *invoice.InvoiceLineItem, settings *types.InvoiceSyncSettings, billingPeriod *string) (qty decimal.Decimal, rate decimal.Decimal) {
	priceType := lo.FromPtr(li.PriceType)

	if priceType == string(types.PRICE_TYPE_FIXED) && settings != nil {
		if n := settings.NormalizedFixedQuantity(billingPeriod); n > 0 {
			dQty := decimal.NewFromInt(int64(n))
			return dQty, li.Amount.Div(dQty)
		}
	}

	return decimal.NewFromInt(1), li.Amount

}

const zohoAPIDateFormat = "2006-01-02"

func formatPeriodDescription(fallback string, start, end *time.Time) string {
	if start == nil || end == nil {
		return fallback
	}
	if fallback != "" {
		return fmt.Sprintf("%s\n(%s - %s)", fallback, start.Format("2006-01-02"), inclusiveEnd(end).Format("2006-01-02"))
	}
	return fmt.Sprintf("(%s - %s)", start.Format("2006-01-02"), inclusiveEnd(end).Format("2006-01-02"))
}

// inclusiveEnd converts FlexPrice's exclusive period end into the inclusive last
// day a tax invoice displays: a period ending 2026-05-01T00:00 reads as 30/04/2026.
func inclusiveEnd(end *time.Time) time.Time {
	return end.Add(-time.Nanosecond)
}

func servicePeriodCustomFields(settings *types.InvoiceSyncSettings, start, end *time.Time) []CustomField {
	if settings == nil || !settings.ServicePeriodCustomFields.IsConfigured() {
		return nil
	}
	if start == nil || end == nil {
		return nil
	}

	return []CustomField{
		NewCustomField(settings.ServicePeriodCustomFields.StartFieldID, start.Format(zohoAPIDateFormat)),
		NewCustomField(settings.ServicePeriodCustomFields.EndFieldID, inclusiveEnd(end).Format(zohoAPIDateFormat)),
	}
}

func (s *InvoiceService) resolveHSNSAC(ctx context.Context, inputs []ItemSyncInput) map[string]string {
	out := make(map[string]string, len(inputs))
	if len(inputs) == 0 {
		return out
	}

	priceIDs := lo.Uniq(lo.Map(inputs, func(in ItemSyncInput, _ int) string { return in.PriceID }))
	priceIDs = lo.Compact(priceIDs)
	if len(priceIDs) == 0 || s.priceRepo == nil {
		return out
	}

	prices, err := s.priceRepo.ListAll(ctx, types.NewNoLimitPriceFilter().WithPriceIDs(priceIDs))
	if err != nil {
		s.logger.Error(ctx, "failed to load prices for HSN/SAC resolution, omitting the code",
			"price_count", len(priceIDs),
			"error", err)
		return out
	}

	pricesByID := lo.SliceToMap(prices, func(p *price.Price) (string, *price.Price) { return p.ID, p })

	// Pull in root plan prices for any override that does not carry its own code.
	rootIDs := make([]string, 0)
	for _, p := range prices {
		if p.HSNSAC() != "" {
			continue
		}
		if rootID := p.GetRootPriceID(); rootID != "" && rootID != p.ID {
			if _, ok := pricesByID[rootID]; !ok {
				rootIDs = append(rootIDs, rootID)
			}
		}
	}

	if len(rootIDs) > 0 {
		roots, err := s.priceRepo.ListAll(ctx, types.NewNoLimitPriceFilter().WithPriceIDs(lo.Uniq(rootIDs)))
		if err != nil {
			s.logger.Error(ctx, "failed to load root prices for HSN/SAC resolution",
				"root_price_count", len(rootIDs),
				"error", err)
		}
		for _, p := range roots {
			pricesByID[p.ID] = p
		}
	}

	for _, in := range inputs {
		out[in.PriceID] = price.ResolveHSNSAC(pricesByID[in.PriceID], pricesByID)
	}
	return out
}
