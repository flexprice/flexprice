package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *SubscriptionChangeV2Suite) deferredRequest(targetPlanID string) dto.SubscriptionChangeV2Request {
	return dto.SubscriptionChangeV2Request{
		TargetPlanID:      targetPlanID,
		ProrationBehavior: types.ProrationBehaviorNone,
		ChangeAt:          lo.ToPtr(types.ScheduleTypePeriodEnd),
	}
}

func (s *SubscriptionChangeV2Suite) pendingPlanChange() *subscription.SubscriptionSchedule {
	sched, err := s.GetStores().SubscriptionScheduleRepo.GetPendingBySubscriptionAndType(
		s.GetContext(), s.td.sub.ID, types.SubscriptionScheduleChangeTypePlanChange,
	)
	s.Require().NoError(err)
	return sched
}

func (s *SubscriptionChangeV2Suite) TestExecute_DeferredSchedulesInsteadOfSwapping() {
	ctx := s.GetContext()

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	s.True(resp.IsScheduled)
	s.Require().NotNil(resp.ScheduleID)
	s.Require().NotNil(resp.ScheduledAt)
	s.True(resp.ScheduledAt.Equal(s.td.periodEnd), "scheduled at the current period end")
	s.True(resp.EffectiveAt.Equal(s.td.periodEnd), "effective_at is the boundary, not now")
	s.Equal(types.SubscriptionChangeTypeUpgrade, resp.ChangeType,
		"the change type is resolved now, not left blank until execution")

	s.Equal(s.td.starter.ID, s.currentSub().PlanID, "nothing swaps yet")
	live := s.liveLineItems()
	s.Require().Len(live, 1)
	s.Equal(s.td.starterBase.ID, live[0].PriceID)

	sched := s.pendingPlanChange()
	s.Require().NotNil(sched)
	s.Equal(*resp.ScheduleID, sched.ID)
	s.True(sched.ScheduledAt.Equal(s.td.periodEnd))

	config, err := sched.GetPlanChangeV2Config()
	s.Require().NoError(err)
	s.True(config.IsV2(), "the row must carry a v2 configuration")
	s.Equal(s.td.pro.ID, config.TargetPlanID)
}

func (s *SubscriptionChangeV2Suite) TestExecute_DeferredCreatesNoInvoices() {
	ctx := s.GetContext()
	before := s.countInvoices()

	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	s.Equal(before, s.countInvoices(), "a deferred change settles nothing at request time")
}

func (s *SubscriptionChangeV2Suite) TestExecute_SecondDeferredRequestIsRejected() {
	ctx := s.GetContext()

	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	lateral := s.createPlan("Lateral", "lateral")
	s.createFixedPrice(lateral.ID, "base_fee", 20)

	_, err = s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(lateral.ID), time.Now().UTC())
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
	s.Contains(err.Error(), "already scheduled")
}

// The guard covers the immediate path too: applying a change now while another is
// queued would leave the pending row pointing at a plan the subscription already left.
func (s *SubscriptionChangeV2Suite) TestExecute_ImmediateRejectedWhileScheduleIsPending() {
	ctx := s.GetContext()

	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	_, err = s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone), time.Now().UTC())
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
	s.Equal(s.td.starter.ID, s.currentSub().PlanID)
}

// The scheduled executor resolves the change while its own pending row exists, so the
// guard must not be reachable from that path.
func (s *SubscriptionChangeV2Suite) TestExecuteScheduledV2_NotBlockedByItsOwnPendingRow() {
	ctx := s.GetContext()

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	sched := s.pendingPlanChange()
	s.Require().NotNil(sched)
	config, err := sched.GetPlanChangeV2Config()
	s.Require().NoError(err)

	s.rollPeriodTo(*resp.ScheduledAt, resp.ScheduledAt.AddDate(0, 0, 30))

	s.Require().NoError(s.svc.ExecuteScheduledPlanChangeV2(ctx, sched, config, s.currentSub()),
		"the executor must not be blocked by the very schedule it is executing")
	s.Equal(s.td.pro.ID, s.currentSub().PlanID)
}

func (s *SubscriptionChangeV2Suite) TestExecute_DeferredRejectsInvalidRequests() {
	ctx := s.GetContext()

	proration := s.deferredRequest(s.td.pro.ID)
	proration.ProrationBehavior = types.ProrationBehaviorCreateProrations
	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, proration, time.Now().UTC())
	s.Require().Error(err)
	s.Contains(err.Error(), "proration is not applicable")
	s.Nil(s.pendingPlanChange(), "a rejected request must not leave a schedule behind")

	// resolvePlanChange runs before the row is written, so a bad target fails now
	// rather than silently at the boundary weeks later.
	_, err = s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.starter.ID), time.Now().UTC())
	s.Require().Error(err)
	s.Contains(err.Error(), "already on the target plan")
	s.Nil(s.pendingPlanChange())

	_, err = s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest("plan_does_not_exist"), time.Now().UTC())
	s.Require().Error(err)
	s.Nil(s.pendingPlanChange())
}

func (s *SubscriptionChangeV2Suite) TestPreview_DeferredPricesAtTheBoundary() {
	ctx := s.GetContext()

	resp, err := s.svc.PreviewPlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.pro.ID))
	s.Require().NoError(err)

	s.True(resp.EffectiveAt.Equal(s.td.periodEnd),
		"preview must report the instant it actually priced, not now")
	s.Empty(resp.ChangedResources.Invoices, "proration none quotes no money")

	// Mirrors what execute would return, without the write: the schedule id is the one
	// thing preview cannot know, because the row does not exist yet.
	s.True(resp.IsScheduled, "a deferred request is reported as scheduled")
	s.Require().NotNil(resp.ScheduledAt)
	s.True(resp.ScheduledAt.Equal(s.td.periodEnd))
	s.Nil(resp.ScheduleID)
	s.Equal(s.td.starter.ID, resp.Subscription.PlanID, "nothing has moved off the current plan yet")
	s.Nil(s.pendingPlanChange(), "preview writes nothing")

	for _, item := range resp.ChangedResources.LineItems {
		switch item.ChangeAction {
		case dto.ChangedLineItemActionEnded:
			s.Require().NotNil(item.EndDate)
			s.True(item.EndDate.Equal(s.td.periodEnd), "closing items end at the boundary")
		case dto.ChangedLineItemActionCreated:
			s.Require().NotNil(item.StartDate)
			s.True(item.StartDate.Equal(s.td.periodEnd), "opening items start at the boundary")
		}
	}
}

// A backlogged subscription resolves to a boundary already in the past, so the change
// is due immediately. Surfacing the resolved instant is what makes that visible.
func (s *SubscriptionChangeV2Suite) TestExecute_DeferredOnBackloggedSubscriptionIsImmediatelyDue() {
	ctx := s.GetContext()

	pastEnd := time.Now().UTC().Truncate(time.Hour).AddDate(0, 0, -10)
	sub := s.currentSub()
	sub.CurrentPeriodStart = pastEnd.AddDate(0, 0, -30)
	sub.CurrentPeriodEnd = pastEnd
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	s.True(resp.IsScheduled)
	s.Require().NotNil(resp.ScheduledAt)
	s.True(resp.ScheduledAt.Equal(pastEnd))
	s.True(resp.ScheduledAt.Before(time.Now().UTC()),
		"the caller can see the change is already due, not a period away")
}

func (s *SubscriptionChangeV2Suite) TestExecute_ImmediateUnaffectedByChangeAtImmediate() {
	ctx := s.GetContext()

	req := s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)
	req.ChangeAt = lo.ToPtr(types.ScheduleTypeImmediate)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, req, time.Now().UTC())
	s.Require().NoError(err)

	s.False(resp.IsScheduled)
	s.Nil(resp.ScheduleID)
	s.Equal(s.td.pro.ID, s.currentSub().PlanID, "explicit immediate still swaps now")
	s.Nil(s.pendingPlanChange())
}
