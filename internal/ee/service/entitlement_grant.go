package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// EntitlementGrantService owns the lifecycle of entitlement_grants rows.
type EntitlementGrantService interface {
	// EnsureGrants opens a live grant per time-boxed entitlement config on the
	// customer's active subscriptions, expires stale rows, and returns the
	// hydrated live set. Idempotent per (config, customer) slot.
	EnsureGrants(ctx context.Context, cust *customer.Customer, at time.Time) ([]*entitlementgrant.EntitlementGrant, error)
}

type entitlementGrantService struct {
	ServiceParams
}

func NewEntitlementGrantService(params ServiceParams) EntitlementGrantService {
	return &entitlementGrantService{ServiceParams: params}
}

// -----------------------------------------------------------------------------
// EnsureGrants
// -----------------------------------------------------------------------------

func (s *entitlementGrantService) EnsureGrants(
	ctx context.Context,
	cust *customer.Customer,
	at time.Time,
) ([]*entitlementgrant.EntitlementGrant, error) {
	if cust == nil {
		return nil, ierr.NewError("customer is required").Mark(ierr.ErrValidation)
	}

	subs, err := s.listActiveSubscriptions(ctx, cust.ID, at)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, nil
	}

	subscriptionSvc := NewSubscriptionService(s.ServiceParams)
	ecsBySub := make(map[string][]*entitlement.Entitlement, len(subs))
	for _, sub := range subs {
		ents, err := subscriptionSvc.GetSubscriptionEntitlements(ctx, sub.ID)
		if err != nil {
			return nil, err
		}
		rows := make([]*entitlement.Entitlement, 0, len(ents))
		for _, e := range ents {
			if e == nil || e.Entitlement == nil {
				continue
			}
			rows = append(rows, e.Entitlement)
		}
		ecsBySub[sub.ID] = rows
	}

	// Read all live grants in one query keyed on customer_id (partial index).
	liveFilter := types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(cust.ID).
		WithLiveOnly(at)
	liveGrants, err := s.EntitlementGrantRepo.List(ctx, liveFilter)
	if err != nil {
		return nil, err
	}

	liveByConfigID := make(map[string]*entitlementgrant.EntitlementGrant, len(liveGrants))
	for _, g := range liveGrants {
		liveByConfigID[g.EntitlementConfigID] = g
	}

	opened, err := s.openMissingGrants(ctx, subs, ecsBySub, liveByConfigID, at)
	if err != nil {
		return nil, err
	}

	if len(opened) == 0 {
		return liveGrants, nil
	}
	return append(liveGrants, opened...), nil
}

func (s *entitlementGrantService) listActiveSubscriptions(
	ctx context.Context,
	customerID string,
	at time.Time,
) ([]*subscription.Subscription, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.CustomerID = customerID
	filter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusActive, types.SubscriptionStatusTrialing}
	filter.ActiveAt = &at
	return s.SubRepo.List(ctx, filter)
}

// openMissingGrants opens the grants each subscription's grant-config ECs call
// for, honoring the aggregation mode per feature:
//   - parallel: one grant per EC — each EC is its own independent bucket.
//   - additive: ONE grant per feature with quota = Σ EC quotas, opened on the
//     lowest-ID ("primary") EC's slot. One bucket downstream means evaluation,
//     exhaustion alerts, and billing overage all treat the group as a single
//     pool with no extra machinery.
//
// EC-set or mode changes mid-window take effect when the live grant expires —
// grant quota is immutable for the life of the grant.
func (s *entitlementGrantService) openMissingGrants(
	ctx context.Context,
	subs []*subscription.Subscription,
	ecsBySub map[string][]*entitlement.Entitlement,
	liveByConfigID map[string]*entitlementgrant.EntitlementGrant,
	at time.Time,
) ([]*entitlementgrant.EntitlementGrant, error) {
	opened := make([]*entitlementgrant.EntitlementGrant, 0)
	for _, sub := range subs {
		byFeature := make(map[string][]*entitlement.Entitlement)
		featureOrder := make([]string, 0)
		for _, ec := range ecsBySub[sub.ID] {
			if !ec.HasGrantConfig() {
				continue
			}
			if _, ok := byFeature[ec.FeatureID]; !ok {
				featureOrder = append(featureOrder, ec.FeatureID)
			}
			byFeature[ec.FeatureID] = append(byFeature[ec.FeatureID], ec)
		}

		for _, featureID := range featureOrder {
			group := byFeature[featureID]

			parallel := lo.SomeBy(group, func(ec *entitlement.Entitlement) bool {
				return ec.AggregationMode == types.EntitlementAggregationModeParallel
			})
			if parallel {
				for _, ec := range group {
					if _, ok := liveByConfigID[ec.ID]; ok {
						continue
					}
					g, err := s.openOneGrant(ctx, sub, ec, at, lo.FromPtr(ec.GrantQuota))
					if err != nil {
						return nil, err
					}
					if g == nil {
						continue
					}
					opened = append(opened, g)
					liveByConfigID[ec.ID] = g
				}
				continue
			}

			primary := group[0]
			total := decimal.Zero
			for _, ec := range group {
				if ec.ID < primary.ID {
					primary = ec
				}
				total = total.Add(lo.FromPtr(ec.GrantQuota))
			}
			if _, ok := liveByConfigID[primary.ID]; ok {
				continue
			}
			g, err := s.openOneGrant(ctx, sub, primary, at, total)
			if err != nil {
				return nil, err
			}
			if g == nil {
				continue
			}
			opened = append(opened, g)
			liveByConfigID[primary.ID] = g
		}
	}
	return opened, nil
}

// openOneGrant expires the stale slot and inserts a new grant. On INSERT conflict
// it re-reads the winner. Returns (nil, nil) when the window is < 1 hour.
func (s *entitlementGrantService) openOneGrant(
	ctx context.Context,
	sub *subscription.Subscription,
	ec *entitlement.Entitlement,
	at time.Time,
	quota decimal.Decimal,
) (*entitlementgrant.EntitlementGrant, error) {
	dur, err := ec.GrantDuration()
	if err != nil {
		return nil, err
	}

	validFrom, validTo, ok, err := s.computeGrantWindow(ctx, ec, sub, at, dur)
	if err != nil {
		return nil, err
	}
	if !ok {
		s.Logger.Debug(ctx, "skipping sub-1-hour trailing grant window",
			"entitlement_config_id", ec.ID,
			"subscription_id", sub.ID,
			"customer_id", sub.CustomerID,
			"valid_from", validFrom,
			"valid_to", validTo)
		return nil, nil
	}

	if _, err := s.EntitlementGrantRepo.ExpireLiveByConfigAndCustomer(ctx, ec.ID, sub.CustomerID, at); err != nil {
		return nil, err
	}

	newGrant := &entitlementgrant.EntitlementGrant{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT_GRANT),
		EntitlementConfigID: ec.ID,
		CustomerID:          sub.CustomerID,
		SubscriptionID:      sub.ID,
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       ec.FeatureID,
		Measure:             ec.GrantMeasure,
		Quota:               quota,
		ValidFrom:           validFrom,
		ValidTo:             validTo,
		GrantStatus:         types.EntitlementGrantStatusActive,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	if err := newGrant.Validate(); err != nil {
		return nil, err
	}

	created, err := s.EntitlementGrantRepo.Create(ctx, newGrant)
	if err == nil {
		return created, nil
	}
	if !ierr.IsAlreadyExists(err) {
		return nil, err
	}
	// Lost the race; re-read the winner so callers see a consistent live set.
	winner, wErr := s.EntitlementGrantRepo.FindLiveByConfigAndCustomer(ctx, ec.ID, sub.CustomerID)
	if wErr != nil {
		return nil, wErr
	}
	return winner, nil
}

// computeGrantWindow derives [valid_from, valid_to) for a new grant:
// anchor on previous valid_to inside the cycle, drift-guard 5min, cap to cycle_end.
// Returns ok=false if the window would be under 1 hour.
func (s *entitlementGrantService) computeGrantWindow(
	ctx context.Context,
	ec *entitlement.Entitlement,
	sub *subscription.Subscription,
	at time.Time,
	dur time.Duration,
) (time.Time, time.Time, bool, error) {
	cycleStart := sub.CurrentPeriodStart
	cycleEnd := sub.CurrentPeriodEnd

	validFrom := latestOf(at, cycleStart)
	last, err := s.EntitlementGrantRepo.FindLastByConfigAndCustomer(ctx, ec.ID, sub.CustomerID)
	if err != nil {
		return time.Time{}, time.Time{}, false, err
	}
	if last != nil && !last.ValidTo.Before(cycleStart) && !last.ValidTo.After(cycleEnd) {
		validFrom = last.ValidTo
	}

	// Drift guard: stale triggers can't credit unpriced historical usage.
	driftCap := at.Add(-5 * time.Minute)
	if validFrom.Before(driftCap) {
		validFrom = driftCap
	}

	// Cycle-boundary cap: grants never straddle two cycles.
	proposedValidTo := validFrom.Add(dur)
	validTo := proposedValidTo
	if validTo.After(cycleEnd) {
		validTo = cycleEnd
	}

	if validTo.Sub(validFrom) < types.EntitlementGrantMinDuration {
		return validFrom, validTo, false, nil
	}
	return validFrom, validTo, true, nil
}

func latestOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// validateEntitlementGrantShape enforces context-dependent grant-config rules
// that need the meter, its prices, and sibling ECs. No-op without a grant config.
//
// Rejection rationale (see docs/design/2026-07-08-FLE-959-Entitlements-Revamp.md §7.1):
//   - MAX aggregation tracks a peak, not additive consumption. A time-boxed
//     grant models "you get N units in this window" — the peak metric can't be
//     decremented against a quota without breaking semantics.
//   - Bucketed meters (bucket_size set) aggregate per bucket independently. A
//     grant window slicing across bucket boundaries would produce ambiguous
//     quota accounting.
//   - Tiered pricing on the amount lane: tier boundaries walk with cumulative
//     cycle quantity. A grant only knows its own window's qty, so it can't
//     price against the right tier. Blocked here so admins get a clean error
//     at EC-write time rather than a silent mispricing at billing time.
//   - Cross-EC coherence: all grant ECs on one feature must share the
//     aggregation mode and measure (billing folds per feature and can't mix),
//     and additive groups must share duration (their quotas sum into one window).
func (s *entitlementService) validateEntitlementGrantShape(
	ctx context.Context,
	e *entitlement.Entitlement,
	m *meter.Meter,
) error {
	if e == nil || !e.HasGrantConfig() {
		return nil
	}

	if m == nil {
		return ierr.NewError("meter is required to validate grant-based entitlements").
			Mark(ierr.ErrValidation)
	}

	if m.Aggregation.Type == types.AggregationMax {
		return ierr.NewError("grant-based entitlements are not supported for MAX meters").
			WithReportableDetails(map[string]interface{}{
				"meter_id":         m.ID,
				"aggregation_type": m.Aggregation.Type,
			}).
			Mark(ierr.ErrValidation)
	}
	if m.Aggregation.BucketSize != "" {
		return ierr.NewError("grant-based entitlements are not supported for bucketed meters").
			WithReportableDetails(map[string]interface{}{
				"meter_id":    m.ID,
				"bucket_size": m.Aggregation.BucketSize,
			}).
			Mark(ierr.ErrValidation)
	}

	if err := s.validateGrantSiblingCoherence(ctx, e); err != nil {
		return err
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

// validateGrantSiblingCoherence keeps all grant ECs on a feature mutually
// consistent: one aggregation mode, one measure, and for additive groups one
// duration (their quotas sum into a single window).
func (s *entitlementService) validateGrantSiblingCoherence(ctx context.Context, e *entitlement.Entitlement) error {
	filter := types.NewNoLimitEntitlementFilter()
	filter.FeatureIDs = []string{e.FeatureID}
	filter.HasGrantConfig = lo.ToPtr(true)
	siblings, err := s.EntitlementRepo.List(ctx, filter)
	if err != nil {
		return ierr.WithError(err).
			WithReportableDetails(map[string]interface{}{"feature_id": e.FeatureID}).
			Mark(ierr.ErrDatabase)
	}

	mode := defaultedMode(e.AggregationMode)
	for _, sib := range siblings {
		if sib.ID == e.ID {
			continue
		}
		if defaultedMode(sib.AggregationMode) != mode {
			return ierr.NewError("aggregation_mode must match the other entitlements on this feature").
				WithHint("A feature's entitlements are either all additive or all parallel").
				WithReportableDetails(map[string]interface{}{
					"feature_id":     e.FeatureID,
					"entitlement_id": sib.ID,
					"existing_mode":  defaultedMode(sib.AggregationMode),
					"requested_mode": mode,
				}).
				Mark(ierr.ErrValidation)
		}
		if sib.GrantMeasure != e.GrantMeasure {
			return ierr.NewError("grant_measure must match the other entitlements on this feature").
				WithReportableDetails(map[string]interface{}{
					"feature_id":       e.FeatureID,
					"entitlement_id":   sib.ID,
					"existing_measure": sib.GrantMeasure,
					"requested":        e.GrantMeasure,
				}).
				Mark(ierr.ErrValidation)
		}
		if mode == types.EntitlementAggregationModeAdditive {
			if lo.FromPtr(sib.GrantDurationValue) != lo.FromPtr(e.GrantDurationValue) ||
				sib.GrantDurationUnit != e.GrantDurationUnit {
				return ierr.NewError("additive entitlements on one feature must share grant_duration").
					WithHint("Additive quotas sum into one window; use parallel for independent windows").
					WithReportableDetails(map[string]interface{}{
						"feature_id":     e.FeatureID,
						"entitlement_id": sib.ID,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	}
	return nil
}

func defaultedMode(m types.EntitlementAggregationMode) types.EntitlementAggregationMode {
	if m == "" {
		return types.EntitlementAggregationModeAdditive
	}
	return m
}
