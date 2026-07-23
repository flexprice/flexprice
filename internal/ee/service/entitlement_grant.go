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
	// EnsureGrants opens a live grant per grant-config entitlement on the
	// customer's active subscriptions, expires stale rows, and returns the
	// hydrated live set. Idempotent per (config, customer) slot.
	EnsureGrants(ctx context.Context, cust *customer.Customer, at time.Time) ([]*entitlementgrant.EntitlementGrant, error)

	// EnsureGrantsForSubscriptions is the data-fed variant: the caller supplies
	// the customer's active subscriptions so no extra DB fetch happens for them.
	EnsureGrantsForSubscriptions(ctx context.Context, cust *customer.Customer, subs []*subscription.Subscription, at time.Time) ([]*entitlementgrant.EntitlementGrant, error)
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
	return s.EnsureGrantsForSubscriptions(ctx, cust, subs, at)
}

func (s *entitlementGrantService) EnsureGrantsForSubscriptions(
	ctx context.Context,
	cust *customer.Customer,
	subs []*subscription.Subscription,
	at time.Time,
) ([]*entitlementgrant.EntitlementGrant, error) {
	if cust == nil {
		return nil, ierr.NewError("customer is required").Mark(ierr.ErrValidation)
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

// grantCandidate is one grant to open: the EC whose slot it occupies and the
// quota it carries (the EC's own quota for parallel, the group sum for additive).
type grantCandidate struct {
	ec    *entitlement.Entitlement
	quota decimal.Decimal
}

// openMissingGrants opens missing grants per feature: parallel = one grant per
// EC; additive = one grant on the primary EC with quota = Σ quotas. Grants are
// immutable, so config changes take effect when the live grant expires.
func (s *entitlementGrantService) openMissingGrants(
	ctx context.Context,
	subs []*subscription.Subscription,
	ecsBySub map[string][]*entitlement.Entitlement,
	liveByConfigID map[string]*entitlementgrant.EntitlementGrant,
	at time.Time,
) ([]*entitlementgrant.EntitlementGrant, error) {
	opened := make([]*entitlementgrant.EntitlementGrant, 0)
	for _, sub := range subs {
		for _, group := range s.eligibleGrantGroups(ctx, sub, ecsBySub[sub.ID]) {
			for _, candidate := range grantCandidatesForGroup(group) {
				g, err := s.openIfSlotFree(ctx, sub, candidate, liveByConfigID, at)
				if err != nil {
					return nil, err
				}
				if g != nil {
					opened = append(opened, g)
				}
			}
		}
	}
	return opened, nil
}

// eligibleGrantGroups groups the sub's openable grant ECs by feature, skipping
// invalid durations and durations >= cycle length — a cycle-long grant is just
// the cycle quota, which usage_reset_period already expresses.
func (s *entitlementGrantService) eligibleGrantGroups(
	ctx context.Context,
	sub *subscription.Subscription,
	ecs []*entitlement.Entitlement,
) map[string][]*entitlement.Entitlement {
	cycleLen := sub.CurrentPeriodEnd.Sub(sub.CurrentPeriodStart)

	byFeature := make(map[string][]*entitlement.Entitlement)
	for _, ec := range ecs {
		if !ec.HasGrantConfig() {
			continue
		}
		dur, err := ec.GrantDuration()
		if err != nil {
			s.Logger.Error(ctx, "invalid grant duration on entitlement, skipping",
				"entitlement_id", ec.ID, "error", err)
			continue
		}
		if dur >= cycleLen {
			s.Logger.Debug(ctx, "grant duration >= subscription cycle, skipping grant open",
				"entitlement_id", ec.ID,
				"subscription_id", sub.ID,
				"grant_duration", dur.String(),
				"cycle_length", cycleLen.String())
			continue
		}
		byFeature[ec.FeatureID] = append(byFeature[ec.FeatureID], ec)
	}
	return byFeature
}

// grantCandidatesForGroup: parallel → one candidate per EC; additive → one
// candidate on the primary (lowest-ID) EC with the summed quota.
func grantCandidatesForGroup(group []*entitlement.Entitlement) []grantCandidate {
	parallel := lo.SomeBy(group, func(ec *entitlement.Entitlement) bool {
		return ec.AggregationMode == types.EntitlementAggregationModeParallel
	})
	if parallel {
		return lo.Map(group, func(ec *entitlement.Entitlement, _ int) grantCandidate {
			return grantCandidate{ec: ec, quota: lo.FromPtr(ec.GrantQuota)}
		})
	}

	primary := group[0]
	total := decimal.Zero
	for _, ec := range group {
		if ec.ID < primary.ID {
			primary = ec
		}
		total = total.Add(lo.FromPtr(ec.GrantQuota))
	}
	return []grantCandidate{{ec: primary, quota: total}}
}

// openIfSlotFree opens the candidate's grant unless its slot already holds a
// live one; a freshly opened grant claims the slot in liveByConfigID.
func (s *entitlementGrantService) openIfSlotFree(
	ctx context.Context,
	sub *subscription.Subscription,
	candidate grantCandidate,
	liveByConfigID map[string]*entitlementgrant.EntitlementGrant,
	at time.Time,
) (*entitlementgrant.EntitlementGrant, error) {
	if _, ok := liveByConfigID[candidate.ec.ID]; ok {
		return nil, nil
	}
	g, err := s.openOneGrant(ctx, sub, candidate.ec, at, candidate.quota)
	if err != nil || g == nil {
		return nil, err
	}
	liveByConfigID[candidate.ec.ID] = g
	return g, nil
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

// computeGrantWindow derives [valid_from, valid_to): butt-joints the previous
// grant's valid_to when one exists in this cycle (no coverage gap; CH recompute
// keeps late catch-up idempotent). Fresh slots start schedule_delay back — the
// debounce window is exactly how old the triggering event can be, so ingested
// events always land inside the first window. Capped at cycle end; ok=false under 1h.
func (s *entitlementGrantService) computeGrantWindow(
	ctx context.Context,
	ec *entitlement.Entitlement,
	sub *subscription.Subscription,
	at time.Time,
	dur time.Duration,
) (time.Time, time.Time, bool, error) {
	cycleStart := sub.CurrentPeriodStart
	cycleEnd := sub.CurrentPeriodEnd

	validFrom := latestOf(at.Add(-s.Config.UsageAlerts.ScheduleDelay), cycleStart)
	last, err := s.EntitlementGrantRepo.FindLastByConfigAndCustomer(ctx, ec.ID, sub.CustomerID)
	if err != nil {
		return time.Time{}, time.Time{}, false, err
	}
	if last != nil && !last.ValidTo.Before(cycleStart) && !last.ValidTo.After(cycleEnd) {
		validFrom = last.ValidTo
	}

	// Cycle-boundary cap: grants never straddle two cycles.
	validTo := validFrom.Add(dur)
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

// validateEntitlementGrantShape enforces grant-config rules that need the
// meter, its prices, and sibling ECs. No-op without a grant config. Rejections:
//   - MAX meters: a peak can't be decremented against a per-window quota.
//   - Bucketed meters: a grant window slices buckets ambiguously.
//   - Tiered prices on amount lane: tiers walk with cumulative cycle qty, not a window.
//   - Sibling coherence: one mode + one measure per feature; additive shares duration.
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
