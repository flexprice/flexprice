package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
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

// amountFn values a commitment's capacity in money.
type amountFn func(capacity decimal.Decimal) decimal.Decimal

// commitmentPeriods returns the subscription billing periods overlapping span, each cut to the commitment's term and
// valued by amountOf. The term ends at the commitment's end_date, or with the subscription when it has none.
func commitmentPeriods(
	c *commitment.Commitment,
	sub *subscription.Subscription,
	amountOf amountFn,
	bucket types.WindowSize,
	span types.Period,
) ([]commitmentPeriod, error) {
	first, err := periodAt(sub, maxTime(span.Start, c.StartDate))
	if err != nil {
		return nil, err
	}

	last, err := periodAt(sub, span.End)
	if err != nil {
		return nil, err
	}

	// The period starting at span.End lies outside span.
	until := last.End
	if !last.Start.Before(span.End) {
		until = span.End
	}

	full, err := types.CalculateBillingPeriods(&types.CalculateBillingPeriodsParams{
		InitialPeriodStart: first.Start,
		EndDate:            &until,
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

	// Periods cut short by the subscription's start or end prorate against the regular period around them. The grid is
	// on whole seconds, like the stored period edges, even under an anchor from before starts were whole seconds.
	grid, err := types.NewBillingPeriodGrid(sub.BillingAnchor.Truncate(time.Second), sub.BillingPeriod, sub.BillingPeriodCount, sub.Timezone)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to compute billing periods for commitment").
			Mark(ierr.ErrInternal)
	}

	termEnd := c.EndDate
	if termEnd == nil {
		termEnd = sub.EndDate
	}

	out := make([]commitmentPeriod, 0, len(full))
	for _, p := range full {
		cut := types.Period{Start: maxTime(p.Start, c.StartDate), End: p.End}
		if termEnd != nil {
			cut.End = minTime(cut.End, *termEnd)
		}

		if !cut.Start.Before(cut.End) {
			continue
		}

		regular := types.FullBillingPeriod(p.Start, grid)
		cp := commitmentPeriod{
			Start:     cut.Start,
			End:       cut.End,
			FullStart: regular.Start,
			FullEnd:   regular.End,
		}
		cp.ActiveBuckets, cp.Capacity = periodCapacity(c, cp, cut, bucket, sub.BillingAnchor)
		cp.Amount = amountOf(cp.Capacity)
		out = append(out, cp)
	}

	return out, nil
}

// clipCommitmentPeriod returns p cut to span, with its capacity and amount recomputed.
// Bounds within the same second as p's edges leave it whole, since computed billing dates drop sub-seconds.
func clipCommitmentPeriod(
	c *commitment.Commitment,
	p commitmentPeriod,
	span types.Period,
	amountOf amountFn,
	bucket types.WindowSize,
	anchor time.Time,
) commitmentPeriod {
	out := p
	if span.Start.Truncate(time.Second).After(p.Start.Truncate(time.Second)) {
		out.Start = minTime(span.Start, p.End)
	}

	if span.End.Truncate(time.Second).Before(p.End.Truncate(time.Second)) {
		out.End = maxTime(span.End, out.Start)
	}

	if out.Start.Equal(p.Start) && out.End.Equal(p.End) {
		return p
	}

	out.Clipped = true
	out.ActiveBuckets, out.Capacity = periodCapacity(c, p, types.Period{Start: out.Start, End: out.End}, bucket, anchor)
	_, before := periodCapacity(c, p, types.Period{Start: p.Start, End: out.Start}, bucket, anchor)
	_, through := periodCapacity(c, p, types.Period{Start: p.Start, End: out.End}, bucket, anchor)
	out.Amount = accruedAmount(amountOf, before, through, c.Currency)
	return out
}

// accruedAmount values the capacity accrued between two cumulative points of a period as the difference of
// their rounded values, so the partial floors of a period sum exactly to its rounded whole.
func accruedAmount(
	amountOf amountFn,
	before, through decimal.Decimal,
	currency string,
) decimal.Decimal {
	amount := types.RoundToCurrencyPrecision(amountOf(through), currency)
	if before.IsPositive() {
		amount = amount.Sub(types.RoundToCurrencyPrecision(amountOf(before), currency))
	}

	return amount
}

// periodCapacity returns the buckets and capacity cp accrues within span: its active buckets for window measurement,
// otherwise its share of the regular billing period.
func periodCapacity(
	c *commitment.Commitment,
	cp commitmentPeriod,
	span types.Period,
	bucket types.WindowSize,
	anchor time.Time,
) (int64, decimal.Decimal) {
	if c.Measurement != types.CommitmentMeasurementWindow {
		fraction := decimal.NewFromInt(int64(span.End.Sub(span.Start))).
			Div(decimal.NewFromInt(int64(cp.FullEnd.Sub(cp.FullStart))))
		return 0, c.Value.Mul(fraction)
	}

	// A bucket starting before the span belongs to the span that already counted it.
	first := truncateToBucketStart(span.Start, bucket, &anchor)
	if first.Before(span.Start) {
		first = nextBucketStart(first, bucket, &anchor)
	}

	var buckets int64
	if dur, fixed := bucketDuration(bucket); fixed && len(c.ActiveWindows) == 0 {
		// Count clock-aligned buckets arithmetically; walking a long term minute by minute is too slow.
		if first.Before(span.End) {
			buckets = int64(span.End.Sub(first)-1)/int64(dur) + 1
		}
	} else {
		loc := c.Location()
		for b := first; b.Before(span.End); b = nextBucketStart(b, bucket, &anchor) {
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
	amountOf amountFn,
	bucket types.WindowSize,
) (*decimal.Decimal, error) {
	if c.EndDate == nil {
		return nil, nil
	}

	periods, err := commitmentPeriods(c, sub, amountOf, bucket, types.Period{Start: c.StartDate, End: *c.EndDate})
	if err != nil {
		return nil, err
	}

	return lo.ToPtr(commitmentValue(periods)), nil
}

// earnedAmount is the commitment's value accrued from its start to at, the period holding at prorated.
func earnedAmount(
	c *commitment.Commitment,
	sub *subscription.Subscription,
	amountOf amountFn,
	bucket types.WindowSize,
	at time.Time,
) (decimal.Decimal, error) {
	if !at.After(c.StartDate) {
		return decimal.Zero, nil
	}

	periods, err := commitmentPeriods(c, sub, amountOf, bucket, types.Period{Start: c.StartDate, End: at})
	if err != nil {
		return decimal.Zero, err
	}

	earned := decimal.Zero
	for _, p := range periods {
		if !p.End.After(at) {
			earned = earned.Add(p.Amount)
			continue
		}

		if p.Start.Before(at) {
			upToAt := types.Period{Start: p.Start, End: at}
			earned = earned.Add(clipCommitmentPeriod(c, p, upToAt, amountOf, bucket, sub.BillingAnchor).Amount)
		}

		break
	}

	return types.RoundToCurrencyPrecision(earned, c.Currency), nil
}

// periodAt returns the subscription billing period holding t, uncut by the subscription end, with the edges
// generating every period from sub.StartDate would give.
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

	// Every later edge is a billing date on the anchor grid, on whole seconds like the stored edges.
	grid, err := types.NewBillingPeriodGrid(sub.BillingAnchor.Truncate(time.Second), sub.BillingPeriod, sub.BillingPeriodCount, sub.Timezone)
	if err != nil {
		return types.Period{}, ierr.WithError(err).
			WithHint("Failed to compute billing periods for commitment").
			Mark(ierr.ErrInternal)
	}

	return types.FullBillingPeriod(t, grid), nil
}

// atPeriodEdge moves t back to the start of the subscription billing period holding it when t falls within that
// period's first second. Period edges are whole seconds, so a date a fraction past one, as a date derived from a
// mid-second subscription start is, means the edge.
func atPeriodEdge(sub *subscription.Subscription, t time.Time) (time.Time, error) {
	holding, err := periodAt(sub, t)
	if err != nil {
		return time.Time{}, err
	}

	if !t.Before(holding.Start) && t.Sub(holding.Start) < time.Second {
		return holding.Start, nil
	}

	return t, nil
}

// alignToPeriodEdges points each set date at its atPeriodEdge value.
func alignToPeriodEdges(sub *subscription.Subscription, dates ...**time.Time) error {
	for _, d := range dates {
		if *d == nil {
			continue
		}

		aligned, err := atPeriodEdge(sub, **d)
		if err != nil {
			return err
		}

		*d = &aligned
	}

	return nil
}

// alignTermDates applies atPeriodEdge to each term's bill_on, start_date and end_date.
func alignTermDates(sub *subscription.Subscription, terms []dto.BillingTermInput) error {
	for i := range terms {
		if err := alignToPeriodEdges(sub, &terms[i].BillOn, &terms[i].StartDate, &terms[i].EndDate); err != nil {
			return err
		}
	}

	return nil
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
	case types.WindowSizeDay, types.WindowSizeWeek, types.WindowSizeMonth:
		return 0, false
	}

	minutes := size.ToMinutes()
	return time.Duration(minutes) * time.Minute, minutes > 0
}
