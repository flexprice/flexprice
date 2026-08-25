package service

import (
	"encoding/json"
	"strings"
	"time"

	cockroachErrors "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// errorDetails reads an ierr error's reportable details the way the HTTP error handler
// does (middleware/errhandler.go), so assertions see the payload a client receives.
func (s *SubscriptionChangeV2Suite) errorDetails(err error) map[string]any {
	const jsonDetailsPrefix = "__json__:"
	for _, sdp := range cockroachErrors.GetAllSafeDetails(err) {
		for _, payload := range sdp.SafeDetails {
			if !strings.HasPrefix(payload, jsonDetailsPrefix) {
				continue
			}
			var details map[string]any
			if json.Unmarshal([]byte(payload[len(jsonDetailsPrefix):]), &details) == nil {
				return details
			}
		}
	}
	s.Failf("no reportable details", "error carried no JSON details: %v", err)
	return nil
}

func (s *SubscriptionChangeV2Suite) withSupersede(req dto.SubscriptionChangeV2Request) dto.SubscriptionChangeV2Request {
	req.OnConflictPolicies = &dto.SubscriptionChangeConflictPolicies{
		OnPendingSchedule: types.OnPendingSchedulePolicySupersede,
	}
	return req
}

func (s *SubscriptionChangeV2Suite) scheduleByID(id string) *subscription.SubscriptionSchedule {
	schedules, err := s.GetStores().SubscriptionScheduleRepo.GetBySubscriptionID(s.GetContext(), s.td.sub.ID)
	s.Require().NoError(err)
	for _, sched := range schedules {
		if sched.ID == id {
			return sched
		}
	}

	// Callers dereference the result, so a miss must fail the assertion rather than
	// panic the whole suite.
	s.Require().Failf("schedule not found", "no schedule with id %s on subscription %s", id, s.td.sub.ID)
	return nil
}

// An immediate change replaces the queued one instead of forcing a cancel-then-change.
func (s *SubscriptionChangeV2Suite) TestExecute_SupersedeReplacesPendingScheduleWithImmediateChange() {
	ctx := s.GetContext()

	scheduled, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)
	s.Require().NotNil(scheduled.ScheduleID)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.withSupersede(s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)), time.Now().UTC())
	s.Require().NoError(err)

	s.Equal([]string{*scheduled.ScheduleID}, resp.SupersededSchedules)
	s.Equal(s.td.pro.ID, s.currentSub().PlanID, "the immediate change applied")
	s.Nil(s.pendingPlanChange(), "no plan change is queued any more")

	cancelled := s.scheduleByID(*scheduled.ScheduleID)
	s.Require().NotNil(cancelled)
	s.Equal(types.ScheduleStatusCancelled, cancelled.Status)
	s.Require().NotNil(cancelled.CancelledAt, "the row records when it was cancelled")
}

// Replacing a queued change with another queued change is the same operation.
func (s *SubscriptionChangeV2Suite) TestExecute_SupersedeReplacesPendingScheduleWithDeferredChange() {
	ctx := s.GetContext()

	first, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)
	s.Require().NotNil(first.ScheduleID)

	lateral := s.createPlan("Lateral", "lateral")
	s.createFixedPrice(lateral.ID, "base_fee", 20)

	second, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.withSupersede(s.deferredRequest(lateral.ID)), time.Now().UTC())
	s.Require().NoError(err)

	s.Equal([]string{*first.ScheduleID}, second.SupersededSchedules)
	s.Require().NotNil(second.ScheduleID)
	s.NotEqual(*first.ScheduleID, *second.ScheduleID)

	pending := s.pendingPlanChange()
	s.Require().NotNil(pending)
	s.Equal(*second.ScheduleID, pending.ID, "exactly one schedule is pending, and it is the new one")

	config, err := pending.GetPlanChangeV2Config()
	s.Require().NoError(err)
	s.Equal(lateral.ID, config.TargetPlanID)

	s.Equal(types.ScheduleStatusCancelled, s.scheduleByID(*first.ScheduleID).Status)
	s.Equal(s.td.starter.ID, s.currentSub().PlanID, "a deferred change still swaps nothing now")
}

// The default is unchanged: reject, naming the schedule and the field that would allow it.
func (s *SubscriptionChangeV2Suite) TestExecute_RejectIsTheDefaultAndNamesTheEscapeHatch() {
	ctx := s.GetContext()

	scheduled, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	for _, req := range []dto.SubscriptionChangeV2Request{
		s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone),
		{
			TargetPlanID:      s.td.pro.ID,
			ProrationBehavior: types.ProrationBehaviorNone,
			OnConflictPolicies: &dto.SubscriptionChangeConflictPolicies{
				OnPendingSchedule: types.OnPendingSchedulePolicyReject,
			},
		},
	} {
		_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, req, time.Now().UTC())
		s.Require().Error(err)
		s.True(ierr.IsValidation(err))

		details := s.errorDetails(err)
		s.Equal(*scheduled.ScheduleID, details["schedule_id"])
		s.Equal("on_conflict_policies.on_pending_schedule", details["policy_field"])
		s.Equal(types.OnPendingSchedulePolicySupersede.String(), details["policy_value"])
		s.NotNil(details["scheduled_at"])
	}

	s.Equal(s.td.starter.ID, s.currentSub().PlanID)
	s.Require().NotNil(s.pendingPlanChange(), "a rejected request leaves the queued change alone")
}

func (s *SubscriptionChangeV2Suite) TestPreview_ReportsWhatExecuteWouldSupersede() {
	ctx := s.GetContext()

	scheduled, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	preview, err := s.svc.PreviewPlanChange(ctx, s.td.sub.ID,
		s.withSupersede(s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)))
	s.Require().NoError(err)
	s.Equal([]string{*scheduled.ScheduleID}, preview.SupersededSchedules)

	pending := s.pendingPlanChange()
	s.Require().NotNil(pending, "preview writes nothing")
	s.Equal(types.ScheduleStatusPending, pending.Status)

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.withSupersede(s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)), time.Now().UTC())
	s.Require().NoError(err)
	s.Equal(preview.SupersededSchedules, resp.SupersededSchedules, "preview quoted what execute did")
}

// Nothing to supersede is not an error, and the response must not claim otherwise.
func (s *SubscriptionChangeV2Suite) TestExecute_SupersedeWithoutAPendingScheduleReportsNothing() {
	ctx := s.GetContext()

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.withSupersede(s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)), time.Now().UTC())
	s.Require().NoError(err)

	s.Empty(resp.SupersededSchedules)
	s.Equal(s.td.pro.ID, s.currentSub().PlanID)
}

// The executor must still ignore its own pending row rather than superseding it.
func (s *SubscriptionChangeV2Suite) TestExecuteScheduledV2_DoesNotSupersedeItsOwnPendingRow() {
	ctx := s.GetContext()

	resp, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	sched := s.pendingPlanChange()
	s.Require().NotNil(sched)
	config, err := sched.GetPlanChangeV2Config()
	s.Require().NoError(err)

	s.rollPeriodTo(*resp.ScheduledAt, resp.ScheduledAt.AddDate(0, 0, 30))
	s.Require().NoError(s.svc.ExecuteScheduledPlanChangeV2(ctx, sched, config, s.currentSub()))

	s.Equal(s.td.pro.ID, s.currentSub().PlanID)
	s.Equal(types.ScheduleStatusExecuted, s.scheduleByID(sched.ID).Status,
		"the schedule executed rather than being cancelled by its own execution")
}

// The policy covers queued plan changes only. A pending cancellation means un-cancelling
// the subscription, which is a different decision and stays a hard rejection.
func (s *SubscriptionChangeV2Suite) TestExecute_SupersedeDoesNotClearAPendingCancellation() {
	ctx := s.GetContext()

	sub := s.currentSub()
	sub.CancelAtPeriodEnd = true
	sub.CancelAt = lo.ToPtr(s.td.periodEnd)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID,
		s.withSupersede(s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)), time.Now().UTC())
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
	s.Contains(err.Error(), "pending cancellation")

	s.Equal(s.td.starter.ID, s.currentSub().PlanID)
	s.True(s.currentSub().CancelAtPeriodEnd, "the cancellation is untouched")
}

func (s *SubscriptionChangeV2Suite) TestExecute_RejectsAnUnknownConflictPolicy() {
	ctx := s.GetContext()

	req := s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)
	req.OnConflictPolicies = &dto.SubscriptionChangeConflictPolicies{OnPendingSchedule: "queue_after"}

	_, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, req, time.Now().UTC())
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
	s.Equal(s.td.starter.ID, s.currentSub().PlanID)
}

// Preview is only useful as a rehearsal if it refuses on the same grounds execute will.
func (s *SubscriptionChangeV2Suite) TestPreview_RejectsTheConflictExecuteWouldReject() {
	ctx := s.GetContext()

	scheduled, err := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, s.deferredRequest(s.td.pro.ID), time.Now().UTC())
	s.Require().NoError(err)

	req := s.changeRequest(s.td.pro.ID, types.ProrationBehaviorNone)

	_, previewErr := s.svc.PreviewPlanChange(ctx, s.td.sub.ID, req)
	s.Require().Error(previewErr)
	s.True(ierr.IsValidation(previewErr))

	_, executeErr := s.svc.ExecutePlanChange(ctx, s.td.sub.ID, req, time.Now().UTC())
	s.Require().Error(executeErr)
	s.Equal(executeErr.Error(), previewErr.Error(), "both refuse for the same stated reason")

	details := s.errorDetails(previewErr)
	s.Equal(*scheduled.ScheduleID, details["schedule_id"])
	s.Equal("on_conflict_policies.on_pending_schedule", details["policy_field"])

	pending := s.pendingPlanChange()
	s.Require().NotNil(pending, "a refused preview leaves the queued change alone")
	s.Equal(types.ScheduleStatusPending, pending.Status)
}
