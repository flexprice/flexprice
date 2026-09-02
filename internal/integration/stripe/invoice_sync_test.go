package stripe

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
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
