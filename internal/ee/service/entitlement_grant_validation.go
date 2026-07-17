package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// validateEntitlementGrantShape enforces the ERD FLE-959 §7.1 EC-validation
// rules that depend on external state (meter + price), on top of the field
// coherence checks that entitlement.validateGrantConfig has already run.
//
// Rules enforced here:
//  1. grant_type=time_boxed is rejected for MAX or bucketed meters.
//  2. grant_measure=amount is rejected when any price on this meter under this
//     entity is TIERED (VOLUME or SLAB) — those need full-cycle pricing scope
//     that a per-grant window can't provide correctly in Phase 1.
//
// Subscription-line-item commitment / true-up (which lives on the sub rather
// than the plan-level price) is NOT checked here — those aren't visible at EC
// create/update time for plan/addon-scoped ECs. The billing integration
// (adjustMeterUsageGrants) is the natural place to guard that at runtime once
// M5 lands.
//
// Callers pass the already-fetched meter to avoid re-loading it; if e.GrantType
// is NONE (the legacy default) this returns nil immediately and the caller can
// skip the meter fetch entirely if it wants to.
func (s *entitlementService) validateEntitlementGrantShape(
	ctx context.Context,
	e *entitlement.Entitlement,
	m *meter.Meter,
) error {
	if e == nil {
		return nil
	}
	if e.GrantType == "" || e.GrantType == types.EntitlementGrantTypeNone {
		return nil
	}

	// From here we're on the time_boxed path. The domain-level Validate has
	// already asserted metered feature + measure + duration + quota.

	if m == nil {
		return ierr.NewError("meter is required to validate time-boxed entitlement grants").
			WithHint("This is an internal bug — the caller must pass the meter for metered features").
			Mark(ierr.ErrValidation)
	}

	// Rule 1: no MAX / bucketed meters. Bucketed meters (either SUM or MAX with
	// bucket_size set) evaluate per-bucket and have no clean per-grant-window
	// semantic. MAX meters track a peak — a "5-hour window" of a peak metric
	// doesn't map to a quota that makes sense.
	if m.Aggregation.Type == types.AggregationMax {
		return ierr.NewError("time-boxed grants are not supported for MAX meters").
			WithHint("MAX aggregation tracks a peak value, not additive usage — grants need additive semantics").
			WithReportableDetails(map[string]interface{}{
				"meter_id":         m.ID,
				"aggregation_type": m.Aggregation.Type,
			}).
			Mark(ierr.ErrValidation)
	}
	if m.Aggregation.BucketSize != "" {
		return ierr.NewError("time-boxed grants are not supported for bucketed meters").
			WithHint("Bucketed meters aggregate per bucket independently — a grant window would slice across buckets ambiguously").
			WithReportableDetails(map[string]interface{}{
				"meter_id":    m.ID,
				"bucket_size": m.Aggregation.BucketSize,
			}).
			Mark(ierr.ErrValidation)
	}

	// Rule 2: amount lane only supports linear per-unit pricing. Tiered pricing
	// requires full-cycle scope (tier boundaries walk with cumulative qty in
	// the cycle, not the grant window), so we reject at EC-create time.
	//
	// Line-item commitment lives on the subscription line, not the price row,
	// so it can't be checked at plan-scoped EC creation. adjustMeterUsageGrants
	// (M5) is the natural place to guard that at runtime.
	if e.GrantMeasure != types.EntitlementGrantMeasureAmount {
		return nil
	}
	priceFilter := types.NewNoLimitPriceFilter()
	priceFilter.MeterIDs = []string{m.ID}
	prices, err := s.PriceRepo.List(ctx, priceFilter)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to look up prices for entitlement grant validation").
			WithReportableDetails(map[string]interface{}{"meter_id": m.ID}).
			Mark(ierr.ErrDatabase)
	}
	for _, p := range prices {
		if p.BillingModel == types.BILLING_MODEL_TIERED {
			return ierr.NewError("amount-based grants are not supported on tiered pricing in Phase 1").
				WithHint(fmt.Sprintf(
					"Price %s uses tiered billing (%s); amount grants require linear per-unit pricing. Use a quantity-based grant instead, or a flat-fee price.",
					p.ID, p.TierMode)).
				WithReportableDetails(map[string]interface{}{
					"price_id":      p.ID,
					"billing_model": p.BillingModel,
					"tier_mode":     p.TierMode,
					"meter_id":      m.ID,
				}).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}
