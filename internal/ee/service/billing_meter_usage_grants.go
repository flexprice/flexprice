package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
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
	// Measure is set on the EC and copied to every grant at open time; EC-write
	// validation keeps it consistent per feature, so we can trust the first row.
	measure := grants[0].Measure
	if measure == "" {
		return adjustMeterUsageGrantsResult{}, false
	}

	// Runtime-only guard: commitments and true-up live on the sub line item, not
	// the plan-level EC, so this check has to happen here — EC-write validation
	// can't see them at config time.
	if measure == types.EntitlementGrantMeasureAmount {
		if guardErr := amountLanePricingGuard(item, matchingCharge.Price); guardErr != nil {
			s.Logger.Error(ctx, "entitlement grant overage: amount lane rejected, skipping grants",
				"meter_id", item.MeterID,
				"line_item_id", item.ID,
				"error", guardErr,
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
//
// Why each of these disqualifies amount grants:
//   - commitment: the pricer walks the sub line's minimum commitment across the
//     full cycle. Amount grants pre-price a slice of usage inside a window and
//     hand back OverageAmount; the pricer would then re-apply commitment on top
//     and either double-charge or under-charge the commit floor.
//   - true-up: same shape — true-up reconciles the whole cycle against actual
//     usage. Pre-priced overage bypasses it and produces a wrong final invoice.
//   - tiered: tier boundaries walk with cumulative cycle quantity. A grant only
//     knows its own window's qty, so it can't price against the right tier.
//     EC-write validation already rejects this, but the guard keeps the
//     billing path safe if a tier is ever added after the EC was created.
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
