package revenue

// Bucketed pricing charges each window on its own quantity, so a cumulative
// curve cannot reproduce it: 5 units/day on a $1-per-10 package with daily
// buckets bills $1 every day, while re-pricing the running total bills $1
// once. This file rebuilds the daily shape the way the engine charges it —
// price each window, then group windows into days.

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// bucketedCurveInput describes one bucketed usage line item to split per day.
type bucketedCurveInput struct {
	Price *price.Price
	Meter *meter.Meter
	Sub   *subscription.Subscription

	// PeriodStart/PeriodEnd bound the half-open billing window [start, end).
	PeriodStart time.Time
	PeriodEnd   time.Time

	// EngineAmount is the charge the billing engine computed for the line —
	// the daily shape always scales to sum exactly to it.
	EngineAmount decimal.Decimal

	ExternalCustomerIDs []string
	Timezone            string
}

// bucketedDayGrain reports whether this pair's windows nest inside days. Week
// and month windows span days, so their charge cannot be attributed to one.
func bucketedDayGrain(p *price.Price, m *meter.Meter) bool {
	if !price.IsBucketed(p, m) {
		return false
	}
	switch price.ResolveBucketSize(p, m) {
	case types.WindowSizeWeek, types.WindowSizeMonth:
		return false
	default:
		return true
	}
}

// readBucketedUsage reads the line's windows exactly as the billing engine
// does — same aggregation, group-by, window size, billing anchor and UTC
// boundaries — so the charges rebuilt from them match what was billed.
func (s *revenueService) readBucketedUsage(ctx context.Context, in bucketedCurveInput) (*events.AggregationResult, error) {
	aggType := bucketedAggregationType(in.Price, in.Meter)
	var paramsGroupBy []string
	if groupBy := price.BucketedGroupBy(in.Price, in.Meter); groupBy != "" && aggType != types.AggregationSum {
		paramsGroupBy = []string{"properties." + groupBy}
	}
	return s.MeterUsageRepo.GetUsageForBucketedMeters(ctx, &events.MeterUsageQueryParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: in.ExternalCustomerIDs,
		MeterID:             in.Meter.ID,
		StartTime:           in.PeriodStart,
		EndTime:             in.PeriodEnd,
		AggregationType:     aggType,
		WindowSize:          price.ResolveBucketSize(in.Price, in.Meter),
		BillingAnchor:       &in.Sub.BillingAnchor,
		GroupBy:             paramsGroupBy,
		UseFinal:            true,
	})
}

// bucketedAggregationType mirrors the engine's choice: a bucketed SUM meter
// sums its windows, everything else takes each window's max.
func bucketedAggregationType(p *price.Price, m *meter.Meter) types.AggregationType {
	if price.IsBucketedSum(p, m) {
		return types.AggregationSum
	}
	return types.AggregationMax
}

// buildBucketedCurve prices each of the line's windows the way the engine does
// and accumulates them into a daily curve. ok=false means the shape is
// unknowable (no windows carry usage) and the caller falls back to period_only.
//
// Windows are cut in UTC — the convention the billing engine's bucketed reads
// use — while revenue days follow the subscription's timezone. A window is
// attributed to the day holding its start, so an offset that does not divide
// the window size can shift one boundary window by a day; the period total is
// unaffected, since the curve is scaled to the engine's own amount.
func (s *revenueService) buildBucketedCurve(ctx context.Context, in bucketedCurveInput) ([]dayCharge, bool, error) {
	usage, err := s.readBucketedUsage(ctx, in)
	if err != nil {
		return nil, false, err
	}
	if usage == nil || len(usage.Results) == 0 {
		return nil, false, nil
	}

	// Each window is priced on its own quantity — per group when the price is
	// tiered and the meter groups, which is how the engine charges it too.
	priceSvc := service.NewPriceService(s.ServiceParams)
	loc := timezoneLocation(in.Timezone)
	chargeByDay := make(map[string]decimal.Decimal, len(usage.Results))
	qtyByDay := make(map[string]decimal.Decimal, len(usage.Results))
	measured := decimal.Zero
	for _, r := range usage.Results {
		key := r.WindowSize.In(loc).Format(dayKeyLayout)
		charge := priceSvc.CalculateCost(ctx, in.Price, r.Value)
		chargeByDay[key] = chargeByDay[key].Add(charge)
		qtyByDay[key] = qtyByDay[key].Add(r.Value)
		measured = measured.Add(charge)
	}

	// The engine prices from its own snapshot and rounds per line, so scale
	// the measured shape onto its amount; TierDelta carries the difference.
	scale := decimal.NewFromInt(1)
	switch {
	case in.EngineAmount.IsZero():
		scale = decimal.Zero
	case measured.IsZero():
		return nil, false, nil
	default:
		scale = in.EngineAmount.Div(measured)
	}

	startDay, endDay := localDayWalk(in.PeriodStart, in.PeriodEnd, loc)
	if tomorrow := todayEnd(loc); tomorrow.Before(endDay) {
		endDay = tomorrow
	}
	if !startDay.Before(endDay) {
		return nil, false, nil
	}

	rate := listRate(in.Price)
	curve := make([]dayCharge, 0)
	runCharge, runQty := decimal.Zero, decimal.Zero
	for cur := startDay; cur.Before(endDay); cur = cur.AddDate(0, 0, 1) {
		key := cur.Format(dayKeyLayout)
		runCharge = runCharge.Add(chargeByDay[key].Mul(scale))
		runQty = runQty.Add(qtyByDay[key])
		curve = append(curve, dayCharge{
			Day:                   time.Date(cur.Year(), cur.Month(), cur.Day(), 0, 0, 0, 0, time.UTC),
			CumulativeCharge:      runCharge,
			CumulativeGrossQty:    runQty,
			CumulativeBillableQty: runQty,
			UsageAtListRate:       runQty.Mul(rate),
			TierDelta:             runCharge.Sub(runQty.Mul(rate)),
		})
	}
	if len(curve) == 0 {
		return nil, false, nil
	}

	// Pin the final cumulative to the engine amount so the marginal rows sum
	// with zero residual under the scaling division.
	last := &curve[len(curve)-1]
	last.CumulativeCharge = in.EngineAmount
	last.TierDelta = in.EngineAmount.Sub(last.UsageAtListRate)
	return curve, true, nil
}

// todayEnd is the local midnight starting tomorrow — the walk bound that keeps
// an open period from booking days that have not happened.
func todayEnd(loc *time.Location) time.Time {
	now := time.Now().In(loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
}

// dailyPartCurve turns per-day amounts into a cumulative curve scaled so its
// final value is exactly total. qtyByDay is optional: the committed-usage part
// carries quantity (so list rate and tier delta mean something), while overage
// and true-up are money with no units behind them.
func dailyPartCurve(days []time.Time, amountByDay, qtyByDay map[string]decimal.Decimal, total, rate decimal.Decimal) []dayCharge {
	measured := decimal.Zero
	for _, d := range days {
		measured = measured.Add(amountByDay[d.Format(dayKeyLayout)])
	}
	scale := decimal.Zero
	if measured.IsPositive() && total.IsPositive() {
		scale = total.Div(measured)
	}

	curve := make([]dayCharge, 0, len(days))
	runCharge, runQty := decimal.Zero, decimal.Zero
	for _, d := range days {
		key := d.Format(dayKeyLayout)
		runCharge = runCharge.Add(amountByDay[key].Mul(scale))
		dc := dayCharge{
			Day:              time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC),
			CumulativeCharge: runCharge,
		}
		if qtyByDay != nil {
			runQty = runQty.Add(qtyByDay[key])
			dc.CumulativeGrossQty = runQty
			dc.CumulativeBillableQty = runQty
			dc.UsageAtListRate = runQty.Mul(rate)
			dc.TierDelta = runCharge.Sub(dc.UsageAtListRate)
		}
		curve = append(curve, dc)
	}
	if len(curve) == 0 {
		return nil
	}

	// Pin the ending to the engine's own amount so the marginal rows sum with
	// zero residual under the scaling division.
	last := &curve[len(curve)-1]
	last.CumulativeCharge = total
	if qtyByDay != nil {
		last.TierDelta = total.Sub(last.UsageAtListRate)
	}
	return curve
}

// decomposeBucketedCommitmentRows splits a bucketed line whose commitment
// settles window by window. Each window's committed usage, overage and true-up
// are known separately, so all three land on the day that window covers
// instead of collapsing onto the period. handled=false means the shape could
// not be derived and the caller falls back to its whole-period split.
func (s *revenueService) decomposeBucketedCommitmentRows(
	ctx context.Context,
	sub *subscription.Subscription,
	base previewLineItem,
	item *dto.CreateInvoiceLineItemRequest,
	itemPeriod revenuePeriod,
	p *price.Price,
	m *meter.Meter,
	info *types.CommitmentInfo,
	extCustomerIDs []string,
) ([]*revenuefact.RevenueFact, bool, error) {
	sli := subLineItemByID(sub, base.SubLineItemID)
	if sli == nil {
		return nil, false, nil
	}
	in := bucketedCurveInput{
		Price: p, Meter: m, Sub: sub,
		PeriodStart: itemPeriod.Start, PeriodEnd: itemPeriod.exclusiveEnd(),
		ExternalCustomerIDs: extCustomerIDs, Timezone: sub.Timezone,
	}
	usage, err := s.readBucketedUsage(ctx, in)
	if err != nil {
		return nil, false, err
	}

	parts, err := service.WindowCommitmentBreakdown(ctx, s.ServiceParams, sli, usage,
		itemPeriod.Start, itemPeriod.exclusiveEnd(), price.ResolveBucketSize(p, m),
		&sub.BillingAnchor, bucketedAggregationType(p, m), p)
	if err != nil {
		return nil, false, err
	}
	if len(parts) == 0 {
		return nil, false, nil
	}

	loc := timezoneLocation(sub.Timezone)
	utilizedByDay := map[string]decimal.Decimal{}
	overageByDay := map[string]decimal.Decimal{}
	trueUpByDay := map[string]decimal.Decimal{}
	qtyByDay := map[string]decimal.Decimal{}
	for _, part := range parts {
		key := part.WindowStart.In(loc).Format(dayKeyLayout)
		utilizedByDay[key] = utilizedByDay[key].Add(part.Utilized)
		overageByDay[key] = overageByDay[key].Add(part.Overage)
		trueUpByDay[key] = trueUpByDay[key].Add(part.TrueUp)
		qtyByDay[key] = qtyByDay[key].Add(part.Value)
	}

	startDay, endDay := localDayWalk(itemPeriod.Start, itemPeriod.exclusiveEnd(), loc)
	if tomorrow := todayEnd(loc); tomorrow.Before(endDay) {
		endDay = tomorrow
	}
	var days []time.Time
	for cur := startDay; cur.Before(endDay); cur = cur.AddDate(0, 0, 1) {
		days = append(days, cur)
	}
	if len(days) == 0 {
		return nil, false, nil
	}

	overageTotal, trueUpTotal := info.ComputedOverageAmount, info.ComputedTrueUpAmount
	withinTotal := item.Amount.Sub(overageTotal).Sub(trueUpTotal)
	rate := listRate(p)

	// Discounts ride on the committed-usage part only, so the line still nets
	// out once across the three sources.
	partBase := base
	partBase.LineDiscount, partBase.InvoiceDiscount = decimal.Zero, decimal.Zero

	var rows []*revenuefact.RevenueFact
	if withinTotal.IsPositive() {
		usageBase := base
		usageBase.EngineAmount = withinTotal
		rows = append(rows, decomposeUsageMarginal(usageBase,
			dailyPartCurve(days, utilizedByDay, qtyByDay, withinTotal, rate))...)
	}
	if overageTotal.IsPositive() {
		ob := partBase
		ob.EngineAmount = overageTotal
		ob.Source = types.RevenueSourceOverage
		rows = append(rows, decomposeUsageMarginal(ob,
			dailyPartCurve(days, overageByDay, nil, overageTotal, rate))...)
	}
	if trueUpTotal.IsPositive() {
		tb := partBase
		if !withinTotal.IsPositive() {
			// Nothing else carries the line's discounts.
			tb = base
		}
		tb.EngineAmount = trueUpTotal
		tb.Source = types.RevenueSourceCommitmentTrueup
		rows = append(rows, decomposeUsageMarginal(tb,
			dailyPartCurve(days, trueUpByDay, nil, trueUpTotal, rate))...)
	}
	if len(rows) == 0 {
		return nil, false, nil
	}
	return rows, true, nil
}
