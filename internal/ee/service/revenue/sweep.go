package revenue

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Drift kinds logged by ReconcileBookedInvoices.
const (
	driftKindMissingFlip    = "missing_flip"    // finalized invoice with no facts booked
	driftKindAmountMismatch = "amount_mismatch" // booked sum diverged from the invoice
	driftKindMissingRevert  = "missing_revert"  // voided invoice whose facts do not net to zero
)

// ReconcileBookedInvoices re-checks every invoice finalized or voided since
// the given time: a finalized invoice's facts must sum to its total, a voided
// one's must net to zero. Mismatches log revenue_facts_drift; repair (rebuild
// the rows from the invoice itself) runs only when
// analytics.revenue_rollup.auto_correct is on. The scan windows on the
// lifecycle timestamps (finalized_at / voided_at), so an old invoice that
// only just finalized is still checked.
func (s *revenueService) ReconcileBookedInvoices(ctx context.Context, since time.Time) (checked, drifted, corrected int, err error) {
	err = s.forEachOptedInEnvironment(ctx, "revenue drift sweep", func(envCtx context.Context) error {
		c, d, cor, envErr := s.reconcileBookedInvoicesForEnvironment(envCtx, since)
		checked += c
		drifted += d
		corrected += cor
		return envErr
	})
	return checked, drifted, corrected, err
}

func (s *revenueService) reconcileBookedInvoicesForEnvironment(ctx context.Context, since time.Time) (checked, drifted, corrected int, err error) {
	// Two passes, each windowed on its own lifecycle timestamp.
	passes := []struct {
		status types.InvoiceStatus
		apply  func(*types.InvoiceFilter)
	}{
		{types.InvoiceStatusFinalized, func(f *types.InvoiceFilter) { f.FinalizedAtGTE = &since }},
		{types.InvoiceStatusVoided, func(f *types.InvoiceFilter) { f.VoidedAtGTE = &since }},
	}

	const batchSize = 500
	for _, pass := range passes {
		offset := 0
		for {
			filter := types.NewInvoiceFilter()
			filter.QueryFilter.Limit = lo.ToPtr(batchSize)
			filter.QueryFilter.Offset = lo.ToPtr(offset)
			filter.InvoiceStatus = []types.InvoiceStatus{pass.status}
			filter.InvoiceType = types.InvoiceTypeSubscription
			pass.apply(filter)

			invoices, listErr := s.InvoiceRepo.List(ctx, filter)
			if listErr != nil {
				return checked, drifted, corrected, listErr
			}

			for _, inv := range invoices {
				checked++
				wasDrifted, wasCorrected := s.reconcileOneInvoice(ctx, inv)
				if wasDrifted {
					drifted++
				}
				if wasCorrected {
					corrected++
				}
			}

			if len(invoices) < batchSize {
				break
			}
			offset += batchSize
		}
	}
	return checked, drifted, corrected, nil
}

// reconcileOneInvoice checks one invoice and, when auto-correct is on,
// repairs it. Errors are logged, never propagated — one bad invoice must not
// abort the run.
func (s *revenueService) reconcileOneInvoice(ctx context.Context, inv *invoice.Invoice) (wasDrifted, wasCorrected bool) {
	rows, err := s.RevenueFactRepo.ListByInvoiceID(ctx, inv.ID)
	if err != nil {
		s.Logger.Error(ctx, "drift sweep failed to list facts for invoice",
			"error", err, "invoice_id", inv.ID)
		return false, false
	}

	// A voided invoice must net to zero; a finalized one to its booked total.
	expected := decimal.Zero
	if inv.InvoiceStatus == types.InvoiceStatusFinalized {
		expected = inv.Subtotal.Sub(inv.TotalDiscount)
	}
	sum := decimal.Zero
	hasReverts := false
	for _, r := range rows {
		sum = sum.Add(r.NetAmount)
		hasReverts = hasReverts || r.IsRevert
	}
	residual := sum.Sub(expected)
	if residual.Abs().LessThanOrEqual(reconcileEpsilon) {
		return false, false
	}

	kind := driftKindAmountMismatch
	switch {
	case inv.InvoiceStatus == types.InvoiceStatusVoided:
		kind = driftKindMissingRevert
	case len(rows) == 0:
		kind = driftKindMissingFlip
	}
	s.Logger.Info(ctx, "revenue_facts_drift",
		"invoice_id", inv.ID,
		"kind", kind,
		"residual", residual.String(),
		"booked", sum.String(),
		"expected", expected.String())

	if s.Config == nil || !s.Config.Analytics.RevenueRollup.AutoCorrect {
		return true, false
	}

	// An invoice already carrying revert rows was corrected once before;
	// RevertByInvoice is a no-op there, so a second repair would double-book.
	// Leave it flagged for a human.
	if kind == driftKindAmountMismatch && hasReverts {
		s.Logger.Info(ctx, "revenue_facts_drift_manual_intervention",
			"invoice_id", inv.ID, "residual", residual.String())
		return true, false
	}

	if _, err := s.RevenueFactRepo.RevertByInvoice(ctx, inv.ID); err != nil {
		s.Logger.Error(ctx, "drift auto-correct revert failed",
			"error", err, "invoice_id", inv.ID)
		return true, false
	}
	if inv.InvoiceStatus == types.InvoiceStatusFinalized {
		if err := s.rollupFromInvoice(ctx, inv); err != nil {
			s.Logger.Error(ctx, "drift auto-correct rollup failed",
				"error", err, "invoice_id", inv.ID)
			return true, false
		}
		if _, _, err := s.flipInvoiceLineItems(ctx, inv); err != nil {
			s.Logger.Error(ctx, "drift auto-correct flip failed",
				"error", err, "invoice_id", inv.ID)
			return true, false
		}
	}

	// Confirm the repair actually reconciled before counting it.
	rows, err = s.RevenueFactRepo.ListByInvoiceID(ctx, inv.ID)
	if err != nil {
		s.Logger.Error(ctx, "drift auto-correct re-check failed",
			"error", err, "invoice_id", inv.ID)
		return true, false
	}
	sum = decimal.Zero
	for _, r := range rows {
		sum = sum.Add(r.NetAmount)
	}
	if sum.Sub(expected).Abs().GreaterThan(reconcileEpsilon) {
		s.Logger.Error(ctx, "drift auto-correct did not reconcile",
			"error", "residual remains after repair",
			"invoice_id", inv.ID,
			"residual", sum.Sub(expected).String())
		return true, false
	}

	s.Logger.Info(ctx, "revenue_facts_drift_corrected", "invoice_id", inv.ID)
	return true, true
}
