package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func convLine(id, currency, amount string) *invoice.InvoiceLineItem {
	return &invoice.InvoiceLineItem{
		ID:       id,
		Currency: currency,
		Amount:   dec(amount),
		Quantity: decimal.NewFromInt(1),
	}
}

// A rate too small for the billing currency's precision would turn a real charge into a zero
// invoice. That must fail rather than finalize a free invoice.
func TestConvertInvoice_NonZeroNetToZeroRejected(t *testing.T) {
	inv := &invoice.Invoice{
		ID:        "inv_tiny",
		Currency:  "usd",
		Subtotal:  dec("1"),
		Total:     dec("1"),
		AmountDue: dec("1"),
		LineItems: []*invoice.InvoiceLineItem{convLine("il_1", "usd", "1")},
	}
	res := &FXRateResolution{Rate: dec("0.001"), To: "jpy", RateID: "fxr_x", Scope: string(types.FXRateScopeTenant)}

	err := ConvertInvoice(inv, res, time.Now().UTC())

	require.Error(t, err)
	require.Equal(t, "usd", inv.Currency, "a rejected conversion must leave the invoice untouched")
	require.Nil(t, inv.FxConversion)
}

// TestConvertInvoice covers §5.3 conversion + rounding: net converted once, each line converted
// and rounded, residual to the largest positive line, originals stamped, fx_conversion recorded.
func TestConvertInvoice(t *testing.T) {
	convertedAt := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

	type lineExp struct {
		id      string
		amount  string
		origCy  string
		origAmt string
	}

	cases := []struct {
		name           string
		charge         string
		billing        string
		rate           string
		subtotal       string
		totalDiscount  string
		totalPrepaid   string
		lines          []*invoice.InvoiceLineItem
		wantSubtotal   string
		wantTotal      string
		wantAmountDue  string
		wantRounding   string
		wantRoundingID string
		wantLines      []lineExp
	}{
		{
			name:           "single line 2dp exact",
			charge:         "usd",
			billing:        "inr",
			rate:           "83.50",
			subtotal:       "100",
			totalDiscount:  "0",
			totalPrepaid:   "0",
			lines:          []*invoice.InvoiceLineItem{convLine("il_1", "usd", "100")},
			wantSubtotal:   "8350",
			wantTotal:      "8350",
			wantAmountDue:  "8350",
			wantRounding:   "0",
			wantRoundingID: "il_1",
			wantLines: []lineExp{
				{id: "il_1", amount: "8350", origCy: "usd", origAmt: "100"},
			},
		},
		{
			name:          "jpy three lines residual to largest positive (ERD example)",
			charge:        "usd",
			billing:       "jpy",
			rate:          "149.37",
			subtotal:      "100",
			totalDiscount: "0",
			totalPrepaid:  "0",
			lines: []*invoice.InvoiceLineItem{
				convLine("il_a", "usd", "33.33"),
				convLine("il_b", "usd", "33.33"),
				convLine("il_c", "usd", "33.34"),
			},
			wantSubtotal:   "14937",
			wantTotal:      "14937",
			wantAmountDue:  "14937",
			wantRounding:   "-1",
			wantRoundingID: "il_c",
			wantLines: []lineExp{
				{id: "il_a", amount: "4979", origCy: "usd", origAmt: "33.33"},
				{id: "il_b", amount: "4979", origCy: "usd", origAmt: "33.33"},
				{id: "il_c", amount: "4979", origCy: "usd", origAmt: "33.34"},
			},
		},
		{
			name:          "negative proration line never absorbs residual",
			charge:        "usd",
			billing:       "jpy",
			rate:          "149.37",
			subtotal:      "66.66", // 100.00 charge minus a 33.34 credit line
			totalDiscount: "0",
			totalPrepaid:  "0",
			lines: []*invoice.InvoiceLineItem{
				convLine("il_pos", "usd", "100"),
				convLine("il_neg", "usd", "-33.34"),
			},
			// net = 66.66 * 149.37 = 9956.808 -> 9957
			// line pos 100*149.37=14937; line neg -33.34*149.37=-4980.0... -> -4980
			// sum = 14937 - 4980 = 9957 -> residual 0, but assert it never lands on il_neg
			wantSubtotal:   "9957",
			wantTotal:      "9957",
			wantAmountDue:  "9957",
			wantRounding:   "0",
			wantRoundingID: "il_pos",
			wantLines: []lineExp{
				{id: "il_pos", amount: "14937", origCy: "usd", origAmt: "100"},
				{id: "il_neg", amount: "-4980", origCy: "usd", origAmt: "-33.34"},
			},
		},
		{
			name:          "discount and prepaid credits converted, net drives total",
			charge:        "usd",
			billing:       "inr",
			rate:          "80",
			subtotal:      "100",
			totalDiscount: "10",
			totalPrepaid:  "20",
			lines: []*invoice.InvoiceLineItem{
				func() *invoice.InvoiceLineItem {
					li := convLine("il_1", "usd", "100")
					li.LineItemDiscount = dec("10")
					li.PrepaidCreditsApplied = dec("20")
					return li
				}(),
			},
			// net = (100-10-20)=70 *80 = 5600
			wantSubtotal:   "8000",
			wantTotal:      "5600",
			wantAmountDue:  "5600",
			wantRounding:   "0",
			wantRoundingID: "il_1",
			wantLines: []lineExp{
				{id: "il_1", amount: "8000", origCy: "usd", origAmt: "100"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv := &invoice.Invoice{
				ID:                         "inv_1",
				Currency:                   c.charge,
				Subtotal:                   dec(c.subtotal),
				TotalDiscount:              dec(c.totalDiscount),
				TotalPrepaidCreditsApplied: dec(c.totalPrepaid),
				LineItems:                  c.lines,
			}
			res := &FXRateResolution{
				Rate:   dec(c.rate),
				RateID: "fxr_x",
				Scope:  string(types.FXRateScopeTenant),
				From:   c.charge,
				To:     c.billing,
			}

			require.NoError(t, ConvertInvoice(inv, res, convertedAt))

			require.Equal(t, c.billing, inv.Currency, "invoice currency becomes billing")
			require.True(t, dec(c.wantSubtotal).Equal(inv.Subtotal), "subtotal: want %s got %s", c.wantSubtotal, inv.Subtotal)
			require.True(t, dec(c.wantTotal).Equal(inv.Total), "total: want %s got %s", c.wantTotal, inv.Total)
			require.True(t, dec(c.wantAmountDue).Equal(inv.AmountDue), "amount_due: want %s got %s", c.wantAmountDue, inv.AmountDue)

			require.NotNil(t, inv.FxConversion)
			fx := inv.FxConversion
			require.Equal(t, c.charge, fx.ChargeCurrency)
			require.Equal(t, c.billing, fx.BillingCurrency)
			require.True(t, dec(c.rate).Equal(fx.Rate))
			require.Equal(t, "fxr_x", fx.RateID)
			require.Equal(t, convertedAt, fx.ConvertedAt)
			require.True(t, dec(c.wantRounding).Equal(fx.RoundingAdjustment), "rounding: want %s got %s", c.wantRounding, fx.RoundingAdjustment)
			require.Equal(t, c.wantRoundingID, fx.RoundingLineItemID)

			// Source snapshot is the pre-conversion charge amounts.
			require.True(t, dec(c.subtotal).Equal(fx.Source.Subtotal))
			require.True(t, dec(c.totalDiscount).Equal(fx.Source.TotalDiscount))
			require.True(t, dec(c.totalPrepaid).Equal(fx.Source.TotalPrepaidCreditsApplied))
			require.True(t, dec(c.subtotal).Sub(dec(c.totalDiscount)).Sub(dec(c.totalPrepaid)).Equal(fx.Source.Net))

			// Lines add up to the net (the PDF invariant).
			netFromLines := decimal.Zero
			for _, li := range inv.LineItems {
				netFromLines = netFromLines.Add(li.Amount).
					Sub(li.LineItemDiscount).Sub(li.InvoiceLevelDiscount).Sub(li.PrepaidCreditsApplied)
			}
			require.True(t, dec(c.wantTotal).Equal(netFromLines), "lines must sum to net: want %s got %s", c.wantTotal, netFromLines)

			byID := lo.SliceToMap(inv.LineItems, func(li *invoice.InvoiceLineItem) (string, *invoice.InvoiceLineItem) {
				return li.ID, li
			})
			for _, wl := range c.wantLines {
				got := byID[wl.id]
				require.NotNil(t, got, "line %s", wl.id)
				require.True(t, dec(wl.amount).Equal(got.Amount), "line %s amount: want %s got %s", wl.id, wl.amount, got.Amount)
				require.Equal(t, c.billing, got.Currency, "line %s currency", wl.id)
				require.NotNil(t, got.OriginalCurrency)
				require.Equal(t, wl.origCy, *got.OriginalCurrency)
				require.NotNil(t, got.OriginalAmount)
				require.True(t, dec(wl.origAmt).Equal(*got.OriginalAmount), "line %s original: want %s got %s", wl.id, wl.origAmt, got.OriginalAmount)
			}
		})
	}
}

// TestConvertInvoice_TieBreakLowestID: two positive lines of equal amount split a residual to the
// lowest line id, for determinism.
func TestConvertInvoice_TieBreakLowestID(t *testing.T) {
	inv := &invoice.Invoice{
		ID:                         "inv_tie",
		Currency:                   "usd",
		Subtotal:                   dec("2"),
		TotalDiscount:              decimal.Zero,
		TotalPrepaidCreditsApplied: decimal.Zero,
		LineItems: []*invoice.InvoiceLineItem{
			convLine("il_b", "usd", "1"),
			convLine("il_a", "usd", "1"),
		},
	}
	res := &FXRateResolution{Rate: dec("149.37"), RateID: "r", Scope: "tenant", From: "usd", To: "jpy"}
	require.NoError(t, ConvertInvoice(inv, res, time.Now()))

	// net = 2 * 149.37 = 298.74 -> 299; each line 1*149.37=149.37->149; sum 298; residual +1 to lowest id (il_a)
	require.Equal(t, "il_a", inv.FxConversion.RoundingLineItemID)
	require.True(t, dec("1").Equal(inv.FxConversion.RoundingAdjustment))
	byID := lo.SliceToMap(inv.LineItems, func(li *invoice.InvoiceLineItem) (string, *invoice.InvoiceLineItem) { return li.ID, li })
	require.True(t, dec("150").Equal(byID["il_a"].Amount))
	require.True(t, dec("149").Equal(byID["il_b"].Amount))
}

// TestConvertInvoice_NoLineItems converts invoice-level totals directly with no rounding line.
func TestConvertInvoice_NoLineItems(t *testing.T) {
	inv := &invoice.Invoice{
		ID:                         "inv_nolines",
		Currency:                   "usd",
		Subtotal:                   dec("100"),
		TotalDiscount:              dec("0"),
		TotalPrepaidCreditsApplied: dec("0"),
	}
	res := &FXRateResolution{Rate: dec("83.50"), RateID: "r", Scope: "tenant", From: "usd", To: "inr"}
	require.NoError(t, ConvertInvoice(inv, res, time.Now()))
	require.True(t, dec("8350").Equal(inv.Subtotal))
	require.True(t, dec("8350").Equal(inv.Total))
	require.Equal(t, "", inv.FxConversion.RoundingLineItemID)
	require.True(t, decimal.Zero.Equal(inv.FxConversion.RoundingAdjustment))
}

// TestConvertInvoice_IdentityNoOp: a same-currency resolution leaves the invoice untouched.
func TestConvertInvoice_IdentityNoOp(t *testing.T) {
	inv := &invoice.Invoice{
		ID:        "inv_id",
		Currency:  "usd",
		Subtotal:  dec("100"),
		Total:     dec("100"),
		AmountDue: dec("100"),
		LineItems: []*invoice.InvoiceLineItem{convLine("il_1", "usd", "100")},
	}
	res := &FXRateResolution{Rate: decimal.NewFromInt(1), Scope: "identity", From: "usd", To: "usd"}
	require.NoError(t, ConvertInvoice(inv, res, time.Now()))
	require.Nil(t, inv.FxConversion, "identity must not write fx_conversion")
	require.True(t, dec("100").Equal(inv.Subtotal))
	require.Nil(t, inv.LineItems[0].OriginalCurrency)
}
