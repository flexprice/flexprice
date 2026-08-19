package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// Bucketed meters price each window independently, so a quota expressed against
// the aggregate has no single window to draw down. That is why entitlements are
// rejected on bucketed MAX meters and grant-based entitlements on any bucketed
// meter.
//
// With bucketing on the price, neither entity can answer the question alone. The
// join is reachable from both directions — price -> meter -> feature ->
// entitlement — so the constraint is enforced at both write points. Either check
// alone leaks: a price created first is missed by the entitlement check, an
// entitlement created first is missed by the price check.

// isLinearBillingModel reports whether cost is a plain linear function of
// quantity, i.e. cost(a+b) == cost(a)+cost(b). Only FLAT_FEE satisfies that.
//
// It matters because the entitlement adjustment path re-prices on the aggregate
// (billing_meter_usage.go, adjustMeterUsageEntitlement) and discards the
// per-bucket cost. Under a linear model the two agree, so the discard is
// harmless. Under TIERED or PACKAGE they do not: the tier allowance is granted
// once per bucket by the bucketed path and once for the whole period by the
// aggregate path, so applying an entitlement can move the bill in either
// direction. See docs/design/2026-08-17-price-level-bucketing.md section 9.
func isLinearBillingModel(m types.BillingModel) bool {
	return m == types.BILLING_MODEL_FLAT_FEE
}

// entitlementsForMeter returns the entitlements attached to the features backed
// by the given meter. Empty meter ID, no features, or no entitlements all yield
// an empty slice rather than an error.
func entitlementsForMeter(ctx context.Context, p ServiceParams, meterID string) ([]*entitlement.Entitlement, error) {
	// Partially-wired ServiceParams (unit tests, narrow constructors) leave these
	// repositories nil. The guard is a cross-entity check, so it can only run
	// when both sides are reachable.
	if meterID == "" || p.FeatureRepo == nil || p.EntitlementRepo == nil {
		return nil, nil
	}

	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.MeterIDs = []string{meterID}
	features, err := p.FeatureRepo.List(ctx, featureFilter)
	if err != nil {
		return nil, err
	}
	if len(features) == 0 {
		return nil, nil
	}

	featureIDs := lo.Map(features, func(f *feature.Feature, _ int) string { return f.ID })

	entFilter := types.NewNoLimitEntitlementFilter()
	entFilter.FeatureIDs = featureIDs
	return p.EntitlementRepo.List(ctx, entFilter)
}

// validateBucketedPriceAgainstEntitlements rejects a bucketed price when the
// meter's feature already carries an entitlement that cannot coexist with
// windowed pricing. Called on price create.
func validateBucketedPriceAgainstEntitlements(ctx context.Context, p ServiceParams, m *meter.Meter, bucketSize types.WindowSize, billingModel types.BillingModel) error {
	if bucketSize == "" || m == nil {
		return nil
	}

	ents, err := entitlementsForMeter(ctx, p, m.ID)
	if err != nil {
		return err
	}

	isMax := m.Aggregation.Type == types.AggregationMax
	for _, e := range ents {
		if e == nil {
			continue
		}
		if e.HasGrantConfig() {
			return ierr.NewError("grant-based entitlements are not supported for bucketed meters").
				WithHint("Remove the grant-based entitlement on this feature before pricing it per window").
				WithReportableDetails(map[string]interface{}{
					"meter_id":       m.ID,
					"entitlement_id": e.ID,
					"bucket_size":    bucketSize,
				}).
				Mark(ierr.ErrValidation)
		}
		if !isLinearBillingModel(billingModel) {
			return ierr.NewError("entitlements are not supported for bucketed non-linear pricing").
				WithHint("Applying an entitlement re-prices on the period total, which grants a tiered or packaged allowance once instead of once per window. Remove the entitlement, or use a flat-fee price.").
				WithReportableDetails(map[string]interface{}{
					"meter_id":       m.ID,
					"entitlement_id": e.ID,
					"bucket_size":    bucketSize,
					"billing_model":  billingModel,
				}).
				Mark(ierr.ErrValidation)
		}
		if isMax {
			return ierr.NewError("entitlements are not supported for bucketed max meters").
				WithHint("Bucketed max pricing charges each window independently, so a feature entitlement has no single window to draw down. Remove the entitlement before pricing this meter per window.").
				WithReportableDetails(map[string]interface{}{
					"meter_id":       m.ID,
					"entitlement_id": e.ID,
					"bucket_size":    bucketSize,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// validateEntitlementAgainstBucketedPrices is the reverse direction: reject an
// entitlement when a live price on the feature's meter is already bucketed.
// grantBased distinguishes the two rules — grants are rejected for any bucketed
// meter, plain entitlements only for MAX.
func validateEntitlementAgainstBucketedPrices(ctx context.Context, p ServiceParams, m *meter.Meter, grantBased bool) error {
	if m == nil || m.ID == "" || p.PriceRepo == nil {
		return nil
	}
	// A legacy meter-level bucket size is partly covered by the meter-level checks
	// (entitlement.go rejects bucketed MAX), but those predate the non-linear rule.
	// Do not early-return: a legacy bucketed SUM meter priced TIERED/PACKAGE must
	// still be rejected, and the loop below evaluates each live price's model.

	// Only currently-effective prices can block an entitlement. A superseded
	// version that happened to be bucketed must not: the repo already excludes
	// end-dated and non-published rows, but state it explicitly here so the
	// guard cannot silently widen if that default ever changes.
	priceFilter := types.NewNoLimitPriceFilter()
	priceFilter.MeterIDs = []string{m.ID}
	priceFilter.AllowExpiredPrices = false
	priceFilter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	prices, err := p.PriceRepo.List(ctx, priceFilter)
	if err != nil {
		return err
	}

	isMax := m.Aggregation.Type == types.AggregationMax
	for _, pr := range prices {
		if pr == nil || !price.IsBucketed(pr, m) {
			continue
		}
		if !grantBased && !isMax && isLinearBillingModel(pr.BillingModel) {
			continue
		}
		hint := "This meter is priced per window on at least one price. Remove the bucket size from that price before adding an entitlement."
		if grantBased {
			hint = "This meter is priced per window on at least one price. Grant-based entitlements cannot be used with windowed pricing."
		} else if !isLinearBillingModel(pr.BillingModel) {
			hint = "This meter is priced per window with tiered or packaged pricing. An entitlement would re-price on the period total and grant the allowance once instead of once per window."
		}
		return ierr.NewError("meter is priced per window").
			WithHint(hint).
			WithReportableDetails(map[string]interface{}{
				"meter_id":      m.ID,
				"price_id":      pr.ID,
				"bucket_size":   price.ResolveBucketSize(pr, m),
				"billing_model": pr.BillingModel,
				"grant_based":   grantBased,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}
