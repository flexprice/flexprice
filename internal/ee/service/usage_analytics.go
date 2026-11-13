package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	ca "github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/usageanalytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Reasons a meter is left out of a usage breakdown.
const (
	excludedNotFlatPrice     = "not_flat_price"
	excludedCustomPriceUnit  = "custom_price_unit"
	excludedUnsupportedMeter = "unsupported_meter"
	excludedCommitment       = "commitment"
	excludedEntitlement      = "entitlement"
	excludedUnmatchedCharge  = "unmatched_charge"
)

// UsageAnalyticsService attributes billed amount to the events, and their properties, that caused it.
type UsageAnalyticsService interface {
	GetBreakdown(ctx context.Context, req *dto.UsageAnalyticsRequest) (*dto.UsageAnalyticsResponse, error)
}

type usageAnalyticsService struct {
	ServiceParams
}

func NewUsageAnalyticsService(params ServiceParams) UsageAnalyticsService {
	return &usageAnalyticsService{ServiceParams: params}
}

// billedSubscription is a requested customer's subscription with the subscription whose lines price it
// (the parent, for an inherited child) and the customers whose events it covers.
type billedSubscription struct {
	sub         *subscription.Subscription
	owner       *subscription.Subscription
	customerIDs []string // external IDs
	start, end  time.Time
}

// queryInputs collects the line windows handed to ClickHouse and what the response needs besides.
type queryInputs struct {
	windows        []usageanalytics.LineWindow
	excluded       map[string]string // meter ID → reason
	lineByID       map[string]*subscription.SubscriptionLineItem
	meterByID      map[string]*meter.Meter
	featureByMeter map[string]*feature.Feature
	subByID        map[string]*subscription.Subscription
	// applyDiscounts prices lines after their percentage coupons; other coupons go to unappliedCoupons.
	applyDiscounts   bool
	unappliedCoupons map[string]bool
}

func (s *usageAnalyticsService) GetBreakdown(ctx context.Context, req *dto.UsageAnalyticsRequest) (*dto.UsageAnalyticsResponse, error) {
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

	meters, err := s.requestedMeters(ctx, q)
	if err != nil {
		return nil, err
	}

	subs, err := s.subscriptionsInRange(ctx, cust, q.Start, q.End)
	if err != nil {
		return nil, err
	}

	in := &queryInputs{
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
			if err := s.addBilledLines(ctx, billing, cs, seg, meters, in); err != nil {
				return nil, err
			}
		}
	}

	if err := unsupportedMetersErr(in.excluded); err != nil {
		return nil, err
	}

	currency, err := singleCurrency(in.windows)
	if err != nil {
		return nil, err
	}
	meterIDs := lo.Uniq(lo.Map(in.windows, func(w usageanalytics.LineWindow, _ int) string { return w.MeterID }))
	if q.Sort.Field == usageanalytics.SortUsage && len(meterIDs) > 1 {
		return nil, ierr.NewError("sort by usage needs a single meter").
			WithHint("Quantities of different meters cannot be compared; pass one meter_id or sort by amount").
			Mark(ierr.ErrValidation)
	}

	result, err := s.UsageAnalyticsRepo.GetBreakdown(ctx, &usageanalytics.QueryParams{
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		Query:         q,
		Lines:         in.windows,
	})
	if err != nil {
		return nil, err
	}

	for _, cs := range subs {
		in.subByID[cs.owner.ID] = cs.owner
	}
	resp := toUsageAnalyticsResponse(q, currency, result, in.lineByID)
	if err := s.expandObjects(ctx, req.Expand, resp, in); err != nil {
		return nil, err
	}
	resp.UnappliedCouponIDs = lo.Keys(in.unappliedCoupons)
	sort.Strings(resp.UnappliedCouponIDs)
	return resp, nil
}

// meterSet is the meters a request covers; nil means every meter.
type meterSet map[string]bool

func (m meterSet) includes(meterID string) bool {
	return meterID != "" && (m == nil || m[meterID])
}

// requestedMeters resolves meter_ids and feature_ids to one meter set.
func (s *usageAnalyticsService) requestedMeters(ctx context.Context, q *usageanalytics.Query) (meterSet, error) {
	if len(q.MeterIDs) == 0 && len(q.FeatureIDs) == 0 {
		return nil, nil
	}

	ids := meterSet{}
	for _, id := range q.MeterIDs {
		ids[id] = true
	}
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
func (s *usageAnalyticsService) subscriptionsInRange(ctx context.Context, cust *customer.Customer, start, end time.Time) ([]billedSubscription, error) {
	subs, err := customerSubscriptionsWithParents(ctx, s.SubRepo, cust.ID, false)
	if err != nil {
		return nil, err
	}
	byID := lo.KeyBy(subs, func(sub *subscription.Subscription) string { return sub.ID })

	subService := NewSubscriptionService(s.ServiceParams)
	out := make([]billedSubscription, 0, len(subs))
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

		cs := billedSubscription{sub: sub, owner: sub, start: subStart, end: subEnd}
		if sub.SubscriptionType == types.SubscriptionTypeInherited && lo.FromPtr(sub.ParentSubscriptionID) != "" {
			// A child is priced on its parent's lines, over its own events only.
			parent := byID[lo.FromPtr(sub.ParentSubscriptionID)]
			if parent == nil {
				continue // the parent is not billed, so neither is the child
			}
			until, covered, err := childCoverageEnd(parent, sub)
			if err != nil {
				return nil, err
			}
			if !covered {
				continue
			}
			if !until.IsZero() {
				if cs.end = minTime(cs.end, until); !cs.start.Before(cs.end) {
					continue
				}
			}
			cs.owner = parent
			cs.customerIDs = []string{cust.ExternalID}
		} else {
			// A parent covers its own and every inherited child's events, as its invoice does.
			cs.customerIDs, err = subService.ExternalCustomerIDsForSubscription(ctx, sub)
			if err != nil {
				return nil, err
			}
			if sub.SubscriptionType == types.SubscriptionTypeParent {
				cancelled, err := s.cancelledChildren(ctx, cs)
				if err != nil {
					return nil, err
				}
				out = append(out, cancelled...)
			}
		}
		if len(cs.customerIDs) > 0 {
			out = append(out, cs)
		}
	}
	return out, nil
}

// childCoverageEnd reports whether the parent's invoices cover an inherited child and until when (zero:
// no end). An invoice counts the children not paused or cancelled when it is computed, at period end,
// so a cancelled child is covered for every period that ends at or before its cancellation. Paused
// children are judged by their current status; pause history is not read.
func childCoverageEnd(parent, child *subscription.Subscription) (time.Time, bool, error) {
	switch child.SubscriptionStatus {
	case types.SubscriptionStatusActive, types.SubscriptionStatusTrialing:
		return time.Time{}, true, nil
	case types.SubscriptionStatusCancelled:
		if child.CancelledAt == nil {
			return time.Time{}, false, nil
		}
		until, err := subPeriodStartAt(parent, *child.CancelledAt)
		return until, err == nil, err
	}
	return time.Time{}, false, nil
}

// cancelledChildren prices a parent's cancelled inherited children for the periods its invoices still
// covered them; ExternalCustomerIDsForSubscription lists only the children covered today.
func (s *usageAnalyticsService) cancelledChildren(ctx context.Context, cs billedSubscription) ([]billedSubscription, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.ParentSubscriptionIDs = []string{cs.sub.ID}
	filter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeInherited}
	filter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusCancelled}
	children, err := s.SubRepo.List(ctx, filter)
	if err != nil || len(children) == 0 {
		return nil, err
	}

	custFilter := types.NewNoLimitCustomerFilter()
	custFilter.CustomerIDs = lo.Uniq(lo.Map(children, func(c *subscription.Subscription, _ int) string { return c.CustomerID }))
	customers, err := s.CustomerRepo.List(ctx, custFilter)
	if err != nil {
		return nil, err
	}
	externalID := lo.SliceToMap(customers, func(c *customer.Customer) (string, string) { return c.ID, c.ExternalID })

	var out []billedSubscription
	for _, child := range children {
		ext := externalID[child.CustomerID]
		if ext == "" || lo.Contains(cs.customerIDs, ext) {
			continue
		}
		until, covered, err := childCoverageEnd(cs.sub, child)
		if err != nil {
			return nil, err
		}
		if end := minTime(cs.end, until); covered && cs.start.Before(end) {
			out = append(out, billedSubscription{sub: cs.sub, owner: cs.sub, customerIDs: []string{ext}, start: cs.start, end: end})
		}
	}
	return out, nil
}

// splitByBillingPeriod cuts a subscription's range at the billing period boundaries of its owner.
func splitByBillingPeriod(cs billedSubscription) ([]usageanalytics.BillingPeriodSegment, error) {
	owner := cs.owner
	grid, err := types.NewBillingPeriodGrid(owner.BillingAnchor, owner.BillingPeriod, max(owner.BillingPeriodCount, 1), owner.Timezone)
	if err != nil {
		return nil, err
	}

	periods := types.BillingPeriodsIn(grid, cs.start, cs.end)
	return lo.Map(periods, func(p types.Period, _ int) usageanalytics.BillingPeriodSegment {
		return usageanalytics.BillingPeriodSegment{
			SubscriptionID: cs.sub.ID,
			PeriodStart:    maxTime(p.Start, owner.StartDate),
			PeriodEnd:      p.End,
			Start:          maxTime(p.Start, cs.start),
			End:            minTime(p.End, cs.end),
		}
	}), nil
}

// addBilledLines prices the owner's billing period through billing, as its invoice is priced, and turns
// each line billing charged at a plain flat rate into a line window clipped to the segment. Every
// other line excludes its meter.
func (s *usageAnalyticsService) addBilledLines(
	ctx context.Context,
	billing *billingService,
	cs billedSubscription,
	seg usageanalytics.BillingPeriodSegment,
	meters meterSet,
	in *queryInputs,
) error {
	// Billing gets every line: subscription-wide rules (grants, commitments) spread across all of them.
	items, err := billing.periodLineItems(ctx, cs.owner.ID, seg.PeriodStart)
	if err != nil {
		return err
	}
	hasUsageLine := lo.ContainsBy(items, func(li *subscription.SubscriptionLineItem) bool {
		return li.PriceType == types.PRICE_TYPE_USAGE && meters.includes(li.MeterID)
	})
	if !hasUsageLine {
		return nil
	}

	// Only UsageCharges are read: fixed charges here skip invoice prep's cadence and one-time rules.
	result, err := billing.calculateMeterUsageCharges(ctx, cs.owner, items, seg.PeriodStart, seg.PeriodEnd, true, nil, items)
	if err != nil {
		return err
	}
	charges := result.UsageCharges
	if err := s.loadMeters(ctx, items, in); err != nil {
		return err
	}

	var discounts *lineDiscounts
	if in.applyDiscounts {
		if discounts, err = s.periodDiscounts(ctx, cs.owner, seg, in); err != nil {
			return err
		}
	}

	itemByID := lo.KeyBy(items, func(li *subscription.SubscriptionLineItem) string { return li.ID })
	for _, c := range charges {
		li := itemByID[lo.FromPtr(c.SubscriptionLineItemID)]
		if li == nil {
			// A charge billing could not tie to a line still hides that meter's usage.
			if meterID := lo.FromPtr(c.MeterID); meters.includes(meterID) {
				in.excluded[meterID] = excludedUnmatchedCharge
			}
			continue
		}
		if li.PriceType != types.PRICE_TYPE_USAGE || !meters.includes(li.MeterID) {
			continue
		}
		if reason := flatExclusion(cs.owner, li, in.meterByID[li.MeterID], c); reason != "" {
			in.excluded[li.MeterID] = reason
			continue
		}

		start, end := maxTime(seg.Start, lo.FromPtr(c.PeriodStart)), minTime(seg.End, lo.FromPtr(c.PeriodEnd))
		if !start.Before(end) {
			continue
		}
		in.lineByID[li.ID] = li
		in.windows = append(in.windows, usageanalytics.LineWindow{
			LineItemID:          li.ID,
			MeterID:             li.MeterID,
			FeatureID:           featureID(in.featureByMeter[li.MeterID]),
			ExternalCustomerIDs: cs.customerIDs,
			Segment:             seg,
			Start:               start,
			End:                 end,
			Currency:            li.Price.Currency,
			UnitPrice:           li.Price.Amount.Mul(discounts.factor(li.ID)),
			ListUnitPrice:       li.Price.Amount,
		})
	}
	return nil
}

// lineDiscounts is each line's price multiplier from its percentage coupons; nil applies none.
type lineDiscounts struct {
	lineCoupons map[string][]*ca.CouponAssociation
	subFactor   decimal.Decimal
}

func (d *lineDiscounts) factor(lineID string) decimal.Decimal {
	if d == nil {
		return decimal.NewFromInt(1)
	}
	return couponFactor(d.lineCoupons[lineID]).Mul(d.subFactor)
}

// periodDiscounts returns each line's price multiplier from the percentage coupons (forever or
// repeated) active in the billing period: line coupons, then subscription coupons, compounding, as
// usage analytics applies them. Any other active coupon is recorded as not applied.
func (s *usageAnalyticsService) periodDiscounts(ctx context.Context, owner *subscription.Subscription, seg usageanalytics.BillingPeriodSegment, in *queryInputs) (*lineDiscounts, error) {
	sel, err := selectSubscriptionCoupons(ctx, s.ServiceParams, []*subscription.Subscription{owner}, seg.PeriodStart, seg.PeriodEnd,
		func(c *coupon.Coupon, _ *ca.CouponAssociation) bool {
			if !c.IsValid() {
				return false
			}
			// Only a recurring percentage can be spread over events; others are reported as not applied.
			spreadable := c.Type == types.CouponTypePercentage && c.PercentageOff != nil &&
				(c.Cadence == types.CouponCadenceForever || c.Cadence == types.CouponCadenceRepeated)
			if !spreadable {
				in.unappliedCoupons[c.ID] = true
				return false
			}
			return true
		})
	if err != nil {
		return nil, err
	}

	return &lineDiscounts{lineCoupons: sel.LineLevel, subFactor: couponFactor(sel.SubLevel[owner.ID])}, nil
}

// couponFactor is the price multiplier of percentage coupons applied one after another.
func couponFactor(assocs []*ca.CouponAssociation) decimal.Decimal {
	hundred := decimal.NewFromInt(100)
	f := decimal.NewFromInt(1)
	for _, a := range assocs {
		f = f.Mul(hundred.Sub(*a.Coupon.PercentageOff)).Div(hundred)
	}
	return decimal.Max(f, decimal.Zero)
}

// flatExclusion returns why a billed line cannot be spread over its events at its unit price, or "".
// Each billing rule that changes a line's money has its own check here; add one with any new rule.
func flatExclusion(owner *subscription.Subscription, li *subscription.SubscriptionLineItem, m *meter.Meter, c dto.CreateInvoiceLineItemRequest) string {
	p := li.Price
	switch {
	case p == nil || p.BillingModel != types.BILLING_MODEL_FLAT_FEE:
		return excludedNotFlatPrice
	case p.PriceUnitType == types.PRICE_UNIT_TYPE_CUSTOM || lo.FromPtr(li.PriceUnitID) != "" || lo.FromPtr(li.PriceUnit) != "":
		return excludedCustomPriceUnit
	case m == nil || !isSupportedMeter(m) || price.IsBucketed(p, m):
		return excludedUnsupportedMeter
	case owner.HasCommitment() || c.CommitmentID != nil || c.CommitmentInfo != nil || c.Metadata[types.MetadataKeyIsOverage] == types.MetadataValueTrue:
		return excludedCommitment
	case lo.FromPtr(c.AdjustedEntitlementQuantity).IsPositive(), c.Metadata[metadataKeySubscriptionEntitlementGrantAllowance] != "":
		return excludedEntitlement
	}
	return ""
}

// unsupportedMetersErr rejects the request when a requested meter has a line usage analytics cannot
// price yet, naming each meter and why, so no total silently leaves usage out.
func unsupportedMetersErr(excluded map[string]string) error {
	if len(excluded) == 0 {
		return nil
	}
	meterIDs := lo.Keys(excluded)
	sort.Strings(meterIDs)
	named := lo.Map(meterIDs, func(id string, _ int) string { return id + " (" + excluded[id] + ")" })
	msg := fmt.Sprintf("usage analytics does not support meters %s yet; pass meter_ids or feature_ids to leave them out", strings.Join(named, ", "))
	return ierr.NewError(msg).
		WithHint(msg).
		WithReportableDetails(map[string]any{"unsupported_meters": excluded}).
		Mark(ierr.ErrValidation)
}

// loadMeters fills the meters and their features for line items not seen yet.
func (s *usageAnalyticsService) loadMeters(ctx context.Context, items []*subscription.SubscriptionLineItem, in *queryInputs) error {
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

// isSupportedMeter reports whether qty_total is the billed quantity of each event.
func isSupportedMeter(m *meter.Meter) bool {
	switch m.Aggregation.Type {
	case types.AggregationSum, types.AggregationCount, types.AggregationSumWithMultiplier:
		return true
	}
	return false
}

func singleCurrency(windows []usageanalytics.LineWindow) (string, error) {
	currencies := lo.Uniq(lo.Map(windows, func(w usageanalytics.LineWindow, _ int) string { return strings.ToLower(w.Currency) }))
	switch len(currencies) {
	case 0:
		return "", nil
	case 1:
		return currencies[0], nil
	}
	return "", ierr.NewError("customer is billed in more than one currency").
		WithHint("Usage analytics supports one currency per customer; filter by meter_ids to one currency").
		WithReportableDetails(map[string]any{"currencies": currencies}).
		Mark(ierr.ErrValidation)
}

// toUsageAnalyticsResponse rounds money once per row and once for totals, names missing values
// UsageNoneValue and keeps the top column values by total amount.
func toUsageAnalyticsResponse(
	q *usageanalytics.Query,
	currency string,
	result *usageanalytics.Result,
	lineByID map[string]*subscription.SubscriptionLineItem,
) *dto.UsageAnalyticsResponse {
	precision := types.GetCurrencyPrecision(currency)
	resp := &dto.UsageAnalyticsResponse{
		Currency:   currency,
		Pagination: types.PaginationResponse{Total: int(result.TotalCount), Limit: q.Limit, Offset: q.Offset},
		Items:      make([]dto.UsageAnalyticsItem, 0, len(result.Items)),
		Totals: dto.UsageAnalyticsTotals{
			UsageAmounts: usageAmounts(result.Totals.Subtotal, result.Totals.Amount, precision),
			Usage:        result.Totals.Quantity.String(),
			EventCount:   result.Totals.EventCount,
		},
	}

	var windows []time.Time
	if q.WindowSize != "" {
		windows = q.Windows()
	}

	var shown map[string]bool
	if q.Columns != nil {
		resp.ColumnValues, shown = topColumnValues(result.Totals.Columns)
		resp.Totals.Columns = amountColumns(result.Totals.Columns, shown, precision)
	}

	for _, row := range result.Items {
		item := dto.UsageAnalyticsItem{
			Group:        make(map[string]string, len(q.GroupBy)),
			UsageAmounts: usageAmounts(row.Subtotal, row.Amount, precision),
			Usage:        row.Quantity.String(),
			EventCount:   row.EventCount,
		}
		for i, k := range q.GroupBy {
			item.Group[k.String()] = noneIfEmpty(row.Group[i])
		}
		if q.Columns != nil {
			item.Columns = amountColumns(row.Columns, shown, precision)
		}
		if len(row.Metrics) > 0 {
			item.Metrics = make(map[string]dto.UsageAnalyticsValue, len(row.Metrics))
			for name, v := range row.Metrics {
				item.Metrics[name] = dto.UsageAnalyticsValue{Count: v.Count, Values: v.Values}
			}
		}
		if q.WindowSize != "" {
			item.Points = usagePoints(row.Points, windows, precision)
		}
		usageItemIDs(&item, row.LineItemIDs, lineByID)
		resp.Items = append(resp.Items, item)
	}
	return resp
}

// usageAmounts rounds subtotal and amount, and takes the discount as their difference so the three agree.
func usageAmounts(subtotal, amount decimal.Decimal, precision int32) dto.UsageAmounts {
	sub, amt := subtotal.Round(precision), amount.Round(precision)
	return dto.UsageAmounts{
		Subtotal: sub.StringFixed(precision),
		Discount: sub.Sub(amt).StringFixed(precision),
		Amount:   amt.StringFixed(precision),
	}
}

// noneIfEmpty shows a missing value as UsageNoneValue.
func noneIfEmpty(v string) string {
	if v == "" {
		return dto.UsageNoneValue
	}
	return v
}

// usagePoints lists every window in the range, at zero where the row had no events.
func usagePoints(points []usageanalytics.Point, windows []time.Time, precision int32) []dto.UsagePoint {
	byStart := lo.KeyBy(points, func(p usageanalytics.Point) int64 { return p.WindowStart.Unix() })
	return lo.Map(windows, func(w time.Time, _ int) dto.UsagePoint {
		p, ok := byStart[w.Unix()]
		if !ok {
			return dto.UsagePoint{Timestamp: w, UsageAmounts: usageAmounts(decimal.Zero, decimal.Zero, precision), Usage: "0"}
		}
		return dto.UsagePoint{Timestamp: w, UsageAmounts: usageAmounts(p.Subtotal, p.Amount, precision), Usage: p.Quantity.String()}
	})
}

// topColumnValues returns the MaxUsageColumns values with the highest total amount, in that order.
func topColumnValues(totals map[string]decimal.Decimal) ([]string, map[string]bool) {
	values := lo.Keys(totals)
	sort.Slice(values, func(i, j int) bool {
		if c := totals[values[i]].Cmp(totals[values[j]]); c != 0 {
			return c > 0
		}
		return values[i] < values[j]
	})
	if len(values) > dto.MaxUsageColumns {
		values = values[:dto.MaxUsageColumns]
	}
	shown := lo.SliceToMap(values, func(v string) (string, bool) { return v, true })
	shownValues := make([]string, len(values))
	for i, v := range values {
		shownValues[i] = noneIfEmpty(v)
	}
	return shownValues, shown
}

// amountColumns keeps the shown column values and sums the rest into UsageOtherColumn, so columns add up to the row.
func amountColumns(amounts map[string]decimal.Decimal, shown map[string]bool, precision int32) map[string]dto.UsageColumn {
	out := make(map[string]dto.UsageColumn, len(amounts))
	other, hasOther := decimal.Zero, false
	for v, c := range amounts {
		if shown[v] {
			out[noneIfEmpty(v)] = dto.UsageColumn{Amount: c.StringFixed(precision)}
			continue
		}
		other, hasOther = other.Add(c), true
	}
	if hasOther {
		out[dto.UsageOtherColumn] = dto.UsageColumn{Amount: other.StringFixed(precision)}
	}
	return out
}

// usageItemIDs sets the prices, plans, add-ons and subscriptions of the lines behind a row.
func usageItemIDs(item *dto.UsageAnalyticsItem, lineIDs []string, lineByID map[string]*subscription.SubscriptionLineItem) {
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

// expandObjects fills the object maps requested in expand, for the IDs on the page's rows.
func (s *usageAnalyticsService) expandObjects(ctx context.Context, expand []types.ExpandableField, resp *dto.UsageAnalyticsResponse, in *queryInputs) error {
	want := lo.SliceToMap(expand, func(e types.ExpandableField) (types.ExpandableField, bool) { return e, true })
	if len(want) == 0 {
		return nil
	}
	priceIDs := sortedIDs(lo.FlatMap(resp.Items, func(it dto.UsageAnalyticsItem, _ int) []string { return it.PriceIDs }))
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

	if want[types.ExpandPrices] {
		resp.Prices = map[string]*dto.PriceResponse{}
		for _, id := range priceIDs {
			if p := priceByID[id]; p != nil {
				resp.Prices[id] = &dto.PriceResponse{Price: p}
			}
		}
	}
	if want[types.ExpandSubscription] {
		resp.Subscriptions = map[string]*dto.SubscriptionResponse{}
		subIDs := sortedIDs(lo.FlatMap(resp.Items, func(it dto.UsageAnalyticsItem, _ int) []string { return it.SubscriptionIDs }))
		for _, id := range subIDs {
			if sub := in.subByID[id]; sub != nil {
				resp.Subscriptions[id] = &dto.SubscriptionResponse{Subscription: sub}
			}
		}
	}
	if want[types.ExpandPlans] {
		resp.Plans = map[string]*dto.PlanResponse{}
		planIDs := sortedIDs(lo.FlatMap(resp.Items, func(it dto.UsageAnalyticsItem, _ int) []string { return it.PlanIDs }))
		if len(planIDs) > 0 {
			plans, err := s.PlanRepo.ListByIDs(ctx, planIDs)
			if err != nil {
				return err
			}
			for _, p := range plans {
				resp.Plans[p.ID] = &dto.PlanResponse{Plan: p}
			}
		}
	}
	if want[types.ExpandAddons] {
		resp.Addons = map[string]*dto.AddonResponse{}
		addonIDs := sortedIDs(lo.FlatMap(resp.Items, func(it dto.UsageAnalyticsItem, _ int) []string { return it.AddonIDs }))
		if len(addonIDs) > 0 {
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
	if want[types.ExpandMeters] {
		resp.Meters = map[string]*dto.MeterResponse{}
		for _, id := range meterIDs {
			if m := in.meterByID[id]; m != nil {
				resp.Meters[id] = dto.ToMeterResponse(m)
			}
		}
	}
	if want[types.ExpandFeatures] {
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
