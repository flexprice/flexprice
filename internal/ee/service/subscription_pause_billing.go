package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// pausedStretch is a part of a window the subscription spent paused.
type pausedStretch struct {
	period types.Period
	// refundable is set when the pause was sent with create_prorations.
	refundable bool
}

// pausedStretches clips every pause overlapping [from, to) to that window, oldest first. A pause
// never runs past the period it started in, and one ended by a cancel runs to that period's end.
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
			period:     types.Period{Start: start, End: end},
			refundable: p.ProrationBehavior == types.ProrationBehaviorCreateProrations,
		})
	}
	sort.Slice(stretches, func(i, j int) bool { return stretches[i].period.Start.Before(stretches[j].period.Start) })
	return stretches
}

// refundablePeriods returns the stretches whose time is taken off fixed charges.
func refundablePeriods(stretches []pausedStretch) []types.Period {
	return lo.FilterMap(stretches, func(s pausedStretch, _ int) (types.Period, bool) {
		return s.period, s.refundable
	})
}

// activeTimeRanges is [from, to) minus the paused periods, which are sorted by start. A window paused
// throughout returns one empty range, so a usage query over it reads nothing instead of the whole window.
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

// creditPausedAdvanceCharges credits the customer's wallet with the refundable paused part of the
// advance fixed charges billed for inv's period, once that period has closed.
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

	pauses, err := s.SubRepo.ListPauses(ctx, *inv.SubscriptionID)
	if err != nil {
		return err
	}
	// The advance charges were billed for the pause's whole period, so the time after a cancel counts too.
	var periodPauses []*subscription.SubscriptionPause
	for _, p := range pauses {
		if p.PauseStart.Before(*inv.PeriodEnd) && p.OriginalPeriodEnd.After(*inv.PeriodStart) {
			periodPauses = append(periodPauses, p)
		}
	}
	if len(periodPauses) == 0 {
		return nil
	}
	billedPeriod := types.Period{Start: periodPauses[0].OriginalPeriodStart, End: periodPauses[0].OriginalPeriodEnd}
	refundable := refundablePeriods(pausedStretches(periodPauses, *inv.PeriodStart, billedPeriod.End))
	if len(refundable) == 0 {
		return nil
	}

	// One credit per subscription per billing period, however many pauses or invoices the period has.
	creditKey := idempotency.NewGenerator().GenerateKey(idempotency.ScopePauseProratedCredit, map[string]interface{}{
		"subscription_id": *inv.SubscriptionID,
		"period_start":    billedPeriod.Start.UTC().Format(time.RFC3339Nano),
	})
	existing, err := s.WalletRepo.GetTransactionByIdempotencyKey(ctx, creditKey)
	if err != nil && !ierr.IsNotFound(err) {
		return err
	}
	if existing != nil {
		return nil
	}

	sub, err := s.SubRepo.Get(ctx, *inv.SubscriptionID)
	if err != nil {
		return err
	}
	lineItemFilter := types.NewNoLimitSubscriptionLineItemFilter()
	lineItemFilter.SubscriptionIDs = []string{sub.ID}
	lineItemFilter.ActiveFilter = true
	lineItemFilter.CurrentPeriodStart = &billedPeriod.Start
	lineItems, err := s.SubscriptionLineItemRepo.List(ctx, lineItemFilter)
	if err != nil {
		return err
	}
	advanceSub := *sub
	advanceSub.LineItems = lo.Filter(lineItems, func(item *subscription.SubscriptionLineItem, _ int) bool {
		return item.PriceType == types.PRICE_TYPE_FIXED && item.InvoiceCadence == types.InvoiceCadenceAdvance
	})

	if len(advanceSub.LineItems) == 0 {
		return nil
	}
	advance, err := NewBillingService(s.ServiceParams).CalculateFixedCharges(ctx, &dto.CalculateFixedChargesParams{
		Subscription: &advanceSub,
		PeriodStart:  billedPeriod.Start,
		PeriodEnd:    billedPeriod.End,
	})
	if err != nil {
		return err
	}
	refundableShare := decimal.NewFromInt(1).Sub(billedFractionOutsidePauses(billedPeriod, refundable))
	creditAmount := types.RoundToCurrencyPrecision(advance.TotalAmount.Mul(refundableShare), inv.Currency)
	if !creditAmount.IsPositive() {
		return nil
	}
	if _, err := NewWalletService(s.ServiceParams).TopUpWalletForProratedCharge(ctx, inv.CustomerID, creditAmount, inv.Currency, creditKey); err != nil {
		return err
	}
	s.Logger.Info(ctx, "credited paused part of advance fixed charges",
		"invoice_id", inv.ID,
		"subscription_id", sub.ID,
		"credit_amount", creditAmount)
	return nil
}
