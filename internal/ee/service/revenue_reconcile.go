package service

import (
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// reconcileEpsilon is the decimal tolerance for residual comparisons —
// never compare reconciliation amounts with ==.
var reconcileEpsilon = decimal.RequireFromString("0.000001")

// reconcileRow checks a marginal USAGE row's NetAmount against its decomposed
// components. Non-usage and period_only rows carry the engine amount whole,
// with nothing decomposed to check, so they always report ok.
func reconcileRow(f *revenuefact.RevenueFact) (residual decimal.Decimal, ok bool) {
	if f.RevenueSource != types.RevenueSourceUsage || f.DecompositionMode == types.PeriodOnly {
		return decimal.Zero, true
	}

	expected := f.UsageAtListRate.
		Add(f.TierDelta).
		Sub(f.EntitlementAmount).
		Sub(f.LineDiscount).
		Sub(f.InvoiceDiscount)

	residual = f.NetAmount.Sub(expected)
	return residual, residual.Abs().LessThanOrEqual(reconcileEpsilon)
}

// reconcileLineItem checks that a line item's decomposed rows sum to the
// billing engine's computed amount for that line item.
func reconcileLineItem(rows []*revenuefact.RevenueFact, engineAmount decimal.Decimal) (residual decimal.Decimal, ok bool) {
	sum := decimal.Zero
	for _, r := range rows {
		sum = sum.Add(r.NetAmount)
	}

	residual = sum.Sub(engineAmount)
	return residual, residual.Abs().LessThanOrEqual(reconcileEpsilon)
}

// reconcileInvoice checks that all of an invoice's decomposed rows sum to
// the invoice's subtotal minus discounts.
func reconcileInvoice(rows []*revenuefact.RevenueFact, subtotalMinusDiscount decimal.Decimal) (residual decimal.Decimal, ok bool) {
	sum := decimal.Zero
	for _, r := range rows {
		sum = sum.Add(r.NetAmount)
	}

	residual = sum.Sub(subtotalMinusDiscount)
	return residual, residual.Abs().LessThanOrEqual(reconcileEpsilon)
}
