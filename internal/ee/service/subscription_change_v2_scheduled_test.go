package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// failingSubRepo turns the locking read into a database error, the shape a
// transient failure takes at execution time.
type failingSubRepo struct {
	subscription.Repository
}

func (r *failingSubRepo) GetForUpdate(ctx context.Context, id string) (*subscription.Subscription, error) {
	return nil, ierr.NewError("database unavailable").Mark(ierr.ErrDatabase)
}

func (s *SubscriptionChangeV2Suite) createV2Schedule(
	targetPlanID string,
	scheduledAt time.Time,
) (*subscription.SubscriptionSchedule, *subscription.PlanChangeV2Configuration) {
	ctx := s.GetContext()

	sched := &subscription.SubscriptionSchedule{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_SCHEDULE),
		SubscriptionID: s.td.sub.ID,
		ScheduleType:   types.SubscriptionScheduleChangeTypePlanChange,
		ScheduledAt:    scheduledAt,
		Status:         types.ScheduleStatusPending,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		StatusColumn:   types.StatusPublished,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	s.Require().NoError(sched.SetPlanChangeV2Config(&subscription.PlanChangeV2Configuration{
		TargetPlanID: targetPlanID,
	}))
	s.Require().NoError(s.GetStores().SubscriptionScheduleRepo.Create(ctx, sched))

	config, err := sched.GetPlanChangeV2Config()
	s.Require().NoError(err)
	s.Require().True(config.IsV2())

	return sched, config
}

// The period roll advances CurrentPeriodStart to the boundary before the plan change
// runs, so an on-time schedule sits exactly on it, not behind it.
func (s *SubscriptionChangeV2Suite) rollPeriodTo(periodStart, periodEnd time.Time) {
	ctx := s.GetContext()
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.td.sub.ID)
	s.Require().NoError(err)
	sub.CurrentPeriodStart = periodStart
	sub.CurrentPeriodEnd = periodEnd
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))
}

// The boundary arithmetic in CalculateFixedCharges only skips the old plan's advance
// charge when the closing item ends exactly at the period end, so the executor must
// use schedule.ScheduledAt rather than time.Now().
func (s *SubscriptionChangeV2Suite) TestExecuteScheduledV2_UsesScheduledAtNotNow() {
	ctx := s.GetContext()
	boundary := s.td.periodEnd

	sched, config := s.createV2Schedule(s.td.pro.ID, boundary)
	s.rollPeriodTo(boundary, boundary.AddDate(0, 0, 30))

	s.Require().NoError(s.svc.ExecuteScheduledPlanChangeV2(ctx, sched, config, s.currentSub()))

	closed, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, s.td.baseLine.ID)
	s.Require().NoError(err)
	s.True(closed.EndDate.Equal(boundary),
		"closing item must end exactly at the boundary, got %v want %v", closed.EndDate, boundary)

	live := s.liveLineItems()
	s.Require().Len(live, 1)
	s.Equal(s.td.proBase.ID, live[0].PriceID)
	s.True(live[0].StartDate.Equal(boundary),
		"opening item must start exactly at the boundary, got %v want %v", live[0].StartDate, boundary)

	s.True(boundary.After(time.Now().UTC()),
		"the boundary must be in the future, otherwise this test cannot distinguish ScheduledAt from now")
}

func (s *SubscriptionChangeV2Suite) currentSub() *subscription.Subscription {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.td.sub.ID)
	s.Require().NoError(err)
	return sub
}

func (s *SubscriptionChangeV2Suite) TestExecuteScheduledV2_MarksExecutedAndRecordsResult() {
	ctx := s.GetContext()
	boundary := s.td.periodEnd

	sched, config := s.createV2Schedule(s.td.pro.ID, boundary)
	s.rollPeriodTo(boundary, boundary.AddDate(0, 0, 30))

	s.Require().NoError(s.svc.ExecuteScheduledPlanChangeV2(ctx, sched, config, s.currentSub()))

	stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, sched.ID)
	s.Require().NoError(err)
	s.Equal(types.ScheduleStatusExecuted, stored.Status)
	s.Require().NotNil(stored.ExecutedAt)
	s.Nil(stored.ErrorMessage)

	result, err := stored.GetPlanChangeV2Result()
	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Equal(s.td.sub.ID, result.SubscriptionID)
	s.Equal(s.td.starter.ID, result.FromPlanID)
	s.Equal(s.td.pro.ID, result.ToPlanID)
	s.Equal(string(types.SubscriptionChangeTypeUpgrade), result.ChangeType)
	s.True(result.EffectiveDate.Equal(boundary),
		"the recorded effective date is the boundary, not the execution time")
}

// A schedule whose boundary is already invoiced must not execute into that closed
// period; it fails loudly instead of end-dating line items behind existing drafts.
func (s *SubscriptionChangeV2Suite) TestExecuteScheduledV2_StaleScheduleFailsWithoutExecuting() {
	ctx := s.GetContext()
	missedBoundary := s.td.periodEnd

	sched, config := s.createV2Schedule(s.td.pro.ID, missedBoundary)

	// A catch-up roll jumped a whole period past the boundary this schedule targets.
	s.rollPeriodTo(missedBoundary.AddDate(0, 0, 30), missedBoundary.AddDate(0, 0, 60))

	err := s.svc.ExecuteScheduledPlanChangeV2(ctx, sched, config, s.currentSub())
	s.Require().Error(err)
	s.Contains(err.Error(), "missed its billing period boundary")

	stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, sched.ID)
	s.Require().NoError(err)
	s.Equal(types.ScheduleStatusFailed, stored.Status)
	s.Require().NotNil(stored.ErrorMessage)

	s.Equal(s.td.starter.ID, s.currentSub().PlanID, "a stale schedule must not swap the plan")
	live := s.liveLineItems()
	s.Require().Len(live, 1)
	s.Equal(s.td.starterBase.ID, live[0].PriceID, "line items must be untouched")
}

// An on-time schedule sits exactly on CurrentPeriodStart, so the staleness guard
// must not fire on the healthy path.
func (s *SubscriptionChangeV2Suite) TestExecuteScheduledV2_OnTimeScheduleIsNotStale() {
	boundary := s.td.periodEnd
	sched, _ := s.createV2Schedule(s.td.pro.ID, boundary)
	s.rollPeriodTo(boundary, boundary.AddDate(0, 0, 30))

	s.False(sched.IsStaleFor(s.currentSub()),
		"scheduled_at == current_period_start is on time, not stale")
}

// A request that can never succeed is terminal; the row must not be retried forever.
func (s *SubscriptionChangeV2Suite) TestExecuteScheduledV2_ValidationFailureMarksFailed() {
	ctx := s.GetContext()

	// Target plan == current plan fails checkPlanChangePreconditions.
	sched, config := s.createV2Schedule(s.td.starter.ID, s.td.periodEnd)
	s.rollPeriodTo(s.td.periodEnd, s.td.periodEnd.AddDate(0, 0, 30))

	err := s.svc.ExecuteScheduledPlanChangeV2(ctx, sched, config, s.currentSub())
	s.Require().Error(err)
	s.True(IsTerminalPlanChangeError(err))

	stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, sched.ID)
	s.Require().NoError(err)
	s.Equal(types.ScheduleStatusFailed, stored.Status)
	s.Require().NotNil(stored.ErrorMessage)
	s.Contains(*stored.ErrorMessage, "already on the target plan")
	s.Equal(s.td.starter.ID, s.currentSub().PlanID)
}

// A target plan that no longer exists cannot succeed on a retry, so it is terminal
// even though it is not a validation error.
func (s *SubscriptionChangeV2Suite) TestExecuteScheduledV2_MissingTargetPlanMarksFailed() {
	ctx := s.GetContext()

	sched, config := s.createV2Schedule("plan_does_not_exist", s.td.periodEnd)
	s.rollPeriodTo(s.td.periodEnd, s.td.periodEnd.AddDate(0, 0, 30))

	err := s.svc.ExecuteScheduledPlanChangeV2(ctx, sched, config, s.currentSub())
	s.Require().Error(err)
	s.True(IsTerminalPlanChangeError(err))

	stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, sched.ID)
	s.Require().NoError(err)
	s.Equal(types.ScheduleStatusFailed, stored.Status)
	s.Require().NotNil(stored.ErrorMessage)
}

// A transient failure must leave the row pending: nothing resurrects a failed schedule,
// so marking it failed on a blip would strand the change permanently.
func (s *SubscriptionChangeV2Suite) TestExecuteScheduledV2_TransientFailureLeavesPending() {
	ctx := s.GetContext()

	sched, config := s.createV2Schedule(s.td.pro.ID, s.td.periodEnd)
	s.rollPeriodTo(s.td.periodEnd, s.td.periodEnd.AddDate(0, 0, 30))
	sub := s.currentSub()

	params := s.serviceParams()
	params.SubRepo = &failingSubRepo{Repository: params.SubRepo}
	failingSvc := NewSubscriptionService(params)

	err := failingSvc.ExecuteScheduledPlanChangeV2(ctx, sched, config, sub)
	s.Require().Error(err)
	s.False(IsTerminalPlanChangeError(err))

	stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, sched.ID)
	s.Require().NoError(err)
	s.Equal(types.ScheduleStatusPending, stored.Status,
		"a transient failure must stay pending so another attempt can run")
	s.Nil(stored.ExecutedAt)
	s.Nil(stored.ErrorMessage)
	s.Equal(s.td.starter.ID, s.currentSub().PlanID)
}

// The dispatch guard rests on this: a v1 blob must never take the v2 path.
func (s *SubscriptionChangeV2Suite) TestScheduleDispatch_V1ConfigIsNotV2() {
	ctx := s.GetContext()

	sched := &subscription.SubscriptionSchedule{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_SCHEDULE),
		SubscriptionID: s.td.sub.ID,
		ScheduleType:   types.SubscriptionScheduleChangeTypePlanChange,
		ScheduledAt:    s.td.periodEnd,
		Status:         types.ScheduleStatusPending,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		StatusColumn:   types.StatusPublished,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	s.Require().NoError(sched.SetPlanChangeConfig(&subscription.PlanChangeConfiguration{
		TargetPlanID:       s.td.pro.ID,
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
	}))
	s.Require().NoError(s.GetStores().SubscriptionScheduleRepo.Create(ctx, sched))

	config, err := sched.GetPlanChangeV2Config()
	s.Require().NoError(err, "a v1 blob still decodes into the v2 struct")
	s.False(config.IsV2(), "a v1 blob must dispatch to the v1 executor")
}

// A carried line is repointed to the new plan's price in place and never end-dated,
// so the boundary invoice prices the closed period with the new PriceID. That is
// harmless only because carrying requires BillsIdenticallyTo, which pins the money.
func (s *SubscriptionChangeV2Suite) TestExecuteScheduledV2_CarriedLineBillsIdentically() {
	ctx := s.GetContext()

	lateral := s.createPlan("Lateral", "lateral")
	lateralBase := s.createFixedPrice(lateral.ID, "base_fee", 20)

	boundary := s.td.periodEnd
	sched, config := s.createV2Schedule(lateral.ID, boundary)
	s.rollPeriodTo(boundary, boundary.AddDate(0, 0, 30))

	s.Require().NoError(s.svc.ExecuteScheduledPlanChangeV2(ctx, sched, config, s.currentSub()))

	live := s.liveLineItems()
	s.Require().Len(live, 1)
	s.Equal(s.td.baseLine.ID, live[0].ID, "the line is carried, not replaced")
	s.Equal(lateralBase.ID, live[0].PriceID, "the carried line now points at the new plan's price")
	s.True(live[0].EndDate.IsZero(), "a carried line is never end-dated at the boundary")

	s.True(s.td.starterBase.BillsIdenticallyTo(lateralBase),
		"carrying is only permitted when the two prices bill identically, so the arrear "+
			"amount for the closed period is unchanged by the swap")
}

// elapsePeriodAndSchedule puts the subscription in the state the period scanner finds
// it in — a period that closed in the past — with a v2 change due at that boundary.
func (s *SubscriptionChangeV2Suite) elapsePeriodAndSchedule(
	targetPlanID string,
) (*subscription.Subscription, *subscription.SubscriptionSchedule) {
	ctx := s.GetContext()

	elapsedStart := time.Now().UTC().Truncate(time.Hour).AddDate(0, 0, -40)
	elapsedEnd := elapsedStart.AddDate(0, 0, 30)

	sub := s.currentSub()
	sub.CurrentPeriodStart = elapsedStart
	sub.CurrentPeriodEnd = elapsedEnd
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	sched := &subscription.SubscriptionSchedule{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_SCHEDULE),
		SubscriptionID: sub.ID,
		ScheduleType:   types.SubscriptionScheduleChangeTypePlanChange,
		ScheduledAt:    elapsedEnd,
		Status:         types.ScheduleStatusPending,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		StatusColumn:   types.StatusPublished,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	s.Require().NoError(sched.SetPlanChangeV2Config(&subscription.PlanChangeV2Configuration{
		TargetPlanID: targetPlanID,
	}))
	s.Require().NoError(s.GetStores().SubscriptionScheduleRepo.Create(ctx, sched))

	return sub, sched
}

// processSubscriptionPeriod's invoice loop computes AND finalizes inline, and a
// finalized invoice makes a later recompute a no-op. So the v2 swap has to land
// before the loop: with the v1 hook's position the plan still ends up swapped, but
// the renewal was already billed on the old plan and cannot be corrected.
func (s *SubscriptionChangeV2Suite) TestProcessSubscriptionPeriod_V2RunsBeforeInvoicing() {
	ctx := s.GetContext()
	sub, sched := s.elapsePeriodAndSchedule(s.td.pro.ID)

	svc, ok := s.svc.(*subscriptionService)
	s.Require().True(ok)
	s.Require().NoError(svc.processSubscriptionPeriod(ctx, sub, time.Now().UTC()))

	s.Equal(s.td.pro.ID, s.currentSub().PlanID)

	stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, sched.ID)
	s.Require().NoError(err)
	s.Equal(types.ScheduleStatusExecuted, stored.Status)

	filter := types.NewInvoiceFilter()
	filter.SubscriptionID = sub.ID
	invoices, err := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Require().Len(invoices, 1, "one boundary invoice for the closed period")

	boundary := invoices[0]
	s.Require().Len(boundary.LineItems, 1)
	advance := boundary.LineItems[0]

	s.Equal(s.td.proBase.ID, lo.FromPtr(advance.PriceID),
		"the advance charge for the next period must be priced on the new plan; "+
			"the old plan's price here means the swap landed after the invoice was finalized")
	s.True(advance.Amount.Equal(decimal.NewFromInt(50)))
	s.True(advance.PeriodStart.Equal(sched.ScheduledAt),
		"the advance window opens exactly at the boundary the schedule targeted")
}

// v1 keeps its original position; only v2 moved ahead of the invoice loop.
func (s *SubscriptionChangeV2Suite) TestProcessSubscriptionPeriod_V1SchedulePositionUnchanged() {
	ctx := s.GetContext()
	sub, sched := s.elapsePeriodAndSchedule(s.td.pro.ID)

	// Overwrite with a v1 configuration, leaving everything else identical.
	s.Require().NoError(sched.SetPlanChangeConfig(&subscription.PlanChangeConfiguration{
		TargetPlanID:       s.td.pro.ID,
		ProrationBehavior:  types.ProrationBehaviorNone,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
	}))
	s.Require().NoError(s.GetStores().SubscriptionScheduleRepo.Update(ctx, sched))

	config, err := sched.GetPlanChangeV2Config()
	s.Require().NoError(err)
	s.Require().False(config.IsV2(), "this row must not be picked up by the v2 pass")

	svc, ok := s.svc.(*subscriptionService)
	s.Require().True(ok)
	s.Require().NoError(svc.processPendingPlanChangeV2(ctx, sub),
		"the v2 pass is a no-op for a v1 row, leaving it to the original hook")

	s.Equal(s.td.starter.ID, s.currentSub().PlanID, "the v2 pass must not swap a v1 schedule")

	stored, err := s.GetStores().SubscriptionScheduleRepo.Get(ctx, sched.ID)
	s.Require().NoError(err)
	s.Equal(types.ScheduleStatusPending, stored.Status, "still pending for the v1 hook")
}
