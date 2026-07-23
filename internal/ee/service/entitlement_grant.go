package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// EntitlementGrantService owns the lifecycle of entitlement_grants rows.
type EntitlementGrantService interface {
	EnsureGrants(ctx context.Context, cust *customer.Customer, at time.Time) ([]*entitlementgrant.EntitlementGrant, *grantEvalMeta, error)

	// EnsureGrantsForSubscriptions is the data-fed variant: the caller supplies
	// the customer's active subscriptions so no extra DB fetch happens for them.
	// The returned meta carries the lookups (features, meters, external ids)
	// built during the pass so the evaluator can reuse them.
	EnsureGrantsForSubscriptions(ctx context.Context, cust *customer.Customer, subs []*subscription.Subscription, at time.Time) ([]*entitlementgrant.EntitlementGrant, *grantEvalMeta, error)
}

type entitlementGrantService struct {
	ServiceParams
}

func NewEntitlementGrantService(params ServiceParams) EntitlementGrantService {
	return &entitlementGrantService{ServiceParams: params}
}

// grantEvalMeta is the per-pass lookup bundle shared by grant opening and the
// evaluator's usage-refresh loop, so each entity is fetched at most once.
type grantEvalMeta struct {
	subsByID    map[string]*subscription.Subscription
	featureByID map[string]*feature.Feature
	meterByID   map[string]*meter.Meter

	// extIDsBySub memoizes ExternalCustomerIDsForSubscription — resolved lazily
	// so customers with no grant work pay nothing.
	extIDsBySub     map[string][]string
	subscriptionSvc SubscriptionService
}

func (m *grantEvalMeta) externalIDs(ctx context.Context, sub *subscription.Subscription) ([]string, error) {
	if ids, ok := m.extIDsBySub[sub.ID]; ok {
		return ids, nil
	}
	ids, err := m.subscriptionSvc.ExternalCustomerIDsForSubscription(ctx, sub)
	if err != nil {
		return nil, err
	}
	m.extIDsBySub[sub.ID] = ids
	return ids, nil
}

// -----------------------------------------------------------------------------
// EnsureGrants
// -----------------------------------------------------------------------------

func (s *entitlementGrantService) EnsureGrants(
	ctx context.Context,
	cust *customer.Customer,
	at time.Time,
) ([]*entitlementgrant.EntitlementGrant, *grantEvalMeta, error) {
	if cust == nil {
		return nil, nil, ierr.NewError("customer is required").Mark(ierr.ErrValidation)
	}
	subs, err := s.listActiveSubscriptions(ctx, cust.ID, at)
	if err != nil {
		return nil, nil, err
	}
	grants, meta, err := s.EnsureGrantsForSubscriptions(ctx, cust, subs, at)
	return grants, meta, err
}

func (s *entitlementGrantService) EnsureGrantsForSubscriptions(
	ctx context.Context,
	cust *customer.Customer,
	subs []*subscription.Subscription,
	at time.Time,
) ([]*entitlementgrant.EntitlementGrant, *grantEvalMeta, error) {
	if cust == nil {
		return nil, nil, ierr.NewError("customer is required").Mark(ierr.ErrValidation)
	}
	if len(subs) == 0 {
		return nil, nil, nil
	}

	subscriptionSvc := NewSubscriptionService(s.ServiceParams)
	ecsBySub := make(map[string][]*entitlement.Entitlement, len(subs))
	for _, sub := range subs {
		ents, err := subscriptionSvc.GetSubscriptionEntitlements(ctx, sub.ID)
		if err != nil {
			return nil, nil, err
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

	// Two constant-size reads regardless of how many windows the cycle has
	// accumulated: (1) max(valid_to) per slot — occupancy plus the covered-until
	// bound for the next window; (2) the working set — open windows plus closed
	// ones whose snapshot predates the close (they owe one final refresh so
	// tail events reach the usage billing reads).
	minCycleStart := subs[0].CurrentPeriodStart
	for _, sub := range subs[1:] {
		if sub.CurrentPeriodStart.Before(minCycleStart) {
			minCycleStart = sub.CurrentPeriodStart
		}
	}
	slotEnds, err := s.EntitlementGrantRepo.LatestWindowEndBySlot(ctx, cust.ID, minCycleStart)
	if err != nil {
		return nil, nil, err
	}
	latestEndBySlot := make(map[string]time.Time, len(slotEnds))
	for _, se := range slotEnds {
		latestEndBySlot[grantSlotKey(se.EntitlementConfigID, se.SubscriptionID)] = se.ValidTo
	}

	rows, err := s.EntitlementGrantRepo.ListOpenOrUnfinalized(ctx, cust.ID, at, minCycleStart)
	if err != nil {
		return nil, nil, err
	}
	live := make([]*entitlementgrant.EntitlementGrant, 0, len(rows))
	finalize := make([]*entitlementgrant.EntitlementGrant, 0)
	for _, g := range rows {
		if g.ValidTo.After(at) {
			live = append(live, g)
		} else {
			finalize = append(finalize, g)
		}
	}

	meta, err := s.buildGrantEvalMeta(ctx, subs, ecsBySub, rows, subscriptionSvc)
	if err != nil {
		return nil, nil, err
	}

	opened, err := s.openMissingGrants(ctx, subs, ecsBySub, latestEndBySlot, meta, at)
	if err != nil {
		return nil, nil, err
	}

	out := append(append(live, opened...), finalize...)
	if len(out) == 0 {
		return nil, nil, nil // nothing to evaluate — nil meta signals the evaluator to skip
	}
	return out, meta, nil
}

// buildGrantEvalMeta loads features and meters for every grant EC and existing
// grant in one query each. External customer ids resolve lazily on first use.
func (s *entitlementGrantService) buildGrantEvalMeta(
	ctx context.Context,
	subs []*subscription.Subscription,
	ecsBySub map[string][]*entitlement.Entitlement,
	grants []*entitlementgrant.EntitlementGrant,
	subscriptionSvc SubscriptionService,
) (*grantEvalMeta, error) {
	meta := &grantEvalMeta{
		subsByID:        lo.KeyBy(subs, func(sub *subscription.Subscription) string { return sub.ID }),
		featureByID:     map[string]*feature.Feature{},
		meterByID:       map[string]*meter.Meter{},
		extIDsBySub:     map[string][]string{},
		subscriptionSvc: subscriptionSvc,
	}

	featureIDs := make([]string, 0)
	for _, ecs := range ecsBySub {
		for _, ec := range ecs {
			if ec.HasGrantConfig() {
				featureIDs = append(featureIDs, ec.FeatureID)
			}
		}
	}
	for _, g := range grants {
		if g.IsFeatureScoped() {
			featureIDs = append(featureIDs, g.ScopeEntityID)
		}
	}
	featureIDs = lo.Uniq(featureIDs)
	if len(featureIDs) == 0 {
		return meta, nil
	}

	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.FeatureIDs = featureIDs
	features, err := s.FeatureRepo.List(ctx, featureFilter)
	if err != nil {
		return nil, err
	}
	meta.featureByID = lo.KeyBy(features, func(f *feature.Feature) string { return f.ID })

	meterIDs := lo.Uniq(lo.FilterMap(features, func(f *feature.Feature, _ int) (string, bool) {
		return f.MeterID, f.MeterID != ""
	}))
	if len(meterIDs) > 0 {
		meterFilter := types.NewNoLimitMeterFilter()
		meterFilter.MeterIDs = meterIDs
		meters, err := s.MeterRepo.List(ctx, meterFilter)
		if err != nil {
			return nil, err
		}
		meta.meterByID = lo.KeyBy(meters, func(m *meter.Meter) string { return m.ID })
	}
	return meta, nil
}

// grantSlotKey identifies the grant slot: one open window at a time per
// (config, subscription) per customer.
func grantSlotKey(entitlementConfigID, subscriptionID string) string {
	return entitlementConfigID + "/" + subscriptionID
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
// immutable, so config changes take effect when the open window ends.
func (s *entitlementGrantService) openMissingGrants(
	ctx context.Context,
	subs []*subscription.Subscription,
	ecsBySub map[string][]*entitlement.Entitlement,
	latestEndBySlot map[string]time.Time,
	meta *grantEvalMeta,
	at time.Time,
) ([]*entitlementgrant.EntitlementGrant, error) {
	opened := make([]*entitlementgrant.EntitlementGrant, 0)
	for _, sub := range subs {
		for _, featureECs := range s.eligibleGrantConfigsByFeature(ctx, sub, ecsBySub[sub.ID]) {
			for _, candidate := range grantCandidatesForFeature(featureECs) {
				g, err := s.openIfSlotFree(ctx, sub, candidate, latestEndBySlot, meta, at)
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

// eligibleGrantConfigsByFeature returns the sub's grant-config ECs grouped by
// feature, skipping invalid durations and durations >= cycle length (a
// cycle-long grant is just the cycle quota — usage_reset_period's job).
func (s *entitlementGrantService) eligibleGrantConfigsByFeature(
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

// grantCandidatesForFeature: parallel → one candidate per EC; additive → one
// candidate on the primary (lowest-ID) EC with the summed quota.
func grantCandidatesForFeature(featureECs []*entitlement.Entitlement) []grantCandidate {
	parallel := lo.SomeBy(featureECs, func(ec *entitlement.Entitlement) bool {
		return ec.AggregationMode == types.EntitlementAggregationModeParallel
	})
	if parallel {
		return lo.Map(featureECs, func(ec *entitlement.Entitlement, _ int) grantCandidate {
			return grantCandidate{ec: ec, quota: lo.FromPtr(ec.GrantQuota)}
		})
	}

	primary := featureECs[0]
	total := decimal.Zero
	for _, ec := range featureECs {
		if ec.ID < primary.ID {
			primary = ec
		}
		total = total.Add(lo.FromPtr(ec.GrantQuota))
	}
	return []grantCandidate{{ec: primary, quota: total}}
}

// openIfSlotFree opens the candidate's grant unless the slot's latest window
// is still open at `at`; a freshly opened grant claims the slot.
func (s *entitlementGrantService) openIfSlotFree(
	ctx context.Context,
	sub *subscription.Subscription,
	candidate grantCandidate,
	latestEndBySlot map[string]time.Time,
	meta *grantEvalMeta,
	at time.Time,
) (*entitlementgrant.EntitlementGrant, error) {
	slot := grantSlotKey(candidate.ec.ID, sub.ID)
	lastEnd := latestEndBySlot[slot]
	if lastEnd.After(at) {
		return nil, nil
	}

	g, err := s.openOneGrant(ctx, sub, candidate.ec, lastEnd, meta, at, candidate.quota)
	if err != nil || g == nil {
		return nil, err
	}

	latestEndBySlot[slot] = g.ValidTo
	return g, nil
}

// openOneGrant inserts the slot's next grant. On INSERT conflict — valid_from
// is deterministic, so two racers collide on the unique (slot, valid_from)
// index — it re-reads the winner. Returns nil when there is no window to open.
func (s *entitlementGrantService) openOneGrant(
	ctx context.Context,
	sub *subscription.Subscription,
	ec *entitlement.Entitlement,
	lastWindowEnd time.Time,
	meta *grantEvalMeta,
	at time.Time,
	quota decimal.Decimal,
) (*entitlementgrant.EntitlementGrant, error) {
	dur, err := ec.GrantDuration()
	if err != nil {
		return nil, err
	}

	validFrom, validTo, ok, err := s.computeGrantWindow(ctx, ec, sub, meta, lastWindowEnd, at, dur)
	if err != nil {
		return nil, err
	}
	if !ok {
		s.Logger.Debug(ctx, "no uncovered usage, no grant to open",
			"entitlement_config_id", ec.ID,
			"subscription_id", sub.ID,
			"customer_id", sub.CustomerID)
		return nil, nil
	}

	newGrant := entitlementgrant.NewEntitlementGrantBuilder(nil).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT_GRANT)).
		WithEntitlementConfigID(ec.ID).
		WithCustomerID(sub.CustomerID).
		WithSubscriptionID(sub.ID).
		WithScope(types.EntitlementGrantScopeFeature, ec.FeatureID).
		WithMeasure(ec.GrantMeasure).
		WithQuota(quota).
		WithWindow(validFrom, validTo).
		WithGrantStatus(types.EntitlementGrantStatusActive).
		WithEnvironmentID(types.GetEnvironmentID(ctx)).
		WithBaseModel(types.GetDefaultBaseModel(ctx)).
		Build()
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
	// Lost the race; re-read the winner so callers see a consistent set.
	return s.EntitlementGrantRepo.FindLastBySlot(ctx, ec.ID, sub.CustomerID, sub.ID)
}

// computeGrantWindow derives [valid_from, valid_to): the window opens at the
// first usage event past the covered range; no uncovered usage → no window.
// At the cycle boundary the window becomes the cycle's last `dur` — backdating
// the start is safe because [coveredUntil, firstUncoveredEventAt) is event-free by
// definition — and it stretches to cycle_end rather than leave a sub-1h stub.
func (s *entitlementGrantService) computeGrantWindow(
	ctx context.Context,
	ec *entitlement.Entitlement,
	sub *subscription.Subscription,
	meta *grantEvalMeta,
	lastWindowEnd time.Time,
	at time.Time,
	dur time.Duration,
) (time.Time, time.Time, bool, error) {
	cycleStart := sub.CurrentPeriodStart
	cycleEnd := sub.CurrentPeriodEnd
	coveredUntil := latestOf(lastWindowEnd, cycleStart)

	// The clamp to cycle_end keeps next-cycle events (sub object not yet rolled)
	// out; an empty/inverted range simply finds nothing.
	firstUncoveredEventAt, err := s.earliestUncoveredUsage(ctx, meta, sub, ec, coveredUntil, earliestOf(at, cycleEnd))
	if err != nil || firstUncoveredEventAt == nil {
		return time.Time{}, time.Time{}, false, err
	}

	validFrom := *firstUncoveredEventAt
	if cycleEnd.Sub(validFrom) < dur {
		validFrom = latestOf(coveredUntil, cycleEnd.Add(-dur))
	}

	validTo := validFrom.Add(dur)
	if cycleEnd.Sub(validTo) < types.EntitlementGrantMinDuration {
		validTo = cycleEnd
	}
	return validFrom, validTo, true, nil
}

// earliestUncoveredUsage returns the first event timestamp in
// [coveredUntil, until) for the EC's meter across the subscription's
// customers, or nil when none.
func (s *entitlementGrantService) earliestUncoveredUsage(
	ctx context.Context,
	meta *grantEvalMeta,
	sub *subscription.Subscription,
	ec *entitlement.Entitlement,
	coveredUntil, until time.Time,
) (*time.Time, error) {
	f := meta.featureByID[ec.FeatureID]
	if f == nil || f.MeterID == "" {
		return nil, nil
	}
	extIDs, err := meta.externalIDs(ctx, sub)
	if err != nil {
		return nil, err
	}
	return s.MeterUsageRepo.GetEarliestUsageTimestamp(ctx, &events.MeterUsageQueryParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: extIDs,
		MeterID:             f.MeterID,
		StartTime:           coveredUntil,
		EndTime:             until,
	})
}

func latestOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earliestOf(a, b time.Time) time.Time {
	if a.Before(b) {
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
