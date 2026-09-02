package zoho

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSyncZohoClient captures the InvoiceCreateRequest that SyncInvoiceToZoho builds.
type fakeSyncZohoClient struct {
	ZohoClient
	createInvoiceReq   *InvoiceCreateRequest
	syncConfig         *types.SyncConfig
	getInvoiceResp     *InvoiceResponse
	createPaymentCalls int
	createPaymentReq   *CustomerPaymentCreateRequest
}

func (f *fakeSyncZohoClient) CreateInvoice(_ context.Context, req *InvoiceCreateRequest) (*InvoiceResponse, error) {
	f.createInvoiceReq = req
	return &InvoiceResponse{InvoiceID: "zoho_inv_new", InvoiceNumber: "INV-000058", Status: "draft"}, nil
}

func (f *fakeSyncZohoClient) GetInvoice(_ context.Context, _ string) (*InvoiceResponse, error) {
	if f.getInvoiceResp != nil {
		return f.getInvoiceResp, nil
	}
	return &InvoiceResponse{
		InvoiceID:  "zoho_inv_new",
		CustomerID: "zoho_cust_1",
		Balance:    decimal.NewFromInt(100),
	}, nil
}

func (f *fakeSyncZohoClient) CreateCustomerPayment(_ context.Context, req *CustomerPaymentCreateRequest) (*CustomerPaymentResponse, error) {
	f.createPaymentCalls++
	f.createPaymentReq = req
	return NewCustomerPaymentResponse("zoho_payment_1"), nil
}

func (f *fakeSyncZohoClient) ResolveInvoiceCurrency(_ context.Context, invoiceCurrency string) (string, float64, error) {
	return invoiceCurrency, 1, nil
}

func (f *fakeSyncZohoClient) GetZohoBooksSyncConfig(_ context.Context) (*types.SyncConfig, error) {
	return f.syncConfig, nil
}

// fakeSyncMappingRepo stores mappings so MarkInvoicePaidInZoho can see a row
// created earlier in the same SyncInvoiceToZoho call.
type fakeSyncMappingRepo struct {
	entityintegrationmapping.Repository
	mappings []*entityintegrationmapping.EntityIntegrationMapping
	created  *entityintegrationmapping.EntityIntegrationMapping
}

func (f *fakeSyncMappingRepo) List(_ context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	var out []*entityintegrationmapping.EntityIntegrationMapping
	for _, m := range f.mappings {
		if filter != nil && filter.EntityID != "" && m.EntityID != filter.EntityID {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeSyncMappingRepo) Create(_ context.Context, m *entityintegrationmapping.EntityIntegrationMapping) error {
	f.created = m
	f.mappings = append(f.mappings, m)
	return nil
}

type fakeSyncCustomerRepo struct {
	customer.Repository
}

func (f *fakeSyncCustomerRepo) Get(_ context.Context, id string) (*customer.Customer, error) {
	return &customer.Customer{ID: id, Name: "Gobblecube"}, nil
}

func (f *fakeSyncCustomerRepo) List(_ context.Context, _ *types.CustomerFilter) ([]*customer.Customer, error) {
	return nil, nil
}

type fakeSyncInvoiceRepo struct {
	invoice.Repository
	inv *invoice.Invoice
}

func (f *fakeSyncInvoiceRepo) Get(_ context.Context, _ string) (*invoice.Invoice, error) {
	return f.inv, nil
}

func (f *fakeSyncInvoiceRepo) Update(_ context.Context, _ *invoice.Invoice) error { return nil }

type fakeSyncCustomerSvc struct{}

func (fakeSyncCustomerSvc) GetOrCreateZohoCustomer(_ context.Context, _ *customer.Customer) (string, error) {
	return "zoho_cust_1", nil
}

func (fakeSyncCustomerSvc) SyncCustomerUpdate(_ context.Context, _ *customer.Customer) error {
	return nil
}

type fakeSyncItemSyncSvc struct{}

func (fakeSyncItemSyncSvc) EnsureItemsMapped(_ context.Context, inputs []ItemSyncInput, _ *ItemTaxResolution) (map[string]string, error) {
	out := make(map[string]string, len(inputs))
	for _, in := range inputs {
		out[in.PriceID] = "zoho_item_" + in.PriceID
	}
	return out, nil
}

type fakeSyncTaxSvc struct {
	ZohoTaxService
}

func (fakeSyncTaxSvc) ResolveItemTax(_ context.Context) (*ItemTaxResolution, error) {
	return &ItemTaxResolution{}, nil
}

// dec parses a decimal for these tests, treating "" as zero so table entries can omit fields.
func dec(s string) decimal.Decimal {
	if s == "" {
		return decimal.Zero
	}
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return v
}

// testLineItem describes one FlexPrice invoice line item for these tests.
type testLineItem struct {
	name      string
	priceID   string
	amount    string
	lineDisc  string
	invDisc   string
	priceType string
}

func buildTestInvoice(currency string, totalDiscount string, prepaidCredits string, billingPeriod string, items []testLineItem) *invoice.Invoice {
	lineItems := make([]*invoice.InvoiceLineItem, 0, len(items))
	for _, it := range items {
		li := &invoice.InvoiceLineItem{
			ID:                   "li_" + it.name,
			DisplayName:          lo.ToPtr(it.name),
			PriceID:              lo.ToPtr(it.priceID),
			Amount:               dec(it.amount),
			Quantity:             decimal.NewFromInt(1),
			Currency:             currency,
			LineItemDiscount:     dec(it.lineDisc),
			InvoiceLevelDiscount: dec(it.invDisc),
		}
		if it.priceType != "" {
			li.PriceType = lo.ToPtr(it.priceType)
		}
		lineItems = append(lineItems, li)
	}

	inv := &invoice.Invoice{
		ID:                         "inv_1",
		CustomerID:                 "cust_1",
		Currency:                   currency,
		TotalDiscount:              dec(totalDiscount),
		TotalPrepaidCreditsApplied: dec(prepaidCredits),
		LineItems:                  lineItems,
	}
	if billingPeriod != "" {
		inv.BillingPeriod = lo.ToPtr(billingPeriod)
	}
	return inv
}

func newSyncTestService(inv *invoice.Invoice, syncConfig *types.SyncConfig) (*InvoiceService, *fakeSyncZohoClient) {
	client := &fakeSyncZohoClient{syncConfig: syncConfig}
	svc := &InvoiceService{
		client:       client,
		customerSvc:  fakeSyncCustomerSvc{},
		itemSyncSvc:  fakeSyncItemSyncSvc{},
		taxSvc:       fakeSyncTaxSvc{},
		customerRepo: &fakeSyncCustomerRepo{},
		invoiceRepo:  &fakeSyncInvoiceRepo{inv: inv},
		mappingRepo:  &fakeSyncMappingRepo{},
		logger:       logger.NewNoopLogger(),
	}
	return svc, client
}

func TestSyncInvoiceToZoho_Discounts(t *testing.T) {
	tests := []struct {
		name string
		// invoice under test
		currency       string
		totalDiscount  string
		prepaidCredits string
		items          []testLineItem
		// expectations
		wantLineDiscounts       []string
		wantDiscountType        string
		wantIsDiscountBeforeTax bool
		wantAdjustment          string
	}{
		{
			// INV-000058: subtotal 6000, invoice-level coupon 1200 distributed 600/400/200.
			// Zoho must tax 4800, not 6000.
			name:          "invoice level discount distributed across lines",
			currency:      "INR",
			totalDiscount: "1200",
			items: []testLineItem{
				{name: "Blinkit - Workspace 1", priceID: "price_1", amount: "3000", lineDisc: "0", invDisc: "600"},
				{name: "Zepto - Workspace 1", priceID: "price_2", amount: "2000", lineDisc: "0", invDisc: "400"},
				{name: "Blinkit - Workspace 2", priceID: "price_3", amount: "1000", lineDisc: "0", invDisc: "200"},
			},
			wantLineDiscounts:       []string{"600", "400", "200"},
			wantDiscountType:        "item_level",
			wantIsDiscountBeforeTax: true,
			wantAdjustment:          "0",
		},
		{
			name:          "no discount leaves payload untouched",
			currency:      "INR",
			totalDiscount: "0",
			items: []testLineItem{
				{name: "Blinkit - Workspace 1", priceID: "price_1", amount: "3000", lineDisc: "0", invDisc: "0"},
			},
			wantLineDiscounts:       []string{"0"},
			wantDiscountType:        "",
			wantIsDiscountBeforeTax: false,
			wantAdjustment:          "0",
		},
		{
			name:          "line item coupon targets a single line",
			currency:      "INR",
			totalDiscount: "500",
			items: []testLineItem{
				{name: "Blinkit - Workspace 1", priceID: "price_1", amount: "3000", lineDisc: "500", invDisc: "0"},
				{name: "Zepto - Workspace 1", priceID: "price_2", amount: "2000", lineDisc: "0", invDisc: "0"},
			},
			wantLineDiscounts:       []string{"500", "0"},
			wantDiscountType:        "item_level",
			wantIsDiscountBeforeTax: true,
			wantAdjustment:          "0",
		},
		{
			name:          "line and invoice level discounts sum onto one line",
			currency:      "INR",
			totalDiscount: "800",
			items: []testLineItem{
				{name: "Blinkit - Workspace 1", priceID: "price_1", amount: "3000", lineDisc: "500", invDisc: "300"},
			},
			wantLineDiscounts:       []string{"800"},
			wantDiscountType:        "item_level",
			wantIsDiscountBeforeTax: true,
			wantAdjustment:          "0",
		},
		{
			name:           "prepaid credits stay in adjustment, discounts stay on lines",
			currency:       "INR",
			totalDiscount:  "600",
			prepaidCredits: "250",
			items: []testLineItem{
				{name: "Blinkit - Workspace 1", priceID: "price_1", amount: "3000", lineDisc: "0", invDisc: "600"},
			},
			wantLineDiscounts:       []string{"600"},
			wantDiscountType:        "item_level",
			wantIsDiscountBeforeTax: true,
			wantAdjustment:          "-250",
		},
		{
			// The coupon engine already rounds both columns to currency precision, so the
			// mapper passes them through as stored rather than rounding again.
			name:          "discount passes through at stored precision",
			currency:      "INR",
			totalDiscount: "600.004",
			items: []testLineItem{
				{name: "Blinkit - Workspace 1", priceID: "price_1", amount: "3000", lineDisc: "600.004", invDisc: "0"},
			},
			wantLineDiscounts:       []string{"600.004"},
			wantDiscountType:        "item_level",
			wantIsDiscountBeforeTax: true,
			wantAdjustment:          "0",
		},
		{
			// Upstream guarantees LineItemDiscount + InvoiceLevelDiscount <= Amount, so this
			// input is unreachable in practice; the mapper does not clamp it.
			name:          "discount is not clamped to the line amount",
			currency:      "INR",
			totalDiscount: "1400",
			items: []testLineItem{
				{name: "Blinkit - Workspace 1", priceID: "price_1", amount: "1000", lineDisc: "900", invDisc: "500"},
			},
			wantLineDiscounts:       []string{"1400"},
			wantDiscountType:        "item_level",
			wantIsDiscountBeforeTax: true,
			wantAdjustment:          "0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inv := buildTestInvoice(tc.currency, tc.totalDiscount, tc.prepaidCredits, "", tc.items)
			svc, client := newSyncTestService(inv, nil)

			_, err := svc.SyncInvoiceToZoho(context.Background(), ZohoInvoiceSyncRequest{InvoiceID: "inv_1"})
			require.NoError(t, err)

			req := client.createInvoiceReq
			require.NotNil(t, req)
			require.Len(t, req.LineItems, len(tc.wantLineDiscounts))

			for i, want := range tc.wantLineDiscounts {
				assert.Truef(t, dec(want).Equal(req.LineItems[i].Discount),
					"line %d discount = %s, want %s", i, req.LineItems[i].Discount, want)
			}

			assert.Equal(t, tc.wantDiscountType, req.DiscountType)
			assert.Equal(t, tc.wantIsDiscountBeforeTax, req.IsDiscountBeforeTax)
			assert.Truef(t, dec(tc.wantAdjustment).Equal(req.Adjustment),
				"adjustment = %s, want %s", req.Adjustment, tc.wantAdjustment)
		})
	}
}

// A fixed charge split into N units must keep the discount on the line total, not divide it.
func TestSyncInvoiceToZoho_NormalizedQuantityKeepsLineTotalDiscount(t *testing.T) {
	inv := buildTestInvoice("INR", "600", "", string(types.BILLING_PERIOD_QUARTER), []testLineItem{
		{
			name:      "Blinkit - Workspace 1",
			priceID:   "price_1",
			amount:    "3000",
			lineDisc:  "0",
			invDisc:   "600",
			priceType: string(types.PRICE_TYPE_FIXED),
		},
	})
	syncConfig := &types.SyncConfig{
		InvoiceSyncSettings: &types.InvoiceSyncSettings{NormalizeFixedTo: types.BILLING_PERIOD_MONTHLY},
	}
	svc, client := newSyncTestService(inv, syncConfig)

	_, err := svc.SyncInvoiceToZoho(context.Background(), ZohoInvoiceSyncRequest{InvoiceID: "inv_1"})
	require.NoError(t, err)

	req := client.createInvoiceReq
	require.NotNil(t, req)
	require.Len(t, req.LineItems, 1)

	li := req.LineItems[0]
	assert.True(t, decimal.NewFromInt(3).Equal(li.Quantity), "quantity = %s, want 3", li.Quantity)
	assert.True(t, decimal.NewFromInt(1000).Equal(li.Rate), "rate = %s, want 1000", li.Rate)
	// Zoho applies item-level discount to rate × quantity, so it must stay the full 600.
	assert.True(t, decimal.NewFromInt(600).Equal(li.Discount), "discount = %s, want 600", li.Discount)
}

// Zero-amount line items are skipped, and they can never carry a discount.
func TestSyncInvoiceToZoho_SkipsZeroAmountLines(t *testing.T) {
	inv := buildTestInvoice("INR", "600", "", "", []testLineItem{
		{name: "Blinkit - Workspace 1", priceID: "price_1", amount: "3000", lineDisc: "0", invDisc: "600"},
		{name: "Zero Charge", priceID: "price_2", amount: "0", lineDisc: "0", invDisc: "0"},
	})
	svc, client := newSyncTestService(inv, nil)

	_, err := svc.SyncInvoiceToZoho(context.Background(), ZohoInvoiceSyncRequest{InvoiceID: "inv_1"})
	require.NoError(t, err)

	req := client.createInvoiceReq
	require.NotNil(t, req)
	require.Len(t, req.LineItems, 1)
	assert.Equal(t, "Blinkit - Workspace 1", req.LineItems[0].Name)
	assert.True(t, decimal.NewFromInt(600).Equal(req.LineItems[0].Discount))
}

func TestTotalLineItemDiscount(t *testing.T) {
	svc := &InvoiceService{}

	assert.True(t, decimal.Zero.Equal(svc.totalLineItemDiscount(nil)))
	assert.True(t, decimal.NewFromInt(1200).Equal(svc.totalLineItemDiscount([]InvoiceLineItem{
		{Discount: decimal.NewFromInt(600)},
		{Discount: decimal.NewFromInt(400)},
		{Discount: decimal.NewFromInt(200)},
	})))
}

func TestSyncInvoiceToZoho_MarksPaidWhenFlexpriceAlreadyPaid(t *testing.T) {
	paidLine := []testLineItem{
		{name: "Charge", priceID: "price_1", amount: "100", lineDisc: "0", invDisc: "0"},
	}

	tests := []struct {
		name              string
		paymentStatus     types.PaymentStatus
		existingMapping   bool
		wantCreateInvoice bool
		wantCreatePayment bool
	}{
		{
			name:              "pending first sync does not record payment",
			paymentStatus:     types.PaymentStatusPending,
			wantCreateInvoice: true,
		},
		{
			name:              "succeeded first sync records payment after create",
			paymentStatus:     types.PaymentStatusSucceeded,
			wantCreateInvoice: true,
			wantCreatePayment: true,
		},
		{
			name:              "overpaid first sync records payment after create",
			paymentStatus:     types.PaymentStatusOverpaid,
			wantCreateInvoice: true,
			wantCreatePayment: true,
		},
		{
			name:              "succeeded existing mapping records payment without create",
			paymentStatus:     types.PaymentStatusSucceeded,
			existingMapping:   true,
			wantCreatePayment: true,
		},
		{
			name:            "pending existing mapping does not record payment",
			paymentStatus:   types.PaymentStatusPending,
			existingMapping: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inv := buildTestInvoice("INR", "0", "", "", paidLine)
			inv.PaymentStatus = tc.paymentStatus

			mappingRepo := &fakeSyncMappingRepo{}
			if tc.existingMapping {
				mappingRepo.mappings = []*entityintegrationmapping.EntityIntegrationMapping{
					{EntityID: inv.ID, ProviderEntityID: "zoho_inv_existing"},
				}
			}

			client := &fakeSyncZohoClient{
				getInvoiceResp: &InvoiceResponse{
					InvoiceID:  "zoho_inv_existing",
					CustomerID: "zoho_cust_1",
					Balance:    decimal.NewFromInt(100),
				},
			}
			svc := &InvoiceService{
				client:       client,
				customerSvc:  fakeSyncCustomerSvc{},
				itemSyncSvc:  fakeSyncItemSyncSvc{},
				taxSvc:       fakeSyncTaxSvc{},
				customerRepo: &fakeSyncCustomerRepo{},
				invoiceRepo:  &fakeSyncInvoiceRepo{inv: inv},
				mappingRepo:  mappingRepo,
				logger:       logger.NewNoopLogger(),
			}

			_, err := svc.SyncInvoiceToZoho(context.Background(), ZohoInvoiceSyncRequest{InvoiceID: inv.ID})
			require.NoError(t, err)

			if tc.wantCreateInvoice {
				require.NotNil(t, client.createInvoiceReq)
			} else {
				assert.Nil(t, client.createInvoiceReq)
			}

			if tc.wantCreatePayment {
				assert.Equal(t, 1, client.createPaymentCalls)
				require.NotNil(t, client.createPaymentReq)
				assert.True(t, decimal.NewFromInt(100).Equal(client.createPaymentReq.Amount()))
			} else {
				assert.Equal(t, 0, client.createPaymentCalls)
			}
		})
	}
}
