package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/costanalytics"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Reasons a meter is left out of a cost breakdown.
const (
	excludedNotFlatPrice      = "not_flat_price"
	excludedCustomPriceUnit   = "custom_price_unit"
	excludedUnsupportedMeter  = "unsupported_meter"
	excludedLineCommitment    = "line_commitment"
	excludedCommitment        = "commitment"
	excludedEntitlement       = "entitlement"
	excludedEntitlementGrant  = "entitlement_grant"
	excludedSubscriptionGrant = "subscription_entitlement_grant"
)

// CostAnalyticsService attributes billed cost to the events, and their properties, that caused it.
type CostAnalyticsService interface {
	GetCostBreakdown(ctx context.Context, req *dto.CostAnalyticsRequest) (*dto.CostAnalyticsResponse, error)
}

type costAnalyticsService struct {
	ServiceParams
	repo costanalytics.Repository
}

func NewCostAnalyticsService(params ServiceParams, repo costanalytics.Repository) CostAnalyticsService {
	return &costAnalyticsService{ServiceParams: params, repo: repo}
}

// costSubscription is a requested customer's subscription with the subscription whose lines price it
// (the parent, for an inherited child) and the customers whose events it covers.
type costSubscription struct {
	sub         *subscription.Subscription
	owner       *subscription.Subscription
	customerIDs []string // external IDs
	start, end  time.Time
}

// costLine is a usage line item with its price and meter.
type costLine struct {
	item      *subscription.SubscriptionLineItem
	price     *price.Price
	meter     *meter.Meter
	featureID string
}

func (s *costAnalyticsService) GetCostBreakdown(ctx context.Context, req *dto.CostAnalyticsRequest) (*dto.CostAnalyticsResponse, error) {
	if req.ExternalCustomerID == "" {
		return nil, ierr.NewError("external_customer_id is required").Mark(ierr.ErrValidation)
	}
	q, err := req.ToQuery(clock.Now())
	if err != nil {
		return nil, err
	}

	cust, err := s.CustomerRepo.GetByLookupKey(ctx, req.ExternalCustomerID)
	if err != nil {
		return nil, err
	}
	q.CustomerID = cust.ID

	meterFilter, err := s.requestedMeterIDs(ctx, q)
	if err != nil {
		return nil, err
	}

	subs, err := s.subscriptionsInRange(ctx, cust, q.Start, q.End)
	if err != nil {
		return nil, err
	}

	var (
		windows  []costanalytics.LineWindow
		excluded = map[string]dto.CostExcludedMeter{}
		lineByID = map[string]*subscription.SubscriptionLineItem{}
	)
	for _, cs := range subs {
		lines, err := s.usageLineItems(ctx, cs.owner, meterFilter)
		if err != nil {
			return nil, err
		}
		if len(lines) == 0 {
			continue
		}
		entitled, err := s.entitledMeters(ctx, cs.owner)
		if err != nil {
			return nil, err
		}
		for _, seg := range splitByBillingPeriod(cs, clock.Now()) {
			ineligible, err := s.checkEligible(ctx, cs.owner, seg, lines, entitled)
			if err != nil {
				return nil, err
			}
			for meterID, e := range ineligible {
				if _, seen := excluded[meterID]; !seen {
					excluded[meterID] = e
				}
			}
			for _, l := range lines {
				start, end := l.item.GetPeriodStart(seg.Start), l.item.GetPeriodEnd(seg.End)
				if !start.Before(end) {
					continue
				}
				lineByID[l.item.ID] = l.item
				windows = append(windows, costanalytics.LineWindow{
					LineItemID:          l.item.ID,
					MeterID:             l.item.MeterID,
					FeatureID:           l.featureID,
					ExternalCustomerIDs: cs.customerIDs,
					Segment:             seg,
					Start:               start,
					End:                 end,
					Currency:            l.price.Currency,
					UnitPrice:           l.price.Amount,
				})
			}
		}
	}

	// A meter with any ineligible line is left out entirely, so no row mixes priced and unpriced usage.
	windows = lo.Filter(windows, func(w costanalytics.LineWindow, _ int) bool { _, out := excluded[w.MeterID]; return !out })

	currency, err := singleCurrency(windows)
	if err != nil {
		return nil, err
	}
	if q.Sort.Field == costanalytics.SortUsage && len(lo.Uniq(lo.Map(windows, func(w costanalytics.LineWindow, _ int) string { return w.MeterID }))) > 1 {
		return nil, ierr.NewError("sort by usage needs a single meter").
			WithHint("Quantities of different meters cannot be compared; pass one meter_id or sort by total").
			Mark(ierr.ErrValidation)
	}

	result, err := s.repo.GetCostBreakdown(ctx, &costanalytics.QueryParams{
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		Query:         q,
		Lines:         windows,
	})
	if err != nil {
		return nil, err
	}

	return toCostAnalyticsResponse(q, req.Expand, currency, result, lineByID, excluded), nil
}

// requestedMeterIDs resolves meter_ids and feature_ids to one meter set; nil means every meter.
func (s *costAnalyticsService) requestedMeterIDs(ctx context.Context, q *costanalytics.Query) (map[string]bool, error) {
	if len(q.MeterIDs) == 0 && len(q.FeatureIDs) == 0 {
		return nil, nil
	}
	ids := lo.SliceToMap(q.MeterIDs, func(id string) (string, bool) { return id, true })
	if len(q.FeatureIDs) > 0 {
		filter := types.NewNoLimitFeatureFilter()
		filter.FeatureIDs = q.FeatureIDs
		features, err := s.FeatureRepo.List(ctx, filter)
		if err != nil {
			return nil, err
		}
		for _, f := range features {
			if f.MeterID != "" {
				ids[f.MeterID] = true
			}
		}
	}
	return ids, nil
}

// subscriptionsInRange returns the customer's subscriptions active in [start, end), each with the
// subscription that prices it and the customers whose events it covers.
func (s *costAnalyticsService) subscriptionsInRange(ctx context.Context, cust *customer.Customer, start, end time.Time) ([]costSubscription, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.CustomerID = cust.ID
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusPaused,
		types.SubscriptionStatusCancelled,
	}
	subs, err := s.SubRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	subService := NewSubscriptionService(s.ServiceParams)
	out := make([]costSubscription, 0, len(subs))
	for _, sub := range subs {
		subStart, subEnd := maxTime(start, sub.StartDate), end
		if sub.EndDate != nil && sub.EndDate.Before(subEnd) {
			subEnd = *sub.EndDate
		}
		if sub.SubscriptionStatus == types.SubscriptionStatusCancelled && sub.CancelledAt != nil && sub.CancelledAt.Before(subEnd) {
			subEnd = *sub.CancelledAt
		}
		if !subStart.Before(subEnd) {
			continue
		}

		cs := costSubscription{sub: sub, owner: sub, start: subStart, end: subEnd}
		if sub.SubscriptionType == types.SubscriptionTypeInherited && lo.FromPtr(sub.ParentSubscriptionID) != "" {
			// A child is priced on its parent's lines, over its own events only.
			parent, err := s.SubRepo.Get(ctx, lo.FromPtr(sub.ParentSubscriptionID))
			if err != nil {
				return nil, err
			}
			cs.owner = parent
			cs.customerIDs = []string{cust.ExternalID}
		} else {
			// A parent covers its own and every inherited child's events, as its invoice does.
			cs.customerIDs, err = subService.ExternalCustomerIDsForSubscription(ctx, sub)
			if err != nil {
				return nil, err
			}
		}
		if len(cs.customerIDs) > 0 {
			out = append(out, cs)
		}
	}
	return out, nil
}

// splitByBillingPeriod cuts a subscription's range at the billing period boundaries of its owner.
func splitByBillingPeriod(cs costSubscription, now time.Time) []costanalytics.BillingPeriodSegment {
	owner := cs.owner
	grid, err := types.NewBillingPeriodGrid(owner.BillingAnchor, owner.BillingPeriod, max(owner.BillingPeriodCount, 1), owner.Timezone)
	if err != nil {
		// A subscription without a valid schedule is one segment; flat cost does not depend on periods.
		return []costanalytics.BillingPeriodSegment{{
			SubscriptionID: cs.sub.ID, PeriodStart: cs.start, PeriodEnd: cs.end, Start: cs.start, End: cs.end,
		}}
	}

	var segments []costanalytics.BillingPeriodSegment
	for t := cs.start; t.Before(cs.end); {
		p := types.FullBillingPeriod(t, grid)
		if !p.End.After(t) {
			break
		}
		seg := costanalytics.BillingPeriodSegment{
			SubscriptionID: cs.sub.ID,
			PeriodStart:    maxTime(p.Start, owner.StartDate),
			PeriodEnd:      p.End,
			Start:          t,
			End:            minTime(p.End, cs.end),
			Closed:         !p.End.After(now),
		}
		segments = append(segments, seg)
		t = seg.End
	}
	return segments
}

// usageLineItems returns the owner's usage line items on the requested meters, with prices and meters.
func (s *costAnalyticsService) usageLineItems(ctx context.Context, owner *subscription.Subscription, meterFilter map[string]bool) ([]costLine, error) {
	filter := types.NewNoLimitSubscriptionLineItemFilter()
	filter.SubscriptionIDs = []string{owner.ID}
	filter.ActiveFilter = false
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	items, err := s.SubscriptionLineItemRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	items = lo.Filter(items, func(li *subscription.SubscriptionLineItem, _ int) bool {
		return li.PriceType == types.PRICE_TYPE_USAGE && li.MeterID != "" && (meterFilter == nil || meterFilter[li.MeterID])
	})
	if len(items) == 0 {
		return nil, nil
	}

	priceFilter := types.NewNoLimitPriceFilter()
	priceFilter.PriceIDs = lo.Uniq(lo.Map(items, func(li *subscription.SubscriptionLineItem, _ int) string { return li.PriceID }))
	priceFilter.AllowExpiredPrices = true
	prices, err := s.PriceRepo.List(ctx, priceFilter)
	if err != nil {
		return nil, err
	}
	priceByID := lo.KeyBy(prices, func(p *price.Price) string { return p.ID })

	meters, err := s.MeterRepo.ListByIDs(ctx, lo.Uniq(lo.Map(items, func(li *subscription.SubscriptionLineItem, _ int) string { return li.MeterID })))
	if err != nil {
		return nil, err
	}
	meterByID := lo.KeyBy(meters, func(m *meter.Meter) string { return m.ID })

	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.MeterIDs = lo.Keys(meterByID)
	features, err := s.FeatureRepo.List(ctx, featureFilter)
	if err != nil {
		return nil, err
	}
	featureByMeter := map[string]string{}
	for _, f := range features {
		if f.MeterID != "" {
			featureByMeter[f.MeterID] = f.ID
		}
	}

	out := make([]costLine, 0, len(items))
	for _, li := range items {
		p, m := priceByID[li.PriceID], meterByID[li.MeterID]
		if p == nil || m == nil {
			return nil, ierr.NewError("price or meter of a usage line item not found").
				WithReportableDetails(map[string]any{"line_item_id": li.ID, "price_id": li.PriceID, "meter_id": li.MeterID}).
				Mark(ierr.ErrNotFound)
		}
		out = append(out, costLine{item: li, price: p, meter: m, featureID: featureByMeter[li.MeterID]})
	}
	return out, nil
}

// checkEligible returns the meters whose lines this segment cannot price as flat, with the first
// line and reason found. Every rule that changes a flat bill (entitlements, grants, commitments) excludes.
func (s *costAnalyticsService) checkEligible(
	ctx context.Context,
	owner *subscription.Subscription,
	seg costanalytics.BillingPeriodSegment,
	lines []costLine,
	entitled map[string]bool,
) (map[string]dto.CostExcludedMeter, error) {
	out := map[string]dto.CostExcludedMeter{}
	exclude := func(l costLine, reason string) {
		if _, seen := out[l.item.MeterID]; !seen {
			out[l.item.MeterID] = dto.CostExcludedMeter{
				MeterID: l.item.MeterID, SubLineItemID: l.item.ID, SubscriptionID: owner.ID, Reason: reason,
			}
		}
	}
	active := lo.Filter(lines, func(l costLine, _ int) bool {
		return l.item.GetPeriodStart(seg.Start).Before(l.item.GetPeriodEnd(seg.End))
	})
	if len(active) == 0 {
		return out, nil
	}

	blockers, err := s.loadBlockers(ctx, owner, seg)
	if err != nil {
		return nil, err
	}

	for _, l := range active {
		switch {
		case l.price.BillingModel != types.BILLING_MODEL_FLAT_FEE:
			exclude(l, excludedNotFlatPrice)
		case l.price.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM || lo.FromPtr(l.item.PriceUnitID) != "":
			exclude(l, excludedCustomPriceUnit)
		case !isCostSupportedMeter(l.meter) || price.IsBucketed(l.price, l.meter):
			exclude(l, excludedUnsupportedMeter)
		case l.item.HasAnyCommitment():
			exclude(l, excludedLineCommitment)
		case blockers.commitment:
			exclude(l, excludedCommitment)
		case entitled[l.item.MeterID]:
			exclude(l, excludedEntitlement)
		case blockers.grantedMeters[l.item.MeterID]:
			exclude(l, excludedEntitlementGrant)
		case blockers.subscriptionGrant:
			exclude(l, excludedSubscriptionGrant)
		}
	}
	return out, nil
}

// costBlockers are the owner-level rules that change a flat bill in a billing period.
type costBlockers struct {
	commitment        bool
	subscriptionGrant bool
	grantedMeters     map[string]bool
}

func (s *costAnalyticsService) loadBlockers(ctx context.Context, owner *subscription.Subscription, seg costanalytics.BillingPeriodSegment) (costBlockers, error) {
	b := costBlockers{grantedMeters: map[string]bool{}}

	b.commitment = owner.HasCommitment()
	if !b.commitment {
		svc := newCommitmentService(s.ServiceParams)
		enabled, err := svc.IsEnabled(ctx)
		if err != nil {
			return b, err
		}
		if enabled {
			commitments, err := svc.ListOverlapping(ctx, owner.ID, seg.PeriodStart, seg.PeriodEnd)
			if err != nil {
				return b, err
			}
			b.commitment = len(commitments) > 0
		}
	}

	if s.EntitlementGrantRepo == nil {
		return b, nil
	}
	grants, err := s.EntitlementGrantRepo.List(ctx, types.NewNoLimitEntitlementGrantFilter().
		WithSubscriptionIDs(owner.ID).
		WithCycleOverlap(seg.PeriodStart, seg.PeriodEnd))
	if err != nil {
		return b, err
	}
	// Grants outlive their entitlement config, so their features are read directly.
	var grantFeatures []string
	for _, g := range grants {
		switch {
		case g.IsSubscriptionScoped():
			b.subscriptionGrant = true
		case g.IsFeatureScoped():
			grantFeatures = append(grantFeatures, g.ScopeEntityID)
		}
	}
	if len(grantFeatures) > 0 {
		filter := types.NewNoLimitFeatureFilter()
		filter.FeatureIDs = lo.Uniq(grantFeatures)
		features, err := s.FeatureRepo.List(ctx, filter)
		if err != nil {
			return b, err
		}
		for _, f := range features {
			if f.MeterID != "" {
				b.grantedMeters[f.MeterID] = true
			}
		}
	}
	return b, nil
}

// entitledMeters returns the meters with an enabled entitlement; billing zeroes or reduces them,
// whether the entitlement is limited or not.
func (s *costAnalyticsService) entitledMeters(ctx context.Context, owner *subscription.Subscription) (map[string]bool, error) {
	entitlements, err := NewSubscriptionService(s.ServiceParams).GetAggregatedSubscriptionEntitlementsForSubscription(ctx, owner, nil)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, f := range entitlements.Features {
		if f.Feature != nil && types.FeatureType(f.Feature.Type) == types.FeatureTypeMetered &&
			f.Feature.MeterID != "" && f.Entitlement != nil && f.Entitlement.IsEnabled {
			out[f.Feature.MeterID] = true
		}
	}
	return out, nil
}

// isCostSupportedMeter reports whether qty_total is the billed quantity of each event.
func isCostSupportedMeter(m *meter.Meter) bool {
	switch m.Aggregation.Type {
	case types.AggregationSum, types.AggregationCount, types.AggregationSumWithMultiplier:
		return true
	}
	return false
}

func singleCurrency(windows []costanalytics.LineWindow) (string, error) {
	currencies := lo.Uniq(lo.Map(windows, func(w costanalytics.LineWindow, _ int) string { return strings.ToLower(w.Currency) }))
	switch len(currencies) {
	case 0:
		return "", nil
	case 1:
		return currencies[0], nil
	}
	return "", ierr.NewError("customer is billed in more than one currency").
		WithHint("Cost analytics supports one currency per customer; filter by meter_ids to one currency").
		WithReportableDetails(map[string]any{"currencies": currencies}).
		Mark(ierr.ErrValidation)
}

// toCostAnalyticsResponse rounds money once per row and once for totals, names missing values
// "(none)" and keeps the top column values by total cost.
func toCostAnalyticsResponse(
	q *costanalytics.Query,
	expand []dto.CostExpand,
	currency string,
	result *costanalytics.Result,
	lineByID map[string]*subscription.SubscriptionLineItem,
	excluded map[string]dto.CostExcludedMeter,
) *dto.CostAnalyticsResponse {
	precision := types.GetCurrencyPrecision(currency)
	money := func(d decimal.Decimal) string { return d.Round(precision).StringFixed(precision) }
	none := func(v string) string { return lo.Ternary(v == "", dto.CostNoneValue, v) }

	resp := &dto.CostAnalyticsResponse{
		Currency:       currency,
		TotalCount:     result.TotalCount,
		Items:          make([]dto.CostAnalyticsItem, 0, len(result.Items)),
		ExcludedMeters: lo.Values(excluded),
		ContractRows:   []dto.CostAnalyticsContract{},
		Totals: dto.CostAnalyticsTotals{
			Cost:       money(result.Totals.Cost),
			Quantity:   result.Totals.Quantity.String(),
			EventCount: result.Totals.EventCount,
		},
	}
	sort.Slice(resp.ExcludedMeters, func(i, j int) bool { return resp.ExcludedMeters[i].MeterID < resp.ExcludedMeters[j].MeterID })

	var shown map[string]bool
	if q.Columns != nil {
		resp.ColumnValues, shown = topColumnValues(result.Totals.Columns)
		resp.Totals.Columns = costColumns(result.Totals.Columns, shown, money, none)
	}

	for _, row := range result.Items {
		item := dto.CostAnalyticsItem{
			Group:      make(map[string]string, len(q.GroupBy)),
			Cost:       money(row.Cost),
			Quantity:   row.Quantity.String(),
			EventCount: row.EventCount,
		}
		for i, k := range q.GroupBy {
			item.Group[k.String()] = none(row.Group[i])
		}
		if q.Columns != nil {
			item.Columns = costColumns(row.Columns, shown, money, none)
		}
		if len(row.Metrics) > 0 {
			item.Metrics = make(map[string]dto.CostAnalyticsValue, len(row.Metrics))
			for name, v := range row.Metrics {
				item.Metrics[name] = dto.CostAnalyticsValue{Count: v.Count, Values: v.Values}
			}
		}
		expandCostItem(&item, expand, row.LineItemIDs, lineByID)
		resp.Items = append(resp.Items, item)
	}
	return resp
}

// topColumnValues returns the MaxCostColumns values with the highest total cost, in that order.
func topColumnValues(totals map[string]decimal.Decimal) ([]string, map[string]bool) {
	values := lo.Keys(totals)
	sort.Slice(values, func(i, j int) bool {
		if c := totals[values[i]].Cmp(totals[values[j]]); c != 0 {
			return c > 0
		}
		return values[i] < values[j]
	})
	if len(values) > dto.MaxCostColumns {
		values = values[:dto.MaxCostColumns]
	}
	shown := lo.SliceToMap(values, func(v string) (string, bool) { return v, true })
	return lo.Map(values, func(v string, _ int) string { return lo.Ternary(v == "", dto.CostNoneValue, v) }), shown
}

// costColumns keeps the shown column values and sums the rest into "(other)", so columns add up to the row.
func costColumns(costs map[string]decimal.Decimal, shown map[string]bool, money func(decimal.Decimal) string, none func(string) string) map[string]dto.CostColumn {
	out := make(map[string]dto.CostColumn, len(costs))
	other, hasOther := decimal.Zero, false
	for v, c := range costs {
		if shown[v] {
			out[none(v)] = dto.CostColumn{Cost: money(c)}
			continue
		}
		other, hasOther = other.Add(c), true
	}
	if hasOther {
		out[dto.CostOtherColumn] = dto.CostColumn{Cost: money(other)}
	}
	return out
}

func expandCostItem(item *dto.CostAnalyticsItem, expand []dto.CostExpand, lineIDs []string, lineByID map[string]*subscription.SubscriptionLineItem) {
	if len(expand) == 0 {
		return
	}
	var prices, plans, addons, subs []string
	for _, id := range lineIDs {
		li := lineByID[id]
		if li == nil {
			continue
		}
		prices = append(prices, li.PriceID)
		subs = append(subs, li.SubscriptionID)
		switch li.EntityType {
		case types.SubscriptionLineItemEntityTypePlan:
			plans = append(plans, li.EntityID)
		case types.SubscriptionLineItemEntityTypeAddon:
			addons = append(addons, li.EntityID)
		}
	}
	ids := func(v []string) []string {
		v = lo.Uniq(lo.Compact(v))
		sort.Strings(v)
		return v
	}
	for _, e := range lo.Uniq(expand) {
		switch e {
		case dto.CostExpandPrice:
			item.PriceIDs = ids(prices)
		case dto.CostExpandPlan:
			item.PlanIDs = ids(plans)
		case dto.CostExpandAddon:
			item.AddonIDs = ids(addons)
		case dto.CostExpandSubscription:
			item.SubscriptionIDs = ids(subs)
		}
	}
}
