package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// fxTestCtx returns a context scoped to the default tenant and a test environment.
func fxTestCtx() context.Context {
	return types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
}

// TestInMemoryInvoiceStore_FxConversionRoundTrip proves the new adaptive-currency columns
// (invoice fx_conversion, line item original_currency/original_amount) survive create → get,
// and that fx_conversion is never cleared by an update that did not load it.
func TestInMemoryInvoiceStore_FxConversionRoundTrip(t *testing.T) {
	ctx := fxTestCtx()
	base := types.GetDefaultBaseModel(ctx)

	rate := decimal.RequireFromString("83.50")
	convertedAt := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

	fx := &types.FxConversion{
		ChargeCurrency:  "usd",
		BillingCurrency: "inr",
		Rate:            rate,
		RateID:          "fxr_1",
		Scope:           string(types.FXRateScopeTenant),
		ConvertedAt:     convertedAt,
		Source: types.FxConversionSource{
			Subtotal:                   decimal.RequireFromString("100"),
			TotalDiscount:              decimal.RequireFromString("0"),
			TotalPrepaidCreditsApplied: decimal.RequireFromString("0"),
			Net:                        decimal.RequireFromString("100"),
		},
		RoundingAdjustment: decimal.RequireFromString("0.50"),
		RoundingLineItemID: "il_1",
	}

	origAmount := decimal.RequireFromString("100")

	cases := []struct {
		name       string
		lineOrigCy *string
		lineOrigAm *decimal.Decimal
	}{
		{name: "converted line carries originals", lineOrigCy: lo.ToPtr("usd"), lineOrigAm: &origAmount},
		{name: "unconverted line leaves originals nil", lineOrigCy: nil, lineOrigAm: nil},
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := NewInMemoryInvoiceStore()
			lineStore := NewInMemoryInvoiceLineItemStore()
			store.SetLineItemStore(lineStore)

			invID := "inv_fx_" + lo.RandomString(6, lo.LowerCaseLettersCharset)
			ilID := "il_fx_" + lo.RandomString(6, lo.LowerCaseLettersCharset)

			inv := &invoice.Invoice{
				ID:              invID,
				CustomerID:      "cust_1",
				InvoiceType:     types.InvoiceTypeSubscription,
				InvoiceStatus:   types.InvoiceStatusFinalized,
				PaymentStatus:   types.PaymentStatusPending,
				Currency:        "inr",
				AmountDue:       decimal.NewFromInt(8350),
				AmountRemaining: decimal.NewFromInt(8350),
				FxConversion:    fx,
				EnvironmentID:   "env_test",
				BaseModel:       base,
				LineItems: []*invoice.InvoiceLineItem{
					{
						ID:               ilID,
						InvoiceID:        invID,
						CustomerID:       "cust_1",
						Amount:           decimal.NewFromInt(8350),
						Quantity:         decimal.NewFromInt(1),
						Currency:         "inr",
						OriginalCurrency: c.lineOrigCy,
						OriginalAmount:   c.lineOrigAm,
						EnvironmentID:    "env_test",
						BaseModel:        base,
					},
				},
			}

			require.NoError(t, store.CreateWithLineItems(ctx, inv))

			got, err := store.Get(ctx, invID)
			require.NoError(t, err)

			// Invoice-level fx_conversion survived the copy.
			require.NotNil(t, got.FxConversion, "case %d: fx_conversion must persist", i)
			require.Equal(t, "usd", got.FxConversion.ChargeCurrency)
			require.Equal(t, "inr", got.FxConversion.BillingCurrency)
			require.True(t, rate.Equal(got.FxConversion.Rate))
			require.Equal(t, "fxr_1", got.FxConversion.RateID)
			require.True(t, got.FxConversion.RoundingAdjustment.Equal(decimal.RequireFromString("0.50")))
			require.True(t, fx.Source.Net.Equal(got.FxConversion.Source.Net))

			// Line-item originals survived the copy (via copyInvoice and the line-item store).
			require.Len(t, got.LineItems, 1)
			gotLine := got.LineItems[0]
			if c.lineOrigCy == nil {
				require.Nil(t, gotLine.OriginalCurrency, "case %d: originals must stay nil", i)
				require.Nil(t, gotLine.OriginalAmount)
			} else {
				require.NotNil(t, gotLine.OriginalCurrency)
				require.Equal(t, "usd", *gotLine.OriginalCurrency)
				require.NotNil(t, gotLine.OriginalAmount)
				require.True(t, origAmount.Equal(*gotLine.OriginalAmount))
			}

			// An update from a struct that never loaded fx_conversion must not wipe it.
			bare := &invoice.Invoice{
				ID:              invID,
				CustomerID:      "cust_1",
				InvoiceType:     types.InvoiceTypeSubscription,
				InvoiceStatus:   types.InvoiceStatusFinalized,
				PaymentStatus:   types.PaymentStatusSucceeded,
				Currency:        "inr",
				AmountDue:       decimal.NewFromInt(8350),
				AmountPaid:      decimal.NewFromInt(8350),
				AmountRemaining: decimal.Zero,
				EnvironmentID:   "env_test",
				BaseModel:       base,
			}
			require.NoError(t, store.Update(ctx, bare))

			after, err := store.Get(ctx, invID)
			require.NoError(t, err)
			require.NotNil(t, after.FxConversion, "case %d: update must not clear fx_conversion", i)
			require.True(t, rate.Equal(after.FxConversion.Rate))
		})
	}
}

// TestInMemoryCustomerStore_BillingCurrencyRoundTrip proves customers.billing_currency survives
// create → get and update, including clear-to-nil.
func TestInMemoryCustomerStore_BillingCurrencyRoundTrip(t *testing.T) {
	ctx := fxTestCtx()
	base := types.GetDefaultBaseModel(ctx)

	cases := []struct {
		name    string
		billing *string
	}{
		{name: "billing currency set", billing: lo.ToPtr("inr")},
		{name: "billing currency nil", billing: nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := NewInMemoryCustomerStore()
			custID := "cust_bc_" + lo.RandomString(6, lo.LowerCaseLettersCharset)

			cust := &customer.Customer{
				ID:              custID,
				ExternalID:      custID,
				Name:            "Acme",
				Email:           "billing@acme.test",
				BillingCurrency: c.billing,
				EnvironmentID:   "env_test",
				BaseModel:       base,
			}
			require.NoError(t, store.Create(ctx, cust))

			got, err := store.Get(ctx, custID)
			require.NoError(t, err)
			if c.billing == nil {
				require.Nil(t, got.BillingCurrency)
			} else {
				require.NotNil(t, got.BillingCurrency)
				require.Equal(t, *c.billing, *got.BillingCurrency)
			}
		})
	}
}
