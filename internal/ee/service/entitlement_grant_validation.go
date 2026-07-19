package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// validateEntitlementGrantShape enforces context-dependent EC rules that need
// the meter (and its prices) — layered on top of domain-level coherence checks.
// Returns nil for grant_type=none (the legacy default).
//
// Rules:
//   1. time_boxed rejects MAX and bucketed meters.
//   2. amount measure rejects tiered prices on the meter.
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

	if m == nil {
		return ierr.NewError("meter is required to validate time-boxed entitlement grants").
			Mark(ierr.ErrValidation)
	}

	if m.Aggregation.Type == types.AggregationMax {
		return ierr.NewError("time-boxed grants are not supported for MAX meters").
			WithReportableDetails(map[string]interface{}{
				"meter_id":         m.ID,
				"aggregation_type": m.Aggregation.Type,
			}).
			Mark(ierr.ErrValidation)
	}
	if m.Aggregation.BucketSize != "" {
		return ierr.NewError("time-boxed grants are not supported for bucketed meters").
			WithReportableDetails(map[string]interface{}{
				"meter_id":    m.ID,
				"bucket_size": m.Aggregation.BucketSize,
			}).
			Mark(ierr.ErrValidation)
	}

	if e.GrantMeasure != types.EntitlementGrantMeasureAmount {
		return nil
	}
	priceFilter := types.NewNoLimitPriceFilter()
	priceFilter.MeterIDs = []string{m.ID}
	prices, err := s.PriceRepo.List(ctx, priceFilter)
	if err != nil {
		return ierr.WithError(err).
			WithReportableDetails(map[string]interface{}{"meter_id": m.ID}).
			Mark(ierr.ErrDatabase)
	}
	for _, p := range prices {
		if p.BillingModel == types.BILLING_MODEL_TIERED {
			return ierr.NewError("amount-based grants are not supported on tiered pricing").
				WithHint(fmt.Sprintf(
					"Price %s uses tiered billing (%s); use a quantity-based grant or a flat-fee price.",
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
