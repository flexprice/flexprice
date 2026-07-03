package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// SubscriptionScheduleServiceSuite tests the subscriptionScheduleService implementation
// in subscription_schedule.go against in-memory stores.
type SubscriptionScheduleServiceSuite struct {
	testutil.BaseServiceTestSuite
	scheduleService SubscriptionScheduleService
	subService      SubscriptionService
	planService     PlanService
	priceService    PriceService
	testData        struct {
		customer    *customer.Customer
		basicPlan   *plan.Plan
		premiumPlan *plan.Plan
		sub         *subscription.Subscription
		now         time.Time
	}
}

func TestSubscriptionScheduleService(t *testing.T) {
	suite.Run(t, new(SubscriptionScheduleServiceSuite))
}

func (s *SubscriptionScheduleServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	params := newTestServiceParams(&s.BaseServiceTestSuite)
	changeService := NewSubscriptionChangeService(params)
	s.scheduleService = NewSubscriptionScheduleService(params, changeService)
	s.subService = NewSubscriptionService(params)
	s.planService = NewPlanService(params)
	s.priceService = NewPriceService(params)
	s.setupTestData()
}

func (s *SubscriptionScheduleServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionScheduleServiceSuite) createPlanWithFixedPrice(name string, amount decimal.Decimal) *plan.Plan {
	ctx := s.GetContext()
	planResp, err := s.planService.CreatePlan(ctx, dto.CreatePlanRequest{
		Name:        name,
		Description: "schedule test plan",
	})
	s.Require().NoError(err)

	amt := amount
	_, err = s.priceService.CreatePrice(ctx, dto.CreatePriceRequest{
		Amount:             &amt,
		Currency:           "usd",
		Type:               types.PRICE_TYPE_FIXED,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           planResp.Plan.ID,
	})
	s.Require().NoError(err)
	return planResp.Plan
}

func (s *SubscriptionScheduleServiceSuite) setupTestData() {
	ctx := s.GetContext()
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "ext_cust_schedule",
		Name:       "Schedule Customer",
		Email:      "schedule@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))

	s.testData.basicPlan = s.createPlanWithFixedPrice("Basic Schedule Plan", decimal.NewFromInt(10))
	s.testData.premiumPlan = s.createPlanWithFixedPrice("Premium Schedule Plan", decimal.NewFromInt(20))

	subResp, err := s.subService.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.basicPlan.ID,
		Currency:           "usd",
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
	})
	s.Require().NoError(err)
	sub, _, err := s.GetStores().SubscriptionRepo.GetWithLineItems(ctx, subResp.Subscription.ID)
	s.Require().NoError(err)
	s.testData.sub = sub
}

// newPlanChangeSchedule creates and persists a plan-change schedule for the given subscription.
func (s *SubscriptionScheduleServiceSuite) newPlanChangeSchedule(subID, targetPlanID string, scheduledAt time.Time, status types.ScheduleStatus) *subscription.SubscriptionSchedule {
	ctx := s.GetContext()
	schedule := &subscription.SubscriptionSchedule{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_SCHEDULE),
		SubscriptionID: subID,
		ScheduleType:   types.SubscriptionScheduleChangeTypePlanChange,
		ScheduledAt:    scheduledAt,
		Status:         status,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		CreatedBy:      types.GetUserID(ctx),
		UpdatedBy:      types.GetUserID(ctx),
		StatusColumn:   types.StatusPublished,
	}
	s.Require().NoError(schedule.SetPlanChangeConfig(&subscription.PlanChangeConfiguration{
		TargetPlanID:       targetPlanID,
		ProrationBehavior:  types.ProrationBehaviorNone,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
	}))
	s.Require().NoError(s.GetStores().SubscriptionScheduleRepo.Create(ctx, schedule))
	return schedule
}

// newCancellationSchedule creates and persists a cancellation schedule with original-state config.
func (s *SubscriptionScheduleServiceSuite) newCancellationSchedule(subID string, scheduledAt time.Time, config *subscription.CancellationConfiguration) *subscription.SubscriptionSchedule {
	ctx := s.GetContext()
	schedule := &subscription.SubscriptionSchedule{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_SCHEDULE),
		SubscriptionID: subID,
		ScheduleType:   types.SubscriptionScheduleChangeTypeCancellation,
		ScheduledAt:    scheduledAt,
		Status:         types.ScheduleStatusPending,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		CreatedBy:      types.GetUserID(ctx),
		UpdatedBy:      types.GetUserID(ctx),
		StatusColumn:   types.StatusPublished,
	}
	s.Require().NoError(schedule.SetCancellationConfig(config))
	s.Require().NoError(s.GetStores().SubscriptionScheduleRepo.Create(ctx, schedule))
	return schedule
}

func (s *SubscriptionScheduleServiceSuite) TestSchedulePlanChange() {
	ctx := s.GetContext()
	config := &subscription.PlanChangeConfiguration{
		TargetPlanID:       s.testData.premiumPlan.ID,
		ProrationBehavior:  types.ProrationBehaviorNone,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
	}

	s.Run("subscription_not_found_returns_error", func() {
		_, err := s.scheduleService.SchedulePlanChange(ctx, "sub_does_not_exist", config)
		s.Error(err)
	})

	s.Run("non_active_subscription_returns_error", func() {
		cancelled := &subscription.Subscription{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
			PlanID:             s.testData.basicPlan.ID,
			CustomerID:         s.testData.customer.ID,
			StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
			CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
			CurrentPeriodEnd:   s.testData.now.Add(6 * 24 * time.Hour),
			BillingAnchor:      s.testData.now.Add(-30 * 24 * time.Hour),
			Currency:           "usd",
			BillingCycle:       types.BillingCycleAnniversary,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			SubscriptionStatus: types.SubscriptionStatusCancelled,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, cancelled))

		_, err := s.scheduleService.SchedulePlanChange(ctx, cancelled.ID, config)
		s.Error(err)
		s.Contains(err.Error(), "active")
	})

	s.Run("pending_plan_change_already_exists_returns_error", func() {
		s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now.Add(24*time.Hour), types.ScheduleStatusPending)

		_, err := s.scheduleService.SchedulePlanChange(ctx, s.testData.sub.ID, config)
		s.Error(err)
	})
}

func (s *SubscriptionScheduleServiceSuite) TestCancelSchedule() {
	ctx := s.GetContext()

	s.Run("schedule_not_found_returns_error", func() {
		err := s.scheduleService.Cancel(ctx, "schd_does_not_exist")
		s.Error(err)
	})

	s.Run("cancels_future_pending_plan_change", func() {
		schedule := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now.Add(24*time.Hour), types.ScheduleStatusPending)

		s.NoError(s.scheduleService.Cancel(ctx, schedule.ID))

		stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.NoError(err)
		s.Equal(types.ScheduleStatusCancelled, stored.Status)
		s.NotNil(stored.CancelledAt)
	})

	s.Run("rejects_already_executed_schedule", func() {
		schedule := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now.Add(24*time.Hour), types.ScheduleStatusExecuted)

		err := s.scheduleService.Cancel(ctx, schedule.ID)
		s.Error(err)
		s.Contains(err.Error(), "cannot be cancelled")
	})

	s.Run("rejects_expired_pending_schedule", func() {
		schedule := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now.Add(-1*time.Hour), types.ScheduleStatusPending)

		err := s.scheduleService.Cancel(ctx, schedule.ID)
		s.Error(err)
	})

	s.Run("restores_subscription_state_for_cancellation_schedule", func() {
		// Build a subscription that has been mutated by a scheduled cancellation.
		originalPeriodEnd := s.testData.now.Add(20 * 24 * time.Hour)
		cancelAt := s.testData.now.Add(10 * 24 * time.Hour)
		sub := &subscription.Subscription{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
			PlanID:             s.testData.basicPlan.ID,
			CustomerID:         s.testData.customer.ID,
			StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
			CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
			CurrentPeriodEnd:   cancelAt, // shortened by the scheduled cancellation
			BillingAnchor:      s.testData.now.Add(-30 * 24 * time.Hour),
			Currency:           "usd",
			BillingCycle:       types.BillingCycleAnniversary,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			SubscriptionStatus: types.SubscriptionStatusActive,
			CancelAtPeriodEnd:  true,
			CancelAt:           &cancelAt,
			EndDate:            &cancelAt,
			BaseModel:          types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

		schedule := s.newCancellationSchedule(sub.ID, s.testData.now.Add(10*24*time.Hour), &subscription.CancellationConfiguration{
			CancellationType:          types.CancellationTypeScheduledDate,
			Reason:                    "customer requested",
			ProrationBehavior:         types.ProrationBehaviorNone,
			OriginalCancelAtPeriodEnd: false,
			OriginalCancelAt:          nil,
			OriginalEndDate:           nil,
			OriginalCurrentPeriodEnd:  &originalPeriodEnd,
		})

		s.NoError(s.scheduleService.Cancel(ctx, schedule.ID))

		storedSchedule, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.NoError(err)
		s.Equal(types.ScheduleStatusCancelled, storedSchedule.Status)
		s.NotNil(storedSchedule.CancelledAt)

		storedSub, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
		s.NoError(err)
		s.False(storedSub.CancelAtPeriodEnd)
		s.Nil(storedSub.CancelAt)
		s.Nil(storedSub.EndDate)
		s.True(storedSub.CurrentPeriodEnd.Equal(originalPeriodEnd))
	})
}

func (s *SubscriptionScheduleServiceSuite) TestCancelBySubscriptionAndType() {
	ctx := s.GetContext()

	s.Run("cancels_pending_schedule_by_type", func() {
		schedule := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now.Add(24*time.Hour), types.ScheduleStatusPending)

		s.NoError(s.scheduleService.CancelBySubscriptionAndType(ctx, s.testData.sub.ID, types.SubscriptionScheduleChangeTypePlanChange))

		stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.NoError(err)
		s.Equal(types.ScheduleStatusCancelled, stored.Status)
	})

	s.Run("errors_when_no_pending_schedule_of_type", func() {
		err := s.scheduleService.CancelBySubscriptionAndType(ctx, s.testData.sub.ID, types.SubscriptionScheduleChangeTypeCancellation)
		s.Error(err)
	})
}

func (s *SubscriptionScheduleServiceSuite) TestCancelPendingForSubscription() {
	ctx := s.GetContext()

	s.Run("returns_nil_when_no_schedules_exist", func() {
		s.NoError(s.scheduleService.CancelPendingForSubscription(ctx, s.testData.sub.ID))
	})

	s.Run("cancels_only_cancellable_pending_schedules", func() {
		pending := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now.Add(24*time.Hour), types.ScheduleStatusPending)
		executed := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now.Add(-24*time.Hour), types.ScheduleStatusExecuted)

		s.NoError(s.scheduleService.CancelPendingForSubscription(ctx, s.testData.sub.ID))

		storedPending, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, pending.ID)
		s.NoError(err)
		s.Equal(types.ScheduleStatusCancelled, storedPending.Status)

		storedExecuted, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, executed.ID)
		s.NoError(err)
		s.Equal(types.ScheduleStatusExecuted, storedExecuted.Status)
	})
}

func (s *SubscriptionScheduleServiceSuite) TestGettersAndList() {
	ctx := s.GetContext()
	schedule := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now.Add(24*time.Hour), types.ScheduleStatusPending)

	s.Run("get_returns_schedule_by_id", func() {
		got, err := s.scheduleService.Get(ctx, schedule.ID)
		s.NoError(err)
		s.Equal(schedule.ID, got.ID)
		s.Equal(types.SubscriptionScheduleChangeTypePlanChange, got.ScheduleType)
	})

	s.Run("get_by_subscription_id_returns_all_schedules", func() {
		got, err := s.scheduleService.GetBySubscriptionID(ctx, s.testData.sub.ID)
		s.NoError(err)
		s.Len(got, 1)
		s.Equal(schedule.ID, got[0].ID)
	})

	s.Run("get_pending_by_subscription_and_type_returns_pending", func() {
		got, err := s.scheduleService.GetPendingBySubscriptionAndType(ctx, s.testData.sub.ID, types.SubscriptionScheduleChangeTypePlanChange)
		s.NoError(err)
		s.NotNil(got)
		s.Equal(schedule.ID, got.ID)
	})

	s.Run("list_filters_by_subscription_id", func() {
		filter := &types.SubscriptionScheduleFilter{
			QueryFilter:     types.NewNoLimitQueryFilter(),
			SubscriptionIDs: []string{s.testData.sub.ID},
		}
		got, err := s.scheduleService.List(ctx, filter)
		s.NoError(err)
		s.Len(got, 1)

		filter.SubscriptionIDs = []string{"sub_other"}
		got, err = s.scheduleService.List(ctx, filter)
		s.NoError(err)
		s.Empty(got)
	})
}

func (s *SubscriptionScheduleServiceSuite) TestMarkAsExecuting() {
	ctx := s.GetContext()

	s.Run("marks_pending_schedule_as_executing", func() {
		schedule := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now.Add(24*time.Hour), types.ScheduleStatusPending)

		s.NoError(s.scheduleService.MarkAsExecuting(ctx, schedule.ID))

		stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.NoError(err)
		s.Equal(types.ScheduleStatusExecuting, stored.Status)
	})

	s.Run("rejects_non_pending_schedule", func() {
		schedule := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now.Add(24*time.Hour), types.ScheduleStatusCancelled)

		err := s.scheduleService.MarkAsExecuting(ctx, schedule.ID)
		s.Error(err)
		s.Contains(err.Error(), "not pending")
	})

	s.Run("schedule_not_found_returns_error", func() {
		s.Error(s.scheduleService.MarkAsExecuting(ctx, "schd_missing"))
	})
}

func (s *SubscriptionScheduleServiceSuite) TestMarkAsExecutedAndFailed() {
	ctx := s.GetContext()

	s.Run("mark_as_executed_stores_plan_change_result", func() {
		schedule := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now.Add(24*time.Hour), types.ScheduleStatusExecuting)

		result := &subscription.PlanChangeResult{
			OldSubscriptionID: s.testData.sub.ID,
			NewSubscriptionID: "sub_new_123",
			ChangeType:        "upgrade",
			EffectiveDate:     s.testData.now,
		}
		s.NoError(s.scheduleService.MarkAsExecuted(ctx, schedule.ID, result))

		stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.NoError(err)
		s.Equal(types.ScheduleStatusExecuted, stored.Status)
		s.NotNil(stored.ExecutedAt)

		storedResult, err := stored.GetPlanChangeResult()
		s.NoError(err)
		s.NotNil(storedResult)
		s.Equal("sub_new_123", storedResult.NewSubscriptionID)
	})

	s.Run("mark_as_executed_with_unexpected_result_type_still_executes", func() {
		schedule := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now.Add(24*time.Hour), types.ScheduleStatusExecuting)

		s.NoError(s.scheduleService.MarkAsExecuted(ctx, schedule.ID, "not_a_plan_change_result"))

		stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.NoError(err)
		s.Equal(types.ScheduleStatusExecuted, stored.Status)
		s.Nil(stored.ExecutionResult)
	})

	s.Run("mark_as_failed_stores_error_message", func() {
		schedule := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now.Add(24*time.Hour), types.ScheduleStatusExecuting)

		s.NoError(s.scheduleService.MarkAsFailed(ctx, schedule.ID, "boom"))

		stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.NoError(err)
		s.Equal(types.ScheduleStatusFailed, stored.Status)
		s.NotNil(stored.ExecutedAt)
		s.Equal("boom", lo.FromPtr(stored.ErrorMessage))
	})

	s.Run("mark_as_failed_not_found_returns_error", func() {
		s.Error(s.scheduleService.MarkAsFailed(ctx, "schd_missing", "boom"))
	})
}

func (s *SubscriptionScheduleServiceSuite) TestExecuteSchedule() {
	ctx := s.GetContext()

	s.Run("executes_plan_change_end_to_end", func() {
		schedule := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now, types.ScheduleStatusPending)

		s.NoError(s.scheduleService.ExecuteSchedule(ctx, schedule.ID))

		stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.NoError(err)
		s.Equal(types.ScheduleStatusExecuted, stored.Status)
		s.NotNil(stored.ExecutedAt)

		result, err := stored.GetPlanChangeResult()
		s.NoError(err)
		s.Require().NotNil(result)
		s.Equal(s.testData.sub.ID, result.OldSubscriptionID)
		s.NotEqual(s.testData.sub.ID, result.NewSubscriptionID)

		// Old subscription is archived, new subscription is on the target plan.
		oldSub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.testData.sub.ID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusCancelled, oldSub.SubscriptionStatus)

		newSub, err := s.GetStores().SubscriptionRepo.Get(ctx, result.NewSubscriptionID)
		s.NoError(err)
		s.Equal(types.SubscriptionStatusActive, newSub.SubscriptionStatus)
		s.Equal(s.testData.premiumPlan.ID, newSub.PlanID)
	})

	s.Run("fails_when_subscription_already_on_target_plan", func() {
		// Fresh subscription; schedule targets the plan it is already on.
		subResp, err := s.subService.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
			CustomerID:         s.testData.customer.ID,
			PlanID:             s.testData.basicPlan.ID,
			Currency:           "usd",
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingCycle:       types.BillingCycleAnniversary,
		})
		s.Require().NoError(err)

		schedule := s.newPlanChangeSchedule(subResp.Subscription.ID, s.testData.basicPlan.ID, s.testData.now, types.ScheduleStatusPending)

		err = s.scheduleService.ExecuteSchedule(ctx, schedule.ID)
		s.Error(err)
		s.Contains(err.Error(), "already on target plan")

		stored, gerr := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.NoError(gerr)
		s.Equal(types.ScheduleStatusFailed, stored.Status)
		s.NotNil(stored.ErrorMessage)
	})

	s.Run("fails_when_subscription_scheduled_for_cancellation", func() {
		subResp, err := s.subService.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
			CustomerID:         s.testData.customer.ID,
			PlanID:             s.testData.basicPlan.ID,
			Currency:           "usd",
			BillingCadence:     types.BILLING_CADENCE_RECURRING,
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			BillingCycle:       types.BillingCycleAnniversary,
		})
		s.Require().NoError(err)

		sub, gerr := s.GetStores().SubscriptionRepo.Get(ctx, subResp.Subscription.ID)
		s.Require().NoError(gerr)
		sub.CancelAtPeriodEnd = true
		s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

		schedule := s.newPlanChangeSchedule(sub.ID, s.testData.premiumPlan.ID, s.testData.now, types.ScheduleStatusPending)

		err = s.scheduleService.ExecuteSchedule(ctx, schedule.ID)
		s.Error(err)
		s.Contains(err.Error(), "cancelled or scheduled for cancellation")

		stored, gerr := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.NoError(gerr)
		s.Equal(types.ScheduleStatusFailed, stored.Status)
	})

	s.Run("fails_for_unsupported_schedule_type", func() {
		schedule := s.newCancellationSchedule(s.testData.sub.ID, s.testData.now.Add(24*time.Hour), &subscription.CancellationConfiguration{
			CancellationType:  types.CancellationTypeEndOfPeriod,
			ProrationBehavior: types.ProrationBehaviorNone,
		})

		err := s.scheduleService.ExecuteSchedule(ctx, schedule.ID)
		s.Error(err)
		s.Contains(err.Error(), "unsupported schedule type")

		stored, gerr := s.GetStores().SubscriptionScheduleRepo.Get(ctx, schedule.ID)
		s.NoError(gerr)
		s.Equal(types.ScheduleStatusFailed, stored.Status)
	})

	s.Run("rejects_non_pending_schedule", func() {
		schedule := s.newPlanChangeSchedule(s.testData.sub.ID, s.testData.premiumPlan.ID, s.testData.now, types.ScheduleStatusExecuted)
		s.Error(s.scheduleService.ExecuteSchedule(ctx, schedule.ID))
	})
}
