package revenue

import (
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// reconcileEpsilon is the decimal tolerance for residual comparisons —
// never compare reconciliation amounts with ==.
var reconcileEpsilon = decimal.RequireFromString("0.000001")

// carriesDecomposition reports whether a row was split into components worth
// checking. A period_only row carries the engine amount whole. So does a
// money-only row — a true-up, or an overage the engine reports as an amount
// with no units behind it — which has no list rate or quantity to check
// against. Everything else claims the identity and is held to it, whatever
// its revenue source: the commitment split maintains it for overage rows too.
func carriesDecomposition(f *revenuefact.RevenueFact) bool {
	if f.DecompositionMode == types.PeriodOnly {
		return false
	}
	return !f.UsageAtListRate.IsZero() || !f.TierDelta.IsZero() ||
		!f.BillableQty.IsZero() || !f.EntitlementQty.IsZero()
}

// reconcileRow checks a decomposed row's NetAmount against its components.
func reconcileRow(f *revenuefact.RevenueFact) (residual decimal.Decimal, ok bool) {
	if !carriesDecomposition(f) {
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
