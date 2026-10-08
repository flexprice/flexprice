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

// fx_conversion and line originals survive create → get, and an update that never loaded
// fx_conversion does not clear it.
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
		Scope:           types.FXRateScopeTenant,
		ConvertedAt:     convertedAt,
		Source: types.FxConversionSource{
			Subtotal:                   decimal.RequireFromString("100"),
			TotalDiscount:              decimal.RequireFromString("0"),
			TotalPrepaidCreditsApplied: decimal.RequireFromString("0"),
			Net:                        decimal.RequireFromString("100"),
		},
	}

	lineFx := &types.FxConversion{ChargeCurrency: "usd", Source: types.FxConversionSource{Subtotal: decimal.RequireFromString("100"), Net: decimal.RequireFromString("100")}}

	cases := []struct {
		name   string
		lineFx *types.FxConversion
	}{
		{name: "converted line carries fx_conversion", lineFx: lineFx},
		{name: "unconverted line leaves fx_conversion nil", lineFx: nil},
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
						ID:            ilID,
						InvoiceID:     invID,
						CustomerID:    "cust_1",
						Amount:        decimal.NewFromInt(8350),
						Quantity:      decimal.NewFromInt(1),
						Currency:      "inr",
						FxConversion:  c.lineFx,
						EnvironmentID: "env_test",
						BaseModel:     base,
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
			require.True(t, fx.Source.Net.Equal(got.FxConversion.Source.Net))

			// Line-item originals survived the copy (via copyInvoice and the line-item store).
			require.Len(t, got.LineItems, 1)
			gotLine := got.LineItems[0]
			if c.lineFx == nil {
				require.Nil(t, gotLine.FxConversion, "case %d: fx_conversion must stay nil", i)
			} else {
				require.NotNil(t, gotLine.FxConversion)
				require.Equal(t, "usd", gotLine.FxConversion.ChargeCurrency)
				require.True(t, lineFx.Source.Subtotal.Equal(gotLine.FxConversion.Source.Subtotal))
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

// TestInMemoryStores_FxFieldsNotAliased proves a caller cannot change a stored fx_conversion, line
// originals or billing currency by mutating a value it got back, without calling Update.
func TestInMemoryStores_FxFieldsNotAliased(t *testing.T) {
	ctx := fxTestCtx()
	base := types.GetDefaultBaseModel(ctx)

	store := NewInMemoryInvoiceStore()
	store.SetLineItemStore(NewInMemoryInvoiceLineItemStore())
	origAmount := decimal.RequireFromString("100")
	require.NoError(t, store.CreateWithLineItems(ctx, &invoice.Invoice{
		ID: "inv_alias", CustomerID: "cust_1", InvoiceType: types.InvoiceTypeOneOff,
		InvoiceStatus: types.InvoiceStatusDraft, Currency: "inr",
		FxConversion:  &types.FxConversion{ChargeCurrency: "usd", BillingCurrency: "inr", Rate: decimal.NewFromInt(83), Scope: "tenant"},
		EnvironmentID: "env_test", BaseModel: base,
		LineItems: []*invoice.InvoiceLineItem{{
			ID: "il_alias", InvoiceID: "inv_alias", CustomerID: "cust_1", Currency: "inr",
			Amount: decimal.NewFromInt(8300), Quantity: decimal.NewFromInt(1),
			FxConversion:  &types.FxConversion{ChargeCurrency: "usd", Source: types.FxConversionSource{Subtotal: origAmount}},
			EnvironmentID: "env_test", BaseModel: base,
		}},
	}))

	got, err := store.Get(ctx, "inv_alias")
	require.NoError(t, err)
	got.FxConversion.Scope = "mutated"
	got.LineItems[0].FxConversion.ChargeCurrency = "eur"
	got.LineItems[0].FxConversion.Source.Subtotal = decimal.NewFromInt(1)

	again, err := store.Get(ctx, "inv_alias")
	require.NoError(t, err)
	require.Equal(t, types.FXRateScopeTenant, again.FxConversion.Scope, "fx_conversion must not alias the stored record")
	require.Equal(t, "usd", again.LineItems[0].FxConversion.ChargeCurrency, "line fx_conversion must not alias")
	require.True(t, decimal.NewFromInt(100).Equal(again.LineItems[0].FxConversion.Source.Subtotal), "line fx_conversion source must not alias")

	custStore := NewInMemoryCustomerStore()
	require.NoError(t, custStore.Create(ctx, &customer.Customer{
		ID: "cust_alias", ExternalID: "cust_alias", Name: "Acme",
		BillingCurrency: lo.ToPtr("inr"), EnvironmentID: "env_test", BaseModel: base,
	}))
	c, err := custStore.Get(ctx, "cust_alias")
	require.NoError(t, err)
	*c.BillingCurrency = "eur"
	c2, err := custStore.Get(ctx, "cust_alias")
	require.NoError(t, err)
	require.Equal(t, "inr", *c2.BillingCurrency, "billing_currency must not alias the stored record")
}
