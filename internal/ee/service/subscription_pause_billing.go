package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// pausedStretch is a part of a window the subscription spent paused.
type pausedStretch struct {
	period types.Period
	pause  *subscription.SubscriptionPause
	// prorated is set when the pause was sent with create_prorations.
	prorated bool
}

// pausedStretches clips every pause overlapping [from, to) to that window, oldest first. A pause
// never runs past the period it started in, and one ended by a cancel runs to that period's end.
// e.g. window [Feb 1, Mar 1), paused Feb 10 and resumed Feb 15: [Feb 10, Feb 15). Not resumed: [Feb 10, Mar 1).
func pausedStretches(pauses []*subscription.SubscriptionPause, from, to time.Time) []pausedStretch {
	stretches := make([]pausedStretch, 0, len(pauses))
	for _, p := range pauses {
		if p.PauseStatus != types.PauseStatusActive && p.PauseStatus != types.PauseStatusCompleted {
			continue
		}
		start := lo.Ternary(p.PauseStart.Before(from), from, p.PauseStart)
		end := p.OriginalPeriodEnd
		if p.ResumedAt != nil && p.ResumedAt.Before(end) {
			end = *p.ResumedAt
		}
		end = lo.Ternary(end.After(to), to, end)
		if !start.Before(end) {
			continue
		}
		stretches = append(stretches, pausedStretch{
			period:   types.Period{Start: start, End: end},
			pause:    p,
			prorated: p.ProrationBehavior == types.ProrationBehaviorCreateProrations,
		})
	}
	sort.Slice(stretches, func(i, j int) bool { return stretches[i].period.Start.Before(stretches[j].period.Start) })
	return stretches
}

// proratedPausedPeriods returns the stretches whose time is taken off fixed charges.
func proratedPausedPeriods(stretches []pausedStretch) []types.Period {
	return lo.FilterMap(stretches, func(s pausedStretch, _ int) (types.Period, bool) {
		return s.period, s.prorated
	})
}

// activeTimeRanges is [from, to) minus the paused periods, which are sorted by start. A window paused
// throughout returns one empty range, so a usage query over it reads nothing instead of the whole window.
// e.g. [Feb 1, Mar 1) paused [Feb 10, Feb 15): [Feb 1, Feb 10) and [Feb 15, Mar 1).
func activeTimeRanges(from, to time.Time, paused []types.Period) []events.TimeRange {
	ranges := make([]events.TimeRange, 0, len(paused)+1)
	cursor := from
	for _, p := range paused {
		if !p.End.After(cursor) || !p.Start.Before(to) {
			continue
		}
		if p.Start.After(cursor) {
			ranges = append(ranges, events.TimeRange{Start: cursor, End: p.Start})
		}
		cursor = p.End
	}
	if to.After(cursor) {
		ranges = append(ranges, events.TimeRange{Start: cursor, End: to})
	}
	if len(ranges) == 0 {
		return []events.TimeRange{{Start: from, End: from}}
	}
	return ranges
}

// billedFractionOutsidePauses is the share of period not covered by the paused periods.
// e.g. [Feb 1, Mar 1) paused [Feb 8, Feb 15): 21/28 = 0.75.
func billedFractionOutsidePauses(period types.Period, paused []types.Period) decimal.Decimal {
	total := period.End.Sub(period.Start)
	if total <= 0 || len(paused) == 0 {
		return decimal.NewFromInt(1)
	}
	pausedTime := pausedDuration(period, paused)
	return decimal.NewFromInt(int64(total - pausedTime)).Div(decimal.NewFromInt(int64(total)))
}

// pausedDuration is how much of period the paused periods cover.
func pausedDuration(period types.Period, paused []types.Period) time.Duration {
	var covered time.Duration
	for _, p := range paused {
		start := lo.Ternary(p.Start.Before(period.Start), period.Start, p.Start)
		end := lo.Ternary(p.End.After(period.End), period.End, p.End)
		if end.After(start) {
			covered += end.Sub(start)
		}
	}
	return covered
}

// creditPausedAdvanceCharges credits the customer's wallet with the paused share of the advance fixed
// charges billed for inv's period, once that period has closed. Each pause is credited once.
func (s *invoiceService) creditPausedAdvanceCharges(ctx context.Context, inv *invoice.Invoice) error {
	reason := types.InvoiceBillingReason(inv.BillingReason)
	if inv.InvoiceType != types.InvoiceTypeSubscription || inv.SubscriptionID == nil ||
		inv.PeriodStart == nil || inv.PeriodEnd == nil ||
		(reason != types.InvoiceBillingReasonSubscriptionCycle && reason != types.InvoiceBillingReasonProration) {
		return nil
	}
	// A draft computed mid-period does not know yet when its pauses end.
	if inv.PeriodEnd.After(clock.Now()) {
		return nil
	}
	billedPeriod := types.Period{Start: *inv.PeriodStart, End: *inv.PeriodEnd}

	pauses, err := s.SubRepo.ListPauses(ctx, *inv.SubscriptionID)
	if err != nil {
		return err
	}
	// A credited pause is left out, so a voided and recomputed invoice does not credit it again.
	uncredited := lo.Reject(pauses, func(p *subscription.SubscriptionPause, _ int) bool { return p.ProratedCreditGranted })
	stretches := lo.Filter(pausedStretches(uncredited, billedPeriod.Start, billedPeriod.End), func(st pausedStretch, _ int) bool {
		return st.prorated
	})
	if len(stretches) == 0 {
		return nil
	}
	creditedPauses := lo.Map(stretches, func(st pausedStretch, _ int) *subscription.SubscriptionPause { return st.pause })

	// The period's advance charges sit on earlier invoices: Feb's fee on Jan's invoice, a mid-Feb upgrade on its proration invoice.
	// TODO: longer-cadence advance charges are not credited past their first month. e.g. a quarterly fee for Jan to Mar sits on
	// December's invoice, which does not touch February, so a February pause gets no credit.
	billedInvoices, err := s.InvoiceRepo.List(ctx, &types.InvoiceFilter{
		QueryFilter:    types.NewNoLimitQueryFilter(),
		SubscriptionID: *inv.SubscriptionID,
		InvoiceType:    types.InvoiceTypeSubscription,
		InvoiceStatus:  []types.InvoiceStatus{types.InvoiceStatusFinalized},
		PeriodStartLTE: &billedPeriod.End,
		PeriodEndGTE:   &billedPeriod.Start,
	})
	if err != nil {
		return err
	}
	credit := decimal.Zero
	for _, billed := range billedInvoices {
		// inv's own fixed lines for this period are arrear, already billed for the active time only.
		if billed.ID == inv.ID {
			continue
		}
		for _, line := range billed.LineItems {
			if lo.FromPtr(line.PriceType) != string(types.PRICE_TYPE_FIXED) || line.PeriodStart == nil || line.PeriodEnd == nil {
				continue
			}
			linePeriod := types.Period{Start: *line.PeriodStart, End: *line.PeriodEnd}
			if !linePeriod.Start.Before(billedPeriod.End) || !linePeriod.End.After(billedPeriod.Start) {
				continue
			}
			// Paused time runs to the line's end, so a pause ended by a cancel also credits the days after the cancel.
			// e.g. $28 charged for [Feb 1, Mar 1), paused Feb 10 and cancelled Feb 20: 28 × 19/28 = $19 credit.
			paused := proratedPausedPeriods(pausedStretches(creditedPauses, billedPeriod.Start, linePeriod.End))
			charged := line.Amount.Sub(line.LineItemDiscount).Sub(line.InvoiceLevelDiscount)
			credit = credit.Add(charged.Mul(decimal.NewFromInt(1).Sub(billedFractionOutsidePauses(linePeriod, paused))))
		}
	}
	creditAmount := types.RoundToCurrencyPrecision(credit, inv.Currency)
	if !creditAmount.IsPositive() {
		return nil
	}

	creditKey := idempotency.NewGenerator().GenerateKey(idempotency.ScopePauseProratedCredit, map[string]interface{}{
		"subscription_id": *inv.SubscriptionID,
		"pause_ids":       lo.Map(creditedPauses, func(p *subscription.SubscriptionPause, _ int) string { return p.ID }),
	})
	if _, err := NewWalletService(s.ServiceParams).TopUpWalletForProratedCharge(ctx, inv.CustomerID, creditAmount, inv.Currency, creditKey); err != nil {
		return err
	}
	for _, p := range creditedPauses {
		p.ProratedCreditGranted = true
		p.UpdatedBy = types.GetUserID(ctx)
		if err := s.SubRepo.UpdatePause(ctx, p); err != nil {
			return err
		}
	}
	s.Logger.Info(ctx, "credited paused part of advance fixed charges",
		"invoice_id", inv.ID,
		"subscription_id", *inv.SubscriptionID,
		"credit_amount", creditAmount)
	return nil
}
