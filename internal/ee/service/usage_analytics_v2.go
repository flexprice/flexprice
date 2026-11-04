package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"
)

// analyticsV2RateScale is the precision rates are carried at between the app and ClickHouse.
const analyticsV2RateScale = 20

// analyticsV2Context is what the request resolves to before ClickHouse is queried.
type analyticsV2Context struct {
	req          *dto.GetUsageAnalyticsV2Request
	currency     string
	timezone     string
	scope        events.AnalyticsV2Scope
	subs         []*subscription.Subscription
	meters       map[string]*meter.Meter
	prices       map[string]*price.Price
	features     map[string]*feature.Feature // by meter ID
	lines        []*analyticsV2LineCost
	ranked       []events.AnalyticsV2Line
	rates        []events.AnalyticsV2Amounts
	unattributed []dto.AnalyticsV2UnattributedCharge
	excluded     []dto.AnalyticsV2ExcludedMeter
	// priceUnit is set when every billed line is priced in one custom unit (e.g. credits) at one rate.
	priceUnit     string
	priceUnitRate decimal.Decimal
}

// GetUsageAnalyticsV2 returns one sorted page of usage cost grouped by the request's keys. Line costs come from
// the invoice code; ClickHouse splits them across rows by usage. req must already be defaulted and validated.
func (s *meterUsageService) GetUsageAnalyticsV2(ctx context.Context, req *dto.GetUsageAnalyticsV2Request) (*dto.GetUsageAnalyticsV2Response, error) {
	dataThrough := clock.Now().Truncate(time.Millisecond)
	empty := &dto.GetUsageAnalyticsV2Response{DataThrough: dataThrough, Items: []dto.AnalyticsV2Item{}, ExcludedMeters: []dto.AnalyticsV2ExcludedMeter{}}

	cust, subs, err := s.resolveCustomerAndSubscriptions(ctx, req.ExternalCustomerID)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return empty, nil
	}
	if err := rejectAnalyticsV2Families(subs); err != nil {
		return nil, err
	}
	if currencies := lo.Uniq(lo.Map(subs, func(sub *subscription.Subscription, _ int) string { return strings.ToLower(sub.Currency) })); len(currencies) > 1 {
		return nil, ierr.NewErrorf("customer has subscriptions in %d currencies", len(currencies)).
			WithHint("Analytics query supports customers billed in one currency").
			WithReportableDetails(map[string]any{"currencies": currencies}).
			Mark(ierr.ErrValidation)
	}

	ac := &analyticsV2Context{
		req:      req,
		currency: subs[0].Currency,
		timezone: cust.Timezone,
		subs:     subs,
		scope: events.AnalyticsV2Scope{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerIDs: []string{cust.ExternalID},
			DataThrough:         dataThrough,
			UseFinal:            req.Deduplicate,
		},
	}
	empty.Currency = ac.currency

	priced, err := s.priceAnalyticsV2Subscriptions(ctx, subs, req.StartTime, req.EndTime)
	if err != nil {
		return nil, err
	}
	s.logger.Debug(ctx, "analytics v2 priced lines", "periods", priced.periods, "lines", len(priced.lines), "elapsed_ms", priced.elapsed.Milliseconds())

	if err := s.loadAnalyticsV2Catalog(ctx, ac, priced); err != nil {
		return nil, err
	}
	if err := s.selectAnalyticsV2Lines(ctx, ac, priced); err != nil {
		return nil, err
	}
	if err := s.buildAnalyticsV2Rates(ctx, ac); err != nil {
		return nil, err
	}
	if len(ac.ranked) == 0 {
		return s.assembleAnalyticsV2(ctx, ac, &events.AnalyticsV2RankResult{}, nil)
	}

	rank := ac.rankParams()
	result, err := s.repo.RankAnalyticsV2Rows(ctx, rank)
	if err != nil {
		return nil, err
	}
	if len(result.Rows) == 0 && req.Offset > 0 {
		// Window totals come back only on returned rows; read them from the first row instead.
		probe := *rank
		probe.Offset, probe.Limit = 0, 1
		first, err := s.repo.RankAnalyticsV2Rows(ctx, &probe)
		if err != nil {
			return nil, err
		}
		result.TotalCount, result.Totals, result.ColumnTotals = first.TotalCount, first.Totals, first.ColumnTotals
	}
	if len(result.ColumnTotals) > dto.AnalyticsV2MaxColumns {
		return nil, ierr.NewErrorf("columns key %q has %d values; the limit is %d", req.Columns, len(result.ColumnTotals), dto.AnalyticsV2MaxColumns).
			WithHintf("Use %s as a group_by key instead", req.Columns).
			Mark(ierr.ErrValidation)
	}

	var detail *events.AnalyticsV2Detail
	distinctKeys := ac.distinctKeys()
	// An empty page still needs the all-rows points when the result has rows.
	if (len(result.Rows) > 0 && len(distinctKeys) > 0) || (result.TotalCount > 0 && req.WindowSize != "") {
		detail, err = s.repo.GetAnalyticsV2Detail(ctx, &events.AnalyticsV2DetailParams{
			Rank:          *rank,
			Rows:          lo.Map(result.Rows, func(r events.AnalyticsV2Row, _ int) []string { return r.Group }),
			WindowSize:    req.WindowSize,
			DistinctKeys:  distinctKeys,
			BillingAnchor: ac.billingAnchor(),
		})
		if err != nil {
			return nil, err
		}
	}
	return s.assembleAnalyticsV2(ctx, ac, result, detail)
}

// loadAnalyticsV2Catalog loads the meters, prices and features of every priced line.
func (s *meterUsageService) loadAnalyticsV2Catalog(ctx context.Context, ac *analyticsV2Context, priced *analyticsV2PricedPeriods) error {
	meterIDs := lo.Uniq(lo.FilterMap(priced.lines, func(l *analyticsV2LineCost, _ int) (string, bool) { return l.meterID, l.meterID != "" }))
	priceIDs := lo.Uniq(lo.FilterMap(priced.lines, func(l *analyticsV2LineCost, _ int) (string, bool) { return l.priceID, l.priceID != "" }))

	ac.meters, ac.prices, ac.features = map[string]*meter.Meter{}, map[string]*price.Price{}, map[string]*feature.Feature{}
	if len(meterIDs) > 0 {
		meters, err := s.MeterRepo.ListByIDs(ctx, meterIDs)
		if err != nil {
			return err
		}
		ac.meters = lo.SliceToMap(meters, func(m *meter.Meter) (string, *meter.Meter) { return m.ID, m })

		filter := types.NewNoLimitFeatureFilter()
		filter.MeterIDs = meterIDs
		features, err := s.FeatureRepo.List(ctx, filter)
		if err != nil {
			return err
		}
		ac.features = lo.SliceToMap(features, func(f *feature.Feature) (string, *feature.Feature) { return f.MeterID, f })
	}
	if len(priceIDs) > 0 {
		prices, err := s.PriceRepo.ListByIDs(ctx, priceIDs)
		if err != nil {
			return err
		}
		ac.prices = lo.SliceToMap(prices, func(p *price.Price) (string, *price.Price) { return p.ID, p })
	}
	return nil
}

// selectAnalyticsV2Lines keeps the requested meters' lines and moves unsupported meters to excluded.
func (s *meterUsageService) selectAnalyticsV2Lines(ctx context.Context, ac *analyticsV2Context, priced *analyticsV2PricedPeriods) error {
	selected := map[string]bool{}
	if len(ac.req.FeatureIDs) > 0 {
		ids, err := featureMeterIDs(ctx, s.FeatureRepo, ac.req.FeatureIDs)
		if err != nil {
			return err
		}
		lo.ForEach(ids, func(id string, _ int) { selected[id] = true })
	}
	lo.ForEach(ac.req.MeterIDs, func(id string, _ int) { selected[id] = true })
	narrowed := len(ac.req.FeatureIDs) > 0 || len(ac.req.MeterIDs) > 0

	lines := lo.Filter(priced.lines, func(l *analyticsV2LineCost, _ int) bool { return !narrowed || selected[l.meterID] })
	ac.lines, ac.excluded = splitAnalyticsV2Excluded(lines, ac.meters, ac.prices, ac.req.StartTime, ac.req.EndTime)
	for i := range ac.excluded {
		if f := ac.features[ac.excluded[i].MeterID]; f != nil {
			ac.excluded[i].FeatureID = f.ID
		}
	}

	for _, u := range priced.unattributed {
		if narrowed && !selected[u.charge.MeterID] {
			continue
		}
		// A charge with no line has no usage to share by, so a partial range carries it in proportion to time.
		f := analyticsV2Overlap(ac.req.StartTime, ac.req.EndTime, u.windowStart, u.windowEnd)
		charge := u.charge
		charge.Cost, charge.Breakdown = charge.Cost.Mul(f), scaleAnalyticsV2Cost(charge.Breakdown, f)
		netAnalyticsV2Charge(&charge, u.discount.Mul(f))
		ac.unattributed = append(ac.unattributed, charge)
	}
	// Only the selected unlinked charges (in ac.unattributed so far) can rule out the price unit.
	ac.setPriceUnit(lines, len(ac.unattributed) > 0)
	return nil
}

// netAnalyticsV2Charge takes a discount off a charge and its (single-component) breakdown.
func netAnalyticsV2Charge(c *dto.AnalyticsV2UnattributedCharge, discount decimal.Decimal) {
	if discount.IsZero() || c.Cost.IsZero() {
		return
	}
	factor := c.Cost.Sub(discount).Div(c.Cost)
	c.Cost = c.Cost.Sub(discount)
	c.Breakdown = scaleAnalyticsV2Cost(c.Breakdown, factor)
}

func scaleAnalyticsV2Cost(c dto.AnalyticsV2Cost, f decimal.Decimal) dto.AnalyticsV2Cost {
	return dto.AnalyticsV2Cost{
		Usage: c.Usage.Mul(f), CommitmentUtilized: c.CommitmentUtilized.Mul(f), Overage: c.Overage.Mul(f), TrueUp: c.TrueUp.Mul(f),
	}
}

// buildAnalyticsV2Rates turns each line's cost into rates: per unit of quantity for additive lines, per unit of
// row peak for MAX lines. Cost no row can carry (true-up on property rows, lines with no usage) becomes unattributed.
func (s *meterUsageService) buildAnalyticsV2Rates(ctx context.Context, ac *analyticsV2Context) error {
	if len(ac.lines) == 0 {
		return nil
	}
	lines := lo.Map(ac.lines, func(l *analyticsV2LineCost, _ int) events.AnalyticsV2Line {
		return events.AnalyticsV2Line{
			ID: l.key.lineItemID, MeterID: l.meterID, Start: l.key.windowStart, End: l.key.windowEnd,
			IsMax: ac.meters[l.meterID] != nil && ac.meters[l.meterID].Aggregation.Type == types.AggregationMax,
		}
	})
	// Quantities and MAX peaks are independent reads; run them side by side.
	var usage []events.AnalyticsV2LineUsage
	var peaks []decimal.Decimal
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		usage, err = s.repo.GetAnalyticsV2LineUsage(gctx, &events.AnalyticsV2LineUsageParams{
			Scope: ac.scope, Lines: lines, RangeStart: ac.req.StartTime, RangeEnd: ac.req.EndTime,
		})
		return err
	})
	g.Go(func() error {
		var err error
		peaks, err = s.repo.GetAnalyticsV2LinePeaks(gctx, &events.AnalyticsV2LinePeakParams{
			Scope: ac.scope, StartTime: ac.req.StartTime, EndTime: ac.req.EndTime, Lines: lines,
			GroupBy: ac.req.GroupBy, MeterFeatures: ac.meterFeatures(),
		})
		return err
	})
	if err := g.Wait(); err != nil {
		return err
	}

	lineRows := lo.SomeBy(ac.req.GroupBy, func(k string) bool {
		return k == "meter_id" || k == "feature_id" || k == "sub_line_item_id"
	})
	// kept stays aligned with ac.ranked and ac.rates, so a row's line indexes resolve to line metadata.
	var kept []*analyticsV2LineCost
	for i, l := range ac.lines {
		cost, discount := l.cost, l.discount
		if lines[i].IsMax {
			// MAX has no per-unit rate; a partial range carries the window's cost in proportion to time.
			f := analyticsV2Overlap(ac.req.StartTime, ac.req.EndTime, l.key.windowStart, l.key.windowEnd)
			cost, discount = scaleAnalyticsV2Cost(cost, f), discount.Mul(f)
		}
		if !lineRows && cost.TrueUp.IsPositive() {
			// The true-up is the line's current value (it moves while the period is open). A partial range
			// carries the share the line-keyed rows would carry: the range's share of the line's usage.
			trueUpDiscount := analyticsV2Share(discount, cost.TrueUp, l.total())
			share := decimal.NewFromInt(1)
			if !lines[i].IsMax {
				share = analyticsV2UsageShare(usage[i], ac.req.StartTime, ac.req.EndTime, l)
			}
			ac.unattributed = append(ac.unattributed, analyticsV2LineCharge(l, dto.AnalyticsV2Cost{TrueUp: cost.TrueUp.Sub(trueUpDiscount).Mul(share)}))
			discount = discount.Sub(trueUpDiscount)
			cost.TrueUp = decimal.Zero
		}

		denom := usage[i].Quantity
		if lines[i].IsMax {
			denom = peaks[i]
		}
		if !denom.IsPositive() {
			if !lines[i].IsMax {
				// No usage to share by: a partial range carries the cost in proportion to time (MAX already is).
				f := analyticsV2Overlap(ac.req.StartTime, ac.req.EndTime, l.key.windowStart, l.key.windowEnd)
				cost, discount = scaleAnalyticsV2Cost(cost, f), discount.Mul(f)
			}
			total := cost.Usage.Add(cost.CommitmentUtilized).Add(cost.Overage).Add(cost.TrueUp)
			if total.IsPositive() {
				charge := analyticsV2LineCharge(l, cost)
				netAnalyticsV2Charge(&charge, discount)
				ac.unattributed = append(ac.unattributed, charge)
			}
			continue
		}

		rate := func(v decimal.Decimal) decimal.Decimal { return v.DivRound(denom, analyticsV2RateScale) }
		r := events.AnalyticsV2Amounts{
			Usage: rate(cost.Usage), CommitmentUtilized: rate(cost.CommitmentUtilized), Overage: rate(cost.Overage),
			TrueUp: rate(cost.TrueUp), Discount: rate(discount),
		}
		if !lines[i].IsMax {
			r.Free = rate(l.freeQuantity)
		}
		ac.ranked = append(ac.ranked, lines[i])
		ac.rates = append(ac.rates, r)
		kept = append(kept, l)
	}
	ac.lines = kept
	return nil
}

func analyticsV2LineCharge(l *analyticsV2LineCost, cost dto.AnalyticsV2Cost) dto.AnalyticsV2UnattributedCharge {
	return dto.AnalyticsV2UnattributedCharge{
		SubscriptionID: l.subscriptionID, PlanID: l.planID, AddonID: l.addonID, MeterID: l.meterID,
		SubLineItemID: l.key.lineItemID, PriceID: l.priceID,
		Cost:      cost.Usage.Add(cost.CommitmentUtilized).Add(cost.Overage).Add(cost.TrueUp),
		Breakdown: cost,
		Reason: lo.Ternary(cost.CommitmentUtilized.IsPositive() || cost.TrueUp.IsPositive(),
			dto.AnalyticsV2ReasonUnusedCommitment, dto.AnalyticsV2ReasonNoUsage),
	}
}

// analyticsV2UsageShare is the range's share of a line window's usage, or of its time when the window has no usage.
func analyticsV2UsageShare(u events.AnalyticsV2LineUsage, start, end time.Time, l *analyticsV2LineCost) decimal.Decimal {
	if u.Quantity.IsPositive() {
		return u.RangeQuantity.Div(u.Quantity)
	}
	return analyticsV2Overlap(start, end, l.key.windowStart, l.key.windowEnd)
}

// analyticsV2Share is part's proportional share of amount, where part is out of whole.
func analyticsV2Share(amount, part, whole decimal.Decimal) decimal.Decimal {
	if whole.IsZero() {
		return decimal.Zero
	}
	return amount.Mul(part).Div(whole)
}

// analyticsV2Overlap is the fraction of [ws, we) covered by [start, end).
func analyticsV2Overlap(start, end, ws, we time.Time) decimal.Decimal {
	from, to := lo.Latest(start, ws), lo.Earliest(end, we)
	if !to.After(from) || !we.After(ws) {
		return decimal.Zero
	}
	return decimal.NewFromInt(int64(to.Sub(from))).Div(decimal.NewFromInt(int64(we.Sub(ws))))
}

// setPriceUnit records the custom price unit when every billed line uses the same one at the same rate, so
// costs can also be shown in that unit. Charges without a line (unlinked) have no unit, so they rule it out.
func (ac *analyticsV2Context) setPriceUnit(lines []*analyticsV2LineCost, hasUnlinked bool) {
	if hasUnlinked {
		return
	}
	var unit string
	var rate decimal.Decimal
	for _, l := range lines {
		if l.total().IsZero() {
			continue
		}
		p := ac.prices[l.priceID]
		if p == nil || p.PriceUnitType != types.PRICE_UNIT_TYPE_CUSTOM || p.PriceUnit == nil || p.ConversionRate == nil || !p.ConversionRate.IsPositive() {
			return
		}
		if unit == "" {
			unit, rate = *p.PriceUnit, *p.ConversionRate
		} else if *p.PriceUnit != unit || !p.ConversionRate.Equal(rate) {
			return
		}
	}
	ac.priceUnit, ac.priceUnitRate = unit, rate
}

// priceUnitCost converts an unrounded fiat cost to the custom price unit (same formula as
// priceunit.ConvertToPriceUnitAmount), or nil when there is none.
func (ac *analyticsV2Context) priceUnitCost(fiat decimal.Decimal) *decimal.Decimal {
	if ac.priceUnit == "" {
		return nil
	}
	v := fiat.Div(ac.priceUnitRate).Round(int32(types.GetCurrencyPrecision(ac.currency)))
	return &v
}

// billingAnchor aligns MONTH windows to billing periods when every subscription shares one anchor.
func (ac *analyticsV2Context) billingAnchor() *time.Time {
	anchor := ac.subs[0].BillingAnchor
	if lo.EveryBy(ac.subs, func(sub *subscription.Subscription) bool { return sub.BillingAnchor.Equal(anchor) }) {
		return &anchor
	}
	return nil
}

func (ac *analyticsV2Context) meterFeatures() map[string]string {
	out := make(map[string]string, len(ac.features))
	for meterID, f := range ac.features {
		out[meterID] = f.ID
	}
	return out
}

func (ac *analyticsV2Context) distinctKeys() []string {
	return lo.FilterMap(ac.req.Metrics, func(m string, _ int) (string, bool) {
		return strings.CutPrefix(m, dto.AnalyticsV2MetricDistinctPrefix)
	})
}

func (ac *analyticsV2Context) filtered() bool {
	return len(ac.req.Filters) > 0 || ac.req.Search != ""
}

func (ac *analyticsV2Context) rankParams() *events.AnalyticsV2RankParams {
	req := ac.req
	return &events.AnalyticsV2RankParams{
		Scope:         ac.scope,
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
		Lines:         ac.ranked,
		Rates:         ac.rates,
		MeterFeatures: ac.meterFeatures(),
		GroupBy:       req.GroupBy,
		Column:        req.Columns,
		Timezone:      ac.timezone,
		Filters: lo.Map(req.Filters, func(f dto.AnalyticsV2Filter, _ int) events.AnalyticsV2Filter {
			return events.AnalyticsV2Filter{Key: f.Key, Exclude: f.Op == dto.AnalyticsV2FilterOpNotIn, Values: f.Values}
		}),
		Search: req.Search,
		Sort: events.AnalyticsV2Sort{
			By:     events.AnalyticsV2SortBy(req.Sort.By),
			Column: req.Sort.Column,
			Desc:   req.Sort.Order == types.SortDirectionDesc,
		},
		Limit:  req.Limit,
		Offset: req.Offset,
	}
}

// assembleAnalyticsV2 builds the response: rows, totals that reconcile to the invoice, unattributed, excluded, expand.
func (s *meterUsageService) assembleAnalyticsV2(
	ctx context.Context,
	ac *analyticsV2Context,
	result *events.AnalyticsV2RankResult,
	detail *events.AnalyticsV2Detail,
) (*dto.GetUsageAnalyticsV2Response, error) {
	req := ac.req
	singleMeter := req.SingleMeter()
	resp := &dto.GetUsageAnalyticsV2Response{
		Currency:       ac.currency,
		TotalCount:     result.TotalCount,
		DataThrough:    ac.scope.DataThrough,
		Items:          make([]dto.AnalyticsV2Item, 0, len(result.Rows)),
		ExcludedMeters: ac.excluded,
	}

	cellsByRow := map[int][]events.AnalyticsV2PointCell{}
	if detail != nil {
		cellsByRow = lo.GroupBy(detail.Points, func(c events.AnalyticsV2PointCell) int { return c.Row })
	}
	for i, row := range result.Rows {
		// A feature_id row of meters without a feature ("") can hold several meters; usage needs exactly one.
		singleMeter := singleMeter && len(ac.rowMeters(row)) <= 1
		item := ac.item(row, singleMeter)
		item.PriceUnitCost = ac.priceUnitCost(row.Measure.Amounts.Cost())
		if detail != nil {
			item.Points = ac.points(cellsByRow[i], singleMeter, item.Cost)
			if i < len(detail.Distinct) && detail.Distinct[i] != nil {
				item.Metrics = make(map[string]dto.AnalyticsV2MetricValue, len(detail.Distinct[i]))
				for key, d := range detail.Distinct[i] {
					item.Metrics[dto.AnalyticsV2MetricDistinctPrefix+key] = dto.AnalyticsV2MetricValue{Count: lo.ToPtr(d.Count), Values: d.Values}
				}
			}
		}
		resp.Items = append(resp.Items, item)
	}

	resp.PriceUnit = ac.priceUnit
	ac.totals(resp, result, cellsByRow[-1], singleMeter)
	if err := s.expandAnalyticsV2(ctx, ac, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (ac *analyticsV2Context) round(v decimal.Decimal) decimal.Decimal {
	return types.RoundToCurrencyPrecision(v, ac.currency)
}

func (ac *analyticsV2Context) cost(a events.AnalyticsV2Amounts) dto.AnalyticsV2Cost {
	return dto.AnalyticsV2Cost{
		Usage: ac.round(a.Usage), CommitmentUtilized: ac.round(a.CommitmentUtilized),
		Overage: ac.round(a.Overage), TrueUp: ac.round(a.TrueUp),
	}
}

// groupMap names a row's values by key, dropping the "properties." prefix.
func (ac *analyticsV2Context) groupMap(values []string) map[string]string {
	out := make(map[string]string, len(values))
	for i, k := range ac.req.GroupBy {
		out[strings.TrimPrefix(k, "properties.")] = values[i]
	}
	return out
}

func (ac *analyticsV2Context) item(row events.AnalyticsV2Row, singleMeter bool) dto.AnalyticsV2Item {
	a := row.Measure.Amounts
	item := dto.AnalyticsV2Item{
		Group:      ac.groupMap(row.Group),
		Subtotal:   ac.round(a.Subtotal()),
		Discount:   ac.round(a.Discount),
		Cost:       ac.round(a.Cost()),
		Breakdown:  ac.cost(a),
		EventCount: row.Measure.EventCount,
	}
	if singleMeter {
		item.Usage = lo.ToPtr(row.Measure.Quantity.Add(row.Measure.Peak))
		item.FreeUsage = lo.ToPtr(a.Free)
		if f := ac.rowFeature(row); f != nil {
			item.Unit, item.UnitPlural, item.ReportingUnit = f.UnitSingular, f.UnitPlural, f.ReportingUnit
		}
	}
	if row.Columns != nil {
		item.Columns = make(map[string]dto.AnalyticsV2Column, len(row.Columns))
		for name, c := range row.Columns {
			col := dto.AnalyticsV2Column{Cost: ac.round(c.Amounts.Usage)}
			if singleMeter {
				col.Usage = lo.ToPtr(c.Quantity.Add(c.Peak))
			}
			item.Columns[name] = col
		}
		reconcileAnalyticsV2Columns(item.Columns, item.Cost)
	}

	var priceIDs, subIDs, planIDs, addonIDs []string
	for _, idx := range row.LineIndexes {
		if idx < 0 || idx >= len(ac.lines) {
			continue
		}
		l := ac.lines[idx]
		priceIDs, subIDs = append(priceIDs, l.priceID), append(subIDs, l.subscriptionID)
		if l.planID != "" {
			planIDs = append(planIDs, l.planID)
		}
		if l.addonID != "" {
			addonIDs = append(addonIDs, l.addonID)
		}
	}
	item.PriceIDs, item.SubscriptionIDs = sortedUniq(priceIDs), sortedUniq(subIDs)
	item.PlanIDs, item.AddonIDs = sortedUniq(planIDs), sortedUniq(addonIDs)
	return item
}

func sortedUniq(v []string) []string {
	if len(v) == 0 {
		return nil
	}
	out := lo.Uniq(v)
	sort.Strings(out)
	return out
}

// rowMeters lists the distinct meters of a row's lines.
func (ac *analyticsV2Context) rowMeters(row events.AnalyticsV2Row) []string {
	return lo.Uniq(lo.FilterMap(row.LineIndexes, func(idx int, _ int) (string, bool) {
		if idx < 0 || idx >= len(ac.lines) {
			return "", false
		}
		return ac.lines[idx].meterID, true
	}))
}

// rowFeature is the feature of a single-meter row's meter.
func (ac *analyticsV2Context) rowFeature(row events.AnalyticsV2Row) *feature.Feature {
	meters := ac.rowMeters(row)
	if len(meters) != 1 {
		return nil
	}
	return ac.features[meters[0]]
}

// points rolls one row's cells (or the all-rows cells) up to one point per window, with columns.
// Rounded point costs add up to target, the already-rounded cost they split.
func (ac *analyticsV2Context) points(cells []events.AnalyticsV2PointCell, singleMeter bool, target decimal.Decimal) []dto.AnalyticsV2Point {
	byWindow := map[time.Time]*dto.AnalyticsV2Point{}
	amounts := map[time.Time]events.AnalyticsV2Amounts{}
	// Usage adds up for additive lines; for MAX lines a window's usage is its highest peak.
	usage, peak := map[time.Time]decimal.Decimal{}, map[time.Time]decimal.Decimal{}
	colUsage := map[time.Time]map[string]decimal.Decimal{}
	for _, c := range cells {
		// Cells arrive priced; usage is a quantity for additive lines and a peak for MAX lines.
		cost, qty := c.Amounts.Cost(), lo.Ternary(c.IsMax, c.Peak, c.Quantity)
		p := byWindow[c.WindowStart]
		if p == nil {
			p = &dto.AnalyticsV2Point{WindowStart: c.WindowStart}
			byWindow[c.WindowStart] = p
			colUsage[c.WindowStart] = map[string]decimal.Decimal{}
		}
		amounts[c.WindowStart] = addAnalyticsV2Amounts(amounts[c.WindowStart], c.Amounts)
		p.EventCount += c.EventCount
		if c.IsMax {
			peak[c.WindowStart] = decimal.Max(peak[c.WindowStart], qty)
		} else {
			usage[c.WindowStart] = usage[c.WindowStart].Add(qty)
		}
		if ac.req.Columns != "" {
			if p.Columns == nil {
				p.Columns = map[string]dto.AnalyticsV2Column{}
			}
			col := p.Columns[c.Column]
			col.Cost = col.Cost.Add(cost)
			p.Columns[c.Column] = col
			colUsage[c.WindowStart][c.Column] = colUsage[c.WindowStart][c.Column].Add(qty)
		}
	}

	out := make([]dto.AnalyticsV2Point, 0, len(byWindow))
	for w, p := range byWindow {
		a := amounts[w]
		p.Cost, p.Discount, p.Breakdown = ac.round(a.Cost()), ac.round(a.Discount), ac.cost(a)
		if singleMeter {
			p.Usage = lo.ToPtr(usage[w].Add(peak[w]))
		}
		for name, col := range p.Columns {
			col.Cost = ac.round(col.Cost)
			if singleMeter {
				col.Usage = lo.ToPtr(colUsage[w][name])
			}
			p.Columns[name] = col
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WindowStart.Before(out[j].WindowStart) })
	reconcileAnalyticsV2Rounding(lo.Map(out, func(_ dto.AnalyticsV2Point, i int) *decimal.Decimal { return &out[i].Cost }), target)
	for i := range out {
		out[i].Subtotal = out[i].Cost.Add(out[i].Discount)
		reconcileAnalyticsV2Columns(out[i].Columns, out[i].Cost)
	}
	return out
}

func addAnalyticsV2Amounts(a, b events.AnalyticsV2Amounts) events.AnalyticsV2Amounts {
	return events.AnalyticsV2Amounts{
		Usage: a.Usage.Add(b.Usage), CommitmentUtilized: a.CommitmentUtilized.Add(b.CommitmentUtilized),
		Overage: a.Overage.Add(b.Overage), TrueUp: a.TrueUp.Add(b.TrueUp), Discount: a.Discount.Add(b.Discount), Free: a.Free.Add(b.Free),
	}
}

// reconcileAnalyticsV2Columns makes rounded column costs add up to cost, in a stable column order.
func reconcileAnalyticsV2Columns(columns map[string]dto.AnalyticsV2Column, cost decimal.Decimal) {
	if len(columns) == 0 {
		return
	}
	names := lo.Keys(columns)
	sort.Strings(names)
	costs := make([]decimal.Decimal, len(names))
	for i, n := range names {
		costs[i] = columns[n].Cost
	}
	reconcileAnalyticsV2Rounding(lo.Map(costs, func(_ decimal.Decimal, i int) *decimal.Decimal { return &costs[i] }), cost)
	for i, n := range names {
		col := columns[n]
		col.Cost = costs[i]
		columns[n] = col
	}
}

// reconcileAnalyticsV2Rounding puts the cents lost to rounding on the largest value, so the values add up to target.
func reconcileAnalyticsV2Rounding(values []*decimal.Decimal, target decimal.Decimal) {
	if len(values) == 0 {
		return
	}
	sum, largest := decimal.Zero, 0
	for i, v := range values {
		sum = sum.Add(*v)
		if v.Abs().GreaterThan(values[largest].Abs()) {
			largest = i
		}
	}
	*values[largest] = values[largest].Add(target.Sub(sum))
}

// totals fills totals so that cost = rows + unattributed + excluded. With filters or search the view is a
// slice of rows, so unattributed is left out and excluded meters carry no cost.
func (ac *analyticsV2Context) totals(resp *dto.GetUsageAnalyticsV2Response, result *events.AnalyticsV2RankResult, allRowCells []events.AnalyticsV2PointCell, singleMeter bool) {
	rows := result.Totals.Amounts
	t := &resp.Totals
	t.RowsCost = ac.round(rows.Cost())
	t.Breakdown = ac.cost(rows)
	t.Discount = ac.round(rows.Discount)

	if !ac.filtered() {
		for _, c := range ac.unattributed {
			resp.Unattributed.Cost = resp.Unattributed.Cost.Add(c.Cost)
			resp.Unattributed.Breakdown = addAnalyticsV2Cost(resp.Unattributed.Breakdown, c.Breakdown)
		}
		resp.Unattributed.Cost = ac.round(resp.Unattributed.Cost)
		resp.Unattributed.Breakdown = roundAnalyticsV2Cost(ac, resp.Unattributed.Breakdown)
		resp.Unattributed.Charges = lo.Map(ac.unattributed, func(c dto.AnalyticsV2UnattributedCharge, _ int) dto.AnalyticsV2UnattributedCharge {
			c.Cost, c.Breakdown = ac.round(c.Cost), roundAnalyticsV2Cost(ac, c.Breakdown)
			return c
		})
		reconcileAnalyticsV2Rounding(lo.Map(resp.Unattributed.Charges, func(_ dto.AnalyticsV2UnattributedCharge, i int) *decimal.Decimal {
			return &resp.Unattributed.Charges[i].Cost
		}), resp.Unattributed.Cost)
		t.UnattributedCost = resp.Unattributed.Cost
		t.Breakdown = addAnalyticsV2Cost(t.Breakdown, resp.Unattributed.Breakdown)

		exact := rows.Cost()
		for _, c := range ac.unattributed {
			exact = exact.Add(c.Cost)
		}
		for i, ex := range resp.ExcludedMeters {
			exact = exact.Add(lo.FromPtr(ex.Cost))
			t.ExcludedCost = t.ExcludedCost.Add(lo.FromPtr(ex.Cost))
			resp.ExcludedMeters[i].Cost = lo.ToPtr(ac.round(lo.FromPtr(ex.Cost)))
		}
		t.ExcludedCost = ac.round(t.ExcludedCost)
		reconcileAnalyticsV2Rounding(lo.FilterMap(resp.ExcludedMeters, func(ex dto.AnalyticsV2ExcludedMeter, _ int) (*decimal.Decimal, bool) {
			return ex.Cost, ex.Cost != nil
		}), t.ExcludedCost)
		t.PriceUnitCost = ac.priceUnitCost(exact)
		t.Breakdown.Usage = t.Breakdown.Usage.Add(t.ExcludedCost)
	} else {
		for i := range resp.ExcludedMeters {
			resp.ExcludedMeters[i].Cost = nil
		}
	}
	t.Cost = t.RowsCost.Add(t.UnattributedCost).Add(t.ExcludedCost)
	t.Subtotal = t.Cost.Add(t.Discount)

	if singleMeter {
		t.Usage = lo.ToPtr(result.Totals.Quantity.Add(result.Totals.Peak))
		t.FreeUsage = lo.ToPtr(rows.Free)
	}
	if result.ColumnTotals != nil {
		t.Columns = make(map[string]dto.AnalyticsV2Column, len(result.ColumnTotals))
		for name, c := range result.ColumnTotals {
			col := dto.AnalyticsV2Column{Cost: ac.round(c.Amounts.Usage)}
			if singleMeter {
				col.Usage = lo.ToPtr(c.Quantity.Add(c.Peak))
			}
			t.Columns[name] = col
			resp.ColumnValues = append(resp.ColumnValues, name)
		}
		sort.Strings(resp.ColumnValues)
	}
	if ac.req.WindowSize != "" && len(allRowCells) > 0 {
		t.Points = ac.points(allRowCells, singleMeter, t.RowsCost)
	}
}

func addAnalyticsV2Cost(a, b dto.AnalyticsV2Cost) dto.AnalyticsV2Cost {
	return dto.AnalyticsV2Cost{
		Usage: a.Usage.Add(b.Usage), CommitmentUtilized: a.CommitmentUtilized.Add(b.CommitmentUtilized),
		Overage: a.Overage.Add(b.Overage), TrueUp: a.TrueUp.Add(b.TrueUp),
	}
}

func roundAnalyticsV2Cost(ac *analyticsV2Context, c dto.AnalyticsV2Cost) dto.AnalyticsV2Cost {
	return dto.AnalyticsV2Cost{
		Usage: ac.round(c.Usage), CommitmentUtilized: ac.round(c.CommitmentUtilized),
		Overage: ac.round(c.Overage), TrueUp: ac.round(c.TrueUp),
	}
}

// expandAnalyticsV2 fills the object maps requested in expand, for objects the response references.
func (s *meterUsageService) expandAnalyticsV2(ctx context.Context, ac *analyticsV2Context, resp *dto.GetUsageAnalyticsV2Response) error {
	want := lo.SliceToMap(ac.req.Expand, func(e string) (string, bool) { return e, true })
	if len(want) == 0 {
		return nil
	}
	collect := func(pick func(dto.AnalyticsV2Item) []string, charge func(dto.AnalyticsV2UnattributedCharge) string) []string {
		var ids []string
		for _, it := range resp.Items {
			ids = append(ids, pick(it)...)
		}
		for _, c := range resp.Unattributed.Charges {
			if id := charge(c); id != "" {
				ids = append(ids, id)
			}
		}
		return sortedUniq(ids)
	}

	if want["price"] {
		resp.Prices = map[string]*dto.PriceResponse{}
		for _, id := range collect(func(it dto.AnalyticsV2Item) []string { return it.PriceIDs }, func(c dto.AnalyticsV2UnattributedCharge) string { return c.PriceID }) {
			if p, ok := ac.prices[id]; ok {
				resp.Prices[id] = &dto.PriceResponse{Price: p}
			}
		}
	}
	if want["subscription"] {
		ids := collect(func(it dto.AnalyticsV2Item) []string { return it.SubscriptionIDs }, func(c dto.AnalyticsV2UnattributedCharge) string { return c.SubscriptionID })
		resp.Subscriptions = map[string]*dto.SubscriptionResponse{}
		for _, sub := range ac.subs {
			if lo.Contains(ids, sub.ID) {
				resp.Subscriptions[sub.ID] = &dto.SubscriptionResponse{Subscription: sub}
			}
		}
	}
	if want["plan"] {
		ids := collect(func(it dto.AnalyticsV2Item) []string { return it.PlanIDs }, func(c dto.AnalyticsV2UnattributedCharge) string { return c.PlanID })
		resp.Plans = map[string]*dto.PlanResponse{}
		if len(ids) > 0 {
			plans, err := s.PlanRepo.ListByIDs(ctx, ids)
			if err != nil {
				return err
			}
			for _, p := range plans {
				resp.Plans[p.ID] = &dto.PlanResponse{Plan: p}
			}
		}
	}
	if want["addon"] {
		ids := collect(func(it dto.AnalyticsV2Item) []string { return it.AddonIDs }, func(c dto.AnalyticsV2UnattributedCharge) string { return c.AddonID })
		resp.Addons = map[string]*dto.AddonResponse{}
		for _, id := range ids {
			a, err := s.AddonRepo.GetByID(ctx, id)
			if err != nil {
				return err
			}
			resp.Addons[id] = &dto.AddonResponse{Addon: a}
		}
	}

	meterIDs := lo.Uniq(lo.FlatMap(resp.Items, func(it dto.AnalyticsV2Item, _ int) []string {
		var ids []string
		for _, l := range ac.lines {
			if lo.Contains(it.PriceIDs, l.priceID) {
				ids = append(ids, l.meterID)
			}
		}
		return ids
	}))
	if want["meter"] {
		resp.Meters = map[string]*dto.MeterResponse{}
		for _, id := range meterIDs {
			if m := ac.meters[id]; m != nil {
				resp.Meters[id] = dto.ToMeterResponse(m)
			}
		}
	}
	if want["feature"] {
		resp.Features = map[string]*dto.FeatureResponse{}
		for _, id := range meterIDs {
			if f := ac.features[id]; f != nil {
				resp.Features[f.ID] = &dto.FeatureResponse{Feature: f}
			}
		}
	}
	return nil
}
