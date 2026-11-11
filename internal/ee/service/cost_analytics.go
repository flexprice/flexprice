package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/costanalytics"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	ca "github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/feature"
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
	excludedNotFlatPrice     = "not_flat_price"
	excludedCustomPriceUnit  = "custom_price_unit"
	excludedUnsupportedMeter = "unsupported_meter"
	excludedCommitment       = "commitment"
	excludedEntitlement      = "entitlement"
	excludedBillingAdjusted  = "billing_adjusted"
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

// costInputs collects the line windows handed to ClickHouse and what the response needs besides.
type costInputs struct {
	windows        []costanalytics.LineWindow
	excluded       map[string]string // meter ID → reason
	lineByID       map[string]*subscription.SubscriptionLineItem
	meterByID      map[string]*meter.Meter
	featureByMeter map[string]*feature.Feature
	subByID        map[string]*subscription.Subscription
	// applyDiscounts prices lines after their percentage coupons; other coupons go to unappliedCoupons.
	applyDiscounts   bool
	unappliedCoupons map[string]bool
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

	in := &costInputs{
		excluded:         map[string]string{},
		applyDiscounts:   req.ApplyDiscounts,
		unappliedCoupons: map[string]bool{},
		lineByID:         map[string]*subscription.SubscriptionLineItem{},
		meterByID:        map[string]*meter.Meter{},
		featureByMeter:   map[string]*feature.Feature{},
		subByID:          map[string]*subscription.Subscription{},
	}
	billing := newBillingService(s.ServiceParams)
	for _, cs := range subs {
		segments, err := splitByBillingPeriod(cs)
		if err != nil {
			return nil, err
		}
		for _, seg := range segments {
			if err := s.addBilledLines(ctx, billing, cs, seg, meterFilter, in); err != nil {
				return nil, err
			}
		}
	}

	// A meter with any line billing did not price flat is left out entirely, so no row mixes priced and unpriced usage.
	windows := lo.Filter(in.windows, func(w costanalytics.LineWindow, _ int) bool { _, out := in.excluded[w.MeterID]; return !out })

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

	for _, cs := range subs {
		in.subByID[cs.owner.ID] = cs.owner
	}
	resp := toCostAnalyticsResponse(q, currency, result, in.lineByID, in.excluded)
	if err := s.expandCostObjects(ctx, req.Expand, resp, in); err != nil {
		return nil, err
	}
	resp.UnappliedCouponIDs = lo.Keys(in.unappliedCoupons)
	sort.Strings(resp.UnappliedCouponIDs)
	return resp, nil
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
	subs, err := customerSubscriptionsWithParents(ctx, s.SubRepo, cust.ID, false)
	if err != nil {
		return nil, err
	}
	byID := lo.KeyBy(subs, func(sub *subscription.Subscription) string { return sub.ID })

	subService := NewSubscriptionService(s.ServiceParams)
	out := make([]costSubscription, 0, len(subs))
	for _, sub := range subs {
		if sub.CustomerID != cust.ID {
			continue // a parent owned by another customer, loaded only to price its inherited child
		}
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
			parent := byID[lo.FromPtr(sub.ParentSubscriptionID)]
			if parent == nil {
				continue // the parent is not billed, so neither is the child
			}
			// The parent's invoice covers only the children billing lists (not paused or cancelled ones).
			covered, err := subService.ExternalCustomerIDsForSubscription(ctx, parent)
			if err != nil {
				return nil, err
			}
			if !lo.Contains(covered, cust.ExternalID) {
				continue
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
func splitByBillingPeriod(cs costSubscription) ([]costanalytics.BillingPeriodSegment, error) {
	owner := cs.owner
	grid, err := types.NewBillingPeriodGrid(owner.BillingAnchor, owner.BillingPeriod, max(owner.BillingPeriodCount, 1), owner.Timezone)
	if err != nil {
		return nil, err
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
		}
		segments = append(segments, seg)
		t = seg.End
	}
	return segments, nil
}

// addBilledLines prices the owner's billing period through billing, as its invoice is priced, and turns
// each line billing charged at a plain flat rate into a line window clipped to the segment. Every
// other line excludes its meter.
func (s *costAnalyticsService) addBilledLines(
	ctx context.Context,
	billing *billingService,
	cs costSubscription,
	seg costanalytics.BillingPeriodSegment,
	meterFilter map[string]bool,
	in *costInputs,
) error {
	// Billing gets every line: subscription-wide rules (grants, commitments) spread across all of them.
	items, err := billing.periodLineItems(ctx, cs.owner.ID, seg.PeriodStart)
	if err != nil {
		return err
	}
	requested := func(meterID string) bool { return meterID != "" && (meterFilter == nil || meterFilter[meterID]) }
	if !lo.ContainsBy(items, func(li *subscription.SubscriptionLineItem) bool {
		return li.PriceType == types.PRICE_TYPE_USAGE && requested(li.MeterID)
	}) {
		return nil
	}

	// Only UsageCharges are read: fixed charges here skip invoice prep's cadence and one-time rules.
	result, err := billing.calculateMeterUsageCharges(ctx, cs.owner, items, seg.PeriodStart, seg.PeriodEnd, true, nil)
	if err != nil {
		return err
	}
	charges := result.UsageCharges
	if err := s.loadMeters(ctx, items, in); err != nil {
		return err
	}

	discount := func(string) decimal.Decimal { return decimal.NewFromInt(1) }
	if in.applyDiscounts {
		if discount, err = s.periodDiscounts(ctx, cs.owner, seg, in); err != nil {
			return err
		}
	}

	itemByID := lo.KeyBy(items, func(li *subscription.SubscriptionLineItem) string { return li.ID })
	precision := types.GetCurrencyPrecision(cs.owner.Currency)
	for _, c := range charges {
		li := itemByID[lo.FromPtr(c.SubscriptionLineItemID)]
		if li == nil {
			// A charge billing could not tie to a line still hides that meter's usage.
			if meterID := lo.FromPtr(c.MeterID); requested(meterID) {
				excludeMeter(in, meterID, excludedBillingAdjusted)
			}
			continue
		}
		if li.PriceType != types.PRICE_TYPE_USAGE || !requested(li.MeterID) {
			continue
		}
		if reason := flatExclusion(cs.owner, li, in.meterByID[li.MeterID], c, precision); reason != "" {
			excludeMeter(in, li.MeterID, reason)
			continue
		}

		start, end := maxTime(seg.Start, lo.FromPtr(c.PeriodStart)), minTime(seg.End, lo.FromPtr(c.PeriodEnd))
		if !start.Before(end) {
			continue
		}
		in.lineByID[li.ID] = li
		in.windows = append(in.windows, costanalytics.LineWindow{
			LineItemID:          li.ID,
			MeterID:             li.MeterID,
			FeatureID:           featureID(in.featureByMeter[li.MeterID]),
			ExternalCustomerIDs: cs.customerIDs,
			Segment:             seg,
			Start:               start,
			End:                 end,
			Currency:            li.Price.Currency,
			UnitPrice:           li.Price.Amount.Mul(discount(li.ID)),
		})
	}
	return nil
}

// periodDiscounts returns each line's price multiplier from the percentage coupons (forever or
// repeated) active in the billing period: line coupons, then subscription coupons, compounding, as
// usage analytics applies them. Any other active coupon is recorded as not applied.
func (s *costAnalyticsService) periodDiscounts(ctx context.Context, owner *subscription.Subscription, seg costanalytics.BillingPeriodSegment, in *costInputs) (func(lineID string) decimal.Decimal, error) {
	applicable := func(c *coupon.Coupon) bool {
		return c.Type == types.CouponTypePercentage && c.PercentageOff != nil &&
			(c.Cadence == types.CouponCadenceForever || c.Cadence == types.CouponCadenceRepeated)
	}
	sel, err := selectSubscriptionCoupons(ctx, s.ServiceParams, []*subscription.Subscription{owner}, seg.PeriodStart, seg.PeriodEnd,
		func(c *coupon.Coupon, _ *ca.CouponAssociation) bool {
			if !c.IsValid() {
				return false
			}
			if !applicable(c) {
				in.unappliedCoupons[c.ID] = true
				return false
			}
			return true
		})
	if err != nil {
		return nil, err
	}

	hundred := decimal.NewFromInt(100)
	factor := func(assocs []*ca.CouponAssociation) decimal.Decimal {
		f := decimal.NewFromInt(1)
		for _, a := range assocs {
			f = f.Mul(hundred.Sub(*a.Coupon.PercentageOff)).Div(hundred)
		}
		return decimal.Max(f, decimal.Zero)
	}
	subFactor := factor(sel.SubLevel[owner.ID])
	return func(lineID string) decimal.Decimal { return factor(sel.LineLevel[lineID]).Mul(subFactor) }, nil
}

// flatExclusion returns why a billed line cannot be spread over its events at its unit price, or "".
// The last check compares billing's amount with quantity × unit price, so any rule billing applied
// that changed the money (grants, adjustments) excludes the line.
func flatExclusion(owner *subscription.Subscription, li *subscription.SubscriptionLineItem, m *meter.Meter, c dto.CreateInvoiceLineItemRequest, precision int32) string {
	p := li.Price
	switch {
	case p == nil || p.BillingModel != types.BILLING_MODEL_FLAT_FEE:
		return excludedNotFlatPrice
	case p.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM || lo.FromPtr(li.PriceUnitID) != "" || lo.FromPtr(li.PriceUnit) != "":
		return excludedCustomPriceUnit
	case m == nil || !isCostSupportedMeter(m) || price.IsBucketed(p, m):
		return excludedUnsupportedMeter
	case owner.HasCommitment() || c.CommitmentID != nil || c.CommitmentInfo != nil || c.Metadata[types.MetadataKeyIsOverage] == types.MetadataValueTrue:
		return excludedCommitment
	case lo.FromPtr(c.AdjustedEntitlementQuantity).IsPositive():
		return excludedEntitlement
	case !c.Amount.Equal(c.Quantity.Mul(p.Amount).Round(precision)):
		return excludedBillingAdjusted
	}
	return ""
}

func excludeMeter(in *costInputs, meterID, reason string) {
	if _, seen := in.excluded[meterID]; !seen {
		in.excluded[meterID] = reason
	}
}

// loadMeters fills the meters and their features for line items not seen yet.
func (s *costAnalyticsService) loadMeters(ctx context.Context, items []*subscription.SubscriptionLineItem, in *costInputs) error {
	var missing []string
	for _, li := range items {
		if _, ok := in.meterByID[li.MeterID]; li.MeterID != "" && !ok {
			missing = append(missing, li.MeterID)
		}
	}
	missing = lo.Uniq(missing)
	if len(missing) == 0 {
		return nil
	}

	meters, err := s.MeterRepo.ListByIDs(ctx, missing)
	if err != nil {
		return err
	}
	for _, m := range meters {
		in.meterByID[m.ID] = m
	}

	filter := types.NewNoLimitFeatureFilter()
	filter.MeterIDs = missing
	features, err := s.FeatureRepo.List(ctx, filter)
	if err != nil {
		return err
	}
	for _, f := range features {
		if f.MeterID != "" {
			in.featureByMeter[f.MeterID] = f
		}
	}
	return nil
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
	currency string,
	result *costanalytics.Result,
	lineByID map[string]*subscription.SubscriptionLineItem,
	excluded map[string]string,
) *dto.CostAnalyticsResponse {
	precision := types.GetCurrencyPrecision(currency)
	money := func(d decimal.Decimal) string { return d.Round(precision).StringFixed(precision) }
	none := func(v string) string { return lo.Ternary(v == "", dto.CostNoneValue, v) }

	resp := &dto.CostAnalyticsResponse{
		Currency:       currency,
		TotalCount:     result.TotalCount,
		Items:          make([]dto.CostAnalyticsItem, 0, len(result.Items)),
		ExcludedMeters: make([]dto.CostExcludedMeter, 0, len(excluded)),
		ContractRows:   []dto.CostAnalyticsContract{},
		Totals: dto.CostAnalyticsTotals{
			Cost:       money(result.Totals.Cost),
			Quantity:   result.Totals.Quantity.String(),
			EventCount: result.Totals.EventCount,
		},
	}
	for meterID, reason := range excluded {
		resp.ExcludedMeters = append(resp.ExcludedMeters, dto.CostExcludedMeter{MeterID: meterID, Reason: reason})
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
		costItemIDs(&item, row.LineItemIDs, lineByID)
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

// costItemIDs sets the prices, plans, add-ons and subscriptions of the lines behind a row.
func costItemIDs(item *dto.CostAnalyticsItem, lineIDs []string, lineByID map[string]*subscription.SubscriptionLineItem) {
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
	item.PriceIDs, item.PlanIDs, item.AddonIDs, item.SubscriptionIDs = sortedIDs(prices), sortedIDs(plans), sortedIDs(addons), sortedIDs(subs)
}

func sortedIDs(ids []string) []string {
	ids = lo.Uniq(lo.Compact(ids))
	sort.Strings(ids)
	return ids
}

// expandCostObjects fills the object maps requested in expand, for the IDs on the page's rows.
func (s *costAnalyticsService) expandCostObjects(ctx context.Context, expand []dto.CostExpand, resp *dto.CostAnalyticsResponse, in *costInputs) error {
	want := lo.SliceToMap(expand, func(e dto.CostExpand) (dto.CostExpand, bool) { return e, true })
	if len(want) == 0 {
		return nil
	}
	ids := func(pick func(dto.CostAnalyticsItem) []string) []string {
		return sortedIDs(lo.FlatMap(resp.Items, func(it dto.CostAnalyticsItem, _ int) []string { return pick(it) }))
	}
	priceIDs := ids(func(it dto.CostAnalyticsItem) []string { return it.PriceIDs })
	priceByID := map[string]*price.Price{}
	for _, li := range in.lineByID {
		if li.Price != nil {
			priceByID[li.PriceID] = li.Price
		}
	}
	meterIDs := sortedIDs(lo.FilterMap(priceIDs, func(id string, _ int) (string, bool) {
		p := priceByID[id]
		return lo.FromPtr(p).MeterID, p != nil
	}))

	if want[dto.CostExpandPrice] {
		resp.Prices = map[string]*dto.PriceResponse{}
		for _, id := range priceIDs {
			if p := priceByID[id]; p != nil {
				resp.Prices[id] = &dto.PriceResponse{Price: p}
			}
		}
	}
	if want[dto.CostExpandSubscription] {
		resp.Subscriptions = map[string]*dto.SubscriptionResponse{}
		for _, id := range ids(func(it dto.CostAnalyticsItem) []string { return it.SubscriptionIDs }) {
			if sub := in.subByID[id]; sub != nil {
				resp.Subscriptions[id] = &dto.SubscriptionResponse{Subscription: sub}
			}
		}
	}
	if want[dto.CostExpandPlan] {
		resp.Plans = map[string]*dto.PlanResponse{}
		if planIDs := ids(func(it dto.CostAnalyticsItem) []string { return it.PlanIDs }); len(planIDs) > 0 {
			plans, err := s.PlanRepo.ListByIDs(ctx, planIDs)
			if err != nil {
				return err
			}
			for _, p := range plans {
				resp.Plans[p.ID] = &dto.PlanResponse{Plan: p}
			}
		}
	}
	if want[dto.CostExpandAddon] {
		resp.Addons = map[string]*dto.AddonResponse{}
		if addonIDs := ids(func(it dto.CostAnalyticsItem) []string { return it.AddonIDs }); len(addonIDs) > 0 {
			filter := types.NewNoLimitAddonFilter()
			filter.AddonIDs = addonIDs
			addons, err := s.AddonRepo.List(ctx, filter)
			if err != nil {
				return err
			}
			for _, a := range addons {
				resp.Addons[a.ID] = &dto.AddonResponse{Addon: a}
			}
		}
	}
	if want[dto.CostExpandMeter] {
		resp.Meters = map[string]*dto.MeterResponse{}
		for _, id := range meterIDs {
			if m := in.meterByID[id]; m != nil {
				resp.Meters[id] = dto.ToMeterResponse(m)
			}
		}
	}
	if want[dto.CostExpandFeature] {
		resp.Features = map[string]*dto.FeatureResponse{}
		for _, id := range meterIDs {
			if f := in.featureByMeter[id]; f != nil {
				resp.Features[f.ID] = &dto.FeatureResponse{Feature: f}
			}
		}
	}
	return nil
}

func featureID(f *feature.Feature) string {
	if f == nil {
		return ""
	}
	return f.ID
}
