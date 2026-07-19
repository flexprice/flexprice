package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// adjustMeterUsageGrantsResult is the per-line-item output of the grant folding.
// Exactly one of {AdjustedQty, OverageAmount} carries the load per invocation,
// selected by Measure.
type adjustMeterUsageGrantsResult struct {
	Measure        types.EntitlementGrantMeasure
	AdjustedQty    decimal.Decimal
	OverageAmount  decimal.Decimal
	AppliedGrantID string
}

// loadEntitlementGrantsByMeterID returns grants overlapping the billing cycle
// for this subscription's features, bucketed by meter id.
func (s *billingService) loadEntitlementGrantsByMeterID(
	ctx context.Context,
	sub *subscription.Subscription,
	aggregatedFeatures []*dto.AggregatedFeature,
	periodStart, periodEnd time.Time,
) (map[string][]*entitlementgrant.EntitlementGrant, error) {
	if s.EntitlementGrantRepo == nil || sub == nil {
		return nil, nil
	}
	featureIDs := lo.Uniq(lo.FilterMap(aggregatedFeatures, func(f *dto.AggregatedFeature, _ int) (string, bool) {
		if f == nil || f.Feature == nil {
			return "", false
		}
		return f.Feature.ID, f.Feature.ID != ""
	}))
	if len(featureIDs) == 0 {
		return nil, nil
	}

	filter := types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(sub.CustomerID).
		WithSubscriptionIDs(sub.ID).
		WithFeatureIDs(featureIDs...).
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
		if f == nil || f.Feature == nil {
			continue
		}
		if f.Feature.MeterID != "" {
			meterByFeatureID[f.Feature.ID] = f.Feature.MeterID
		}
	}

	out := make(map[string][]*entitlementgrant.EntitlementGrant)
	for _, g := range grants {
		if g == nil || !g.IsFeatureScoped() {
			continue
		}
		meterID := meterByFeatureID[g.ScopeEntityID]
		if meterID == "" {
			continue
		}
		out[meterID] = append(out[meterID], g)
	}
	return out, nil
}

// adjustMeterUsageGrants folds Σ max(0, grant.usage − grant.quota) into the line
// item. Quantity lane returns AdjustedQty for the pricer; amount lane returns
// OverageAmount (already priced) and zeros the qty. Returns applied=false when
// the runtime pricing guard rejects an amount-lane line item.
func (s *billingService) adjustMeterUsageGrants(
	ctx context.Context,
	item *subscription.SubscriptionLineItem,
	matchingCharge *dto.SubscriptionUsageByMetersResponse,
	grants []*entitlementgrant.EntitlementGrant,
	priceService PriceService,
) (adjustMeterUsageGrantsResult, bool) {
	if len(grants) == 0 {
		return adjustMeterUsageGrantsResult{}, false
	}
	measure := grants[0].Measure
	if measure == "" {
		return adjustMeterUsageGrantsResult{}, false
	}

	// EC validation keeps measure consistent per feature; drop out if runtime disagrees.
	for _, g := range grants[1:] {
		if g.Measure != measure {
			s.Logger.Warn(ctx, "entitlement grant overage: mixed measures on same feature, skipping grants",
				"meter_id", item.MeterID,
				"line_item_id", item.ID)
			return adjustMeterUsageGrantsResult{}, false
		}
	}

	// Belt-and-braces guard for amount lane; EC validation should have caught this.
	if measure == types.EntitlementGrantMeasureAmount {
		if guardErr := amountLanePricingGuard(item, matchingCharge.Price); guardErr != nil {
			s.Logger.Warn(ctx, "entitlement grant overage: amount lane rejected by runtime pricing guard, skipping grants",
				"meter_id", item.MeterID,
				"line_item_id", item.ID,
				"reason", guardErr.Error(),
			)
			return adjustMeterUsageGrantsResult{}, false
		}
	}

	res := adjustMeterUsageGrantsResult{Measure: measure}
	for _, g := range grants {
		if g == nil {
			continue
		}
		overage := g.Overage()
		if !overage.IsPositive() {
			continue
		}
		if res.AppliedGrantID == "" {
			res.AppliedGrantID = g.ID
		}
		switch measure {
		case types.EntitlementGrantMeasureQuantity:
			res.AdjustedQty = res.AdjustedQty.Add(overage)
		case types.EntitlementGrantMeasureAmount:
			res.OverageAmount = res.OverageAmount.Add(overage)
		}
	}

	switch measure {
	case types.EntitlementGrantMeasureQuantity:
		if matchingCharge.Price != nil {
			adjustedAmount := priceService.CalculateCost(ctx, matchingCharge.Price, res.AdjustedQty)
			matchingCharge.Amount = priceDomain.FormatAmountToFloat64WithPrecision(adjustedAmount, matchingCharge.Price.Currency)
		} else {
			matchingCharge.Amount = 0
		}
		matchingCharge.Quantity = res.AdjustedQty.InexactFloat64()
	case types.EntitlementGrantMeasureAmount:
		if matchingCharge.Price != nil {
			matchingCharge.Amount = priceDomain.FormatAmountToFloat64WithPrecision(res.OverageAmount, matchingCharge.Price.Currency)
		} else {
			matchingCharge.Amount = res.OverageAmount.InexactFloat64()
		}
		// Amount-lane grants are already priced; zero the qty so aggregation doesn't double-count.
		matchingCharge.Quantity = 0
	}
	return res, true
}

// amountLanePricingGuard returns nil when the line item is safe for the amount lane.
func amountLanePricingGuard(item *subscription.SubscriptionLineItem, price *priceDomain.Price) error {
	if item == nil {
		return nil
	}
	if item.HasAnyCommitment() {
		return errAmountLaneCommitment
	}
	if item.HasTrueUpEnabled() {
		return errAmountLaneTrueUp
	}
	if price == nil {
		return nil
	}
	if price.BillingModel == types.BILLING_MODEL_TIERED {
		return errAmountLaneTiered
	}
	return nil
}

var (
	errAmountLaneCommitment = errAmountLaneReason("line item carries a commitment")
	errAmountLaneTrueUp     = errAmountLaneReason("line item enables true-up")
	errAmountLaneTiered     = errAmountLaneReason("price uses tiered billing")
)

type errAmountLaneReason string

func (e errAmountLaneReason) Error() string { return string(e) }
