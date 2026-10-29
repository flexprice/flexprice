package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/meter"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

var (
	errSubGrantCommitment       = grantGuardError("subscription has a minimum spend")
	errSubGrantCadence          = grantGuardError("usage line is billed at a different cadence than the subscription")
	errSubGrantWithFeatureGrant = grantGuardError("subscription also has feature entitlement grants")
	errSubGrantNeverReset       = grantGuardError("usage line's meter never resets usage")
)

// subscriptionEntitlementGrantState is what decides whether a subscription can carry a subscription entitlement grant.
type subscriptionEntitlementGrantState struct {
	sub                        *subscription.Subscription
	items                      []*subscription.SubscriptionLineItem
	priceByItemID              map[string]*priceDomain.Price
	meters                     map[string]*meter.Meter
	hasFeatureEntitlementGrant bool
	hasActiveCommitment        bool
	commitmentLineIDs          map[string]bool
	periodStart                time.Time
}

// subscriptionEntitlementGrantBlocker returns why the subscription cannot carry a subscription entitlement grant, or nil.
// The grant covers every usage line once, so one incompatible line blocks it everywhere.
func subscriptionEntitlementGrantBlocker(st subscriptionEntitlementGrantState) error {
	if st.hasActiveCommitment || st.sub.HasCommitment() || lo.FromPtr(st.sub.CommitmentAmount).IsPositive() {
		return errSubGrantCommitment
	}
	if st.hasFeatureEntitlementGrant {
		return errSubGrantWithFeatureGrant
	}
	for _, item := range st.items {
		if item == nil || item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}
		// Lines scheduled for a later period are judged now, since the grant will cover them when they start.
		if !item.EndDate.IsZero() && !item.EndDate.After(st.periodStart) {
			continue
		}
		if st.commitmentLineIDs[item.ID] {
			return errSubGrantCommitment
		}
		if item.BillingPeriod != st.sub.BillingPeriod || max(item.BillingPeriodCount, 1) != max(st.sub.BillingPeriodCount, 1) {
			return errSubGrantCadence
		}
		if m := st.meters[item.MeterID]; m != nil && m.ResetUsage == types.ResetUsageNever {
			return errSubGrantNeverReset
		}
		if reason := GrantPricingGuard(types.EntitlementGrantMeasureAmount, item, st.priceByItemID[item.ID], st.meters[item.MeterID]); reason != nil {
			return reason
		}
	}
	return nil
}

// isFeatureEntitlementGrant reports whether an entitlement makes some feature usage free: a feature grant,
// limited or unlimited, or a legacy usage limit.
func isFeatureEntitlementGrant(e *entitlement.Entitlement) bool {
	if e == nil || !e.IsEnabled || e.IsSubscriptionScoped() || e.FeatureType != types.FeatureTypeMetered {
		return false
	}
	return e.HasGrantConfig() || e.UsageLimit != nil
}

// subscriptionEntitlementGrantChange is a write about to land on a subscription: line items it adds or
// replaces, prices not stored yet, and entitlements it adds.
type subscriptionEntitlementGrantChange struct {
	items        []*subscription.SubscriptionLineItem
	prices       []*priceDomain.Price
	entitlements []*entitlement.Entitlement
}

// hasSubscriptionScopedEntitlement reports whether the subscription has a published, subscription-scoped
// entitlement that has not ended. It reads the entitlement, not its windows, so it holds before the first window opens.
func (s *entitlementGrantService) hasSubscriptionScopedEntitlement(ctx context.Context, subID string) (bool, error) {
	// Partially wired test services have no entitlement store, and so no subscription entitlement grants.
	if s.EntitlementRepo == nil {
		return false, nil
	}
	ecs, err := s.EntitlementRepo.List(ctx, subscriptionEntitlementGrantConfigFilter(subID))
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	return lo.ContainsBy(ecs, func(e *entitlement.Entitlement) bool {
		return e.EndDate == nil || e.EndDate.After(now)
	}), nil
}

// validateWithSubscriptionScopedEntitlement refuses the change when the subscription has a subscription-scoped
// entitlement and the change would break it; without one it allows anything.
func (s *entitlementGrantService) validateWithSubscriptionScopedEntitlement(ctx context.Context, sub *subscription.Subscription, change subscriptionEntitlementGrantChange) error {
	granted, err := s.hasSubscriptionScopedEntitlement(ctx, sub.ID)
	if err != nil {
		return err
	}
	if !granted {
		return nil
	}
	return s.validateChange(ctx, sub, change)
}

// validateChange refuses when the subscription, with the change applied, cannot carry a subscription entitlement grant.
func (s *entitlementGrantService) validateChange(ctx context.Context, sub *subscription.Subscription, change subscriptionEntitlementGrantChange) error {
	st, err := s.subscriptionEntitlementGrantStateAfter(ctx, sub, change)
	if err != nil {
		return err
	}
	reason := subscriptionEntitlementGrantBlocker(st)
	if reason == nil {
		return nil
	}
	return ierr.NewErrorf("subscription entitlement grant not supported: %s", reason).
		WithHint("A subscription entitlement grant needs every usage line on the subscription to be compatible with it.").
		WithReportableDetails(map[string]any{"subscription_id": sub.ID, "reason": reason.Error()}).
		Mark(ierr.ErrValidation)
}

// subscriptionEntitlementGrantStateAfter is the subscription's billing setup with the change applied, as subscriptionEntitlementGrantBlocker reads it.
func (s *entitlementGrantService) subscriptionEntitlementGrantStateAfter(ctx context.Context, sub *subscription.Subscription, change subscriptionEntitlementGrantChange) (subscriptionEntitlementGrantState, error) {
	stored, err := s.SubscriptionLineItemRepo.ListBySubscription(ctx, sub)
	if err != nil {
		return subscriptionEntitlementGrantState{}, err
	}
	byID := lo.KeyBy(stored, func(li *subscription.SubscriptionLineItem) string { return li.ID })
	for _, li := range change.items {
		byID[li.ID] = li
	}
	items := lo.Values(byID)

	usageItems := lo.Filter(items, func(li *subscription.SubscriptionLineItem, _ int) bool {
		return li.PriceType == types.PRICE_TYPE_USAGE
	})
	priceIDs := lo.Uniq(lo.Map(usageItems, func(li *subscription.SubscriptionLineItem, _ int) string { return li.PriceID }))
	meterIDs := lo.Uniq(lo.FilterMap(usageItems, func(li *subscription.SubscriptionLineItem, _ int) (string, bool) {
		return li.MeterID, li.MeterID != ""
	}))

	prices, err := s.PriceRepo.ListByIDs(ctx, priceIDs)
	if err != nil {
		return subscriptionEntitlementGrantState{}, err
	}
	priceByID := lo.KeyBy(append(prices, change.prices...), func(p *priceDomain.Price) string { return p.ID })
	meters, err := s.MeterRepo.ListByIDs(ctx, meterIDs)
	if err != nil {
		return subscriptionEntitlementGrantState{}, err
	}

	current, err := NewSubscriptionService(s.ServiceParams).GetSubscriptionEntitlementsForSubscription(ctx, sub)
	if err != nil {
		return subscriptionEntitlementGrantState{}, err
	}
	hasFeatureEntitlementGrant := lo.ContainsBy(current, func(r *dto.EntitlementResponse) bool {
		return r != nil && isFeatureEntitlementGrant(r.Entitlement)
	}) || lo.ContainsBy(change.entitlements, isFeatureEntitlementGrant)
	committed, err := s.hasActiveCommitment(ctx, sub)
	if err != nil {
		return subscriptionEntitlementGrantState{}, err
	}

	return subscriptionEntitlementGrantState{
		sub:   sub,
		items: items,
		priceByItemID: lo.MapValues(lo.KeyBy(usageItems, func(li *subscription.SubscriptionLineItem) string { return li.ID }),
			func(li *subscription.SubscriptionLineItem, _ string) *priceDomain.Price { return priceByID[li.PriceID] }),
		meters:                     lo.KeyBy(meters, func(m *meter.Meter) string { return m.ID }),
		hasFeatureEntitlementGrant: hasFeatureEntitlementGrant,
		hasActiveCommitment:        committed,
		periodStart:                sub.CurrentPeriodStart,
	}, nil
}

// hasActiveCommitment reports a commitment live in the current period: its lines are priced
// outside the per-line pass the grant reduces.
func (s *entitlementGrantService) hasActiveCommitment(ctx context.Context, sub *subscription.Subscription) (bool, error) {
	svc := &commitmentService{ServiceParams: s.ServiceParams}
	enabled, err := svc.IsEnabled(ctx)
	if err != nil {
		return false, err
	}
	if !enabled {
		return false, nil
	}
	active, err := svc.ActiveForPeriod(ctx, sub.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	if err != nil {
		return false, err
	}

	// A terminated open-ended commitment matches the active window forever; only a live term blocks the grant.
	return lo.SomeBy(active, func(c *commitment.Commitment) bool { return !c.EndedBy(sub.CurrentPeriodStart) }), nil
}

// CreateSubscriptionEntitlementGrant creates a dollar entitlement grant shared by every usage line on one subscription. It
// starts now unless the request dates it; inside a caller's transaction the webhook waits for its commit.
func (s *entitlementService) CreateSubscriptionEntitlementGrant(ctx context.Context, req dto.CreateEntitlementRequest, sub *subscription.Subscription) (*dto.EntitlementResponse, error) {
	e := req.ToEntitlement(ctx)
	e.EntityType = types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION
	e.EntityID = sub.ID
	if e.StartDate == nil {
		e.StartDate = lo.ToPtr(time.Now().UTC())
	}
	e.ApplyGrantDefaults()
	if err := e.Validate(); err != nil {
		return nil, err
	}
	// Windows as long as the cycle never open; that grant is what subscription_period is for.
	if dur, err := e.GrantDuration(); err == nil && dur > 0 && dur >= sub.CurrentPeriodEnd.Sub(sub.CurrentPeriodStart) {
		return nil, ierr.NewError("grant window is as long as the billing period").
			WithHint("Use grant_duration_unit=subscription_period for a grant per billing period.").
			Mark(ierr.ErrValidation)
	}

	enabled, err := s.entitlementAlertsEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ierr.NewError("subscription entitlement grants need entitlement alerts").
			WithHint("Contact Flexprice support to enable this feature.").
			Mark(ierr.ErrValidation)
	}

	var created *entitlement.Entitlement
	if err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.validateSubscriptionEntitlementGrantSiblings(txCtx, e); err != nil {
			return err
		}
		if err := (&entitlementGrantService{ServiceParams: s.ServiceParams}).validateChange(txCtx, sub, subscriptionEntitlementGrantChange{}); err != nil {
			return err
		}
		created, err = s.EntitlementRepo.Create(txCtx, e)
		return err
	}); err != nil {
		return nil, err
	}

	// The publisher writes to Postgres, so it must not run on the caller's transaction.
	publishCtx := types.WithoutDBTransaction(ctx)
	publish := func() { s.publishSystemEvent(publishCtx, types.WebhookEventEntitlementCreated, created.ID) }
	if !types.RegisterPostCommit(ctx, publish) {
		publish()
	}
	return &dto.EntitlementResponse{Entitlement: created}, nil
}

// validateSubscriptionEntitlementGrantSiblings keeps one mode per subscription, and one window length when the
// grants pool additively.
func (s *entitlementService) validateSubscriptionEntitlementGrantSiblings(ctx context.Context, e *entitlement.Entitlement) error {
	siblings, err := s.EntitlementRepo.List(ctx, subscriptionEntitlementGrantConfigFilter(e.EntityID))
	if err != nil {
		return err
	}
	for _, other := range siblings {
		if other.ID == e.ID {
			continue
		}
		if defaultedMode(other.AggregationMode) != defaultedMode(e.AggregationMode) {
			return ierr.NewError("additive and parallel subscription entitlement grants cannot be mixed").
				WithHint("Use one aggregation mode for every subscription entitlement grant on the subscription.").
				WithReportableDetails(map[string]any{"subscription_id": e.EntityID, "existing_entitlement_id": other.ID}).
				Mark(ierr.ErrValidation)
		}
		if defaultedMode(e.AggregationMode) == types.EntitlementAggregationModeAdditive && !sameGrantWindow(other, e) {
			return ierr.NewError("additive subscription entitlement grants must share one window length").
				WithHint("Give every additive subscription entitlement grant on the subscription the same grant duration.").
				WithReportableDetails(map[string]any{"subscription_id": e.EntityID, "existing_entitlement_id": other.ID}).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

func sameGrantWindow(a, b *entitlement.Entitlement) bool {
	return a.GrantDurationUnit == b.GrantDurationUnit &&
		lo.FromPtr(a.GrantDurationValue) == lo.FromPtr(b.GrantDurationValue) &&
		a.GrantAllocationBehavior == b.GrantAllocationBehavior
}

// validateCatalogChange refuses a plan or addon change that would break the subscription entitlement grant on any
// subscription using that plan or addon. change builds the write as it lands on one subscription.
func (s *entitlementGrantService) validateCatalogChange(
	ctx context.Context,
	entityType types.EntitlementEntityType,
	entityID string,
	change func(sub *subscription.Subscription) subscriptionEntitlementGrantChange,
) error {
	subs, err := s.subscriptionsWithSubscriptionScopedEntitlementOn(ctx, entityType, entityID)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		if err := s.validateChange(ctx, sub, change(sub)); err != nil {
			return err
		}
	}
	return nil
}

// subscriptionsWithSubscriptionScopedEntitlementOn returns the live subscriptions on the plan or addon that have a published,
// subscription-scoped entitlement which has not ended.
func (s *entitlementGrantService) subscriptionsWithSubscriptionScopedEntitlementOn(ctx context.Context, entityType types.EntitlementEntityType, entityID string) ([]*subscription.Subscription, error) {
	if s.EntitlementRepo == nil {
		return nil, nil
	}
	ecs, err := s.EntitlementRepo.List(ctx, subscriptionEntitlementGrantConfigFilter())
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	ecs = lo.Filter(ecs, func(ec *entitlement.Entitlement, _ int) bool { return ec.EndDate == nil || ec.EndDate.After(now) })
	if len(ecs) == 0 {
		return nil, nil
	}
	subIDs := lo.Uniq(lo.Map(ecs, func(ec *entitlement.Entitlement, _ int) string { return ec.EntityID }))

	if entityType == types.ENTITLEMENT_ENTITY_TYPE_ADDON {
		assocFilter := types.NewNoLimitAddonAssociationFilter()
		assocFilter.AddonIDs = []string{entityID}
		assocFilter.EntityIDs = subIDs
		assocFilter.EntityType = lo.ToPtr(types.AddonAssociationEntityTypeSubscription)
		assocs, err := s.AddonAssociationRepo.List(ctx, assocFilter)
		if err != nil {
			return nil, err
		}
		subIDs = lo.Uniq(lo.Map(assocs, func(a *addonassociation.AddonAssociation, _ int) string { return a.EntityID }))
		if len(subIDs) == 0 {
			return nil, nil
		}
	}

	subFilter := types.NewNoLimitSubscriptionFilter()
	subFilter.SubscriptionIDs = subIDs
	subFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusDraft,
		types.SubscriptionStatusIncomplete,
		types.SubscriptionStatusPaused,
	}
	if entityType == types.ENTITLEMENT_ENTITY_TYPE_PLAN {
		subFilter.PlanID = entityID
	}
	return s.SubRepo.List(ctx, subFilter)
}

// updateSubscriptionEntitlementGrant changes a subscription entitlement grant's quota. The live window closes and a successor
// carries what was left plus the change, so an edit never hands out a fresh grant.
func (s *entitlementService) updateSubscriptionEntitlementGrant(ctx context.Context, existing *entitlement.Entitlement, req dto.UpdateEntitlementRequest) (*dto.EntitlementResponse, error) {
	if !req.OnlySetsGrantQuota() {
		return nil, ierr.NewError("only grant_quota can change on a subscription entitlement grant").
			WithHint("Delete the subscription entitlement grant and create a new one to change anything else.").
			WithReportableDetails(map[string]any{"entitlement_id": existing.ID}).
			Mark(ierr.ErrValidation)
	}
	if req.GrantQuota == nil {
		return &dto.EntitlementResponse{Entitlement: existing}, nil
	}

	updated := existing
	if err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Locked and re-read, so concurrent edits each see the quota the other left.
		locked, err := s.EntitlementRepo.GetForUpdate(txCtx, existing.ID)
		if err != nil {
			return err
		}
		if entitlement.QuotaEquals(locked.GrantQuota, req.GrantQuota) {
			updated = locked
			return nil
		}
		delta := req.GrantQuota.Sub(lo.FromPtr(locked.GrantQuota))
		locked.GrantQuota = req.GrantQuota
		if err := locked.Validate(); err != nil {
			return err
		}
		if updated, err = s.EntitlementRepo.Update(txCtx, locked); err != nil {
			return err
		}
		grants := &entitlementGrantService{ServiceParams: s.ServiceParams}
		live, err := grants.liveSubscriptionEntitlementGrantWindows(txCtx, updated)
		if err != nil {
			return err
		}
		return grants.reissueSubscriptionEntitlementGrantWindows(txCtx, live, "", delta, time.Now().UTC(), "entitlement_updated")
	}); err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventEntitlementUpdated, updated.ID)
	return &dto.EntitlementResponse{Entitlement: updated}, nil
}

// settleDeletedSubscriptionEntitlementGrant ends what a deleted subscription entitlement grant funded. Additive survivors keep
// the pool less the deleted quota; otherwise the windows close.
func (s *entitlementService) settleDeletedSubscriptionEntitlementGrant(ctx context.Context, deleted *entitlement.Entitlement) error {
	grants := &entitlementGrantService{ServiceParams: s.ServiceParams}
	live, err := grants.liveSubscriptionEntitlementGrantWindows(ctx, deleted)
	if err != nil {
		return err
	}
	if len(live) == 0 {
		return nil
	}
	if defaultedMode(deleted.AggregationMode) == types.EntitlementAggregationModeAdditive {
		survivors, err := s.EntitlementRepo.List(ctx, subscriptionEntitlementGrantConfigFilter(deleted.EntityID))
		if err != nil {
			return err
		}
		survivors = lo.Reject(survivors, func(ec *entitlement.Entitlement, _ int) bool { return ec.ID == deleted.ID })
		if len(survivors) > 0 {
			slotECID := grantCandidates(survivors)[0].ec.ID
			return grants.reissueSubscriptionEntitlementGrantWindows(ctx, live, slotECID, lo.FromPtr(deleted.GrantQuota).Neg(), time.Now().UTC(), "entitlement_deleted")
		}
	}

	_, err = grants.CloseEntitlementGrants(ctx, live, time.Now().UTC(), false)
	return err
}

// liveSubscriptionEntitlementGrantWindows returns the live windows an entitlement feeds: the pooled window when
// additive, its own windows when parallel.
func (s *entitlementGrantService) liveSubscriptionEntitlementGrantWindows(ctx context.Context, ec *entitlement.Entitlement) ([]*entitlementgrant.EntitlementGrant, error) {
	sub, err := s.SubRepo.Get(ctx, ec.EntityID)
	if err != nil {
		return nil, err
	}
	filter := types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(sub.CustomerID).
		WithSubscriptionIDs(sub.ID).
		WithScopeEntityType(types.EntitlementGrantScopeSubscription).
		WithLiveOnly(time.Now().UTC())
	if defaultedMode(ec.AggregationMode) == types.EntitlementAggregationModeParallel {
		filter = filter.WithEntitlementConfigIDs(ec.ID)
	}
	return s.EntitlementGrantRepo.List(ctx, filter)
}

// reissueSubscriptionEntitlementGrantWindows closes the windows at `at` and opens successors for the rest of each
// window with what was left plus delta. An empty slotECID keeps each window's slot.
func (s *entitlementGrantService) reissueSubscriptionEntitlementGrantWindows(
	ctx context.Context,
	live []*entitlementgrant.EntitlementGrant,
	slotECID string,
	delta decimal.Decimal,
	at time.Time,
	source string,
) error {
	if err := s.measureSubscriptionEntitlementGrantWindows(ctx, live, at); err != nil {
		return err
	}
	closedByID, err := s.CloseEntitlementGrants(ctx, live, at, false)
	if err != nil {
		return err
	}

	for _, g := range live {
		closed := closedByID[g.ID]
		if closed == nil || !closed.ValidTo.Before(g.ValidTo) {
			continue
		}
		// A never-evaluated window is removed rather than closed; the successor then spans all of it.
		start := closed.ValidTo
		remaining, _ := closed.Remaining()
		quota := decimal.Max(remaining.Add(delta), decimal.Zero)

		// A spent successor still holds the slot: it opens already crossed, so every unit in it bills.
		status := types.EntitlementGrantStatusActive
		var crossedAt *time.Time
		if quota.IsZero() {
			status = types.EntitlementGrantStatusExhausted
			crossedAt = &start
		}

		successor := entitlementgrant.NewEntitlementGrantBuilder(g).
			WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT_GRANT)).
			WithEntitlementConfigID(lo.Ternary(slotECID == "", g.EntitlementConfigID, slotECID)).
			WithQuota(quota).
			WithWindow(start, g.ValidTo).
			WithGrantStatus(status).
			WithUsage(decimal.Zero).
			WithLastComputedAt(nil).
			WithQuotaCrossedAt(crossedAt).
			WithMetadata(types.Metadata{
				"reissue_source": source,
				"reissued_from":  g.ID,
				"reissue_delta":  delta.String(),
			}).
			WithEnvironmentID(types.GetEnvironmentID(ctx)).
			WithBaseModel(types.GetDefaultBaseModel(ctx)).
			Build()
		if err := successor.Validate(); err != nil {
			return err
		}
		if _, err := s.EntitlementGrantRepo.Create(ctx, successor); err != nil {
			return err
		}
	}
	return nil
}

// measureSubscriptionEntitlementGrantWindows brings each window's usage up to `at`, so the balance a successor
// carries is not the last evaluation's.
func (s *entitlementGrantService) measureSubscriptionEntitlementGrantWindows(ctx context.Context, live []*entitlementgrant.EntitlementGrant, at time.Time) error {
	if len(live) == 0 {
		return nil
	}
	sub, err := s.SubRepo.Get(ctx, live[0].SubscriptionID)
	if err != nil {
		return err
	}
	for _, g := range live {
		end := types.EarliestOf(at, g.ValidTo)
		if !end.After(g.ValidFrom) {
			continue
		}
		usage, _, err := NewMeterUsageService(s.ServiceParams).GetSubscriptionWindowSpend(ctx, sub, g.ValidFrom, end)
		if err != nil {
			return err
		}
		g.Usage = decimal.Max(usage, decimal.Zero)
		g.LastComputedAt = &end
	}
	return nil
}

// refreshSubscriptionEntitlementGrantUsage measures each subscription entitlement grant window in the period up to `at` and
// stores it, so billing does not read usage the alert refresh has not caught up with. It leaves
// last_computed_at alone: finalizing a window, and firing its exhaustion alert, is alert evaluation's job.
func refreshSubscriptionEntitlementGrantUsage(ctx context.Context, params ServiceParams, sub *subscription.Subscription, periodStart, periodEnd, at time.Time) error {
	if params.EntitlementGrantRepo == nil || sub == nil {
		return nil
	}
	filter := types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(sub.CustomerID).
		WithSubscriptionIDs(sub.ID).
		WithScopeEntityType(types.EntitlementGrantScopeSubscription).
		WithCycleOverlap(periodStart, periodEnd)
	grants, err := params.EntitlementGrantRepo.List(ctx, filter)
	if err != nil {
		return err
	}
	for _, g := range grants {
		if !at.After(g.ValidFrom) {
			continue
		}
		// Stored usage already reaches as far as this refresh would measure: a finalized window, or a
		// historical recalculation older than the last evaluation.
		if g.LastComputedAt != nil && !g.LastComputedAt.Before(types.EarliestOf(at, g.ValidTo)) {
			continue
		}
		usage, _, err := NewMeterUsageService(params).GetSubscriptionWindowSpend(ctx, sub, g.ValidFrom, types.EarliestOf(at, g.ValidTo))
		if err != nil {
			return err
		}
		refreshed := entitlementgrant.NewEntitlementGrantBuilder(grantUsageSnapshot(g, decimal.Max(usage, decimal.Zero), at)).
			WithLastComputedAt(g.LastComputedAt).
			Build()
		if err := params.EntitlementGrantRepo.UpdateSnapshot(ctx, refreshed); err != nil {
			return err
		}
	}
	return nil
}

// subscriptionEntitlementGrantConfigFilter matches the published subscription entitlement grants on subIDs, or on every
// subscription when none are given.
func subscriptionEntitlementGrantConfigFilter(subIDs ...string) *types.EntitlementFilter {
	filter := types.NewNoLimitEntitlementFilter().
		WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION).
		WithStatus(types.StatusPublished)
	if len(subIDs) > 0 {
		filter = filter.WithEntityIDs(subIDs)
	}
	filter.GrantScopes = []types.EntitlementGrantScopeEntityType{types.EntitlementGrantScopeSubscription}
	return filter
}

// subscriptionUsageMeterIDs is the meters of the usage lines priced from `from`, the usage a
// subscription entitlement grant covers.
func (s *entitlementGrantService) subscriptionUsageMeterIDs(ctx context.Context, sub *subscription.Subscription, from time.Time) ([]string, error) {
	items, err := NewMeterUsageService(s.ServiceParams).ListUsageLineItems(ctx, sub, from, false)
	if err != nil {
		return nil, err
	}
	return lo.Uniq(lo.FilterMap(items, func(item *subscription.SubscriptionLineItem, _ int) (string, bool) {
		return item.MeterID, item.PriceType == types.PRICE_TYPE_USAGE && item.MeterID != ""
	})), nil
}

// subscriptionEntitlementGrantConfigsBySub is the published subscription entitlement grants of each subscription.
func (s *entitlementGrantService) subscriptionEntitlementGrantConfigsBySub(
	ctx context.Context,
	subs []*subscription.Subscription,
) (map[string][]*entitlement.Entitlement, error) {
	subIDs := lo.Map(subs, func(sub *subscription.Subscription, _ int) string { return sub.ID })
	ecs, err := s.EntitlementRepo.List(ctx, subscriptionEntitlementGrantConfigFilter(subIDs...))
	if err != nil {
		return nil, err
	}
	return lo.GroupBy(ecs, func(ec *entitlement.Entitlement) string { return ec.EntityID }), nil
}

// SubscriptionEntitlementGrantState is the ledger of the subscription's own grants, or nil when it has none.
func (s *entitlementGrantService) SubscriptionEntitlementGrantState(
	ctx context.Context,
	sub *subscription.Subscription,
	at time.Time,
) (*dto.GrantState, error) {
	states, err := s.grantStateBy(ctx, sub, types.EntitlementGrantScopeSubscription, at,
		func(g *entitlementgrant.EntitlementGrant) string { return g.ScopeEntityID })
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, nil
	}
	return states[sub.ID], nil
}

// subscriptionEntitlementGrantsView returns the subscription's grants and their ledger, or nil when it has none.
func (s *entitlementGrantService) subscriptionEntitlementGrantsView(ctx context.Context, sub *subscription.Subscription) (*dto.SubscriptionEntitlementGrantsResponse, error) {
	ecs, err := s.EntitlementRepo.List(ctx, subscriptionEntitlementGrantConfigFilter(sub.ID))
	if err != nil {
		return nil, err
	}
	if len(ecs) == 0 {
		return nil, nil
	}

	state, err := s.SubscriptionEntitlementGrantState(ctx, sub, time.Now().UTC())
	if err != nil {
		s.Logger.Error(ctx, "failed to load subscription entitlement grant state, returning grants without it",
			"error", err, "subscription_id", sub.ID)
	}
	return &dto.SubscriptionEntitlementGrantsResponse{
		Entitlements: lo.Map(ecs, func(ec *entitlement.Entitlement, _ int) *dto.EntitlementResponse {
			return &dto.EntitlementResponse{Entitlement: ec}
		}),
		GrantState: state,
	}, nil
}
