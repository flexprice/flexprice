package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Drift kinds logged by SweepDrift.
const (
	driftKindMissingFlip    = "missing_flip"    // finalized invoice with no facts booked
	driftKindAmountMismatch = "amount_mismatch" // booked sum diverged from the invoice
	driftKindMissingRevert  = "missing_revert"  // voided invoice whose facts do not net to zero
)

// SweepDrift compares what each recently finalized or voided invoice should
// have booked against its stamped facts. Drift is always logged
// (revenue_facts_drift); repair runs only when analytics.revenue_rollup.
// auto_correct is on, using the same revert / derive-from-invoice / flip
// primitives as the lifecycle hooks. The scan windows on invoice created_at,
// so deep backfills use the workflow's explicit Since input.
func (s *revenueService) SweepDrift(ctx context.Context, since time.Time) (checked, drifted, corrected int, err error) {
	err = s.forEachOptedInEnvironment(ctx, "revenue drift sweep", func(envCtx context.Context) error {
		c, d, cor, envErr := s.sweepDriftForEnvironment(envCtx, since)
		checked += c
		drifted += d
		corrected += cor
		return envErr
	})
	return checked, drifted, corrected, err
}

func (s *revenueService) sweepDriftForEnvironment(ctx context.Context, since time.Time) (checked, drifted, corrected int, err error) {
	const batchSize = 500
	offset := 0

	for {
		filter := types.NewInvoiceFilter()
		filter.QueryFilter.Limit = lo.ToPtr(batchSize)
		filter.QueryFilter.Offset = lo.ToPtr(offset)
		filter.TimeRangeFilter = &types.TimeRangeFilter{StartTime: &since}
		filter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusFinalized, types.InvoiceStatusVoided}
		filter.InvoiceType = types.InvoiceTypeSubscription

		invoices, listErr := s.InvoiceRepo.List(ctx, filter)
		if listErr != nil {
			return checked, drifted, corrected, listErr
		}
		if len(invoices) == 0 {
			return checked, drifted, corrected, nil
		}

		for _, inv := range invoices {
			checked++
			wasDrifted, wasCorrected := s.sweepInvoice(ctx, inv)
			if wasDrifted {
				drifted++
			}
			if wasCorrected {
				corrected++
			}
		}

		if len(invoices) < batchSize {
			return checked, drifted, corrected, nil
		}
		offset += batchSize
	}
}

// sweepInvoice checks one invoice and, when auto-correct is on, repairs it.
// Errors are logged, never propagated — one invoice must not abort the sweep.
func (s *revenueService) sweepInvoice(ctx context.Context, inv *invoice.Invoice) (wasDrifted, wasCorrected bool) {
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
