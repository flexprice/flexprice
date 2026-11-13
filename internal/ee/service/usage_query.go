package service

import (
	"context"
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

// UsageQueryService attributes billed amount to the events, and their properties, that caused it.
type UsageQueryService interface {
	GetBreakdown(ctx context.Context, req *dto.UsageQueryRequest) (*dto.UsageQueryResponse, error)
}

type usageQueryService struct {
	ServiceParams
	repo usageanalytics.Repository
}

func NewUsageQueryService(params ServiceParams, repo usageanalytics.Repository) UsageQueryService {
	return &usageQueryService{ServiceParams: params, repo: repo}
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

func (s *usageQueryService) GetBreakdown(ctx context.Context, req *dto.UsageQueryRequest) (*dto.UsageQueryResponse, error) {
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
			if err := s.addBilledLines(ctx, billing, cs, seg, meterFilter, in); err != nil {
				return nil, err
			}
		}
	}

	if err := unsupportedMetersErr(in.excluded); err != nil {
		return nil, err
	}
	windows := in.windows

	currency, err := singleCurrency(windows)
	if err != nil {
		return nil, err
	}
	if q.Sort.Field == usageanalytics.SortUsage && len(lo.Uniq(lo.Map(windows, func(w usageanalytics.LineWindow, _ int) string { return w.MeterID }))) > 1 {
		return nil, ierr.NewError("sort by usage needs a single meter").
			WithHint("Quantities of different meters cannot be compared; pass one meter_id or sort by total").
			Mark(ierr.ErrValidation)
	}

	result, err := s.repo.GetBreakdown(ctx, &usageanalytics.QueryParams{
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
	resp := toUsageQueryResponse(q, currency, result, in.lineByID)
	if err := s.expandObjects(ctx, req.Expand, resp, in); err != nil {
		return nil, err
	}
	resp.UnappliedCouponIDs = lo.Keys(in.unappliedCoupons)
	sort.Strings(resp.UnappliedCouponIDs)
	return resp, nil
}

// requestedMeterIDs resolves meter_ids and feature_ids to one meter set; nil means every meter.
func (s *usageQueryService) requestedMeterIDs(ctx context.Context, q *usageanalytics.Query) (map[string]bool, error) {
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
func (s *usageQueryService) subscriptionsInRange(ctx context.Context, cust *customer.Customer, start, end time.Time) ([]billedSubscription, error) {
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
func (s *usageQueryService) cancelledChildren(ctx context.Context, cs billedSubscription) ([]billedSubscription, error) {
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

	var segments []usageanalytics.BillingPeriodSegment
	for t := cs.start; t.Before(cs.end); {
		p := types.FullBillingPeriod(t, grid)
		if !p.End.After(t) {
			break
		}
		seg := usageanalytics.BillingPeriodSegment{
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
func (s *usageQueryService) addBilledLines(
	ctx context.Context,
	billing *billingService,
	cs billedSubscription,
	seg usageanalytics.BillingPeriodSegment,
	meterFilter map[string]bool,
	in *queryInputs,
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
	result, err := billing.calculateMeterUsageCharges(ctx, cs.owner, items, seg.PeriodStart, seg.PeriodEnd, true, nil, items)
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
	for _, c := range charges {
		li := itemByID[lo.FromPtr(c.SubscriptionLineItemID)]
		if li == nil {
			// A charge billing could not tie to a line still hides that meter's usage.
			if meterID := lo.FromPtr(c.MeterID); requested(meterID) {
				excludeMeter(in, meterID, excludedUnmatchedCharge)
			}
			continue
		}
		if li.PriceType != types.PRICE_TYPE_USAGE || !requested(li.MeterID) {
			continue
		}
		if reason := flatExclusion(cs.owner, li, in.meterByID[li.MeterID], c); reason != "" {
			excludeMeter(in, li.MeterID, reason)
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
			UnitPrice:           li.Price.Amount.Mul(discount(li.ID)),
			ListUnitPrice:       li.Price.Amount,
		})
	}
	return nil
}

// periodDiscounts returns each line's price multiplier from the percentage coupons (forever or
// repeated) active in the billing period: line coupons, then subscription coupons, compounding, as
// usage analytics applies them. Any other active coupon is recorded as not applied.
func (s *usageQueryService) periodDiscounts(ctx context.Context, owner *subscription.Subscription, seg usageanalytics.BillingPeriodSegment, in *queryInputs) (func(lineID string) decimal.Decimal, error) {
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
	return ierr.NewErrorf("usage analytics does not support meters %s yet", strings.Join(named, ", ")).
		WithHint("Only flat-priced usage without commitments or entitlements is supported; pass meter_ids or feature_ids to leave these meters out").
		WithReportableDetails(map[string]any{"unsupported_meters": excluded}).
		Mark(ierr.ErrValidation)
}

func excludeMeter(in *queryInputs, meterID, reason string) {
	if _, seen := in.excluded[meterID]; !seen {
		in.excluded[meterID] = reason
	}
}

// loadMeters fills the meters and their features for line items not seen yet.
func (s *usageQueryService) loadMeters(ctx context.Context, items []*subscription.SubscriptionLineItem, in *queryInputs) error {
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

// toUsageQueryResponse rounds money once per row and once for totals, names missing values
// UsageNoneValue and keeps the top column values by total amount.
func toUsageQueryResponse(
	q *usageanalytics.Query,
	currency string,
	result *usageanalytics.Result,
	lineByID map[string]*subscription.SubscriptionLineItem,
) *dto.UsageQueryResponse {
	precision := types.GetCurrencyPrecision(currency)
	money := func(d decimal.Decimal) string { return d.Round(precision).StringFixed(precision) }
	amounts := func(subtotal, amount decimal.Decimal) dto.UsageAmounts {
		sub, amt := subtotal.Round(precision), amount.Round(precision)
		return dto.UsageAmounts{Subtotal: sub.StringFixed(precision), Discount: sub.Sub(amt).StringFixed(precision), Amount: amt.StringFixed(precision)}
	}
	none := func(v string) string { return lo.Ternary(v == "", dto.UsageNoneValue, v) }

	resp := &dto.UsageQueryResponse{
		Currency:   currency,
		Pagination: types.PaginationResponse{Total: int(result.TotalCount), Limit: q.Limit, Offset: q.Offset},
		Items:      make([]dto.UsageQueryItem, 0, len(result.Items)),
		Totals: dto.UsageQueryTotals{
			UsageAmounts: amounts(result.Totals.Subtotal, result.Totals.Amount),
			Usage:        result.Totals.Quantity.String(),
			EventCount:   result.Totals.EventCount,
		},
	}

	var windows []time.Time
	if q.WindowSize != "" {
		windows = q.Windows()
		resp.Totals.Points = usagePoints(result.Totals.Points, windows, amounts)
	}

	var shown map[string]bool
	if q.Columns != nil {
		resp.ColumnValues, shown = topColumnValues(result.Totals.Columns)
		resp.Totals.Columns = amountColumns(result.Totals.Columns, shown, money, none)
	}

	for _, row := range result.Items {
		item := dto.UsageQueryItem{
			Group:        make(map[string]string, len(q.GroupBy)),
			UsageAmounts: amounts(row.Subtotal, row.Amount),
			Usage:        row.Quantity.String(),
			EventCount:   row.EventCount,
		}
		for i, k := range q.GroupBy {
			item.Group[k.String()] = none(row.Group[i])
		}
		if q.Columns != nil {
			item.Columns = amountColumns(row.Columns, shown, money, none)
		}
		if len(row.Metrics) > 0 {
			item.Metrics = make(map[string]dto.UsageQueryValue, len(row.Metrics))
			for name, v := range row.Metrics {
				item.Metrics[name] = dto.UsageQueryValue{Count: v.Count, Values: v.Values}
			}
		}
		if q.WindowSize != "" {
			item.Points = usagePoints(row.Points, windows, amounts)
		}
		usageItemIDs(&item, row.LineItemIDs, lineByID)
		resp.Items = append(resp.Items, item)
	}
	return resp
}

// usagePoints lists every window in the range, at zero where the row had no events.
func usagePoints(points []usageanalytics.Point, windows []time.Time, amounts func(subtotal, amount decimal.Decimal) dto.UsageAmounts) []dto.UsagePoint {
	byStart := lo.KeyBy(points, func(p usageanalytics.Point) int64 { return p.WindowStart.Unix() })
	return lo.Map(windows, func(w time.Time, _ int) dto.UsagePoint {
		p, ok := byStart[w.Unix()]
		if !ok {
			return dto.UsagePoint{Timestamp: w, UsageAmounts: amounts(decimal.Zero, decimal.Zero), Usage: "0"}
		}
		return dto.UsagePoint{Timestamp: w, UsageAmounts: amounts(p.Subtotal, p.Amount), Usage: p.Quantity.String()}
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
	return lo.Map(values, func(v string, _ int) string { return lo.Ternary(v == "", dto.UsageNoneValue, v) }), shown
}

// amountColumns keeps the shown column values and sums the rest into UsageOtherColumn, so columns add up to the row.
func amountColumns(amounts map[string]decimal.Decimal, shown map[string]bool, money func(decimal.Decimal) string, none func(string) string) map[string]dto.UsageColumn {
	out := make(map[string]dto.UsageColumn, len(amounts))
	other, hasOther := decimal.Zero, false
	for v, c := range amounts {
		if shown[v] {
			out[none(v)] = dto.UsageColumn{Amount: money(c)}
			continue
		}
		other, hasOther = other.Add(c), true
	}
	if hasOther {
		out[dto.UsageOtherColumn] = dto.UsageColumn{Amount: money(other)}
	}
	return out
}

// usageItemIDs sets the prices, plans, add-ons and subscriptions of the lines behind a row.
func usageItemIDs(item *dto.UsageQueryItem, lineIDs []string, lineByID map[string]*subscription.SubscriptionLineItem) {
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
func (s *usageQueryService) expandObjects(ctx context.Context, expand []dto.UsageExpand, resp *dto.UsageQueryResponse, in *queryInputs) error {
	want := lo.SliceToMap(expand, func(e dto.UsageExpand) (dto.UsageExpand, bool) { return e, true })
	if len(want) == 0 {
		return nil
	}
	ids := func(pick func(dto.UsageQueryItem) []string) []string {
		return sortedIDs(lo.FlatMap(resp.Items, func(it dto.UsageQueryItem, _ int) []string { return pick(it) }))
	}
	priceIDs := ids(func(it dto.UsageQueryItem) []string { return it.PriceIDs })
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

	if want[dto.UsageExpandPrice] {
		resp.Prices = map[string]*dto.PriceResponse{}
		for _, id := range priceIDs {
			if p := priceByID[id]; p != nil {
				resp.Prices[id] = &dto.PriceResponse{Price: p}
			}
		}
	}
	if want[dto.UsageExpandSubscription] {
		resp.Subscriptions = map[string]*dto.SubscriptionResponse{}
		for _, id := range ids(func(it dto.UsageQueryItem) []string { return it.SubscriptionIDs }) {
			if sub := in.subByID[id]; sub != nil {
				resp.Subscriptions[id] = &dto.SubscriptionResponse{Subscription: sub}
			}
		}
	}
	if want[dto.UsageExpandPlan] {
		resp.Plans = map[string]*dto.PlanResponse{}
		if planIDs := ids(func(it dto.UsageQueryItem) []string { return it.PlanIDs }); len(planIDs) > 0 {
			plans, err := s.PlanRepo.ListByIDs(ctx, planIDs)
			if err != nil {
				return err
			}
			for _, p := range plans {
				resp.Plans[p.ID] = &dto.PlanResponse{Plan: p}
			}
		}
	}
	if want[dto.UsageExpandAddon] {
		resp.Addons = map[string]*dto.AddonResponse{}
		if addonIDs := ids(func(it dto.UsageQueryItem) []string { return it.AddonIDs }); len(addonIDs) > 0 {
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
	if want[dto.UsageExpandMeter] {
		resp.Meters = map[string]*dto.MeterResponse{}
		for _, id := range meterIDs {
			if m := in.meterByID[id]; m != nil {
				resp.Meters[id] = dto.ToMeterResponse(m)
			}
		}
	}
	if want[dto.UsageExpandFeature] {
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
