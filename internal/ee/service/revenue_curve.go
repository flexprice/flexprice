package service

import (
	"context"
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
}

// buildUsageCurve returns one dayCharge per calendar day of the period: it
// reads the cumulative daily usage once, then re-prices each day's cumulative
// billable quantity with CalculateCost. Summing the day-over-day deltas
// equals the full-period charge for flat and graduated pricing (see
// TestMarginalPrefixSumEqualsPeriodCharge).
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

	points, err := s.MeterUsageRepo.GetCumulativeDailyUsage(ctx, &events.CumulativeDailyUsageParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		MeterID:             in.MeterID,
		ExternalCustomerIDs: in.ExternalCustomerIDs,
		StartTime:           in.PeriodStart,
		EndTime:             in.PeriodEnd,
		UseFinal:            true,
		Timezone:            in.Timezone,
	})
	if err != nil {
		return nil, err
	}

	cumByDay := make(map[string]decimal.Decimal, len(points))
	for _, p := range points {
		cumByDay[p.Day.Format(dayKeyLayout)] = p.CumulativeQty
	}

	limit := in.EntitlementLimit
	if limit.IsNegative() {
		limit = decimal.Zero
	}
	tier1Rate := listRate(in.Price)
	priceSvc := NewPriceService(s.ServiceParams)

	startLocal := in.PeriodStart.In(loc)
	cur := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, loc)

	// Walk local calendar days up to (not including) the local date of the
	// exclusive PeriodEnd, so no emitted Day lands past the period's last day
	// even when period bounds are not local midnights. Usage the store
	// grouped onto that excluded date (a period ending mid-day) is folded
	// into the last emitted day below, keeping totals intact.
	endLocal := in.PeriodEnd.In(loc)
	endDay := time.Date(endLocal.Year(), endLocal.Month(), endLocal.Day(), 0, 0, 0, 0, loc)
	if !cur.Before(endDay) {
		// The whole period sits inside one local day — emit that one day.
		endDay = cur.AddDate(0, 0, 1)
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
