package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// ConvertInvoice rewrites a draft invoice's charge-currency amounts into the billing currency at a
// frozen rate (§5.3). It converts the net once, converts and rounds each line, drops the rounding
// residual on the largest positive line so the lines still sum to the net, stamps each line's
// original amount, and records the fx_conversion snapshot. Tax is not touched here — it is recomputed
// on the converted amounts in the finalize step that follows. inv is mutated in place.
func ConvertInvoice(inv *invoice.Invoice, resolution *FXRateResolution, convertedAt time.Time) error {
	if inv == nil {
		return ierr.NewError("invoice cannot be nil").WithHint("invoice cannot be nil").Mark(ierr.ErrValidation)
	}
	if resolution == nil {
		return ierr.NewError("fx resolution cannot be nil").WithHint("a resolved rate is required").Mark(ierr.ErrValidation)
	}

	chargeCurrency := inv.Currency
	billingCurrency := resolution.To

	// Defensive no-op: nothing to convert when the currencies match.
	if types.IsMatchingCurrency(chargeCurrency, billingCurrency) {
		return nil
	}

	rate := resolution.Rate
	if rate.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("fx rate must be positive").
			WithHint("Cannot convert an invoice with a non-positive rate").
			WithReportableDetails(map[string]any{"rate": rate.String()}).
			Mark(ierr.ErrValidation)
	}

	srcSubtotal := inv.Subtotal
	srcDiscount := inv.TotalDiscount
	srcPrepaid := inv.TotalPrepaidCreditsApplied
	netCharge := srcSubtotal.Sub(srcDiscount).Sub(srcPrepaid)
	netBilling := types.RoundToCurrencyPrecision(netCharge.Mul(rate), billingCurrency)

	convert := func(v decimal.Decimal) decimal.Decimal {
		return types.RoundToCurrencyPrecision(v.Mul(rate), billingCurrency)
	}

	var roundingAdjustment decimal.Decimal
	roundingLineItemID := ""

	if len(inv.LineItems) > 0 {
		for _, li := range inv.LineItems {
			li.OriginalCurrency = lo.ToPtr(li.Currency)
			li.OriginalAmount = lo.ToPtr(li.Amount)
			li.Amount = convert(li.Amount)
			li.LineItemDiscount = convert(li.LineItemDiscount)
			li.InvoiceLevelDiscount = convert(li.InvoiceLevelDiscount)
			li.PrepaidCreditsApplied = convert(li.PrepaidCreditsApplied)
			li.Currency = billingCurrency
		}

		netFromLines := decimal.Zero
		for _, li := range inv.LineItems {
			netFromLines = netFromLines.Add(li.Amount).
				Sub(li.LineItemDiscount).Sub(li.InvoiceLevelDiscount).Sub(li.PrepaidCreditsApplied)
		}

		target := residualLine(inv.LineItems)
		roundingAdjustment = netBilling.Sub(netFromLines)
		target.Amount = target.Amount.Add(roundingAdjustment)
		roundingLineItemID = target.ID

		inv.Subtotal = decimal.Zero
		inv.TotalDiscount = decimal.Zero
		inv.TotalPrepaidCreditsApplied = decimal.Zero
		for _, li := range inv.LineItems {
			inv.Subtotal = inv.Subtotal.Add(li.Amount)
			inv.TotalDiscount = inv.TotalDiscount.Add(li.LineItemDiscount).Add(li.InvoiceLevelDiscount)
			inv.TotalPrepaidCreditsApplied = inv.TotalPrepaidCreditsApplied.Add(li.PrepaidCreditsApplied)
		}
	} else {
		inv.Subtotal = convert(srcSubtotal)
		inv.TotalDiscount = convert(srcDiscount)
		inv.TotalPrepaidCreditsApplied = convert(srcPrepaid)
	}

	// Pre-tax total; tax is added on the converted amounts in the finalize step that follows.
	inv.Total = inv.Subtotal.Sub(inv.TotalDiscount).Sub(inv.TotalPrepaidCreditsApplied)
	inv.AmountDue = inv.Total
	inv.AmountRemaining = inv.Total // amount_paid is 0 when converting (§8.4)
	inv.Currency = billingCurrency

	inv.FxConversion = &types.FxConversion{
		ChargeCurrency:  chargeCurrency,
		BillingCurrency: billingCurrency,
		Rate:            rate,
		RateID:          resolution.RateID,
		Scope:           resolution.Scope,
		ConvertedAt:     convertedAt,
		Source: types.FxConversionSource{
			Subtotal:                   srcSubtotal,
			TotalDiscount:              srcDiscount,
			TotalPrepaidCreditsApplied: srcPrepaid,
			Net:                        netCharge,
		},
		RoundingAdjustment: roundingAdjustment,
		RoundingLineItemID: roundingLineItemID,
	}

	return nil
}

// residualLine picks the line that absorbs the rounding residual: the largest positive amount,
// falling back to the largest by absolute value; ties break on the lowest line id (§5.3).
func residualLine(lines []*invoice.InvoiceLineItem) *invoice.InvoiceLineItem {
	var best *invoice.InvoiceLineItem
	for _, li := range lines {
		if best == nil {
			best = li
			continue
		}
		if betterResidualCandidate(li, best) {
			best = li
		}
	}
	return best
}

func betterResidualCandidate(candidate, best *invoice.InvoiceLineItem) bool {
	cPos := candidate.Amount.IsPositive()
	bPos := best.Amount.IsPositive()

	// A positive line always beats a non-positive one.
	if cPos != bPos {
		return cPos
	}

	if cPos {
		// Both positive: larger amount wins, lower id breaks ties.
		if candidate.Amount.Equal(best.Amount) {
			return candidate.ID < best.ID
		}
		return candidate.Amount.GreaterThan(best.Amount)
	}

	// Neither positive: larger absolute value wins, lower id breaks ties.
	cAbs := candidate.Amount.Abs()
	bAbs := best.Amount.Abs()
	if cAbs.Equal(bAbs) {
		return candidate.ID < best.ID
	}
	return cAbs.GreaterThan(bAbs)
}
