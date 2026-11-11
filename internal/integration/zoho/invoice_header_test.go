package zoho

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tPtr(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func TestFormatPeriodDescription(t *testing.T) {
	start := tPtr("2026-04-01T00:00:00Z")
	end := tPtr("2026-05-01T00:00:00Z")

	tests := []struct {
		name     string
		fallback string
		start    *time.Time
		end      *time.Time
		want     string
	}{
		{
			name:     "name and period",
			fallback: "BBNow",
			start:    start,
			end:      end,
			want:     "BBNow\n(2026-04-01 - 2026-04-30)",
		},
		{
			name:     "period only when no name",
			fallback: "",
			start:    start,
			end:      end,
			want:     "(2026-04-01 - 2026-04-30)",
		},
		{
			name:     "no period falls back to the name",
			fallback: "BBNow",
			start:    nil,
			end:      end,
			want:     "BBNow",
		},
		{
			name:     "missing end falls back to the name",
			fallback: "BBNow",
			start:    start,
			end:      nil,
			want:     "BBNow",
		},
		{
			name:     "zero-width one-time period stays on the billing date",
			fallback: "Mandate Registration",
			start:    tPtr("2026-09-17T00:00:00Z"),
			end:      tPtr("2026-09-17T00:00:00Z"),
			want:     "Mandate Registration\n(2026-09-17 - 2026-09-17)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatPeriodDescription(tt.fallback, tt.start, tt.end))
		})
	}
}

// period_end is exclusive in FlexPrice; the invoice must show the inclusive last day.
func TestInclusiveEnd(t *testing.T) {
	assert.Equal(t, "2026-04-30", inclusiveEnd(tPtr("2026-04-01T00:00:00Z"), tPtr("2026-05-01T00:00:00Z")).Format(zohoAPIDateFormat))
	assert.Equal(t, "2026-12-31", inclusiveEnd(tPtr("2026-12-01T00:00:00Z"), tPtr("2027-01-01T00:00:00Z")).Format(zohoAPIDateFormat))
	assert.Equal(t, "2026-09-17", inclusiveEnd(tPtr("2026-09-17T00:00:00Z"), tPtr("2026-09-17T00:00:00Z")).Format(zohoAPIDateFormat),
		"one-time PeriodStart == PeriodEnd must not walk to the previous day")
}

func TestServicePeriodCustomFields(t *testing.T) {
	start := tPtr("2026-04-01T00:00:00Z")
	end := tPtr("2026-05-01T00:00:00Z")
	configured := &types.InvoiceSyncSettings{
		ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{
			ServicePeriodCustomFields: &types.ServicePeriodCustomFields{
				StartFieldID: "4069923000000000001",
				EndFieldID:   "4069923000000000002",
			},
		},
	}

	t.Run("populated when configured with numeric ids", func(t *testing.T) {
		got := servicePeriodCustomFields(configured, start, end)
		assert.Equal(t, []CustomField{
			{CustomFieldID: "4069923000000000001", Value: "2026-04-01"},
			{CustomFieldID: "4069923000000000002", Value: "2026-04-30"},
		}, got)
	})

	t.Run("nil when settings absent", func(t *testing.T) {
		assert.Nil(t, servicePeriodCustomFields(nil, start, end))
	})

	t.Run("nil when field IDs unset", func(t *testing.T) {
		assert.Nil(t, servicePeriodCustomFields(&types.InvoiceSyncSettings{}, start, end))
	})

	t.Run("nil when only one field ID is set", func(t *testing.T) {
		half := &types.InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{
				ServicePeriodCustomFields: &types.ServicePeriodCustomFields{StartFieldID: "cf_service_period_start"},
			},
		}
		assert.Nil(t, servicePeriodCustomFields(half, start, end),
			"a start date with no end date would render a broken header")
	})

	t.Run("nil when the invoice has no period", func(t *testing.T) {
		assert.Nil(t, servicePeriodCustomFields(configured, nil, nil))
	})
}

func TestServicePeriodCustomFieldsValidate(t *testing.T) {
	tests := []struct {
		name    string
		fields  *types.ServicePeriodCustomFields
		wantErr bool
	}{
		{"nil", nil, false},
		{"both empty", &types.ServicePeriodCustomFields{}, false},
		{"both set", &types.ServicePeriodCustomFields{StartFieldID: "a", EndFieldID: "b"}, false},
		{"only start", &types.ServicePeriodCustomFields{StartFieldID: "a"}, true},
		{"only end", &types.ServicePeriodCustomFields{EndFieldID: "b"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fields.Validate()
			assert.Equal(t, tt.wantErr, err != nil)
		})
	}
}

// gstCustomerRepo serves a customer carrying a GSTIN so place_of_supply resolves.
type gstCustomerRepo struct {
	customer.Repository
	metadata map[string]string
}

func (f *gstCustomerRepo) Get(_ context.Context, id string) (*customer.Customer, error) {
	return &customer.Customer{ID: id, Name: "MCGILL FOODS PRIVATE LIMITED", Metadata: f.metadata}, nil
}

func (f *gstCustomerRepo) List(_ context.Context, _ *types.CustomerFilter) ([]*customer.Customer, error) {
	return nil, nil
}

func TestSyncInvoiceSendsGSTHeaderFields(t *testing.T) {
	inv := &invoice.Invoice{
		ID:          "inv_1",
		CustomerID:  "cust_1",
		Currency:    "INR",
		IssueDate:   tPtr("2026-04-22T00:00:00Z"),
		DueDate:     tPtr("2026-05-07T00:00:00Z"),
		PeriodStart: tPtr("2026-04-01T00:00:00Z"),
		PeriodEnd:   tPtr("2026-05-01T00:00:00Z"),
		LineItems: []*invoice.InvoiceLineItem{
			func() *invoice.InvoiceLineItem {
				li := hsnLineItem("li_a", "price_a")
				li.PeriodStart = tPtr("2026-04-01T00:00:00Z")
				li.PeriodEnd = tPtr("2026-05-01T00:00:00Z")
				return li
			}(),
		},
	}

	syncConfig := &types.SyncConfig{
		InvoiceSyncSettings: &types.InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{
				ServicePeriodCustomFields: &types.ServicePeriodCustomFields{
					StartFieldID: "cf_start",
					EndFieldID:   "cf_end",
				},
			},
		},
	}

	client := &fakeSyncZohoClient{syncConfig: syncConfig}
	svc := &InvoiceService{
		client:       client,
		customerSvc:  fakeSyncCustomerSvc{},
		itemSyncSvc:  &capturingItemSyncSvc{},
		taxSvc:       fakeSyncTaxSvc{},
		customerRepo: &gstCustomerRepo{metadata: map[string]string{types.MetadataKeyGSTIN: "27AAMCM4148E1ZD"}},
		invoiceRepo:  &fakeSyncInvoiceRepo{inv: inv},
		priceRepo:    &fakeHSNPriceRepo{prices: map[string]*price.Price{"price_a": hsnPrice("price_a", "998415", "")}},
		mappingRepo:  &fakeSyncMappingRepo{},
		logger:       logger.NewNoopLogger(),
	}

	_, err := svc.SyncInvoiceToZoho(context.Background(), ZohoInvoiceSyncRequest{InvoiceID: "inv_1"})
	require.NoError(t, err)
	require.NotNil(t, client.createInvoiceReq)

	assert.Equal(t, "27", client.createInvoiceReq.PlaceOfSupply, "derived from the customer GSTIN")
	assert.Equal(t, []CustomField{
		{APIName: "cf_start", Value: "2026-04-01"},
		{APIName: "cf_end", Value: "2026-04-30"},
	}, client.createInvoiceReq.CustomFields)

	require.Len(t, client.createInvoiceReq.LineItems, 1)
	assert.Equal(t, "998415", client.createInvoiceReq.LineItems[0].HSNOrSAC)
	assert.Contains(t, client.createInvoiceReq.LineItems[0].Description, "(2026-04-01 - 2026-04-30)")
}

func TestSyncInvoiceOmitsGSTFieldsForNonIndianCustomer(t *testing.T) {
	inv := &invoice.Invoice{
		ID:         "inv_2",
		CustomerID: "cust_2",
		Currency:   "USD",
		LineItems:  []*invoice.InvoiceLineItem{hsnLineItem("li_a", "price_a")},
	}

	client := &fakeSyncZohoClient{}
	svc := &InvoiceService{
		client:       client,
		customerSvc:  fakeSyncCustomerSvc{},
		itemSyncSvc:  &capturingItemSyncSvc{},
		taxSvc:       fakeSyncTaxSvc{},
		customerRepo: &gstCustomerRepo{},
		invoiceRepo:  &fakeSyncInvoiceRepo{inv: inv},
		priceRepo:    &fakeHSNPriceRepo{prices: map[string]*price.Price{}},
		mappingRepo:  &fakeSyncMappingRepo{},
		logger:       logger.NewNoopLogger(),
	}

	_, err := svc.SyncInvoiceToZoho(context.Background(), ZohoInvoiceSyncRequest{InvoiceID: "inv_2"})
	require.NoError(t, err)

	assert.Empty(t, client.createInvoiceReq.PlaceOfSupply)
	assert.Empty(t, client.createInvoiceReq.CustomFields, "no custom fields when the connection has none configured")
}

func TestCustomFieldRefRouting(t *testing.T) {
	tests := []struct {
		name        string
		ref         string
		wantAPIName string
		wantID      string
	}{
		{"api_name from Zoho's UI", "cf_service_period_start", "cf_service_period_start", ""},
		{"api_name with stray whitespace", "  cf_service_period_end  ", "cf_service_period_end", ""},
		{"numeric customfield_id", "4069923000000123456", "", "4069923000000123456"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cf := NewCustomField(tt.ref, "01/04/2026")
			assert.Equal(t, tt.wantAPIName, cf.APIName)
			assert.Equal(t, tt.wantID, cf.CustomFieldID)
			assert.Equal(t, "01/04/2026", cf.Value)
		})
	}
}

// Zoho rejects an api_name sent as customfield_id with "Invalid value passed for
// customfield_id", so only the matching key may appear on the wire.
func TestCustomFieldSerialisesOnlyOneKey(t *testing.T) {
	byName, err := json.Marshal(NewCustomField("cf_service_period_start", "01/04/2026"))
	require.NoError(t, err)
	assert.Contains(t, string(byName), `"api_name":"cf_service_period_start"`)
	assert.NotContains(t, string(byName), "customfield_id")

	byID, err := json.Marshal(NewCustomField("4069923000000123456", "30/04/2026"))
	require.NoError(t, err)
	assert.Contains(t, string(byID), `"customfield_id":"4069923000000123456"`)
	assert.NotContains(t, string(byID), "api_name")
}

func TestServicePeriodCustomFieldsUsesAPINames(t *testing.T) {
	settings := &types.InvoiceSyncSettings{
		ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{
			ServicePeriodCustomFields: &types.ServicePeriodCustomFields{
				StartFieldID: "cf_service_period_start",
				EndFieldID:   "cf_service_period_end",
			},
		},
	}
	got := servicePeriodCustomFields(settings, tPtr("2026-04-01T00:00:00Z"), tPtr("2026-05-01T00:00:00Z"))
	require.Len(t, got, 2)
	assert.Equal(t, "cf_service_period_start", got[0].APIName)
	assert.Equal(t, "2026-04-01", got[0].Value, "ISO on the wire; Zoho renders dd/MM/yyyy per org locale")
	assert.Equal(t, "cf_service_period_end", got[1].APIName)
	assert.Equal(t, "2026-04-30", got[1].Value, "period_end is exclusive; the invoice shows the prior day")
	assert.Empty(t, got[0].CustomFieldID)
}

func TestMetadataCustomFields(t *testing.T) {
	settings := &types.InvoiceSyncSettings{
		ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{
			MetadataCustomFields: []types.MetadataCustomField{
				{Source: types.MetadataCustomFieldSourceCustomer, MetadataKey: "hubspot_company_id", Field: "cf_hubspot_id"},
				{Source: types.MetadataCustomFieldSourceCustomer, MetadataKey: "brand_name", Field: "4069923000000000009"},
				{Source: types.MetadataCustomFieldSourceInvoice, MetadataKey: "po_number", Field: "cf_po"},
			},
		},
	}
	customerMeta := map[string]string{"hubspot_company_id": "164213581519", "brand_name": "Mankind Pharma"}
	invoiceMeta := map[string]string{"po_number": "4106065378"}

	t.Run("copies values from the configured source", func(t *testing.T) {
		assert.Equal(t, []CustomField{
			{APIName: "cf_hubspot_id", Value: "164213581519"},
			{CustomFieldID: "4069923000000000009", Value: "Mankind Pharma"},
			{APIName: "cf_po", Value: "4106065378"},
		}, metadataCustomFields(settings, invoiceMeta, customerMeta))
	})

	t.Run("skips absent and blank keys", func(t *testing.T) {
		got := metadataCustomFields(settings, nil, map[string]string{
			"hubspot_company_id": "164213581519",
			"brand_name":         "   ",
		})
		assert.Equal(t, []CustomField{{APIName: "cf_hubspot_id", Value: "164213581519"}}, got)
	})

	t.Run("does not cross sources", func(t *testing.T) {
		assert.Nil(t, metadataCustomFields(settings, customerMeta, nil),
			"customer-sourced keys are not read off the invoice")
	})

	t.Run("nil when unconfigured", func(t *testing.T) {
		assert.Nil(t, metadataCustomFields(nil, invoiceMeta, customerMeta))
		assert.Nil(t, metadataCustomFields(&types.InvoiceSyncSettings{}, invoiceMeta, customerMeta))
	})
}

func TestGlobalCustomFields(t *testing.T) {
	settings := &types.InvoiceSyncSettings{
		ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{
			GlobalCustomFields: []types.GlobalCustomField{
				{Field: "cf_source", Value: "FlexPrice"},
				{Field: "4069923000000000009", Value: "IN"},
				{Field: "cf_blank", Value: "   "},
			},
		},
	}

	assert.Equal(t, []CustomField{
		{APIName: "cf_source", Value: "FlexPrice"},
		{CustomFieldID: "4069923000000000009", Value: "IN"},
	}, globalCustomFields(settings))

	assert.Nil(t, globalCustomFields(nil))
	assert.Nil(t, globalCustomFields(&types.InvoiceSyncSettings{}))
}

func TestSyncInvoiceSendsMetadataCustomFieldsAlongsideServicePeriod(t *testing.T) {
	inv := &invoice.Invoice{
		ID:          "inv_3",
		CustomerID:  "cust_3",
		Currency:    "INR",
		PeriodStart: tPtr("2026-08-01T00:00:00Z"),
		PeriodEnd:   tPtr("2026-09-01T00:00:00Z"),
		Metadata:    types.Metadata{"po_number": "4106065378"},
		LineItems:   []*invoice.InvoiceLineItem{hsnLineItem("li_a", "price_a")},
	}

	inv.LineItems[0].PeriodStart = tPtr("2026-08-01T00:00:00Z")
	inv.LineItems[0].PeriodEnd = tPtr("2026-09-01T00:00:00Z")

	client := &fakeSyncZohoClient{syncConfig: &types.SyncConfig{
		InvoiceSyncSettings: &types.InvoiceSyncSettings{
			ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{
				ServicePeriodCustomFields: &types.ServicePeriodCustomFields{
					StartFieldID: "cf_start",
					EndFieldID:   "cf_end",
				},
				MetadataCustomFields: []types.MetadataCustomField{
					{Source: types.MetadataCustomFieldSourceCustomer, MetadataKey: "hubspot_company_id", Field: "cf_hubspot_id"},
					{Source: types.MetadataCustomFieldSourceCustomer, MetadataKey: "brand_name", Field: "cf_brand_name"},
					{Source: types.MetadataCustomFieldSourceInvoice, MetadataKey: "po_number", Field: "cf_po"},
					{Source: types.MetadataCustomFieldSourceCustomer, MetadataKey: "absent", Field: "cf_absent"},
				},
			},
		},
	}}

	svc := &InvoiceService{
		client:      client,
		customerSvc: fakeSyncCustomerSvc{},
		itemSyncSvc: &capturingItemSyncSvc{},
		taxSvc:      fakeSyncTaxSvc{},
		customerRepo: &gstCustomerRepo{metadata: map[string]string{
			"hubspot_company_id": "164213581519",
			"brand_name":         "Mankind Pharma",
		}},
		invoiceRepo: &fakeSyncInvoiceRepo{inv: inv},
		priceRepo:   &fakeHSNPriceRepo{prices: map[string]*price.Price{}},
		mappingRepo: &fakeSyncMappingRepo{},
		logger:      logger.NewNoopLogger(),
	}

	_, err := svc.SyncInvoiceToZoho(context.Background(), ZohoInvoiceSyncRequest{InvoiceID: "inv_3"})
	require.NoError(t, err)
	require.NotNil(t, client.createInvoiceReq)

	assert.Equal(t, []CustomField{
		{APIName: "cf_start", Value: "2026-08-01"},
		{APIName: "cf_end", Value: "2026-08-31"},
		{APIName: "cf_hubspot_id", Value: "164213581519"},
		{APIName: "cf_brand_name", Value: "Mankind Pharma"},
		{APIName: "cf_po", Value: "4106065378"},
	}, client.createInvoiceReq.CustomFields)
}

func TestSyncInvoiceResolvesServicePeriodFromSentItems(t *testing.T) {
	item := func(id, start, end string) *invoice.InvoiceLineItem {
		li := hsnLineItem(id, "price_a")
		if start != "" {
			li.PeriodStart = tPtr(start)
		}
		if end != "" {
			li.PeriodEnd = tPtr(end)
		}
		return li
	}
	const sept = "2026-09-12T00:00:00Z"
	const oct = "2026-10-12T00:00:00Z"
	const nov = "2026-11-12T00:00:00Z"
	tests := []struct {
		name               string
		items              func() []*invoice.InvoiceLineItem
		wantStart, wantEnd string
		count              int
	}{
		{"advance invoice with discounted services and skipped anchors", func() []*invoice.InvoiceLineItem {
			a, b := item("blinkit", oct, nov), item("instamart", oct, nov)
			a.LineItemDiscount, b.LineItemDiscount = a.Amount, b.Amount
			anchor := item("anchor", "2026-01-01T00:00:00Z", "2027-01-01T00:00:00Z")
			anchor.Amount = decimal.Zero
			return []*invoice.InvoiceLineItem{nil, anchor, a, b}
		}, "2026-10-12", "2026-11-11", 2},
		{"mixed periods independent of item order", func() []*invoice.InvoiceLineItem {
			return []*invoice.InvoiceLineItem{item("advance", oct, nov), item("arrears", sept, oct)}
		}, "2026-09-12", "2026-11-11", 2},
		{"arrears", func() []*invoice.InvoiceLineItem { return []*invoice.InvoiceLineItem{item("arrears", sept, oct)} }, "2026-09-12", "2026-10-11", 1},
		{"one time", func() []*invoice.InvoiceLineItem { return []*invoice.InvoiceLineItem{item("one-time", oct, oct)} }, "2026-10-12", "2026-10-12", 1},
		{"no dates does not fall back to billing cycle", func() []*invoice.InvoiceLineItem { return []*invoice.InvoiceLineItem{item("undated", "", "")} }, "", "", 1},
		{"missing price mapping still sent", func() []*invoice.InvoiceLineItem {
			li := item("custom", oct, nov)
			li.PriceID = nil
			return []*invoice.InvoiceLineItem{li}
		}, "2026-10-12", "2026-11-11", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := &invoice.Invoice{ID: "inv_advance", CustomerID: "cust_1", Currency: "INR", PeriodStart: tPtr(sept), PeriodEnd: tPtr(oct), LineItems: tt.items()}
			client := &fakeSyncZohoClient{syncConfig: &types.SyncConfig{InvoiceSyncSettings: &types.InvoiceSyncSettings{
				ZohoInvoiceSyncSettings: types.ZohoInvoiceSyncSettings{ServicePeriodCustomFields: &types.ServicePeriodCustomFields{StartFieldID: "cf_start", EndFieldID: "cf_end"}},
			}}}
			svc := &InvoiceService{client: client, customerSvc: fakeSyncCustomerSvc{}, itemSyncSvc: &capturingItemSyncSvc{}, taxSvc: fakeSyncTaxSvc{}, customerRepo: &gstCustomerRepo{}, invoiceRepo: &fakeSyncInvoiceRepo{inv: inv}, priceRepo: &fakeHSNPriceRepo{prices: map[string]*price.Price{}}, mappingRepo: &fakeSyncMappingRepo{}, logger: logger.NewNoopLogger()}
			_, err := svc.SyncInvoiceToZoho(context.Background(), ZohoInvoiceSyncRequest{InvoiceID: inv.ID})
			require.NoError(t, err)
			require.NotNil(t, client.createInvoiceReq)
			require.Len(t, client.createInvoiceReq.LineItems, tt.count)
			if tt.wantStart == "" {
				assert.Empty(t, client.createInvoiceReq.CustomFields)
			} else {
				assert.Equal(t, []CustomField{{APIName: "cf_start", Value: tt.wantStart}, {APIName: "cf_end", Value: tt.wantEnd}}, client.createInvoiceReq.CustomFields)
			}
			assert.Equal(t, tPtr(sept), inv.PeriodStart)
			assert.Equal(t, tPtr(oct), inv.PeriodEnd)
		})
	}
}

func TestResolveServicePeriod(t *testing.T) {
	date := func(month time.Month, day int) *time.Time {
		v := time.Date(2026, month, day, 0, 0, 0, 0, time.UTC)
		return &v
	}
	sept, oct, nov := date(9, 12), date(10, 12), date(11, 12)
	tests := []struct {
		name       string
		items      []*invoice.InvoiceLineItem
		start, end *time.Time
	}{
		{"empty", nil, nil, nil},
		{"advance", []*invoice.InvoiceLineItem{{PeriodStart: oct, PeriodEnd: nov}}, oct, nov},
		{"mixed unordered", []*invoice.InvoiceLineItem{{PeriodStart: oct, PeriodEnd: nov}, {PeriodStart: sept, PeriodEnd: oct}}, sept, nov},
		{"one time", []*invoice.InvoiceLineItem{{PeriodStart: oct, PeriodEnd: oct}}, oct, oct},
		{"invalid", []*invoice.InvoiceLineItem{nil, {}, {PeriodStart: sept}, {PeriodEnd: nov}, {PeriodStart: nov, PeriodEnd: sept}, {PeriodStart: &time.Time{}, PeriodEnd: nov}}, nil, nil},
		{"invalid alongside valid", []*invoice.InvoiceLineItem{{PeriodStart: nov, PeriodEnd: sept}, {PeriodStart: oct, PeriodEnd: nov}}, oct, nov},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := &invoice.Invoice{LineItems: tt.items}
			start, end := inv.ResolveServicePeriod(nil)
			assert.Equal(t, tt.start, start)
			assert.Equal(t, tt.end, end)
			if start != nil {
				require.NotSame(t, tt.start, start)
				require.NotSame(t, tt.end, end)
			}
		})
	}
}
