package revenue

import (
	"context"
	"github.com/flexprice/flexprice/internal/ee/service"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// dayKeyLayout keys days by calendar date, not time.Time: two time.Time
// values for the same instant can carry different *time.Location pointers
// (ClickHouse vs in-memory) and would not be == comparable as map keys.
const dayKeyLayout = "2006-01-02"

// usageCurveInput describes one usage line item to price day by day.
// Tenant/environment come from ctx.
type usageCurveInput struct {
	Price   *price.Price
	MeterID string

	// Usage is this meter's per-day quantities, pre-read for the whole
	// subscription. An empty (non-nil) slice means "pre-read, no usage"; nil
	// means "not pre-read" and falls back to reading just this meter, which the
	// rollup must never do — that is one round-trip per line item.
	Usage []events.DailyUsagePoint

	PeriodStart time.Time
	// PeriodEnd is exclusive: the period is [PeriodStart, PeriodEnd).
	PeriodEnd time.Time

	// EntitlementLimit is the free quantity consumed before billing starts.
	// Negative values are treated as zero.
	EntitlementLimit decimal.Decimal

	// ExternalCustomerIDs scope the usage read to this subscription's
	// customers; empty means no customer filter.
	ExternalCustomerIDs []string

	// Timezone is the IANA name used to split usage into calendar days.
	// Empty means UTC.
	Timezone string

	// AsOf is the instant the curve is evaluated at: an open period stops
	// after AsOf's day. Zero means now.
	AsOf time.Time
}

// buildUsageCurve returns one dayCharge per calendar day of the period: it
// reads the cumulative daily usage once, then re-prices each day's cumulative
// billable quantity with CalculateCost. Summing the day-over-day deltas
// equals the full-period charge for flat and graduated pricing (see
// TestMarginalPrefixSumEqualsPeriodCharge).
// dailyUsage returns the meter's per-day quantities, preferring the
// subscription-wide read the rollup already performed. Falling back to a
// single-meter read keeps one-off callers working; the rollup must not take
// that path, or it is back to one round-trip per line item.
func (s *revenueService) dailyUsage(ctx context.Context, in usageCurveInput) ([]events.DailyUsagePoint, error) {
	if in.Usage != nil {
		return in.Usage, nil
	}
	byMeter, err := s.MeterUsageRepo.GetDailyUsageByMeter(ctx, &events.DailyUsageParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		MeterIDs:            []string{in.MeterID},
		ExternalCustomerIDs: in.ExternalCustomerIDs,
		StartTime:           in.PeriodStart,
		EndTime:             in.PeriodEnd,
		UseFinal:            true,
		Timezone:            in.Timezone,
	})
	if err != nil {
		return nil, err
	}
	return byMeter[in.MeterID], nil
}

func (s *revenueService) buildUsageCurve(ctx context.Context, in usageCurveInput) ([]dayCharge, error) {
	if in.Price == nil {
		return nil, ierr.NewError("price is required").
			WithHint("buildUsageCurve requires a non-nil price").
			Mark(ierr.ErrValidation)
	}
	if in.MeterID == "" {
		return nil, ierr.NewError("meter id is required").
			WithHint("buildUsageCurve requires a meter id").
			Mark(ierr.ErrValidation)
	}
	if !in.PeriodStart.Before(in.PeriodEnd) {
		return nil, ierr.NewError("invalid period").
			WithHint("period start must be before period end").
			Mark(ierr.ErrValidation)
	}

	loc := time.UTC
	if in.Timezone != "" {
		if l, err := time.LoadLocation(in.Timezone); err == nil {
			loc = l
		}
	}

	points, err := s.dailyUsage(ctx, in)
	if err != nil {
		return nil, err
	}

	// Accumulate here rather than in the repository: the read is shared across
	// every line item of the subscription, and each one runs its total from its
	// own period start.
	// Bound by local calendar date, not by instant. A point's Day is the local
	// midnight its usage was bucketed into, so comparing it against an exact
	// PeriodStart (09:37, say) would drop the period's own first day.
	//
	// The end day is kept only when the period ends part-way through it: that
	// day's usage up to the end instant belongs here, and the walk below folds
	// it back into the last emitted day. A midnight end owns none of its day —
	// that day opens the next period, and folding it in would charge one line
	// item for the next one's usage.
	startKey := in.PeriodStart.In(loc).Format(dayKeyLayout)
	endLocal := in.PeriodEnd.In(loc)
	endKey := endLocal.Format(dayKeyLayout)
	endOwnsItsDay := !endLocal.Equal(time.Date(endLocal.Year(), endLocal.Month(), endLocal.Day(), 0, 0, 0, 0, loc))

	cumByDay := make(map[string]decimal.Decimal, len(points))
	running := decimal.Zero
	for _, p := range points {
		key := p.Day.Format(dayKeyLayout)
		if key < startKey {
			continue
		}
		if key > endKey || (key == endKey && !endOwnsItsDay) {
			continue
		}
		running = running.Add(p.Qty)
		cumByDay[key] = running
	}

	limit := in.EntitlementLimit
	if limit.IsNegative() {
		limit = decimal.Zero
	}
	tier1Rate := listRate(in.Price)
	priceSvc := service.NewPriceService(s.ServiceParams)

	startLocal := in.PeriodStart.In(loc)
	cur := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, loc)

	// Walk local calendar days up to (not including) the local date of the
	// exclusive PeriodEnd, so no emitted Day lands past the period's last day
	// even when period bounds are not local midnights. Usage the store
	// grouped onto that excluded date (a period ending mid-day) is folded
	// into the last emitted day below, keeping totals intact.
	endDay := time.Date(endLocal.Year(), endLocal.Month(), endLocal.Day(), 0, 0, 0, 0, loc)
	if !cur.Before(endDay) {
		// The whole period sits inside one local day — emit that one day.
		endDay = cur.AddDate(0, 0, 1)
	}

	// An open period's remaining days have not happened yet: stop after today
	// rather than writing a zero row per future day. Once the period closes
	// (and before any finalize/flip) today is past it and the walk is whole.
	// Clamping after the one-day fallback keeps a short period intact, and a
	// period that has not started yet clamps away to nothing.
	asOf := in.AsOf
	if asOf.IsZero() {
		asOf = time.Now()
	}
	nowLocal := asOf.In(loc)
	tomorrow := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	if tomorrow.Before(endDay) {
		endDay = tomorrow
	}
	if !cur.Before(endDay) {
		return nil, nil
	}

	curve := make([]dayCharge, 0)
	runningGross := decimal.Zero
	for cur.Before(endDay) {
		// Days with no new usage carry the prior running total forward.
		if qty, ok := cumByDay[cur.Format(dayKeyLayout)]; ok {
			runningGross = qty
		}
		if isLast := !cur.AddDate(0, 0, 1).Before(endDay); isLast {
			if qty, ok := cumByDay[endDay.Format(dayKeyLayout)]; ok && qty.GreaterThan(runningGross) {
				runningGross = qty
			}
		}

		billable := runningGross.Sub(limit)
		if billable.IsNegative() {
			billable = decimal.Zero
		}
		entitlementQty := limit
		if runningGross.LessThan(limit) {
			entitlementQty = runningGross
		}

		charge := priceSvc.CalculateCost(ctx, in.Price, billable)

		curve = append(curve, dayCharge{
			Day:                      time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, time.UTC),
			CumulativeCharge:         charge,
			CumulativeGrossQty:       runningGross,
			CumulativeBillableQty:    billable,
			CumulativeEntitlementQty: entitlementQty,
			UsageAtListRate:          runningGross.Mul(tier1Rate),
			TierDelta:                charge.Sub(billable.Mul(tier1Rate)),
		})

		cur = cur.AddDate(0, 0, 1)
	}

	return curve, nil
}
