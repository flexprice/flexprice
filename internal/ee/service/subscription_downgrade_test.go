package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// createFreePlan creates a plan with a fixed, recurring, zero-amount price — the shape the
// downgrade logic recognises as the tenant's free tier.
func (s *SubscriptionServiceSuite) createFreePlan(planID, priceID string) *plan.Plan {
	ctx := s.GetContext()

	freePlan := &plan.Plan{
		ID:          planID,
		Name:        "Free Plan",
		Description: "Free tier",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, freePlan))

	freePrice := &price.Price{
		ID:                 priceID,
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           freePlan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, freePrice))

	return freePlan
}

// makeCustomer creates and persists a customer for downgrade scenarios.
func (s *SubscriptionServiceSuite) makeCustomer(id string) *customer.Customer {
	ctx := s.GetContext()
	c := &customer.Customer{
		ID:         id,
		ExternalID: "ext_" + id,
		Name:       "Downgrade Test Customer",
		Email:      id + "@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, c))
	return c
}

// makeActiveSub persists an active subscription (no line items needed for these tests).
func (s *SubscriptionServiceSuite) makeActiveSub(id, customerID, planID string) *subscription.Subscription {
	ctx := s.GetContext()
	sub := &subscription.Subscription{
		ID:                 id,
		CustomerID:         customerID,
		PlanID:             planID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(6 * 24 * time.Hour),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		BaseModel:          types.GetDefaultBaseModel(ctx),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, sub.LineItems))
	return sub
}

// activeSubsOnPlan returns the customer's active/trialing subscriptions on the given plan.
func (s *SubscriptionServiceSuite) activeSubsOnPlan(customerID, planID string) []*subscription.Subscription {
	ctx := s.GetContext()
	subs, err := s.GetStores().SubscriptionRepo.List(ctx, &types.SubscriptionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		CustomerID:  customerID,
		SubscriptionStatus: []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
			types.SubscriptionStatusTrialing,
		},
	})
	s.NoError(err)
	matched := make([]*subscription.Subscription, 0)
	for _, sub := range subs {
		if sub.PlanID == planID {
			matched = append(matched, sub)
		}
	}
	return matched
}

// createPaidPlan creates a plan with a fixed, recurring, non-zero price.
func (s *SubscriptionServiceSuite) createPaidPlan(planID, priceID string, amount decimal.Decimal) *plan.Plan {
	ctx := s.GetContext()

	p := &plan.Plan{
		ID:          planID,
		Name:        "Paid Plan " + planID,
		Description: "Paid tier",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, p))

	pr := &price.Price{
		ID:                 priceID,
		Amount:             amount,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           p.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, pr))

	return p
}

// createSubViaService creates a real subscription (with line items) through the service.
func (s *SubscriptionServiceSuite) createSubViaService(customerID, planID string) string {
	resp, err := s.service.CreateSubscription(s.GetContext(), dto.CreateSubscriptionRequest{
		CustomerID:         customerID,
		PlanID:             planID,
		Currency:           "usd",
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		StartDate:          lo.ToPtr(s.testData.now),
	})
	s.NoError(err)
	return resp.Subscription.ID
}

func (s *SubscriptionServiceSuite) cancelImmediately(subID string) {
	_, err := s.service.CancelSubscription(s.GetContext(), subID, &dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeImmediate,
		ProrationBehavior: types.ProrationBehaviorNone,
		Reason:            "downgrade_test",
	})
	s.NoError(err)
}

func (s *SubscriptionServiceSuite) TestAutoDowngradeToFreeTierOnCancellation() {
	// A single free plan exists for the tenant across all sub-tests.
	freePlan := s.createFreePlan("plan_free_tier", "price_free_tier")

	s.Run("downgrades_to_free_tier_when_last_paid_sub_is_cancelled", func() {
		cust := s.makeCustomer("cust_downgrade_happy")
		paidSub := s.makeActiveSub("sub_paid_happy", cust.ID, s.testData.plan.ID)

		s.cancelImmediately(paidSub.ID)

		// Paid subscription is cancelled.
		cancelled, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), paidSub.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusCancelled, cancelled.SubscriptionStatus)

		// A new active subscription on the free plan now exists for the customer.
		freeSubs := s.activeSubsOnPlan(cust.ID, freePlan.ID)
		s.Len(freeSubs, 1, "exactly one free-tier subscription should be created")
		s.Equal(types.SubscriptionStatusActive, freeSubs[0].SubscriptionStatus)
	})

	s.Run("does_not_downgrade_when_another_paid_sub_still_active", func() {
		cust := s.makeCustomer("cust_downgrade_multi")
		paidA := s.makeActiveSub("sub_paid_a", cust.ID, s.testData.plan.ID)
		_ = s.makeActiveSub("sub_paid_b", cust.ID, s.testData.plan.ID)

		s.cancelImmediately(paidA.ID)

		// No free-tier subscription because the customer still has an active paid sub.
		freeSubs := s.activeSubsOnPlan(cust.ID, freePlan.ID)
		s.Len(freeSubs, 0, "no free-tier subscription while another paid sub is active")
	})

	s.Run("does_not_loop_when_free_sub_itself_is_cancelled", func() {
		cust := s.makeCustomer("cust_downgrade_loop")
		freeSub := s.makeActiveSub("sub_free_existing", cust.ID, freePlan.ID)

		s.cancelImmediately(freeSub.ID)

		// The cancelled subscription was already on the free plan; no replacement is created.
		freeSubs := s.activeSubsOnPlan(cust.ID, freePlan.ID)
		s.Len(freeSubs, 0, "cancelling a free-plan sub must not spawn another free sub")
	})
}

func (s *SubscriptionServiceSuite) TestAutoDowngradeNoOpWhenNoFreePlan() {
	// No free plan is created in this test — only the default usage-based test plan exists.
	cust := s.makeCustomer("cust_no_free_plan")
	paidSub := s.makeActiveSub("sub_no_free_plan", cust.ID, s.testData.plan.ID)

	s.cancelImmediately(paidSub.ID)

	// Cancellation still succeeds; the customer simply has no active subscription afterwards.
	cancelled, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), paidSub.ID)
	s.NoError(err)
	s.Equal(types.SubscriptionStatusCancelled, cancelled.SubscriptionStatus)

	active, err := s.GetStores().SubscriptionRepo.List(s.GetContext(), &types.SubscriptionFilter{
		QueryFilter:        types.NewNoLimitQueryFilter(),
		CustomerID:         cust.ID,
		SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusActive},
	})
	s.NoError(err)
	s.Len(active, 0, "with no free plan, cancellation leaves no active subscription")
}

// SkipAutoDowngrade must suppress the downgrade even when a free plan exists and this is the
// customer's last subscription (the caller is responsible for the replacement).
func (s *SubscriptionServiceSuite) TestAutoDowngradeSkippedWhenReplacementFollows() {
	freePlan := s.createFreePlan("plan_free_skip", "price_free_skip")
	cust := s.makeCustomer("cust_skip_downgrade")
	paidSub := s.makeActiveSub("sub_skip", cust.ID, s.testData.plan.ID)

	_, err := s.service.CancelSubscription(s.GetContext(), paidSub.ID, &dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeImmediate,
		ProrationBehavior: types.ProrationBehaviorNone,
		Reason:            "downgrade_test",
		SkipAutoDowngrade: true,
	})
	s.NoError(err)

	freeSubs := s.activeSubsOnPlan(cust.ID, freePlan.ID)
	s.Len(freeSubs, 0, "SkipAutoDowngrade must suppress the free-tier downgrade")
}

// A plan change internally cancels the old subscription, which must NOT trigger the
// auto-downgrade — otherwise the customer ends up with a stray free sub next to the target.
func (s *SubscriptionServiceSuite) TestPlanChangeDoesNotCreateJunkFreeSubscription() {
	freePlan := s.createFreePlan("plan_free_pc", "price_free_pc")
	sourcePlan := s.createPaidPlan("plan_src_pc", "price_src_pc", decimal.NewFromInt(50))
	targetPlan := s.createPaidPlan("plan_tgt_pc", "price_tgt_pc", decimal.NewFromInt(100))

	cust := s.makeCustomer("cust_plan_change")
	subID := s.createSubViaService(cust.ID, sourcePlan.ID)

	// Change source -> target (internally: cancel source, create target).
	// The SubscriptionServiceSuite doesn't wire EntityIntegrationMappingRepo, which the change
	// service touches for Paddle mapping carryover — provide it on the local params copy.
	params := s.service.(*subscriptionService).ServiceParams
	params.EntityIntegrationMappingRepo = s.GetStores().EntityIntegrationMappingRepo
	_, err := NewSubscriptionChangeService(params).ExecuteSubscriptionChange(
		s.GetContext(), subID, dto.SubscriptionChangeRequest{
			TargetPlanID:       targetPlan.ID,
			ProrationBehavior:  types.ProrationBehaviorNone,
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingCycle:       types.BillingCycleAnniversary,
		})
	s.NoError(err)

	// No stray free subscription, and the customer is on the target plan.
	s.Len(s.activeSubsOnPlan(cust.ID, freePlan.ID), 0, "plan change must not create a free subscription")
	s.Len(s.activeSubsOnPlan(cust.ID, targetPlan.ID), 1, "customer should be on the target plan")
}
