package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestCreateInvoiceRequest_ZeroOutAmounts(t *testing.T) {
	req := CreateInvoiceRequest{
		Subtotal:  decimal.NewFromInt(99),
		Total:     decimal.NewFromInt(99),
		AmountDue: decimal.NewFromInt(99),
		LineItems: []CreateInvoiceLineItemRequest{
			{Amount: decimal.NewFromInt(99), Quantity: decimal.NewFromInt(2)},
			{Amount: decimal.NewFromInt(49), Quantity: decimal.NewFromInt(1)},
		},
	}

	req.ZeroOutAmounts()

	assert.True(t, req.Subtotal.IsZero(), "Subtotal must be zero")
	assert.True(t, req.Total.IsZero(), "Total must be zero")
	assert.True(t, req.AmountDue.IsZero(), "AmountDue must be zero")

	for i, li := range req.LineItems {
		assert.True(t, li.Amount.IsZero(), "line item %d Amount must be zero", i)
		// Quantity is deliberately preserved — it shows the pricing skeleton.
		assert.False(t, li.Quantity.IsZero(), "line item %d Quantity must be preserved", i)
	}
}

func TestCreateInvoiceRequest_ZeroOutAmounts_EmptyLineItems(t *testing.T) {
	req := CreateInvoiceRequest{
		Subtotal:  decimal.NewFromInt(50),
		Total:     decimal.NewFromInt(50),
		AmountDue: decimal.NewFromInt(50),
	}
	req.ZeroOutAmounts() // must not panic on nil/empty LineItems
	assert.True(t, req.Subtotal.IsZero())
	assert.True(t, req.Total.IsZero())
	assert.True(t, req.AmountDue.IsZero())
}

func TestBuildPreviewTaxes(t *testing.T) {
	pct := decimal.NewFromInt(10)
	fixed := decimal.NewFromFloat(2.50)
	rates := []*TaxRateResponse{
		{TaxRate: &taxrate.TaxRate{ID: "taxrate_pct", TaxRateType: types.TaxRateTypePercentage, PercentageValue: &pct}},
		{TaxRate: &taxrate.TaxRate{ID: "taxrate_fixed", TaxRateType: types.TaxRateTypeFixed, FixedValue: &fixed}},
		// No usable value — must be skipped, not emitted as a zero entry.
		{TaxRate: &taxrate.TaxRate{ID: "taxrate_pct_nil", TaxRateType: types.TaxRateTypePercentage}},
	}

	inv := &invoice.Invoice{
		ID:            "inv_preview",
		Currency:      "usd",
		Subtotal:      decimal.NewFromInt(100),
		TotalDiscount: decimal.NewFromInt(20),
	}

	taxes := BuildPreviewTaxes(inv, rates)

	assert.Len(t, taxes, 2)
	assert.Equal(t, "taxrate_pct", taxes[0].TaxRateID)
	// Taxable base is subtotal - discount, matching ToInvoice's preview math.
	assert.True(t, taxes[0].TaxableAmount.Equal(decimal.NewFromInt(80)), "taxable amount: %s", taxes[0].TaxableAmount)
	assert.True(t, taxes[0].TaxAmount.Equal(decimal.NewFromInt(8)), "tax amount: %s", taxes[0].TaxAmount)
	assert.Equal(t, types.TaxRateEntityTypeInvoice, taxes[0].EntityType)
	assert.Equal(t, "inv_preview", taxes[0].EntityID)
	assert.Equal(t, "usd", taxes[0].Currency)

	assert.Equal(t, "taxrate_fixed", taxes[1].TaxRateID)
	assert.True(t, taxes[1].TaxAmount.Equal(fixed), "tax amount: %s", taxes[1].TaxAmount)
}

func TestBuildPreviewTaxes_NoRates(t *testing.T) {
	inv := &invoice.Invoice{ID: "inv_preview", Subtotal: decimal.NewFromInt(100)}
	assert.Nil(t, BuildPreviewTaxes(inv, nil))
	assert.Nil(t, BuildPreviewTaxes(nil, []*TaxRateResponse{}))
}

func TestBuildPreviewTaxes_DiscountBeyondSubtotalClampsToZero(t *testing.T) {
	pct := decimal.NewFromInt(10)
	rates := []*TaxRateResponse{
		{TaxRate: &taxrate.TaxRate{ID: "taxrate_pct", TaxRateType: types.TaxRateTypePercentage, PercentageValue: &pct}},
	}
	inv := &invoice.Invoice{
		ID:            "inv_preview",
		Subtotal:      decimal.NewFromInt(10),
		TotalDiscount: decimal.NewFromInt(30),
	}

	taxes := BuildPreviewTaxes(inv, rates)

	assert.Len(t, taxes, 1)
	assert.True(t, taxes[0].TaxableAmount.IsZero(), "taxable amount: %s", taxes[0].TaxableAmount)
	assert.True(t, taxes[0].TaxAmount.IsZero(), "tax amount: %s", taxes[0].TaxAmount)
}

func TestBuildPreviewTaxes_NilEmbeddedTaxRate(t *testing.T) {
	// TaxRateResponse embeds *taxrate.TaxRate; a zero-value entry must be
	// skipped rather than dereferenced.
	inv := &invoice.Invoice{ID: "inv_preview", Currency: "usd", Subtotal: decimal.NewFromInt(100)}
	assert.NotPanics(t, func() {
		assert.Nil(t, BuildPreviewTaxes(inv, []*TaxRateResponse{{}}))
	})
}

func TestBuildPreviewTaxes_RoundsToCurrencyPrecision(t *testing.T) {
	// 7.5% of 100.01 = 7.50075 — must not surface fractional cents that the
	// charged amount will never match.
	pct := decimal.NewFromFloat(7.5)
	inv := &invoice.Invoice{
		ID:        "inv_preview",
		Currency:  "usd",
		Subtotal:  decimal.NewFromFloat(100.01),
		BaseModel: types.BaseModel{TenantID: "tenant_1"},
	}
	taxes := BuildPreviewTaxes(inv, []*TaxRateResponse{
		{TaxRate: &taxrate.TaxRate{ID: "taxrate_pct", TaxRateType: types.TaxRateTypePercentage, PercentageValue: &pct}},
	})

	assert.Len(t, taxes, 1)
	assert.Equal(t, "7.5", taxes[0].TaxAmount.String())
	assert.Equal(t, "tenant_1", taxes[0].TenantID)
}

func TestRecalculatePreviewTotals_TaxesTheDiscountedSubtotal(t *testing.T) {
	// A coupon resolved at the service layer sets TotalDiscount after
	// ToInvoice has already taxed the full subtotal, so the second pass must
	// tax what remains — not the original amount.
	pct := decimal.NewFromInt(10)
	rates := []*TaxRateResponse{
		{TaxRate: &taxrate.TaxRate{ID: "taxrate_pct", TaxRateType: types.TaxRateTypePercentage, PercentageValue: &pct}},
	}
	inv := &invoice.Invoice{
		ID:            "inv_preview",
		Currency:      "usd",
		Subtotal:      decimal.NewFromInt(100),
		TotalDiscount: decimal.NewFromInt(20),
	}

	RecalculatePreviewTotals(inv, rates)

	assert.Equal(t, "8", inv.TotalTax.String(), "tax must be 10%% of 80, not of 100")
	assert.Equal(t, "88", inv.Total.String())
	assert.Equal(t, "88", inv.AmountDue.String())
	assert.Equal(t, "88", inv.AmountRemaining.String())
}

func TestRecalculatePreviewTotals_DiscountBeyondSubtotal(t *testing.T) {
	pct := decimal.NewFromInt(10)
	rates := []*TaxRateResponse{
		{TaxRate: &taxrate.TaxRate{ID: "taxrate_pct", TaxRateType: types.TaxRateTypePercentage, PercentageValue: &pct}},
	}
	inv := &invoice.Invoice{
		ID:            "inv_preview",
		Currency:      "usd",
		Subtotal:      decimal.NewFromInt(10),
		TotalDiscount: decimal.NewFromInt(30),
	}

	RecalculatePreviewTotals(inv, rates)

	assert.True(t, inv.TotalTax.IsZero(), "tax: %s", inv.TotalTax)
	assert.True(t, inv.Total.IsZero(), "total: %s", inv.Total)
}
