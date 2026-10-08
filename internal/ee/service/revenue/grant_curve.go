// Grant-billed usage: when entitlement grants cover a line item, the engine
// charges only the usage that fell inside the grants' quota-crossed windows.
// This file rebuilds that shape per day so marginal revenue_facts rows show
// "out of X entitled, Y billed today" — buildGrantOverageCurve is the entry
// point, everything below it is window/day bookkeeping.
package revenue

import (
	"context"
	"github.com/flexprice/flexprice/internal/ee/service"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// grantCurveInput describes one grant-billed usage line item to split per day.
type grantCurveInput struct {
	Price *price.Price
	Meter *meter.Meter

	// PeriodStart/PeriodEnd bound the half-open billing window [start, end).
	PeriodStart time.Time
	PeriodEnd   time.Time

	// EngineAmount is the charge the billing engine computed for the line —
	// the daily curve's shape always scales to sum exactly to it.
	EngineAmount decimal.Decimal

	Grants              []*entitlementgrant.EntitlementGrant
	ExternalCustomerIDs []string
	Timezone            string

	// Usage is the meter's per-day quantities, pre-read for the whole
	// subscription. Every window below is derived from it in memory: reading
	// per window instead is one round-trip per grant window per line item.
	Usage []events.DailyUsagePoint
}

// grantsBillable reports whether the billing engine folded these grants into
// the line item's charge — the same conditions service.GrantPricingGuard enforces.
// When it did not, the line was billed on the normal path and the normal
// daily curve applies.
func grantsBillable(sli *subscription.SubscriptionLineItem, p *price.Price, m *meter.Meter, grants []*entitlementgrant.EntitlementGrant) bool {
	if len(grants) == 0 {
		return false
	}
	measure := grants[0].Measure
	if measure == "" {
		return false
	}
	return service.GrantPricingGuard(measure, sli, p, m) == nil
}

// buildGrantOverageCurve splits a grant-billed line item per day, best effort:
// usage inside the grants' merged quota-crossed windows is the billed
// quantity, everything else is entitled. The money shape is scaled so the
// days sum exactly to the engine's charge (the engine may price from frozen
// snapshots); the scale adjustment lands in TierDelta, keeping the row
// identity exact. ok=false means the shape is unknowable (engine billed an
// amount but no window carries usage) — the caller falls back to period_only.
func (s *revenueService) buildGrantOverageCurve(ctx context.Context, in grantCurveInput) (curve []dayCharge, ok bool, err error) {
	windows := grantOverageWindows(in.Grants, in.PeriodStart, in.PeriodEnd)

	grossByDay, err := s.cumulativeUsageByDay(ctx, in, in.PeriodStart, in.PeriodEnd)
	if err != nil {
		return nil, false, err
	}

	// Billed usage per day: each merged window contributes its own daily
	// marginals; windows never overlap after merging, so a unit counts once.
	billedMarginalByDay := make(map[string]decimal.Decimal)
	for _, w := range windows {
		windowCum, wErr := s.cumulativeUsageByDay(ctx, in, w.Start, w.End)
		if wErr != nil {
			return nil, false, wErr
		}
		addDailyMarginals(billedMarginalByDay, windowCum, w.Start, w.End, in.Timezone)
	}

	loc := timezoneLocation(in.Timezone)
	startDay, endDay := localDayWalk(in.PeriodStart, in.PeriodEnd, loc)
	rate := listRate(in.Price)

	runningGross := decimal.Zero
	billedCum := decimal.Zero
	var unscaled []dayCharge
	for cur := startDay; cur.Before(endDay); cur = cur.AddDate(0, 0, 1) {
		if qty, found := grossByDay[cur.Format(dayKeyLayout)]; found {
			runningGross = qty
		}
		billedCum = billedCum.Add(billedMarginalByDay[cur.Format(dayKeyLayout)])
		if isLast := !cur.AddDate(0, 0, 1).Before(endDay); isLast {
			// A period ending mid-day folds its tail into the last day.
			if qty, found := grossByDay[endDay.Format(dayKeyLayout)]; found && qty.GreaterThan(runningGross) {
				runningGross = qty
			}
			billedCum = billedCum.Add(billedMarginalByDay[endDay.Format(dayKeyLayout)])
		}

		unscaled = append(unscaled, dayCharge{
			Day:                      time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, time.UTC),
			CumulativeGrossQty:       runningGross,
			CumulativeBillableQty:    billedCum,
			CumulativeEntitlementQty: runningGross.Sub(billedCum),
			UsageAtListRate:          runningGross.Mul(rate),
		})
	}
	if len(unscaled) == 0 {
		return nil, false, nil
	}

	measuredTotal := billedCum.Mul(rate)
	scale := decimal.Zero
	switch {
	case in.EngineAmount.IsZero():
		// Nothing billed — every day is pure entitled usage.
	case measuredTotal.IsZero():
		// The engine billed an amount our windows can't see — no shape.
		return nil, false, nil
	default:
		scale = in.EngineAmount.Div(measuredTotal)
	}

	for i := range unscaled {
		charge := unscaled[i].CumulativeBillableQty.Mul(rate).Mul(scale)
		unscaled[i].CumulativeCharge = charge
		// TierDelta absorbs the snapshot-vs-measured scale so the identity
		// net == list + tier - entitlement holds exactly per day.
		unscaled[i].TierDelta = charge.Sub(unscaled[i].CumulativeBillableQty.Mul(rate))
	}
	// Force the final cumulative charge to the engine amount exactly, so the
	// marginal rows sum with zero residual even under division rounding.
	if !in.EngineAmount.IsZero() {
		last := &unscaled[len(unscaled)-1]
		last.CumulativeCharge = in.EngineAmount
		last.TierDelta = in.EngineAmount.Sub(last.CumulativeBillableQty.Mul(rate))
	}

	return unscaled, true, nil
}

// grantOverageWindows merges the grants' [quota_crossed_at, valid_to) windows,
// clipped to the billing period. Uncrossed grants bill nothing.
func grantOverageWindows(grants []*entitlementgrant.EntitlementGrant, periodStart, periodEnd time.Time) []service.TimeInterval {
	intervals := make([]service.TimeInterval, 0, len(grants))
	for _, g := range grants {
		if g == nil || g.QuotaCrossedAt == nil {
			continue
		}
		start := *g.QuotaCrossedAt
		if start.Before(periodStart) {
			start = periodStart
		}
		end := g.ValidTo
		if end.After(periodEnd) {
			end = periodEnd
		}
		intervals = append(intervals, service.TimeInterval{Start: start, End: end})
	}
	return service.MergeIntervals(intervals)
}

// cumulativeUsageByDay reads the meter's cumulative daily usage over
// [start, end), keyed by local calendar date.
func (s *revenueService) cumulativeUsageByDay(ctx context.Context, in grantCurveInput, start, end time.Time) (map[string]decimal.Decimal, error) {
	if !start.Before(end) {
		return map[string]decimal.Decimal{}, nil
	}

	points := in.Usage
	if points == nil {
		byMeter, err := s.MeterUsageRepo.GetDailyUsageByMeter(ctx, &events.DailyUsageParams{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			MeterIDs:            []string{in.Meter.ID},
			ExternalCustomerIDs: in.ExternalCustomerIDs,
			StartTime:           in.PeriodStart,
			EndTime:             in.PeriodEnd,
			UseFinal:            true,
			Timezone:            in.Timezone,
		})
		if err != nil {
			return nil, err
		}
		points = byMeter[in.Meter.ID]
	}

	// Slice the shared per-day points to this window. Grant windows open at
	// QuotaCrossedAt -- an event instant, so a mid-day boundary is the norm --
	// and a whole-day quantity would put that day's pre-crossing usage on the
	// wrong side of the boundary. Only a boundary that is not local midnight
	// needs an exact read, so an aligned window still costs nothing.
	loc := timezoneLocation(in.Timezone)
	startKey := start.In(loc).Format(dayKeyLayout)
	endKey := end.In(loc).Format(dayKeyLayout)

	qtyByDay := make(map[string]decimal.Decimal, len(points))
	for _, p := range points {
		key := p.Day.Format(dayKeyLayout)
		if key < startKey || key > endKey {
			continue
		}
		qtyByDay[key] = p.Qty
	}

	startAligned := isLocalMidnight(start, loc)
	endAligned := isLocalMidnight(end, loc)

	switch {
	case startKey == endKey:
		// The whole window sits inside one local day.
		if !startAligned || !endAligned {
			qty, err := s.exactUsage(ctx, in, start, end)
			if err != nil {
				return nil, err
			}
			qtyByDay[startKey] = qty
		}
	default:
		if !startAligned {
			qty, err := s.exactUsage(ctx, in, start, nextLocalMidnight(start, loc))
			if err != nil {
				return nil, err
			}
			qtyByDay[startKey] = qty
		}
		if endAligned {
			// A midnight end owns none of its own day.
			delete(qtyByDay, endKey)
		} else {
			qty, err := s.exactUsage(ctx, in, localMidnight(end, loc), end)
			if err != nil {
				return nil, err
			}
			qtyByDay[endKey] = qty
		}
	}

	days := make([]string, 0, len(qtyByDay))
	for key := range qtyByDay {
		days = append(days, key)
	}
	sort.Strings(days)

	byDay := make(map[string]decimal.Decimal, len(days))
	running := decimal.Zero
	for _, key := range days {
		running = running.Add(qtyByDay[key])
		byDay[key] = running
	}
	return byDay, nil
}

// addDailyMarginals converts one window's cumulative curve into per-day
// deltas and accumulates them into acc.
func addDailyMarginals(acc map[string]decimal.Decimal, windowCum map[string]decimal.Decimal, start, end time.Time, tz string) {
	loc := timezoneLocation(tz)
	dayStart, dayEnd := localDayWalk(start, end, loc)
	prev := decimal.Zero
	for cur := dayStart; cur.Before(dayEnd.AddDate(0, 0, 1)); cur = cur.AddDate(0, 0, 1) {
		key := cur.Format(dayKeyLayout)
		cum, found := windowCum[key]
		if !found {
			continue
		}
		acc[key] = acc[key].Add(cum.Sub(prev))
		prev = cum
	}
}

// localDayWalk returns the local-midnight walk bounds for a half-open window:
// [startDay, endDay), with a window inside one local day walking that one day.
func localDayWalk(start, end time.Time, loc *time.Location) (time.Time, time.Time) {
	startLocal := start.In(loc)
	startDay := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, loc)
	endLocal := end.In(loc)
	endDay := time.Date(endLocal.Year(), endLocal.Month(), endLocal.Day(), 0, 0, 0, 0, loc)
	if !startDay.Before(endDay) {
		endDay = startDay.AddDate(0, 0, 1)
	}
	return startDay, endDay
}

// timezoneLocation loads tz, falling back to UTC on empty or invalid names.
func timezoneLocation(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc
	}
	return time.UTC
}

// subLineItemByID finds the subscription line item behind a preview line, nil
// when the id is synthetic or unknown.
func subLineItemByID(sub *subscription.Subscription, id string) *subscription.SubscriptionLineItem {
	for _, li := range sub.LineItems {
		if li != nil && li.ID == id {
			return li
		}
	}
	return nil
}

// localMidnight is the start of t's day in loc.
func localMidnight(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}

func nextLocalMidnight(t time.Time, loc *time.Location) time.Time {
	return localMidnight(t, loc).AddDate(0, 0, 1)
}

func isLocalMidnight(t time.Time, loc *time.Location) bool {
	return t.In(loc).Equal(localMidnight(t, loc))
}

// exactUsage reads the metered quantity in [from, to) at timestamp resolution,
// for a grant-window boundary that falls part-way through a day. Everything
// else is served from the subscription's single pre-read.
func (s *revenueService) exactUsage(ctx context.Context, in grantCurveInput, from, to time.Time) (decimal.Decimal, error) {
	if !from.Before(to) {
		return decimal.Zero, nil
	}
	byMeter, err := s.MeterUsageRepo.GetDailyUsageByMeter(ctx, &events.DailyUsageParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		MeterIDs:            []string{in.Meter.ID},
		ExternalCustomerIDs: in.ExternalCustomerIDs,
		StartTime:           from,
		EndTime:             to,
		UseFinal:            true,
		Timezone:            in.Timezone,
	})
	if err != nil {
		return decimal.Zero, err
	}
	total := decimal.Zero
	for _, p := range byMeter[in.Meter.ID] {
		total = total.Add(p.Qty)
	}
	return total, nil
}
