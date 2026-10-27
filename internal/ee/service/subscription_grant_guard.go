package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/meter"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

// subscriptionGrantConflict returns why the subscription cannot carry a subscription grant, or nil.
// The grant covers every usage line once, so one incompatible line blocks it everywhere.
func subscriptionGrantConflict(st subscriptionGrantState) error {
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
// replaces and entitlements it adds.
type subscriptionGrantChange struct {
	items        []*subscription.SubscriptionLineItem
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
	reason := subscriptionGrantConflict(st)
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
	priceByID := lo.KeyBy(prices, func(p *priceDomain.Price) string { return p.ID })
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
