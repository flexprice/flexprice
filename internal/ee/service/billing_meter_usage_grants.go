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

// adjustMeterUsageGrantsResult carries what the per-line-item integration point
// needs to know: an adjusted billable quantity for the pricer to consume, an
// overage amount ready to plug straight into the line item's Amount field, and
// the grant lane it came from. Exactly one of {AdjustedQty, OverageAmount}
// carries the load per invocation.
type adjustMeterUsageGrantsResult struct {
	Measure        types.EntitlementGrantMeasure
	AdjustedQty    decimal.Decimal
	OverageAmount  decimal.Decimal
	AppliedGrantID string // audit — the first grant that contributed to overage; may be empty when none breached
}

// loadEntitlementGrantsByMeterID pulls every grant whose window overlaps the
// billing cycle for the given subscription/customer and buckets them by the
// meter their feature points at. Same shape as the sibling
// entitlementsByMeterID map in CalculateMeterUsageCharges so both paths can be
// consulted symmetrically in the per-line-item loop.
//
// The billing-path filter (WithCycleOverlap + WithScopeEntityIDs) matches the
// composite (tenant, env, customer, scope_entity_type, scope_entity_id,
// valid_from, valid_to) index, so this is one indexed PG hit per cycle build.
func (s *billingService) loadEntitlementGrantsByMeterID(
	ctx context.Context,
	sub *subscription.Subscription,
	aggregatedFeatures []*dto.AggregatedFeature,
	periodStart, periodEnd time.Time,
) (map[string][]*entitlementgrant.EntitlementGrant, error) {
	if s.EntitlementGrantRepo == nil || sub == nil {
		return nil, nil
	}
	// Feature IDs the subscription actually touches — anything else can't be
	// referenced by a grant snapshot we'd act on here.
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

	// Feature id → meter id lookup so we can key the return map by meter id
	// (what the outer loop's line item exposes).
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

// adjustMeterUsageGrants folds the summed per-grant overage into the line item.
// The math matches ERD §8.6 and §11.3: each grant is an independent budget and
// its overage contributes to billable regardless of what other grants have
// left — "combined-pool" was rejected precisely because it silently masked
// overage across parallel budgets.
//
// Two lanes, mutually exclusive per line item (grant_measure is per-EC and
// EC-validated to stay consistent across all grants on a feature):
//
//   - Quantity lane: adjusted_qty = Σ max(0, grant.usage − grant.quota) across
//     the cycle-overlapping grants. Returned as AdjustedQty for the pricer to
//     apply commit / tier / true-up on top of. `matchingCharge.Amount` is
//     recomputed from this qty against the line item's price.
//
//   - Amount lane: overage_amount = Σ max(0, grant.usage − grant.quota) in
//     currency units. Overage is already priced (amount grants are validated
//     as flat/linear at EC-write time, so the multiplication happened when the
//     grant refreshed its snapshot). Returned as OverageAmount, which the
//     caller writes into matchingCharge.Amount and zeros the qty out — the
//     pricer sees no work to do for this line item.
//
// Runtime guard: if the caller passes an amount-lane line item whose price
// carries commit / tier / true-up (should be rejected by
// validateEntitlementGrantShape at EC-write time), we log a warning and
// return `applied=false` so the caller falls through to the legacy
// entitlement adjustment rather than silently mis-charge.
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

	// All grants on a feature share a measure (per EC validation). If a
	// mismatch shows up at runtime (schema drift, admin surgery), fall
	// through to the legacy path — safer than picking one measure and
	// silently ignoring the other set.
	for _, g := range grants[1:] {
		if g.Measure != measure {
			s.Logger.Warn(ctx, "entitlement grant overage: mixed measures on same feature, skipping grants",
				"meter_id", item.MeterID,
				"line_item_id", item.ID)
			return adjustMeterUsageGrantsResult{}, false
		}
	}

	// Amount-lane guard: reject if the line item's pricing has commit / tier /
	// true-up. Amount grants aren't safe on top of these because the pricer
	// needs full-cycle scope to be correct (ERD §8.6). Config validation
	// should have kept this from happening; belt-and-braces.
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

	// Update matchingCharge in place so the caller's downstream reads
	// (line_item_amount, quantity, is_overage) reflect the grant math. This
	// mirrors what adjustMeterUsageEntitlement does today.
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
		// The pricer has nothing to do for amount-lane grants — the grant's
		// usage snapshot was already priced at refresh time. Zero the qty so
		// downstream aggregation (line item quantity) doesn't double-count.
		matchingCharge.Quantity = 0
	}
	return res, true
}

// amountLanePricingGuard defends adjustMeterUsageGrants against complex-priced
// line items that the EC-write-time validator was supposed to reject.
// Returns nil when the line item is safe for the amount lane, and an error
// naming the offending piece when it isn't.
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

// Sentinel-typed errors so the log line is grep-friendly and tests can assert
// on the exact reason without string-matching prose.
var (
	errAmountLaneCommitment = errAmountLaneReason("line item carries a commitment")
	errAmountLaneTrueUp     = errAmountLaneReason("line item enables true-up")
	errAmountLaneTiered     = errAmountLaneReason("price uses tiered billing")
)

type errAmountLaneReason string

func (e errAmountLaneReason) Error() string { return string(e) }
