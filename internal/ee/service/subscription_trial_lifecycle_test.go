package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// SubscriptionTrialLifecycleSuite tests the trial-end processing flows in
// subscription_trial.go (ProcessTrialEndDue, processSubscriptionTrialEnd and cascades).
type SubscriptionTrialLifecycleSuite struct {
	testutil.BaseServiceTestSuite
	svc SubscriptionService
}

func TestSubscriptionTrialLifecycle(t *testing.T) {
	suite.Run(t, new(SubscriptionTrialLifecycleSuite))
}

func (s *SubscriptionTrialLifecycleSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewSubscriptionService(newTestServiceParams(&s.BaseServiceTestSuite))
}

func (s *SubscriptionTrialLifecycleSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionTrialLifecycleSuite) internalService() *subscriptionService {
	return s.svc.(*subscriptionService)
}

type trialSubOpts struct {
	status           types.SubscriptionStatus
	subType          types.SubscriptionType
	parentSubID      *string
	trialStart       *time.Time
	trialEnd         *time.Time
	withFixedCharge  bool
	collectionMethod types.CollectionMethod
	paymentBehavior  types.PaymentBehavior
}

// createTrialSub creates a customer + plan + subscription in one go and returns the subscription.
func (s *SubscriptionTrialLifecycleSuite) createTrialSub(opts trialSubOpts) *subscription.Subscription {
	ctx := s.GetContext()

	cust := &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "ext_" + s.GetUUID(),
		Name:       "Trial Lifecycle Customer",
		Email:      "trial-lifecycle@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	pl := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Trial Lifecycle Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	trialStart := lo.FromPtrOr(opts.trialStart, time.Now().UTC().Add(-14*24*time.Hour))
	trialEnd := lo.FromPtrOr(opts.trialEnd, time.Now().UTC().Add(-1*time.Hour))

	status := opts.status
	if status == "" {
		status = types.SubscriptionStatusTrialing
	}
	subType := opts.subType
	if subType == "" {
		subType = types.SubscriptionTypeStandalone
	}
	collectionMethod := opts.collectionMethod
	if collectionMethod == "" {
		collectionMethod = types.CollectionMethodSendInvoice
	}
	paymentBehavior := opts.paymentBehavior
	if paymentBehavior == "" {
		paymentBehavior = types.PaymentBehaviorDefaultIncomplete
	}

	sub := &subscription.Subscription{
		ID:                   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:           cust.ID,
		PlanID:               pl.ID,
		SubscriptionStatus:   status,
		SubscriptionType:     subType,
		ParentSubscriptionID: opts.parentSubID,
		Currency:             "usd",
		BillingAnchor:        trialStart,
		BillingCycle:         types.BillingCycleAnniversary,
		BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:   1,
		BillingCadence:       types.BILLING_CADENCE_RECURRING,
		StartDate:            trialStart,
		CurrentPeriodStart:   trialStart,
		CurrentPeriodEnd:     trialEnd,
		TrialStart:           &trialStart,
		TrialEnd:             &trialEnd,
		CollectionMethod:     string(collectionMethod),
		PaymentBehavior:      string(paymentBehavior),
		BaseModel:            types.GetDefaultBaseModel(ctx),
	}

	var lineItems []*subscription.SubscriptionLineItem
	if opts.withFixedCharge {
		p := &price.Price{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
			Amount:             decimal.NewFromInt(10),
			Currency:           "usd",
			EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:           pl.ID,
			Type:               types.PRICE_TYPE_FIXED,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingModel:       types.BILLING_MODEL_FLAT_FEE,
			InvoiceCadence:     types.InvoiceCadenceAdvance,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, p))
		lineItems = append(lineItems, &subscription.SubscriptionLineItem{
			ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
			SubscriptionID:  sub.ID,
			CustomerID:      cust.ID,
			EntityID:        pl.ID,
			EntityType:      types.SubscriptionLineItemEntityTypePlan,
			PlanDisplayName: pl.Name,
			PriceID:         p.ID,
			PriceType:       p.Type,
			DisplayName:     "Fixed Charge",
			Quantity:        decimal.NewFromInt(1),
			Currency:        sub.Currency,
			BillingPeriod:   sub.BillingPeriod,
			InvoiceCadence:  types.InvoiceCadenceAdvance,
			StartDate:       trialStart,
			BaseModel:       types.GetDefaultBaseModel(ctx),
		})
	}

	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, lineItems))
	return sub
}

func (s *SubscriptionTrialLifecycleSuite) TestProcessSingleSubscriptionTrialEnd_Guards() {
	ctx := s.GetContext()
	now := time.Now().UTC()
	futureEnd := now.Add(7 * 24 * time.Hour)

	testCases := []struct {
		name string
		sub  func() *subscription.Subscription
	}{
		{
			name: "inherited_subscription_is_skipped",
			sub: func() *subscription.Subscription {
				parent := "sub_parent_x"
				return s.createTrialSub(trialSubOpts{subType: types.SubscriptionTypeInherited, parentSubID: &parent})
			},
		},
		{
			name: "paused_subscription_is_skipped",
			sub: func() *subscription.Subscription {
				return s.createTrialSub(trialSubOpts{status: types.SubscriptionStatusPaused})
			},
		},
		{
			name: "non_trialing_subscription_is_skipped",
			sub: func() *subscription.Subscription {
				return s.createTrialSub(trialSubOpts{status: types.SubscriptionStatusActive})
			},
		},
		{
			name: "trial_end_in_future_is_skipped",
			sub: func() *subscription.Subscription {
				return s.createTrialSub(trialSubOpts{trialEnd: &futureEnd})
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			sub := tc.sub()
			before, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
			s.Require().NoError(err)

			inv, err := s.internalService().ProcessSingleSubscriptionTrialEnd(ctx, sub, now)
			s.NoError(err)
			s.Nil(inv)

			after, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
			s.NoError(err)
			s.Equal(before.SubscriptionStatus, after.SubscriptionStatus)
			s.True(before.CurrentPeriodEnd.Equal(after.CurrentPeriodEnd))
		})
	}

	s.Run("missing_trial_bounds_is_skipped", func() {
		sub := s.createTrialSub(trialSubOpts{})
		sub.TrialStart = nil
		sub.TrialEnd = nil
		s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

		inv, err := s.internalService().ProcessSingleSubscriptionTrialEnd(ctx, sub, now)
		s.NoError(err)
		s.Nil(inv)

		after, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusTrialing, after.SubscriptionStatus)
	})
}

func (s *SubscriptionTrialLifecycleSuite) TestProcessSingleSubscriptionTrialEnd_ZeroAmountActivates() {
	ctx := s.GetContext()
	now := time.Now().UTC()

	sub := s.createTrialSub(trialSubOpts{})
	trialEnd := lo.FromPtr(sub.TrialEnd)

	inv, err := s.internalService().ProcessSingleSubscriptionTrialEnd(ctx, sub, now)
	s.NoError(err)
	// No billable line items → zero-dollar invoice is skipped and the sub activates directly.
	s.Nil(inv)

	after, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, after.SubscriptionStatus)

	// Billing anchor and period are re-anchored at the trial end.
	s.True(after.BillingAnchor.Equal(trialEnd))
	s.True(after.CurrentPeriodStart.Equal(trialEnd))

	expectedPeriodEnd, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: trialEnd,
		BillingAnchor:      trialEnd,
		Unit:               1,
		Period:             types.BILLING_PERIOD_MONTHLY,
	})
	s.Require().NoError(err)
	s.True(after.CurrentPeriodEnd.Equal(expectedPeriodEnd))
}

func (s *SubscriptionTrialLifecycleSuite) TestProcessSingleSubscriptionTrialEnd_WithChargesCreatesInvoice() {
	ctx := s.GetContext()
	now := time.Now().UTC()

	sub := s.createTrialSub(trialSubOpts{withFixedCharge: true})
	trialEnd := lo.FromPtr(sub.TrialEnd)

	inv, err := s.internalService().ProcessSingleSubscriptionTrialEnd(ctx, sub, now)
	s.Require().NoError(err)
	s.Require().NotNil(inv)

	// Advance fixed charge of $10 for the first real period.
	s.True(inv.Total.Equal(decimal.NewFromInt(10)), "expected total 10, got %s", inv.Total)
	s.Equal(string(types.InvoiceBillingReasonSubscriptionTrialEnd), inv.BillingReason)

	after, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	// Unpaid trial-end invoice keeps the subscription incomplete.
	s.Equal(types.SubscriptionStatusIncomplete, after.SubscriptionStatus)
	s.True(after.CurrentPeriodStart.Equal(trialEnd))

	// Idempotency: re-running for the same subscription is a no-op because it is no
	// longer trialing.
	updated, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	secondInv, err := s.internalService().ProcessSingleSubscriptionTrialEnd(ctx, updated, now)
	s.NoError(err)
	s.Nil(secondInv)
}

func (s *SubscriptionTrialLifecycleSuite) TestProcessTrialEndDue() {
	ctx := s.GetContext()

	s.Run("no_due_subscriptions_returns_empty_response", func() {
		resp, err := s.svc.ProcessTrialEndDue(ctx)
		s.NoError(err)
		s.Equal(0, resp.TotalSuccess)
		s.Equal(0, resp.TotalFailed)
		s.Empty(resp.Items)
	})

	s.Run("processes_due_trialing_subscriptions_and_skips_future_trials", func() {
		dueSub1 := s.createTrialSub(trialSubOpts{})
		dueSub2 := s.createTrialSub(trialSubOpts{})
		futureEnd := time.Now().UTC().Add(7 * 24 * time.Hour)
		notDueSub := s.createTrialSub(trialSubOpts{trialEnd: &futureEnd})

		resp, err := s.svc.ProcessTrialEndDue(ctx)
		s.NoError(err)
		s.Equal(2, resp.TotalSuccess)
		s.Equal(0, resp.TotalFailed)
		s.Len(resp.Items, 2)
		for _, item := range resp.Items {
			s.True(item.Success)
		}

		after1, err := s.GetStores().SubscriptionRepo.Get(ctx, dueSub1.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusActive, after1.SubscriptionStatus)

		after2, err := s.GetStores().SubscriptionRepo.Get(ctx, dueSub2.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusActive, after2.SubscriptionStatus)

		afterNotDue, err := s.GetStores().SubscriptionRepo.Get(ctx, notDueSub.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusTrialing, afterNotDue.SubscriptionStatus)
	})
}

func (s *SubscriptionTrialLifecycleSuite) TestCascadeTrialEndToInherited() {
	ctx := s.GetContext()

	s.Run("non_parent_subscription_is_noop", func() {
		standalone := s.createTrialSub(trialSubOpts{})
		s.NoError(s.internalService().cascadeTrialEndToInherited(ctx, standalone))
	})

	s.Run("propagates_trial_end_state_to_inherited_children", func() {
		parent := s.createTrialSub(trialSubOpts{subType: types.SubscriptionTypeParent})
		child := s.createTrialSub(trialSubOpts{
			subType:     types.SubscriptionTypeInherited,
			parentSubID: &parent.ID,
			status:      types.SubscriptionStatusTrialing,
		})

		// Simulate parent state after processSubscriptionTrialEnd advanced the period.
		newPeriodStart := lo.FromPtr(parent.TrialEnd)
		newPeriodEnd := newPeriodStart.AddDate(0, 1, 0)
		parent.BillingAnchor = newPeriodStart
		parent.CurrentPeriodStart = newPeriodStart
		parent.CurrentPeriodEnd = newPeriodEnd

		s.NoError(s.internalService().cascadeTrialEndToInherited(ctx, parent))

		storedChild, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
		s.Require().NoError(err)
		s.Equal(types.SubscriptionStatusIncomplete, storedChild.SubscriptionStatus)
		s.True(storedChild.BillingAnchor.Equal(newPeriodStart))
		s.True(storedChild.CurrentPeriodStart.Equal(newPeriodStart))
		s.True(storedChild.CurrentPeriodEnd.Equal(newPeriodEnd))
		s.Require().NotNil(storedChild.TrialEnd)
		s.True(storedChild.TrialEnd.Equal(lo.FromPtr(parent.TrialEnd)))
	})
}

func (s *SubscriptionTrialLifecycleSuite) TestCascadeTrialActivationToInherited() {
	ctx := s.GetContext()

	s.Run("non_parent_subscription_is_noop", func() {
		standalone := s.createTrialSub(trialSubOpts{})
		s.NoError(s.internalService().cascadeTrialActivationToInherited(ctx, standalone))
	})

	s.Run("activates_trialing_inherited_children", func() {
		parent := s.createTrialSub(trialSubOpts{subType: types.SubscriptionTypeParent})
		child := s.createTrialSub(trialSubOpts{
			subType:     types.SubscriptionTypeInherited,
			parentSubID: &parent.ID,
			status:      types.SubscriptionStatusTrialing,
		})

		s.NoError(s.internalService().cascadeTrialActivationToInherited(ctx, parent))

		storedChild, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
		s.Require().NoError(err)
		s.Equal(types.SubscriptionStatusActive, storedChild.SubscriptionStatus)
	})
}
