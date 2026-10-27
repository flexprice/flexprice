package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
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
	errSubGrantWithFeatureGrant = grantGuardError("subscription also has feature usage allowances")
)

// subscriptionGrantState is what decides whether a subscription can carry a subscription grant.
type subscriptionGrantState struct {
	sub                 *subscription.Subscription
	items               []*subscription.SubscriptionLineItem
	priceByItemID       map[string]*priceDomain.Price
	meters              map[string]*meter.Meter
	hasFeatureAllowance bool
	commitmentLineIDs   map[string]bool
	periodStart         time.Time
	periodEnd           time.Time
}

// subscriptionGrantBlocker returns why the subscription cannot carry a subscription grant, or nil.
// The grant covers every usage line once, so one incompatible line blocks it everywhere.
func subscriptionGrantBlocker(st subscriptionGrantState) error {
	if st.sub.HasCommitment() || lo.FromPtr(st.sub.CommitmentAmount).IsPositive() {
		return errSubGrantCommitment
	}
	if st.hasFeatureAllowance {
		return errSubGrantWithFeatureGrant
	}
	for _, item := range st.items {
		if item == nil || item.PriceType != types.PRICE_TYPE_USAGE || !overlapsPeriod(item, st.periodStart, st.periodEnd) {
			continue
		}
		if st.commitmentLineIDs[item.ID] {
			return errSubGrantCommitment
		}
		if item.BillingPeriod != st.sub.BillingPeriod || max(item.BillingPeriodCount, 1) != max(st.sub.BillingPeriodCount, 1) {
			return errSubGrantCadence
		}
		if reason := GrantPricingGuard(types.EntitlementGrantMeasureAmount, item, st.priceByItemID[item.ID], st.meters[item.MeterID]); reason != nil {
			return reason
		}
	}
	return nil
}

func overlapsPeriod(item *subscription.SubscriptionLineItem, periodStart, periodEnd time.Time) bool {
	startsInTime := item.StartDate.IsZero() || item.StartDate.Before(periodEnd)
	endsInTime := item.EndDate.IsZero() || item.EndDate.After(periodStart)
	return startsInTime && endsInTime
}

// isFeatureAllowance reports whether an entitlement makes some feature usage free: a feature grant,
// limited or unlimited, or a legacy usage limit.
func isFeatureAllowance(e *entitlement.Entitlement) bool {
	if e == nil || !e.IsEnabled || e.IsSubscriptionScoped() || e.FeatureType != types.FeatureTypeMetered {
		return false
	}
	return e.HasGrantConfig() || e.UsageLimit != nil
}

// subscriptionGrantChange is a write about to land on a subscription: line items it adds or
// replaces, prices not stored yet, and entitlements it adds.
type subscriptionGrantChange struct {
	items        []*subscription.SubscriptionLineItem
	prices       []*priceDomain.Price
	entitlements []*entitlement.Entitlement
}

type subscriptionGrantGuard struct {
	ServiceParams
}

func newSubscriptionGrantGuard(params ServiceParams) *subscriptionGrantGuard {
	return &subscriptionGrantGuard{ServiceParams: params}
}

// hasSubscriptionGrant reports whether the subscription has a published subscription grant.
func (g *subscriptionGrantGuard) hasSubscriptionGrant(ctx context.Context, subID string) (bool, error) {
	// Partially wired test services have no entitlement store, and so no subscription grants.
	if g.EntitlementRepo == nil {
		return false, nil
	}
	filter := types.NewNoLimitEntitlementFilter().
		WithEntityIDs([]string{subID}).
		WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION).
		WithStatus(types.StatusPublished)
	filter.GrantScopes = []types.EntitlementGrantScopeEntityType{types.EntitlementGrantScopeSubscription}
	filter.Limit = lo.ToPtr(1)
	ecs, err := g.EntitlementRepo.List(ctx, filter)
	if err != nil {
		return false, err
	}
	return len(ecs) > 0, nil
}

// checkIfGranted refuses the change when the subscription has a subscription grant it would break.
func (g *subscriptionGrantGuard) checkIfGranted(ctx context.Context, sub *subscription.Subscription, change subscriptionGrantChange) error {
	granted, err := g.hasSubscriptionGrant(ctx, sub.ID)
	if err != nil || !granted {
		return err
	}
	return g.check(ctx, sub, change)
}

// checkAddonAttach refuses an addon whose lines or feature allowances would break the subscription grant.
func (g *subscriptionGrantGuard) checkAddonAttach(ctx context.Context, sub *subscription.Subscription, addonID string, items []*subscription.SubscriptionLineItem) error {
	granted, err := g.hasSubscriptionGrant(ctx, sub.ID)
	if err != nil || !granted {
		return err
	}
	addonEntitlements, err := NewEntitlementService(g.ServiceParams).GetAddonEntitlements(ctx, addonID)
	if err != nil {
		return err
	}
	return g.check(ctx, sub, subscriptionGrantChange{
		items: items,
		entitlements: lo.Map(addonEntitlements.Items, func(r *dto.EntitlementResponse, _ int) *entitlement.Entitlement {
			return r.Entitlement
		}),
	})
}

// check refuses when the subscription, with the change applied, cannot carry a subscription grant.
func (g *subscriptionGrantGuard) check(ctx context.Context, sub *subscription.Subscription, change subscriptionGrantChange) error {
	st, err := g.load(ctx, sub, change)
	if err != nil {
		return err
	}
	reason := subscriptionGrantBlocker(st)
	if reason == nil {
		committed, err := g.hasActiveCommitment(ctx, sub)
		if err != nil {
			return err
		}
		if !committed {
			return nil
		}
		reason = errSubGrantCommitment
	}
	return ierr.NewErrorf("subscription grant not supported: %s", reason).
		WithHint("A subscription grant needs every usage line on the subscription to be compatible with it.").
		WithReportableDetails(map[string]any{"subscription_id": sub.ID, "reason": reason.Error()}).
		Mark(ierr.ErrValidation)
}

func (g *subscriptionGrantGuard) load(ctx context.Context, sub *subscription.Subscription, change subscriptionGrantChange) (subscriptionGrantState, error) {
	stored, err := g.SubscriptionLineItemRepo.ListBySubscription(ctx, sub)
	if err != nil {
		return subscriptionGrantState{}, err
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

	prices, err := g.PriceRepo.ListByIDs(ctx, priceIDs)
	if err != nil {
		return subscriptionGrantState{}, err
	}
	priceByID := lo.KeyBy(append(prices, change.prices...), func(p *priceDomain.Price) string { return p.ID })
	meters, err := g.MeterRepo.ListByIDs(ctx, meterIDs)
	if err != nil {
		return subscriptionGrantState{}, err
	}

	current, err := NewSubscriptionService(g.ServiceParams).GetSubscriptionEntitlementsForSubscription(ctx, sub)
	if err != nil {
		return subscriptionGrantState{}, err
	}
	hasFeatureAllowance := lo.ContainsBy(current, func(r *dto.EntitlementResponse) bool {
		return r != nil && isFeatureAllowance(r.Entitlement)
	}) || lo.ContainsBy(change.entitlements, isFeatureAllowance)

	return subscriptionGrantState{
		sub:   sub,
		items: items,
		priceByItemID: lo.MapValues(lo.KeyBy(usageItems, func(li *subscription.SubscriptionLineItem) string { return li.ID }),
			func(li *subscription.SubscriptionLineItem, _ string) *priceDomain.Price { return priceByID[li.PriceID] }),
		meters:              lo.KeyBy(meters, func(m *meter.Meter) string { return m.ID }),
		hasFeatureAllowance: hasFeatureAllowance,
		periodStart:         sub.CurrentPeriodStart,
		periodEnd:           sub.CurrentPeriodEnd,
	}, nil
}

// hasActiveCommitment reports a commitment live in the current period: its lines are priced
// outside the per-line pass the grant reduces.
func (g *subscriptionGrantGuard) hasActiveCommitment(ctx context.Context, sub *subscription.Subscription) (bool, error) {
	svc := &commitmentService{ServiceParams: g.ServiceParams}
	enabled, err := svc.IsEnabled(ctx)
	if err != nil || !enabled {
		return false, err
	}
	active, err := svc.ActiveForPeriod(ctx, sub.ID, sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	return len(active) > 0, err
}

// createSubscriptionGrant creates a dollar allowance shared by every usage line on one subscription.
func (s *entitlementService) createSubscriptionGrant(ctx context.Context, req dto.CreateEntitlementRequest, sub *subscription.Subscription) (*dto.EntitlementResponse, error) {
	e := req.ToEntitlement(ctx)
	e.EntityType = types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION
	e.EntityID = sub.ID
	e.ApplyGrantDefaults()
	if err := e.Validate(); err != nil {
		return nil, err
	}

	enabled, err := s.entitlementAlertsEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ierr.NewError("subscription grants need entitlement alerts").
			WithHint("Contact Flexprice support to enable this feature.").
			Mark(ierr.ErrValidation)
	}

	var created *entitlement.Entitlement
	if err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.validateSubscriptionGrantSiblings(txCtx, e); err != nil {
			return err
		}
		if err := newSubscriptionGrantGuard(s.ServiceParams).check(txCtx, sub, subscriptionGrantChange{}); err != nil {
			return err
		}
		created, err = s.EntitlementRepo.Create(txCtx, e)
		return err
	}); err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventEntitlementCreated, created.ID)
	return &dto.EntitlementResponse{Entitlement: created}, nil
}

// validateSubscriptionGrantSiblings keeps one mode per subscription, and one window length when the
// grants pool additively.
func (s *entitlementService) validateSubscriptionGrantSiblings(ctx context.Context, e *entitlement.Entitlement) error {
	siblings, err := s.subscriptionGrantConfigs(ctx, e.EntityID)
	if err != nil {
		return err
	}
	for _, other := range siblings {
		if other.ID == e.ID {
			continue
		}
		if defaultedMode(other.AggregationMode) != defaultedMode(e.AggregationMode) {
			return ierr.NewError("additive and parallel subscription grants cannot be mixed").
				WithHint("Use one aggregation mode for every subscription grant on the subscription.").
				WithReportableDetails(map[string]any{"subscription_id": e.EntityID, "existing_entitlement_id": other.ID}).
				Mark(ierr.ErrValidation)
		}
		if defaultedMode(e.AggregationMode) == types.EntitlementAggregationModeAdditive && !sameGrantWindow(other, e) {
			return ierr.NewError("additive subscription grants must share one window length").
				WithHint("Give every additive subscription grant on the subscription the same grant duration.").
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

// subscriptionGrantConfigs returns the published subscription grants on one subscription.
func (s *entitlementService) subscriptionGrantConfigs(ctx context.Context, subID string) ([]*entitlement.Entitlement, error) {
	filter := types.NewNoLimitEntitlementFilter().
		WithEntityIDs([]string{subID}).
		WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION).
		WithStatus(types.StatusPublished)
	filter.GrantScopes = []types.EntitlementGrantScopeEntityType{types.EntitlementGrantScopeSubscription}
	return s.EntitlementRepo.List(ctx, filter)
}

// checkCatalogChange refuses a plan or addon change that would break the subscription grant on any
// subscription using that plan or addon. change builds the write as it lands on one subscription.
func (g *subscriptionGrantGuard) checkCatalogChange(
	ctx context.Context,
	entityType types.EntitlementEntityType,
	entityID string,
	change func(sub *subscription.Subscription) subscriptionGrantChange,
) error {
	subs, err := g.grantedSubscriptions(ctx, entityType, entityID)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		if err := g.check(ctx, sub, change(sub)); err != nil {
			return err
		}
	}
	return nil
}

// grantedSubscriptions returns the live subscriptions with a subscription grant that use the plan or addon.
func (g *subscriptionGrantGuard) grantedSubscriptions(ctx context.Context, entityType types.EntitlementEntityType, entityID string) ([]*subscription.Subscription, error) {
	if g.EntitlementRepo == nil {
		return nil, nil
	}
	filter := types.NewNoLimitEntitlementFilter().
		WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION).
		WithStatus(types.StatusPublished)
	filter.GrantScopes = []types.EntitlementGrantScopeEntityType{types.EntitlementGrantScopeSubscription}
	ecs, err := g.EntitlementRepo.List(ctx, filter)
	if err != nil || len(ecs) == 0 {
		return nil, err
	}
	subIDs := lo.Uniq(lo.Map(ecs, func(ec *entitlement.Entitlement, _ int) string { return ec.EntityID }))

	if entityType == types.ENTITLEMENT_ENTITY_TYPE_ADDON {
		assocFilter := types.NewNoLimitAddonAssociationFilter()
		assocFilter.AddonIDs = []string{entityID}
		assocFilter.EntityIDs = subIDs
		assocFilter.EntityType = lo.ToPtr(types.AddonAssociationEntityTypeSubscription)
		assocs, err := g.AddonAssociationRepo.List(ctx, assocFilter)
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
	}
	if entityType == types.ENTITLEMENT_ENTITY_TYPE_PLAN {
		subFilter.PlanID = entityID
	}
	return g.SubRepo.List(ctx, subFilter)
}

// priceChange is how a new plan or addon price lands on a subscription: one line, or nothing when the
// price's cadence or currency keeps it off that subscription.
func priceChange(p *priceDomain.Price) func(sub *subscription.Subscription) subscriptionGrantChange {
	return func(sub *subscription.Subscription) subscriptionGrantChange {
		candidates := []*dto.PriceResponse{{Price: p}}
		lands := filterValidPricesForSubscription(candidates, sub, nil)
		if p.EntityType == types.PRICE_ENTITY_TYPE_ADDON {
			lands = filterAddonPricesForSubscription(candidates, sub)
		}
		if len(lands) == 0 {
			return subscriptionGrantChange{}
		}
		return subscriptionGrantChange{
			items:  []*subscription.SubscriptionLineItem{pendingLineFromPrice(sub, p)},
			prices: []*priceDomain.Price{p},
		}
	}
}

func pendingLineFromPrice(sub *subscription.Subscription, p *priceDomain.Price) *subscription.SubscriptionLineItem {
	return &subscription.SubscriptionLineItem{
		ID:                 "pending_" + p.ID,
		SubscriptionID:     sub.ID,
		PriceID:            p.ID,
		PriceType:          p.Type,
		MeterID:            p.MeterID,
		BillingPeriod:      p.BillingPeriod,
		BillingPeriodCount: p.BillingPeriodCount,
	}
}

// updateSubscriptionGrant changes a subscription grant's quota. The live window closes and a successor
// carries what was left plus the change, so an edit never hands out a fresh allowance.
func (s *entitlementService) updateSubscriptionGrant(ctx context.Context, existing *entitlement.Entitlement, req dto.UpdateEntitlementRequest) (*dto.EntitlementResponse, error) {
	if req.IsEnabled != nil || req.UsageLimit != nil || req.UsageResetPeriod != "" || req.IsSoftLimit != nil ||
		req.StaticValue != "" || req.ConfigValue != nil || req.ClearGrantConfig != nil || req.GrantMeasure != nil ||
		req.GrantDurationValue != nil || req.GrantDurationUnit != nil || req.GrantAllocationBehavior != nil ||
		req.AggregationMode != nil || req.GrantUnlimited != nil {
		return nil, ierr.NewError("only grant_quota can change on a subscription grant").
			WithHint("Delete the subscription grant and create a new one to change anything else.").
			WithReportableDetails(map[string]any{"entitlement_id": existing.ID}).
			Mark(ierr.ErrValidation)
	}
	if req.GrantQuota == nil || entitlement.QuotaEquals(existing.GrantQuota, req.GrantQuota) {
		return &dto.EntitlementResponse{Entitlement: existing}, nil
	}

	delta := req.GrantQuota.Sub(lo.FromPtr(existing.GrantQuota))
	existing.GrantQuota = req.GrantQuota
	if err := existing.Validate(); err != nil {
		return nil, err
	}

	var updated *entitlement.Entitlement
	if err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		var err error
		updated, err = s.EntitlementRepo.Update(txCtx, existing)
		if err != nil {
			return err
		}
		live, err := s.liveSubscriptionGrantWindows(txCtx, updated)
		if err != nil {
			return err
		}
		return s.reissueSubscriptionGrantWindows(txCtx, live, "", delta, time.Now().UTC(), "entitlement_updated")
	}); err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventEntitlementUpdated, updated.ID)
	return &dto.EntitlementResponse{Entitlement: updated}, nil
}

// settleDeletedSubscriptionGrant ends what a deleted subscription grant funded. Additive survivors keep
// the pool less the deleted quota; otherwise the windows close.
func (s *entitlementService) settleDeletedSubscriptionGrant(ctx context.Context, deleted *entitlement.Entitlement) error {
	live, err := s.liveSubscriptionGrantWindows(ctx, deleted)
	if err != nil || len(live) == 0 {
		return err
	}
	if defaultedMode(deleted.AggregationMode) == types.EntitlementAggregationModeAdditive {
		survivors, err := s.subscriptionGrantConfigs(ctx, deleted.EntityID)
		if err != nil {
			return err
		}
		survivors = lo.Reject(survivors, func(ec *entitlement.Entitlement, _ int) bool { return ec.ID == deleted.ID })
		if len(survivors) > 0 {
			slotECID := grantCandidates(survivors)[0].ec.ID
			return s.reissueSubscriptionGrantWindows(ctx, live, slotECID, lo.FromPtr(deleted.GrantQuota).Neg(), time.Now().UTC(), "entitlement_deleted")
		}
	}

	_, err = NewEntitlementGrantService(s.ServiceParams).CloseEntitlementGrants(ctx, live, time.Now().UTC())
	return err
}

// liveSubscriptionGrantWindows returns the live windows an entitlement feeds: the pooled window when
// additive, its own windows when parallel.
func (s *entitlementService) liveSubscriptionGrantWindows(ctx context.Context, ec *entitlement.Entitlement) ([]*entitlementgrant.EntitlementGrant, error) {
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

// reissueSubscriptionGrantWindows closes the windows at `at` and opens successors for the rest of each
// window with what was left plus delta. An empty slotECID keeps each window's slot.
func (s *entitlementService) reissueSubscriptionGrantWindows(
	ctx context.Context,
	live []*entitlementgrant.EntitlementGrant,
	slotECID string,
	delta decimal.Decimal,
	at time.Time,
	source string,
) error {
	closedByID, err := NewEntitlementGrantService(s.ServiceParams).CloseEntitlementGrants(ctx, live, at)
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
