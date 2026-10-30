package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// commitmentPeriod is one subscription billing period clipped to the commitment term.
type commitmentPeriod struct {
	Start         time.Time
	End           time.Time
	FullStart     time.Time // the uncut billing period, used for proration
	FullEnd       time.Time
	ActiveBuckets int64 // window measurement only
	Capacity      decimal.Decimal
	Amount        decimal.Decimal // money value of Capacity
	Clipped       bool            // cut to part of the period by the invoice window or a termination
}

// commitmentPeriods returns the billing periods clipped to the commitment term from the one holding `from`, valued
// by amountOf; with no end_date they run through the period holding `through`.
func commitmentPeriods(
	c *commitment.Commitment,
	sub *subscription.Subscription,
	amountOf func(capacity decimal.Decimal) decimal.Decimal,
	bucket types.WindowSize,
	from time.Time,
	through time.Time,
) ([]commitmentPeriod, error) {
	horizon := lo.FromPtr(c.EndDate)
	if c.EndDate == nil {
		var err error
		if horizon, err = periodEndAt(sub, through); err != nil {
			return nil, err
		}

		// The subscription's own end bounds an open-ended commitment: no floor accrues past it.
		if sub.EndDate != nil {
			horizon = minTime(horizon, *sub.EndDate)
		}
	}

	first, err := periodAt(sub, maxTime(from, c.StartDate))
	if err != nil {
		return nil, err
	}

	full, err := types.CalculateBillingPeriods(&types.CalculateBillingPeriodsParams{
		InitialPeriodStart: first.Start,
		EndDate:            &horizon,
		Anchor:             sub.BillingAnchor,
		PeriodCount:        sub.BillingPeriodCount,
		BillingPeriod:      sub.BillingPeriod,
		Timezone:           sub.Timezone,
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to compute billing periods for commitment").
			Mark(ierr.ErrInternal)
	}

	// Periods cut short by the subscription's start or end prorate against the regular period around them.
	grid, err := types.NewBillingPeriodGrid(sub.BillingAnchor, sub.BillingPeriod, sub.BillingPeriodCount, sub.Timezone)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to compute billing periods for commitment").
			Mark(ierr.ErrInternal)
	}

	out := make([]commitmentPeriod, 0, len(full))
	for _, p := range full {
		start, end := maxTime(p.Start, c.StartDate), minTime(p.End, horizon)
		if !start.Before(end) {
			continue
		}

		regular := types.FullBillingPeriod(p.Start, grid)
		cp := commitmentPeriod{
			Start:     start,
			End:       end,
			FullStart: regular.Start,
			FullEnd:   regular.End,
		}
		cp.ActiveBuckets, cp.Capacity = periodCapacity(c, cp, start, end, bucket, sub.BillingAnchor)
		cp.Amount = amountOf(cp.Capacity)
		out = append(out, cp)
	}

	return out, nil
}

// clipCommitmentPeriod returns p cut to [from, until), with its capacity and amount recomputed.
// Bounds within the same second as p's edges leave it whole, since computed billing dates drop sub-seconds.
func clipCommitmentPeriod(
	c *commitment.Commitment,
	p commitmentPeriod,
	from, until time.Time,
	amountOf func(capacity decimal.Decimal) decimal.Decimal,
	bucket types.WindowSize,
	anchor time.Time,
) commitmentPeriod {
	out := p
	if from.Truncate(time.Second).After(p.Start.Truncate(time.Second)) {
		out.Start = minTime(from, p.End)
	}

	if until.Truncate(time.Second).Before(p.End.Truncate(time.Second)) {
		out.End = maxTime(until, out.Start)
	}

	if out.Start.Equal(p.Start) && out.End.Equal(p.End) {
		return p
	}

	out.Clipped = true
	out.ActiveBuckets, out.Capacity = periodCapacity(c, p, out.Start, out.End, bucket, anchor)
	_, before := periodCapacity(c, p, p.Start, out.Start, bucket, anchor)
	_, through := periodCapacity(c, p, p.Start, out.End, bucket, anchor)
	out.Amount = accruedAmount(amountOf, before, through, c.Currency)
	return out
}

// accruedAmount values the capacity accrued between two cumulative points of a period as the difference of
// their rounded values, so the partial floors of a period sum exactly to its rounded whole.
func accruedAmount(
	amountOf func(capacity decimal.Decimal) decimal.Decimal,
	before, through decimal.Decimal,
	currency string,
) decimal.Decimal {
	amount := types.RoundToCurrencyPrecision(amountOf(through), currency)
	if before.IsPositive() {
		amount = amount.Sub(types.RoundToCurrencyPrecision(amountOf(before), currency))
	}

	return amount
}

// periodCapacity returns the buckets and capacity cp accrues in [from, to): its active buckets for window
// measurement, otherwise its share of the regular billing period.
func periodCapacity(
	c *commitment.Commitment,
	cp commitmentPeriod,
	from, to time.Time,
	bucket types.WindowSize,
	anchor time.Time,
) (int64, decimal.Decimal) {
	if c.Measurement != types.CommitmentMeasurementWindow {
		fraction := decimal.NewFromInt(int64(to.Sub(from))).Div(decimal.NewFromInt(int64(cp.FullEnd.Sub(cp.FullStart))))
		return 0, c.Value.Mul(fraction)
	}

	// A bucket starting before the span belongs to the span that already counted it.
	first := truncateToBucketStart(from, bucket, &anchor)
	if first.Before(from) {
		first = nextBucketStart(first, bucket, &anchor)
	}

	var buckets int64
	if dur, fixed := bucketDuration(bucket); fixed && len(c.ActiveWindows) == 0 {
		// Clock-aligned buckets with no active windows count arithmetically: a minute-bucket
		// year would otherwise walk half a million instants on every value computation.
		if first.Before(to) {
			buckets = int64(to.Sub(first)-1)/int64(dur) + 1
		}
	} else {
		loc := c.Location()
		for b := first; b.Before(to); b = nextBucketStart(b, bucket, &anchor) {
			if c.ActiveWindows.Contains(b, loc) {
				buckets++
			}
		}
	}

	return buckets, c.Value.Mul(decimal.NewFromInt(buckets))
}

// commitmentValue sums the money value of all periods.
func commitmentValue(periods []commitmentPeriod) decimal.Decimal {
	total := decimal.Zero
	for _, p := range periods {
		total = total.Add(p.Amount)
	}

	return total
}

// termValue values the commitment's whole term, or returns nil for an open-ended commitment, which has no
// finite value.
func termValue(
	c *commitment.Commitment,
	sub *subscription.Subscription,
	amountOf func(capacity decimal.Decimal) decimal.Decimal,
	bucket types.WindowSize,
) (*decimal.Decimal, error) {
	if c.EndDate == nil {
		return nil, nil
	}

	periods, err := commitmentPeriods(c, sub, amountOf, bucket, c.StartDate, *c.EndDate)
	if err != nil {
		return nil, err
	}

	return lo.ToPtr(commitmentValue(periods)), nil
}

// periodAt returns the subscription billing period [Start, End) holding t, uncut by the subscription
// end, with the edges generating every period from sub.StartDate gives, without generating the ones before it.
func periodAt(sub *subscription.Subscription, t time.Time) (types.Period, error) {
	firstEnd, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: sub.StartDate,
		BillingAnchor:      sub.BillingAnchor,
		Unit:               sub.BillingPeriodCount,
		Period:             sub.BillingPeriod,
		Timezone:           sub.Timezone,
	})
	if err != nil {
		return types.Period{}, ierr.WithError(err).
			WithHint("Failed to compute billing periods for commitment").
			Mark(ierr.ErrInternal)
	}

	if t.Before(firstEnd) {
		return types.Period{Start: sub.StartDate, End: firstEnd}, nil
	}

	// Multi-day and multi-week cadences step from the first period's end, not along the anchor grid: walk them.
	if (sub.BillingPeriod == types.BILLING_PERIOD_DAILY || sub.BillingPeriod == types.BILLING_PERIOD_WEEKLY) &&
		sub.BillingPeriodCount > 1 {
		return walkToPeriodAt(sub, t)
	}

	// Every later edge is a billing date on the anchor grid.
	grid, err := types.NewBillingPeriodGrid(sub.BillingAnchor, sub.BillingPeriod, sub.BillingPeriodCount, sub.Timezone)
	if err != nil {
		return types.Period{}, ierr.WithError(err).
			WithHint("Failed to compute billing periods for commitment").
			Mark(ierr.ErrInternal)
	}

	return types.FullBillingPeriod(t, grid), nil
}

// periodEndAt returns the end of the subscription billing period holding t, or t itself past the
// subscription's last period.
func periodEndAt(sub *subscription.Subscription, t time.Time) (time.Time, error) {
	holding, err := periodAt(sub, t)
	if err != nil {
		return time.Time{}, err
	}

	// t on a period edge closes the period before it.
	if holding.Start.Equal(t) && t.After(sub.StartDate) {
		return t, nil
	}

	// Run the periods one billing date past t, so the period holding it is not cut at t.
	horizon, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart:  t,
		BillingAnchor:       sub.BillingAnchor,
		Unit:                sub.BillingPeriodCount,
		Period:              sub.BillingPeriod,
		SubscriptionEndDate: sub.EndDate,
		Timezone:            sub.Timezone,
	})
	if err != nil {
		return time.Time{}, ierr.WithError(err).
			WithHint("Failed to compute billing periods for commitment").
			Mark(ierr.ErrInternal)
	}

	periods, err := types.CalculateBillingPeriods(&types.CalculateBillingPeriodsParams{
		InitialPeriodStart: holding.Start,
		EndDate:            &horizon,
		Anchor:             sub.BillingAnchor,
		PeriodCount:        sub.BillingPeriodCount,
		BillingPeriod:      sub.BillingPeriod,
		Timezone:           sub.Timezone,
	})
	if err != nil {
		return time.Time{}, ierr.WithError(err).
			WithHint("Failed to compute billing periods for commitment").
			Mark(ierr.ErrInternal)
	}

	for _, p := range periods {
		if !p.End.Before(t) {
			return p.End, nil
		}
	}

	return t, nil
}

// walkToPeriodAt generates the subscription's billing periods from its start up to the one holding t.
func walkToPeriodAt(sub *subscription.Subscription, t time.Time) (types.Period, error) {
	horizon, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: t,
		BillingAnchor:      sub.BillingAnchor,
		Unit:               sub.BillingPeriodCount,
		Period:             sub.BillingPeriod,
		Timezone:           sub.Timezone,
	})
	if err != nil {
		return types.Period{}, ierr.WithError(err).
			WithHint("Failed to compute billing periods for commitment").
			Mark(ierr.ErrInternal)
	}

	periods, err := types.CalculateBillingPeriods(&types.CalculateBillingPeriodsParams{
		InitialPeriodStart: sub.StartDate,
		EndDate:            &horizon,
		Anchor:             sub.BillingAnchor,
		PeriodCount:        sub.BillingPeriodCount,
		BillingPeriod:      sub.BillingPeriod,
		Timezone:           sub.Timezone,
	})
	if err != nil {
		return types.Period{}, ierr.WithError(err).
			WithHint("Failed to compute billing periods for commitment").
			Mark(ierr.ErrInternal)
	}

	for _, p := range periods {
		if p.End.After(t) {
			return p, nil
		}
	}

	return periods[len(periods)-1], nil
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}

	return b
}

// bucketDuration returns the fixed step of clock-aligned bucket sizes; calendar sizes (day, week,
// month) step by calendar rules and return false.
func bucketDuration(size types.WindowSize) (time.Duration, bool) {
	switch size {
	case types.WindowSizeMinute:
		return time.Minute, true
	case types.WindowSize15Min:
		return 15 * time.Minute, true
	case types.WindowSize30Min:
		return 30 * time.Minute, true
	case types.WindowSizeHour:
		return time.Hour, true
	case types.WindowSize3Hour:
		return 3 * time.Hour, true
	case types.WindowSize6Hour:
		return 6 * time.Hour, true
	case types.WindowSize12Hour:
		return 12 * time.Hour, true
	default:
		return 0, false
	}
}
