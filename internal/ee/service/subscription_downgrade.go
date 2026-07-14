package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// ensureFreePlanSubscriptionOnCancellation starts a free-plan subscription when a customer
// cancels their last paid one, so they keep free-tier entitlements (FLE-973).
// Must be called AFTER the cancellation commits; best-effort and idempotent (the "last
// active sub" guard covers Temporal retries). startDate is when the cancellation takes effect.
func (s *subscriptionService) ensureFreePlanSubscriptionOnCancellation(
	ctx context.Context,
	cancelledSub *subscription.Subscription,
	startDate time.Time,
) error {
	logger := s.Logger.With(
		"customer_id", cancelledSub.CustomerID,
		"cancelled_subscription_id", cancelledSub.ID,
	)

	// Inherited subs follow the parent lifecycle.
	if cancelledSub.SubscriptionType == types.SubscriptionTypeInherited {
		return nil
	}

	// Only downgrade if this was the customer's last active/trialing sub. The cancelled sub is
	// already persisted as cancelled, so it isn't counted — which also makes this idempotent.
	activeSubs, err := s.SubRepo.List(ctx, &types.SubscriptionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		CustomerID:  cancelledSub.CustomerID,
		SubscriptionStatus: []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
			types.SubscriptionStatusTrialing,
		},
		WithLineItems: false,
	})
	if err != nil {
		return err
	}
	if len(activeSubs) > 0 {
		logger.Info(ctx, "customer still has active subscriptions, skipping free-tier downgrade",
			"active_subscription_count", len(activeSubs))
		return nil
	}

	// ctx is scoped to the customer's tenant, so this finds that tenant's free plan.
	freePlan, freePrice, err := s.findFreePlan(ctx)
	if err != nil {
		return err
	}
	if freePlan == nil || freePrice == nil {
		logger.Info(ctx, "no free plan configured, skipping free-tier downgrade")
		return nil
	}

	// Loop guard: don't re-create a free sub when the cancelled one was already the free plan.
	if cancelledSub.PlanID == freePlan.ID {
		logger.Info(ctx, "cancelled subscription was already on the free plan, skipping downgrade",
			"free_plan_id", freePlan.ID)
		return nil
	}

	if startDate.IsZero() {
		startDate = time.Now().UTC()
	}

	newSub, err := s.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         cancelledSub.CustomerID,
		PlanID:             freePlan.ID,
		Currency:           freePrice.Currency,
		BillingCadence:     freePrice.BillingCadence,
		BillingPeriod:      freePrice.BillingPeriod,
		BillingPeriodCount: freePrice.BillingPeriodCount,
		StartDate:          lo.ToPtr(startDate),
		BillingCycle:       types.BillingCycleAnniversary,
	})
	if err != nil {
		return err
	}

	logger.Info(ctx, "created free-tier subscription on cancellation",
		"free_plan_id", freePlan.ID,
		"free_subscription_id", newSub.ID,
		"start_date", startDate)
	return nil
}

// findFreePlan returns the free plan (fixed, recurring, $0 price) for the tenant in ctx and
// its qualifying price, or (nil, nil, nil) if none. First match wins if there are several.
// Private for now — promote to PlanService only if a second caller appears.
func (s *subscriptionService) findFreePlan(ctx context.Context) (*dto.PlanResponse, *dto.PriceResponse, error) {
	planFilter := types.NewNoLimitPlanFilter()
	planFilter.Expand = lo.ToPtr(string(types.ExpandPrices))

	plans, err := NewPlanService(s.ServiceParams).GetPlans(ctx, planFilter)
	if err != nil {
		return nil, nil, err
	}

	for _, p := range plans.Items {
		for _, price := range p.Prices {
			if price.Type == types.PRICE_TYPE_FIXED &&
				price.BillingCadence == types.BILLING_CADENCE_RECURRING &&
				price.Amount.IsZero() {
				return p, price, nil
			}
		}
	}

	return nil, nil, nil
}
