package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// adjustMeterUsageGrantsResult is the per-line-item output of the grant folding.
// Overage is in meter units for the quantity measure and in currency for the
// amount measure; PerECOverage decomposes it per entitlement config.
type adjustMeterUsageGrantsResult struct {
	Measure      types.EntitlementGrantMeasure
	Overage      decimal.Decimal
	PerECOverage map[string]decimal.Decimal
}

// loadEntitlementGrantsByMeterID returns grants overlapping the billing cycle
// for this subscription, bucketed by meter id.
//
// One query, all scopes; folding is feature-scope only. A GROUP or SUBSCRIPTION
// grant spans multiple meters, and this map is consumed per line item — putting
// the same grant in more than one meter bucket would count its overage once per
// meter. Those scopes need a cross-line allocation pass at the invoice level
// before they can bill; until that exists they are intentionally not folded.
func (s *billingService) loadEntitlementGrantsByMeterID(
	ctx context.Context,
	sub *subscription.Subscription,
	aggregatedFeatures []*dto.AggregatedFeature,
	periodStart, periodEnd time.Time,
) (map[string][]*entitlementgrant.EntitlementGrant, error) {
	if s.EntitlementGrantRepo == nil || sub == nil {
		return nil, nil
	}

	filter := types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(sub.CustomerID).
		WithSubscriptionIDs(sub.ID).
		WithCycleOverlap(periodStart, periodEnd)
	grants, err := s.EntitlementGrantRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(grants) == 0 {
		return nil, nil
	}

	meterByFeatureID := make(map[string]string, len(aggregatedFeatures))
	for _, f := range aggregatedFeatures {
		if f == nil || f.Feature == nil || f.Feature.MeterID == "" {
			continue
		}
		meterByFeatureID[f.Feature.ID] = f.Feature.MeterID
	}

	out := make(map[string][]*entitlementgrant.EntitlementGrant)
	for _, g := range grants {
		if g == nil || !g.IsFeatureScoped() {
			continue
		}
		if meterID := meterByFeatureID[g.ScopeEntityID]; meterID != "" {
			out[meterID] = append(out[meterID], g)
		}
	}
	return out, nil
}

// adjustMeterUsageGrants folds the meter's EG overage into the line item.
// Quantity measure: overage re-enters the pricer as the billable quantity.
// Amount measure: overage is already money and bypasses the pricer.
// applied=false when the pricing guard rejects the line item; a measurement
// error propagates — billing a knowingly wrong number is never an option.
func (s *billingService) adjustMeterUsageGrants(
	ctx context.Context,
	item *subscription.SubscriptionLineItem,
	matchingCharge *dto.SubscriptionUsageByMetersResponse,
	grants []*entitlementgrant.EntitlementGrant,
	priceService PriceService,
	m *meter.Meter,
	sub *subscription.Subscription,
	extCustomerIDs []string,
) (adjustMeterUsageGrantsResult, bool, error) {
	if len(grants) == 0 {
		return adjustMeterUsageGrantsResult{}, false, nil
	}
	// Measure is copied from the EC to every grant; EC-write validation keeps
	// it consistent per feature, so the first row is authoritative.
	measure := grants[0].Measure
	if measure == "" {
		return adjustMeterUsageGrantsResult{}, false, nil
	}

	if guardErr := grantPricingGuard(measure, item, matchingCharge.Price, m); guardErr != nil {
		s.Logger.Error(ctx, "entitlement grant overage: line item rejected, skipping grants",
			"meter_id", item.MeterID,
			"line_item_id", item.ID,
			"measure", measure,
			"error", guardErr,
		)
		return adjustMeterUsageGrantsResult{}, false, nil
	}

	// Per-EC violation totals (usage − quota). ECs share the usage stream, so
	// these overlap — attribution only, never summed into the bill directly.
	perECOverage := make(map[string]decimal.Decimal)
	ecIDs := make(map[string]struct{})
	for _, g := range grants {
		if g == nil {
			continue
		}
		ecIDs[g.EntitlementConfigID] = struct{}{}
		if overage := g.Overage(); overage.IsPositive() {
			perECOverage[g.EntitlementConfigID] = perECOverage[g.EntitlementConfigID].Add(overage)
		}
	}

	res := adjustMeterUsageGrantsResult{Measure: measure, PerECOverage: perECOverage}
	if len(ecIDs) <= 1 {
		// Single EC: its windows never overlap, so the snapshot sum is exact
		for _, total := range perECOverage {
			res.Overage = res.Overage.Add(total)
		}
	} else {
		overage, err := s.mergedOverage(ctx, m, sub, extCustomerIDs, grants, measure)
		if err != nil {
			return adjustMeterUsageGrantsResult{}, false, err
		}
		res.Overage = overage
	}

	switch measure {
	case types.EntitlementGrantMeasureQuantity:
		matchingCharge.SetQuantityDecimal(res.Overage)
		if matchingCharge.Price != nil {
			cost := priceService.CalculateCost(ctx, matchingCharge.Price, res.Overage)
			matchingCharge.SetAmountWithCurrencyPrecision(cost, matchingCharge.Price.Currency)
		} else {
			matchingCharge.SetAmountDecimal(decimal.Zero)
		}
	case types.EntitlementGrantMeasureAmount:
		// Already money — zero the qty so aggregation doesn't double-count.
		if matchingCharge.Price != nil {
			matchingCharge.SetAmountWithCurrencyPrecision(res.Overage, matchingCharge.Price.Currency)
		} else {
			matchingCharge.SetAmountDecimal(res.Overage)
		}
		matchingCharge.SetQuantityDecimal(decimal.Zero)
	}
	return res, true, nil
}

// mergedOverage bills usage inside the unique overage windows across the
// meter's ECs. Each quota-crossed EG contributes [quota_crossed_at, valid_to);
// overlapping windows merge, so a unit bills once no matter how many EGs were in overage.
//
// Quantity measure: one ClickHouse query over all windows (TimeRanges)
// Amount measure: one billing-path pricing per window.
// Zero queries when nothing crossed.
func (s *billingService) mergedOverage(
	ctx context.Context,
	m *meter.Meter,
	sub *subscription.Subscription,
	extCustomerIDs []string,
	grants []*entitlementgrant.EntitlementGrant,
	measure types.EntitlementGrantMeasure,
) (decimal.Decimal, error) {
	overageIntervals := make([]timeInterval, 0, len(grants))
	for _, g := range grants {
		if g != nil && g.QuotaCrossedAt != nil {
			overageIntervals = append(overageIntervals, timeInterval{start: *g.QuotaCrossedAt, end: g.ValidTo})
		}
	}
	if len(overageIntervals) == 0 {
		return decimal.Zero, nil
	}
	if m == nil {
		return decimal.Zero, errGrantDepsMissing
	}

	overageWindows := mergeIntervals(overageIntervals)
	billableRanges := make([]events.TimeRange, 0, len(overageWindows))
	for _, w := range overageWindows {
		billableRanges = append(billableRanges, events.TimeRange{Start: w.start, End: w.end})
	}

	meterUsageSvc := NewMeterUsageService(s.ServiceParams)
	total := decimal.Zero
	switch measure {
	case types.EntitlementGrantMeasureQuantity:
		usage, err := meterUsageSvc.GetUsageTotal(ctx, &dto.UsageTotalRequest{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerIDs: extCustomerIDs,
			MeterID:             m.ID,
			AggregationType:     m.Aggregation.Type,
			TimeRanges:          billableRanges,
		})
		if err != nil {
			return decimal.Zero, err
		}
		total = usage
	case types.EntitlementGrantMeasureAmount:
		if sub == nil {
			return decimal.Zero, errGrantDepsMissing
		}
		for _, r := range billableRanges {
			cost, err := meterUsageSvc.GetMeterWindowCost(ctx, sub, m.ID, r.Start, r.End)
			if err != nil {
				return decimal.Zero, err
			}
			total = total.Add(cost)
		}
	}
	return total, nil
}

// timeInterval is a half-open [start, end) range.
type timeInterval struct {
	start, end time.Time
}

// mergeIntervals coalesces overlapping/touching intervals; empty ones drop.
func mergeIntervals(in []timeInterval) []timeInterval {
	valid := make([]timeInterval, 0, len(in))
	for _, iv := range in {
		if iv.end.After(iv.start) {
			valid = append(valid, iv)
		}
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].start.Before(valid[j].start) })

	out := make([]timeInterval, 0, len(valid))
	for _, iv := range valid {
		if n := len(out); n > 0 && !iv.start.After(out[n-1].end) {
			if iv.end.After(out[n-1].end) {
				out[n-1].end = iv.end
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}

// grantPricingGuard returns nil when grants may fold into this line item.
// The fold assumes usage is additive over disjoint time windows (snapshot sums
// per window, merged-window measurement) — so non-additive aggregations
// (MAX, LATEST, AVG, ...) and bucketed meters are rejected outright.
// Tiered prices are rejected for both measures (tiers walk with cumulative
// cycle quantity; overage priced standalone would land in the wrong tier).
// Commitment/true-up reject only the amount measure — they reconcile the
// whole cycle, which pre-priced overage bypasses; the quantity measure feeds
// its qty back into the normal pipeline where they compose correctly.
func grantPricingGuard(measure types.EntitlementGrantMeasure, item *subscription.SubscriptionLineItem, price *priceDomain.Price, m *meter.Meter) error {
	if m != nil {
		switch m.Aggregation.Type {
		case types.AggregationSum, types.AggregationCount, types.AggregationSumWithMultiplier:
		default:
			return errGrantNonAdditiveAggregation
		}
		if m.Aggregation.BucketSize != "" {
			return errGrantBucketedMeter
		}
	}
	if price != nil && price.BillingModel == types.BILLING_MODEL_TIERED {
		return errGrantTiered
	}
	if measure != types.EntitlementGrantMeasureAmount || item == nil {
		return nil
	}
	if item.HasAnyCommitment() {
		return errGrantAmountCommitment
	}
	if item.HasTrueUpEnabled() {
		return errGrantAmountTrueUp
	}
	return nil
}

var (
	errGrantAmountCommitment       = grantGuardError("line item carries a commitment")
	errGrantAmountTrueUp           = grantGuardError("line item enables true-up")
	errGrantTiered                 = grantGuardError("price uses tiered billing")
	errGrantDepsMissing            = grantGuardError("measurement dependencies unavailable")
	errGrantNonAdditiveAggregation = grantGuardError("meter aggregation is not additive over time windows")
	errGrantBucketedMeter          = grantGuardError("meter uses bucketed aggregation")
)

type grantGuardError string

func (e grantGuardError) Error() string { return string(e) }
