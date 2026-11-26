package stripe

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v82"
)

func TestBuildInvoiceItemParams_PriceDataWhenMapped(t *testing.T) {
	lineItem := &invoice.InvoiceLineItem{
		ID:          "li_1",
		PriceID:     lo.ToPtr("price_1"),
		DisplayName: lo.ToPtr("API calls"),
		Amount:      decimal.NewFromFloat(12.34),
		Quantity:    decimal.NewFromInt(1),
		Currency:    "USD",
	}

	params := buildInvoiceItemParams(lineItem, "cus_1", "in_1", map[string]string{"price_1": "prod_1"})

	require.Nil(t, params.Amount)
	require.Nil(t, params.Currency)
	require.Nil(t, params.Quantity)
	require.NotNil(t, params.PriceData)
	require.Equal(t, "prod_1", *params.PriceData.Product)
	require.Equal(t, "usd", *params.PriceData.Currency)
	require.Equal(t, int64(1234), *params.PriceData.UnitAmount)
	require.Equal(t, "prod_1", params.Metadata["flexprice_stripe_product_id"])
	require.Equal(t, "price_1", params.Metadata["flexprice_price_id"])
}

func TestBuildInvoiceItemParams_AmountFallbackWhenUnmapped(t *testing.T) {
	lineItem := &invoice.InvoiceLineItem{
		ID: "li_2", PriceID: lo.ToPtr("price_2"),
		Amount: decimal.NewFromFloat(5.00), Quantity: decimal.NewFromInt(1), Currency: "USD",
	}

	params := buildInvoiceItemParams(lineItem, "cus_1", "in_1", map[string]string{"price_1": "prod_1"})

	require.Nil(t, params.PriceData)
	require.Equal(t, int64(500), *params.Amount)
	require.Equal(t, "usd", *params.Currency)
}

func TestBuildInvoiceItemParams_AmountFallbackWhenNoPriceID(t *testing.T) {
	lineItem := &invoice.InvoiceLineItem{
		ID: "li_3", Amount: decimal.NewFromFloat(2.50), Quantity: decimal.NewFromInt(1), Currency: "USD",
	}

	params := buildInvoiceItemParams(lineItem, "cus_1", "in_1", map[string]string{"price_2": "prod_2"})

	require.Nil(t, params.PriceData)
	require.Equal(t, int64(250), *params.Amount)
}

func TestBuildInvoiceItemParams_AmountFallbackWhenFeatureOff(t *testing.T) {
	lineItem := &invoice.InvoiceLineItem{
		ID: "li_4", PriceID: lo.ToPtr("price_1"),
		Amount: decimal.NewFromFloat(2.50), Quantity: decimal.NewFromInt(1), Currency: "USD",
	}

	// nil map == feature off (linkStripeProduct was false, so the caller never built a map)
	params := buildInvoiceItemParams(lineItem, "cus_1", "in_1", nil)

	require.Nil(t, params.PriceData)
	require.Equal(t, int64(250), *params.Amount)
}

func TestBuildInvoiceItemParams_SmallestUnit(t *testing.T) {
	cases := []struct {
		name     string
		amount   decimal.Decimal
		currency string
		want     int64
	}{
		{"USD control", decimal.RequireFromString("12.34"), "USD", 1234},
		{"JPY zero-decimal", decimal.NewFromInt(12000), "JPY", 12000},
		{"KRW zero-decimal", decimal.NewFromInt(50000), "KRW", 50000},
		{"KWD three-decimal", decimal.RequireFromString("12.345"), "KWD", 12345},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/amount fallback", func(t *testing.T) {
			li := &invoice.InvoiceLineItem{ID: "li_1", Amount: tc.amount, Quantity: decimal.NewFromInt(1), Currency: tc.currency}
			params := buildInvoiceItemParams(li, "cus_1", "in_1", nil)
			require.Equal(t, tc.want, *params.Amount)
		})
		t.Run(tc.name+"/price_data", func(t *testing.T) {
			li := &invoice.InvoiceLineItem{ID: "li_1", PriceID: lo.ToPtr("price_1"), Amount: tc.amount, Quantity: decimal.NewFromInt(1), Currency: tc.currency}
			params := buildInvoiceItemParams(li, "cus_1", "in_1", map[string]string{"price_1": "prod_1"})
			require.Equal(t, tc.want, *params.PriceData.UnitAmount)
		})
	}
}

func TestBuildInvoiceItemsParams(t *testing.T) {
	cases := []struct {
		name      string
		discount  string
		credits   string
		tax       string
		wantItems map[string]int64
	}{
		{"lines only", "0", "0", "0", map[string]int64{}},
		{"discount", "10", "0", "0", map[string]int64{"Discount": -1000}},
		{"prepaid credits", "0", "30.50", "0", map[string]int64{"Prepaid credits applied": -3050}},
		{"exclusive tax", "0", "0", "18", map[string]int64{"Tax": 1800}},
		{"all", "10", "30", "12.60", map[string]int64{"Discount": -1000, "Prepaid credits applied": -3000, "Tax": 1260}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := &invoice.Invoice{
				ID: "inv_1", Currency: "USD",
				TotalDiscount:              decimal.RequireFromString(tc.discount),
				TotalPrepaidCreditsApplied: decimal.RequireFromString(tc.credits),
				LineItems: []*invoice.InvoiceLineItem{
					{ID: "li_1", Amount: decimal.NewFromInt(60), Currency: "USD", PrepaidCreditsApplied: decimal.NewFromInt(20)},
					{ID: "li_2", Amount: decimal.NewFromInt(40), Currency: "USD", LineItemDiscount: decimal.NewFromInt(5)},
				},
			}
			items := buildInvoiceItemsParams(inv, decimal.RequireFromString(tc.tax), "cus_1", "in_1", nil)

			require.Len(t, items, 2+len(tc.wantItems))
			require.Equal(t, "li_1", items[0].Metadata["flexprice_line_item_id"])
			require.Equal(t, int64(6000), *items[0].Amount)
			require.Equal(t, "li_2", items[1].Metadata["flexprice_line_item_id"])
			require.Equal(t, int64(4000), *items[1].Amount)

			gotItems := map[string]int64{}
			for _, item := range items[2:] {
				require.Equal(t, "inv_1", item.Metadata["flexprice_invoice_id"])
				gotItems[*item.Description] = *item.Amount
			}
			require.Equal(t, tc.wantItems, gotItems)

			retry := buildInvoiceItemsParams(inv, decimal.RequireFromString(tc.tax), "cus_1", "in_1", nil)
			keys := map[string]bool{}
			for i, item := range items {
				require.Contains(t, *item.IdempotencyKey, "in_1")
				require.Equal(t, *item.IdempotencyKey, *retry[i].IdempotencyKey)
				keys[*item.IdempotencyKey] = true
			}
			require.Len(t, keys, len(items))
		})
	}
}

func TestShouldAdoptStripeInvoiceNumber(t *testing.T) {
	cases := []struct {
		name         string
		number       *string
		metadata     types.Metadata
		stripeNumber string
		want         bool
	}{
		{"different number", lo.ToPtr("INV-1"), nil, "ABC-0001", true},
		{"no number yet", nil, nil, "ABC-0001", true},
		{"same number, not yet linked", lo.ToPtr("ABC-0001"), nil, "ABC-0001", true},
		{"already adopted", lo.ToPtr("ABC-0001"), types.Metadata{"stripe_invoice_id": "in_1"}, "ABC-0001", false},
		{"no stripe number", lo.ToPtr("INV-1"), nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := &invoice.Invoice{ID: "inv_1", InvoiceNumber: tc.number, Metadata: tc.metadata}
			require.Equal(t, tc.want, shouldAdoptStripeInvoiceNumber(inv, &stripe.Invoice{ID: "in_1", Number: tc.stripeNumber}))
		})
	}
}

func TestSettlingPaymentIntent(t *testing.T) {
	stripeGateway := lo.ToPtr(string(types.PaymentGatewayTypeStripe))
	cases := []struct {
		name     string
		payments []*payment.Payment
		want     string
	}{
		{"succeeded stripe payment", []*payment.Payment{
			{PaymentGateway: stripeGateway, PaymentStatus: types.PaymentStatusFailed, GatewayPaymentID: lo.ToPtr("pi_failed")},
			{PaymentGateway: stripeGateway, PaymentStatus: types.PaymentStatusSucceeded, GatewayPaymentID: lo.ToPtr("pi_paid")},
		}, "pi_paid"},
		{"paid on another gateway", []*payment.Payment{
			{PaymentGateway: lo.ToPtr(string(types.PaymentGatewayTypeRazorpay)), PaymentStatus: types.PaymentStatusSucceeded, GatewayPaymentID: lo.ToPtr("pay_rzp")},
		}, ""},
		{"no payments", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, settlingPaymentIntent(tc.payments))
		})
	}
}
