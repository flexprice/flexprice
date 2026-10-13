package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// commitmentRates carries the per-unit rates resolved from the commitment's prices.
type commitmentRates struct {
	Committed decimal.Decimal  // quantity: committed price amount; amount: 1
	Overage   *decimal.Decimal // nil = line-item price
}

// commitmentPeriod is one subscription billing period clipped to the commitment term.
type commitmentPeriod struct {
	Start, End         time.Time
	FullStart, FullEnd time.Time // the uncut billing period, used for proration
	ActiveBuckets      int64     // window measurement only
	Capacity           decimal.Decimal
	Amount             decimal.Decimal // money value of Capacity
}

// commitmentPeriods returns the subscription billing periods clipped to the commitment term, with their value.
func commitmentPeriods(c *commitment.Commitment, sub *subscription.Subscription, rates commitmentRates, bucket types.WindowSize) ([]commitmentPeriod, error) {
	full, err := types.CalculateBillingPeriods(&types.CalculateBillingPeriodsParams{
		InitialPeriodStart: sub.StartDate,
		EndDate:            &c.EndDate,
		Anchor:             sub.BillingAnchor,
		PeriodCount:        sub.BillingPeriodCount,
		BillingPeriod:      sub.BillingPeriod,
		Timezone:           sub.Timezone,
	})
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to compute billing periods for commitment").Mark(ierr.ErrInternal)
	}

	rate := rates.Committed
	if c.Type == types.COMMITMENT_TYPE_AMOUNT {
		rate = decimal.NewFromInt(1)
	}
	loc := c.Location()
	out := make([]commitmentPeriod, 0, len(full))
	for _, p := range full {
		start, end := maxTime(p.Start, c.StartDate), minTime(p.End, c.EndDate)
		if !start.Before(end) {
			continue
		}
		cp := commitmentPeriod{Start: start, End: end, FullStart: p.Start, FullEnd: p.End}
		if c.Measurement == types.CommitmentMeasurementWindow {
			for _, b := range generateBucketStarts(start, end, bucket, &sub.BillingAnchor) {
				if c.ActiveWindows.Contains(b, loc) {
					cp.ActiveBuckets++
				}
			}
			cp.Capacity = c.Value.Mul(decimal.NewFromInt(cp.ActiveBuckets))
		} else {
			fraction := decimal.NewFromInt(int64(end.Sub(start))).Div(decimal.NewFromInt(int64(p.End.Sub(p.Start))))
			cp.Capacity = c.Value.Mul(fraction)
		}
		cp.Amount = cp.Capacity.Mul(rate)
		out = append(out, cp)
	}
	return out, nil
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
