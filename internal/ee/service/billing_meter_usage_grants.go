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
	"github.com/samber/lo"
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

	// A grant outlives the config that funded it, so a feature whose last live
	// entitlement is gone is missing from aggregatedFeatures. Resolve those
	// features directly — the lookup the evaluator already does — or the grant
	// silently stops discounting while the evaluator keeps maintaining it.
	missing := make([]string, 0)
	for _, g := range grants {
		if g == nil || !g.IsFeatureScoped() {
			continue
		}
		if _, ok := meterByFeatureID[g.ScopeEntityID]; !ok {
			missing = append(missing, g.ScopeEntityID)
		}
	}
	if len(missing) > 0 {
		featureFilter := types.NewNoLimitFeatureFilter()
		featureFilter.FeatureIDs = lo.Uniq(missing)
		orphanFeatures, err := s.FeatureRepo.List(ctx, featureFilter)
		if err != nil {
			return nil, err
		}
		for _, f := range orphanFeatures {
			if f != nil && f.MeterID != "" {
				meterByFeatureID[f.ID] = f.MeterID
			}
		}
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

	if guardErr := GrantPricingGuard(measure, item, matchingCharge.Price, m); guardErr != nil {
		s.Logger.Error(ctx, "entitlement grant overage: line item rejected, skipping grants",
			"meter_id", item.MeterID,
			"line_item_id", item.ID,
			"measure", measure,
			"error", guardErr,
		)
		return adjustMeterUsageGrantsResult{}, false, nil
	}

	// Per-EC violation totals (usage − quota). Overlapping windows share the usage
	// stream, so these can double count — attribution only, never summed blindly.
	perECOverage := make(map[string]decimal.Decimal)
	for _, g := range grants {
		if g == nil {
			continue
		}
		if overage := g.Overage(); overage.IsPositive() {
			perECOverage[g.EntitlementConfigID] = perECOverage[g.EntitlementConfigID].Add(overage)
		}
	}

	res := adjustMeterUsageGrantsResult{Measure: measure, PerECOverage: perECOverage}
	if !grantWindowsOverlap(grants) {
		// Disjoint => an event lands in exactly one window, so the overages just add up.
		// Two ECs is NOT overlap: a detach re-keys the pooled slot and the cycle tiles.
		//   EG1 [t0,t1) EC1 Q=1000 U=1200 => over 200
		//   EG2 [t1,t2) EC2 Q= 800 U= 900 => over 100
		//   bill 300 — the 1200 and the 900 are different events
		for _, total := range perECOverage {
			res.Overage = res.Overage.Add(total)
		}
	} else {
		// Overlapping => parallel ECs meter the SAME events against their own quotas,
		// so adding the overages bills one unit twice.
		//   EG1 [t0,t2) EC1 Q=500 U=900 => over 400
		//   EG2 [t0,t2) EC2 Q=400 U=900 => over 500
		//   adding gives 900 for 900 units of usage — every unit billed twice.
		// Measure the merged overage window instead: 900 units, crossing at 600,
		// bills the 300 spent past it, once.
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
	overageIntervals := make([]TimeInterval, 0, len(grants))
	for _, g := range grants {
		if g != nil && g.QuotaCrossedAt != nil {
			overageIntervals = append(overageIntervals, TimeInterval{Start: *g.QuotaCrossedAt, End: g.ValidTo})
		}
	}
	if len(overageIntervals) == 0 {
		return decimal.Zero, nil
	}
	if m == nil {
		return decimal.Zero, errGrantDepsMissing
	}

	overageWindows := MergeIntervals(overageIntervals)
	billableRanges := make([]events.TimeRange, 0, len(overageWindows))
	for _, w := range overageWindows {
		billableRanges = append(billableRanges, events.TimeRange{Start: w.Start, End: w.End})
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

// grantWindowsOverlap reports whether any two grant windows share an instant — the
// only case where one event can be counted against two quotas.
// [t0,t1) + [t1,t2) => false, a re-keyed pool tiling the cycle.
// [t0,t2) + [t0,t2) => true, parallel ECs on one feature.
func grantWindowsOverlap(grants []*entitlementgrant.EntitlementGrant) bool {
	windows := make([]TimeInterval, 0, len(grants))
	for _, g := range grants {
		if g != nil && g.ValidTo.After(g.ValidFrom) {
			windows = append(windows, TimeInterval{Start: g.ValidFrom, End: g.ValidTo})
		}
	}
	if len(windows) < 2 {
		return false
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].Start.Before(windows[j].Start) })

	maxEnd := windows[0].End
	for _, w := range windows[1:] {
		if w.Start.Before(maxEnd) {
			return true
		}
		if w.End.After(maxEnd) {
			maxEnd = w.End
		}
	}
	return false
}

// TimeInterval is a half-open [Start, End) range.
type TimeInterval struct {
	Start, End time.Time
}

// MergeIntervals coalesces overlapping/touching intervals; empty ones drop.
func MergeIntervals(in []TimeInterval) []TimeInterval {
	valid := make([]TimeInterval, 0, len(in))
	for _, iv := range in {
		if iv.End.After(iv.Start) {
			valid = append(valid, iv)
		}
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].Start.Before(valid[j].Start) })

	out := make([]TimeInterval, 0, len(valid))
	for _, iv := range valid {
		if n := len(out); n > 0 && !iv.Start.After(out[n-1].End) {
			if iv.End.After(out[n-1].End) {
				out[n-1].End = iv.End
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}

// GrantPricingGuard returns nil when grants may fold into this line item.
// The fold assumes usage is additive over disjoint time windows (snapshot sums
// per window, merged-window measurement) — so non-additive aggregations
// (MAX, LATEST, AVG, ...) and bucketed meters are rejected outright.
// Tiered prices are rejected for both measures (tiers walk with cumulative
// cycle quantity; overage priced standalone would land in the wrong tier).
// Commitment/true-up reject only the amount measure — they reconcile the
// whole cycle, which pre-priced overage bypasses; the quantity measure feeds
// its qty back into the normal pipeline where they compose correctly.
func GrantPricingGuard(measure types.EntitlementGrantMeasure, item *subscription.SubscriptionLineItem, price *priceDomain.Price, m *meter.Meter) error {
	if m != nil {
		switch m.Aggregation.Type {
		case types.AggregationSum, types.AggregationCount, types.AggregationSumWithMultiplier:
		default:
			return errGrantNonAdditiveAggregation
		}
		if priceDomain.IsBucketed(price, m) {
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
