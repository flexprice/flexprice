package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// lineKey identifies one usage line over one line window inside a billing period.
type lineKey struct {
	lineItemID  string
	windowStart time.Time
	windowEnd   time.Time
}

// pricedLine is a usage line's invoice cost for one window, split by component.
// TrueUp is set only for line-level commitments; it is attributable to the line, never to an event value.
type pricedLine struct {
	key            lineKey
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
	// rate is the cost per unit of usage, set by buildLineRates for lines that have usage.
	rate events.AnalyticsV2Amounts
	// bucket is set for bucketed MAX lines: usage is the sum of each billing bucket's peak.
	bucket *lineBucket
}

type lineBucket struct {
	size          types.WindowSize
	group         string
	billingAnchor time.Time
}

// window is the line as the queries see it: which meter's events, between which times, and its buckets.
func (c *pricedLine) window() events.AnalyticsV2Line {
	w := events.AnalyticsV2Line{ID: c.key.lineItemID, MeterID: c.meterID, Start: c.key.windowStart, End: c.key.windowEnd}
	if c.bucket != nil {
		w.BucketSize, w.BucketGroup, w.BillingAnchor = c.bucket.size, c.bucket.group, c.bucket.billingAnchor
	}
	return w
}

func (c *pricedLine) total() decimal.Decimal {
	return c.cost.Usage.Add(c.cost.CommitmentUtilized).Add(c.cost.Overage).Add(c.cost.TrueUp)
}

// pricedPeriods is the invoice view of every billing period in the requested range.
type pricedPeriods struct {
	lines []*pricedLine
	// periods and elapsed record how much invoice pricing the request needed, for tracing.
	periods int
	elapsed time.Duration
}

// overlappingBillingPeriods returns the subscription's full billing periods that overlap [start, end).
func overlappingBillingPeriods(sub *subscription.Subscription, start, end time.Time) ([]types.Period, error) {
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

// classifyInvoiceLines turns billing's usage rows for one subscription into per-line costs.
// Rows without a line (subscription-level commitment true-up or overage) are not covered yet and are skipped.
func classifyInvoiceLines(subscriptionID, currency string, rows []dto.CreateInvoiceLineItemRequest) *pricedPeriods {
	byKey := make(map[lineKey]*pricedLine)
	out := &pricedPeriods{}

	for _, row := range rows {
		// Zero rows on a line are kept: a free meter still shows usage and event counts, and an
		// unsupported meter is still reported when it costs nothing.
		lineID := lo.FromPtr(row.SubscriptionLineItemID)
		if lineID == "" {
			continue
		}

		key := lineKey{lineItemID: lineID, windowStart: lo.FromPtr(row.PeriodStart), windowEnd: lo.FromPtr(row.PeriodEnd)}
		line := byKey[key]
		if line == nil {
			line = &pricedLine{
				key:            key,
				subscriptionID: subscriptionID,
				currency:       currency,
				meterID:        lo.FromPtr(row.MeterID),
				priceID:        lo.FromPtr(row.PriceID),
			}
			line.planID, line.addonID = rowOwner(row)
			byKey[key] = line
			out.lines = append(out.lines, line)
		}
		addRowCost(line, row)
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

// addRowCost adds one billing row to its line, by the component the row represents.
func addRowCost(line *pricedLine, row dto.CreateInvoiceLineItemRequest) {
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

// rowOwner returns the plan or addon a billing row belongs to.
func rowOwner(row dto.CreateInvoiceLineItemRequest) (planID, addonID string) {
	switch lo.FromPtr(row.EntityType) {
	case string(types.SubscriptionLineItemEntityTypePlan):
		return lo.FromPtr(row.EntityID), ""
	case string(types.SubscriptionLineItemEntityTypeAddon):
		return "", lo.FromPtr(row.EntityID)
	default:
		return "", ""
	}
}

// priceSubscriptions prices every billing period overlapping [start, end) with the invoice code.
func (s *meterUsageService) priceSubscriptions(
	ctx context.Context,
	subs []*subscription.Subscription,
	start, end time.Time,
) (*pricedPeriods, error) {
	began := clock.Now()
	billing := newBillingService(s.ServiceParams)
	out := &pricedPeriods{}

	for _, sub := range subs {
		usageItems := lo.Filter(sub.LineItems, func(li *subscription.SubscriptionLineItem, _ int) bool {
			return li.PriceType == types.PRICE_TYPE_USAGE && li.MeterID != ""
		})
		if len(usageItems) == 0 {
			continue
		}

		periods, err := overlappingBillingPeriods(sub, start, end)
		if err != nil {
			return nil, err
		}
		out.periods += len(periods)
		for _, p := range periods {
			result, err := billing.calculateMeterUsageCharges(ctx, sub, usageItems, p.Start, p.End, true, nil)
			if err != nil {
				return nil, err
			}
			priced := classifyInvoiceLines(sub.ID, sub.Currency, result.UsageCharges)
			out.lines = append(out.lines, priced.lines...)
		}
	}

	if err := s.applyCoupons(ctx, subs, out, start, end); err != nil {
		return nil, err
	}
	out.elapsed = clock.Now().Sub(began)
	return out, nil
}

// applyCoupons sets each line's discount from the percentage coupons
// active in its billing period: line coupons first, then subscription coupons, compounding.
func (s *meterUsageService) applyCoupons(
	ctx context.Context,
	subs []*subscription.Subscription,
	priced *pricedPeriods,
	start, end time.Time,
) error {
	from, to := start, end
	for _, l := range priced.lines {
		from, to = lo.Earliest(from, l.key.windowStart), lo.Latest(to, l.key.windowEnd)
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
	return nil
}

// rejectSubscriptionFamilies fails for parent/child subscriptions, whose lines are priced on the whole family's usage.
func rejectSubscriptionFamilies(subs []*subscription.Subscription) error {
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

// isSupportedPricing reports whether a line's cost can be split across event values: additive meters, whose
// usage is a sum of event quantities, and bucketed MAX, whose usage is a sum of bucket peaks. Plain MAX and
// other aggregations are left out of analytics query.
func isSupportedPricing(m *meter.Meter, p *price.Price) bool {
	if m == nil {
		return false
	}
	if price.IsBucketedMax(p, m) {
		return true
	}
	switch m.Aggregation.Type {
	case types.AggregationSum, types.AggregationCount, types.AggregationSumWithMultiplier:
		return true
	default:
		return false
	}
}
