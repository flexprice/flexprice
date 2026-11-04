package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// analyticsV2LineKey identifies one usage line over one line window inside a billing period.
type analyticsV2LineKey struct {
	lineItemID  string
	windowStart time.Time
	windowEnd   time.Time
}

// analyticsV2LineCost is a usage line's invoice cost for one window, split by component.
// TrueUp is set only for line-level commitments; it is attributable to the line, never to an event value.
type analyticsV2LineCost struct {
	key            analyticsV2LineKey
	subscriptionID string
	meterID        string
	priceID        string
	planID         string
	addonID        string
	currency       string
	cost           dto.AnalyticsV2Cost
	freeQuantity   decimal.Decimal
	// discount is the percentage-coupon discount on total(), as the invoice would apply it.
	discount decimal.Decimal
}

func (c *analyticsV2LineCost) total() decimal.Decimal {
	return c.cost.Usage.Add(c.cost.CommitmentUtilized).Add(c.cost.Overage).Add(c.cost.TrueUp)
}

// analyticsV2UnattributedCost is billed cost no event value used, with the subscription and window it belongs to.
type analyticsV2UnattributedCost struct {
	subscriptionID string
	currency       string
	windowStart    time.Time
	windowEnd      time.Time
	charge         dto.AnalyticsV2UnattributedCharge
	discount       decimal.Decimal
}

// analyticsV2PricedPeriods is the invoice view of every billing period in the requested range.
type analyticsV2PricedPeriods struct {
	lines        []*analyticsV2LineCost
	unattributed []*analyticsV2UnattributedCost
	// periods and elapsed record how much invoice pricing the request needed, for tracing.
	periods int
	elapsed time.Duration
}

// analyticsV2BillingPeriods returns the subscription's full billing periods that overlap [start, end).
func analyticsV2BillingPeriods(sub *subscription.Subscription, start, end time.Time) ([]types.Period, error) {
	grid, err := types.NewBillingPeriodGrid(sub.BillingAnchor, sub.BillingPeriod, lo.Max([]int{sub.BillingPeriodCount, 1}), sub.Timezone)
	if err != nil {
		return nil, err
	}

	lower := lo.Latest(start, sub.StartDate)
	upper := end
	if sub.EndDate != nil && sub.EndDate.Before(upper) {
		upper = *sub.EndDate
	}
	if sub.CancelledAt != nil && sub.CancelledAt.Before(upper) {
		upper = *sub.CancelledAt
	}

	var periods []types.Period
	for t := lower; t.Before(upper); {
		p := types.FullBillingPeriod(t, grid)
		if !p.End.After(t) {
			break
		}
		periods = append(periods, p)
		t = p.End
	}
	return periods, nil
}

// classifyAnalyticsV2InvoiceLines turns billing's usage rows for one subscription into per-line costs.
// Rows without a line (plan or commitment true-up) are unattributed: no event value used them.
func classifyAnalyticsV2InvoiceLines(subscriptionID, currency string, rows []dto.CreateInvoiceLineItemRequest) *analyticsV2PricedPeriods {
	byKey := make(map[analyticsV2LineKey]*analyticsV2LineCost)
	out := &analyticsV2PricedPeriods{}

	for _, row := range rows {
		// Zero rows on a line are kept: a free meter still shows usage and event counts, and an
		// unsupported meter is still reported when it costs nothing.
		lineID := lo.FromPtr(row.SubscriptionLineItemID)
		if lineID == "" {
			if row.Amount.IsZero() {
				continue
			}
			out.unattributed = append(out.unattributed, &analyticsV2UnattributedCost{
				subscriptionID: subscriptionID,
				currency:       currency,
				windowStart:    lo.FromPtr(row.PeriodStart),
				windowEnd:      lo.FromPtr(row.PeriodEnd),
				charge:         analyticsV2UnlinkedRow(subscriptionID, row),
			})
			continue
		}

		key := analyticsV2LineKey{lineItemID: lineID, windowStart: lo.FromPtr(row.PeriodStart), windowEnd: lo.FromPtr(row.PeriodEnd)}
		line := byKey[key]
		if line == nil {
			line = &analyticsV2LineCost{
				key:            key,
				subscriptionID: subscriptionID,
				currency:       currency,
				meterID:        lo.FromPtr(row.MeterID),
				priceID:        lo.FromPtr(row.PriceID),
			}
			line.planID, line.addonID = analyticsV2RowOwner(row)
			byKey[key] = line
			out.lines = append(out.lines, line)
		}
		addAnalyticsV2RowCost(line, row)
	}

	sort.SliceStable(out.lines, func(i, j int) bool {
		a, b := out.lines[i].key, out.lines[j].key
		if a.lineItemID != b.lineItemID {
			return a.lineItemID < b.lineItemID
		}
		return a.windowStart.Before(b.windowStart)
	})
	return out
}

// addAnalyticsV2RowCost adds one billing row to its line, by the component the row represents.
func addAnalyticsV2RowCost(line *analyticsV2LineCost, row dto.CreateInvoiceLineItemRequest) {
	line.freeQuantity = line.freeQuantity.Add(lo.FromPtr(row.AdjustedEntitlementQuantity))
	c := &line.cost

	switch {
	case row.Metadata[types.MetadataKeyCommitmentLineKind] == string(types.CommitmentLineUtilized):
		c.CommitmentUtilized = c.CommitmentUtilized.Add(row.Amount)
	case row.Metadata[types.MetadataKeyCommitmentLineKind] == string(types.CommitmentLineOverage),
		row.Metadata[types.MetadataKeyIsOverage] == types.MetadataValueTrue:
		c.Overage = c.Overage.Add(row.Amount)
	case row.CommitmentInfo != nil:
		info := row.CommitmentInfo
		parts := info.ComputedCommitmentUtilizedAmount.Add(info.ComputedOverageAmount).Add(info.ComputedTrueUpAmount)
		c.CommitmentUtilized = c.CommitmentUtilized.Add(info.ComputedCommitmentUtilizedAmount)
		c.Overage = c.Overage.Add(info.ComputedOverageAmount)
		c.TrueUp = c.TrueUp.Add(info.ComputedTrueUpAmount)
		// Keep the row's billed amount exact if the reported parts don't add up to it.
		c.Usage = c.Usage.Add(row.Amount.Sub(parts))
	default:
		c.Usage = c.Usage.Add(row.Amount)
	}
}

// analyticsV2RowOwner returns the plan or addon a billing row belongs to.
func analyticsV2RowOwner(row dto.CreateInvoiceLineItemRequest) (planID, addonID string) {
	switch lo.FromPtr(row.EntityType) {
	case string(types.SubscriptionLineItemEntityTypePlan):
		return lo.FromPtr(row.EntityID), ""
	case string(types.SubscriptionLineItemEntityTypeAddon):
		return "", lo.FromPtr(row.EntityID)
	default:
		return "", ""
	}
}

func analyticsV2UnlinkedRow(subscriptionID string, row dto.CreateInvoiceLineItemRequest) dto.AnalyticsV2UnattributedCharge {
	reason := dto.AnalyticsV2ReasonUnlinkedCharge
	cost := dto.AnalyticsV2Cost{Usage: row.Amount}
	isTrueUp := row.Metadata[types.MetadataKeyCommitmentLineKind] == string(types.CommitmentLineTrueUp) ||
		row.Metadata[types.MetadataKeyIsCommitmentTrueup] == types.MetadataValueTrue
	if isTrueUp {
		reason = dto.AnalyticsV2ReasonUnusedCommitment
		cost = dto.AnalyticsV2Cost{TrueUp: row.Amount}
	}
	planID, addonID := analyticsV2RowOwner(row)
	return dto.AnalyticsV2UnattributedCharge{
		SubscriptionID: subscriptionID,
		PlanID:         planID,
		AddonID:        addonID,
		MeterID:        lo.FromPtr(row.MeterID),
		PriceID:        lo.FromPtr(row.PriceID),
		Cost:           row.Amount,
		Breakdown:      cost,
		Reason:         reason,
	}
}

// priceAnalyticsV2Subscriptions prices every billing period overlapping [start, end) with the invoice code.
func (s *meterUsageService) priceAnalyticsV2Subscriptions(
	ctx context.Context,
	subs []*subscription.Subscription,
	start, end time.Time,
) (*analyticsV2PricedPeriods, error) {
	began := clock.Now()
	billing := newBillingService(s.ServiceParams)
	out := &analyticsV2PricedPeriods{}

	for _, sub := range subs {
		usageItems := lo.Filter(sub.LineItems, func(li *subscription.SubscriptionLineItem, _ int) bool {
			return li.PriceType == types.PRICE_TYPE_USAGE && li.MeterID != ""
		})
		if len(usageItems) == 0 {
			continue
		}

		periods, err := analyticsV2BillingPeriods(sub, start, end)
		if err != nil {
			return nil, err
		}
		out.periods += len(periods)
		for _, p := range periods {
			result, err := billing.calculateMeterUsageCharges(ctx, sub, usageItems, p.Start, p.End, true, nil)
			if err != nil {
				return nil, err
			}
			priced := classifyAnalyticsV2InvoiceLines(sub.ID, sub.Currency, result.UsageCharges)
			out.lines = append(out.lines, priced.lines...)
			out.unattributed = append(out.unattributed, priced.unattributed...)
		}
	}

	if err := s.applyAnalyticsV2Coupons(ctx, subs, out, start, end); err != nil {
		return nil, err
	}
	out.elapsed = clock.Now().Sub(began)
	return out, nil
}

// applyAnalyticsV2Coupons sets each line's and unattributed cost's discount from the percentage coupons
// active in its billing period: line coupons first, then subscription coupons, compounding.
func (s *meterUsageService) applyAnalyticsV2Coupons(
	ctx context.Context,
	subs []*subscription.Subscription,
	priced *analyticsV2PricedPeriods,
	start, end time.Time,
) error {
	from, to := start, end
	for _, l := range priced.lines {
		from, to = lo.Earliest(from, l.key.windowStart), lo.Latest(to, l.key.windowEnd)
	}
	for _, u := range priced.unattributed {
		from, to = lo.Earliest(from, u.windowStart), lo.Latest(to, u.windowEnd)
	}

	sel, err := selectSubscriptionCoupons(ctx, s.ServiceParams, subs, from, to, isAnalyticsCoupon)
	if err != nil {
		return err
	}
	lineCoupons, subCoupons := projectAnalyticsCoupons(sel)
	if len(lineCoupons) == 0 && len(subCoupons) == 0 {
		return nil
	}

	activeIn := func(ws, we time.Time) func(*analyticsCoupon) bool {
		return func(c *analyticsCoupon) bool { return c.activeOverlaps(ws, we) }
	}
	for _, l := range priced.lines {
		l.discount = compound(l.total(), l.currency, lineCoupons[l.key.lineItemID], subCoupons[l.subscriptionID],
			activeIn(l.key.windowStart, l.key.windowEnd))
	}
	for _, u := range priced.unattributed {
		u.discount = compound(u.charge.Cost, u.currency, nil, subCoupons[u.subscriptionID], activeIn(u.windowStart, u.windowEnd))
	}
	return nil
}

// rejectAnalyticsV2Families fails for parent/child subscriptions, whose lines are priced on the whole family's usage.
func rejectAnalyticsV2Families(subs []*subscription.Subscription) error {
	for _, sub := range subs {
		if sub.SubscriptionType == types.SubscriptionTypeParent || sub.SubscriptionType == types.SubscriptionTypeInherited {
			return ierr.NewError("parent and child subscriptions are not supported by analytics query yet").
				WithHint("Use the /events/analytics endpoint for customers with parent or child subscriptions").
				WithReportableDetails(map[string]any{"subscription_id": sub.ID, "subscription_type": sub.SubscriptionType}).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// analyticsV2Supported reports whether a line's cost can be split across event values: additive meters and MAX.
func analyticsV2Supported(m *meter.Meter, p *price.Price) bool {
	if m == nil {
		return false
	}
	if price.IsBucketedMax(p, m) {
		return false
	}
	switch m.Aggregation.Type {
	case types.AggregationSum, types.AggregationCount, types.AggregationSumWithMultiplier, types.AggregationMax:
		return true
	default:
		return false
	}
}

// splitAnalyticsV2Excluded moves lines on unsupported meters out, summing their cost per meter. Their usage
// can't be split, so a partial range carries each line's cost in proportion to time.
func splitAnalyticsV2Excluded(
	lines []*analyticsV2LineCost,
	meters map[string]*meter.Meter,
	prices map[string]*price.Price,
	start, end time.Time,
) ([]*analyticsV2LineCost, []dto.AnalyticsV2ExcludedMeter) {
	kept := make([]*analyticsV2LineCost, 0, len(lines))
	excludedByMeter := make(map[string]*dto.AnalyticsV2ExcludedMeter)
	var order []string

	for _, line := range lines {
		m := meters[line.meterID]
		if analyticsV2Supported(m, prices[line.priceID]) {
			kept = append(kept, line)
			continue
		}
		ex := excludedByMeter[line.meterID]
		if ex == nil {
			ex = &dto.AnalyticsV2ExcludedMeter{MeterID: line.meterID, Cost: lo.ToPtr(decimal.Zero), Reason: dto.AnalyticsV2ReasonAggregationNotSupported}
			if m != nil {
				ex.Aggregation = m.Aggregation.Type
			}
			excludedByMeter[line.meterID] = ex
			order = append(order, line.meterID)
		}
		f := analyticsV2Overlap(start, end, line.key.windowStart, line.key.windowEnd)
		ex.Cost = lo.ToPtr(ex.Cost.Add(line.total().Sub(line.discount).Mul(f)))
	}

	excluded := make([]dto.AnalyticsV2ExcludedMeter, 0, len(order))
	for _, id := range order {
		excluded = append(excluded, *excludedByMeter[id])
	}
	return kept, excluded
}
