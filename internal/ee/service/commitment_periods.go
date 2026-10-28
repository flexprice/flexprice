package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
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

// commitmentPeriods returns the subscription billing periods clipped to the commitment term, with their value;
// amountOf turns a period's capacity into money.
func commitmentPeriods(
	c *commitment.Commitment,
	sub *subscription.Subscription,
	amountOf func(capacity decimal.Decimal) decimal.Decimal,
	bucket types.WindowSize,
) ([]commitmentPeriod, error) {
	full, err := types.CalculateBillingPeriods(&types.CalculateBillingPeriodsParams{
		InitialPeriodStart: sub.StartDate,
		EndDate:            &c.EndDate,
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
		start, end := maxTime(p.Start, c.StartDate), minTime(p.End, c.EndDate)
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
