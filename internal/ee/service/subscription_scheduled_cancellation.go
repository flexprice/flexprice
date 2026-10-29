package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/activity"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *subscriptionService) CompleteScheduledCancellation(ctx context.Context, id string, periodEnd time.Time) (bool, error) {
	if !commitmentTermsDate(periodEnd) {
		return false, ierr.Validation("cancellation requires a finite period end")
	}
	if err := s.validateSubscriptionLifecycle(ctx, id); err != nil {
		return false, err
	}
	nested := s.DB.TxFromContext(ctx) != nil || s.BillingTermsStore.InTransaction(ctx)
	var cancelled bool
	err := s.DB.WithTx(types.WithForceWriter(ctx), func(tx context.Context) error {
		lifecycle, err := s.lockSubscriptionLifecycle(tx, id, nested)
		if err != nil {
			return err
		}
		tx = lifecycle.ctx
		current := lifecycle.current
		if current.SubscriptionStatus == types.SubscriptionStatusCancelled {
			cancelled = true
			return nil
		}
		effectiveAt := scheduledCancellationDate(current, periodEnd, time.Now().UTC())
		if effectiveAt == nil {
			return nil
		}
		updated := subscription.NewSubscriptionBuilder(current).
			WithStatus(types.SubscriptionStatusCancelled).WithCancelledAt(effectiveAt).WithEndDate(effectiveAt).
			WithPauseStatus(types.PauseStatusNone).WithActivePauseID(nil).Build()
		if current.CancelAtPeriodEnd && current.CancelAt != nil {
			if err := s.TerminateSubscriptionResources(tx, dto.TerminateSubscriptionResourcesRequest{
				SubscriptionID: id, EffectiveDate: *effectiveAt, CancellationReason: current.Metadata["cancellation_reason"],
			}); err != nil {
				return err
			}
		}
		if err := s.settleTerminalBillingTerms(tx, updated); err != nil {
			return err
		}
		for _, participant := range append([]*subscription.Subscription{current}, lifecycle.children...) {
			if err := s.completeCancellationPause(tx, participant); err != nil {
				return err
			}
		}
		if err := s.SubRepo.Update(activity.WithAction(tx, id, activity.SubscriptionCancelled), updated); err != nil {
			return err
		}
		if err := s.cascadeCancellation(tx, updated, lifecycle.children); err != nil {
			return err
		}
		if err := s.MarkCancellationScheduleAsExecuted(tx, id); err != nil {
			return err
		}
		notify := func() {
			detached := types.WithoutDBTransaction(context.WithoutCancel(tx))
			detached, cancel := context.WithTimeout(detached, 10*time.Second)
			defer cancel()
			s.PublishCancellationEvents(detached, updated)
			NotifySubscriptionChanged(detached, s.ServiceParams, id)
		}
		if !types.RegisterPostCommit(tx, notify) {
			return ierr.NewError("cancellation requires an active postcommit queue").Mark(ierr.ErrInvalidOperation)
		}
		cancelled = true
		return nil
	})
	return cancelled && err == nil, err
}

func scheduledCancellationDate(sub *subscription.Subscription, periodEnd, now time.Time) *time.Time {
	var effectiveAt *time.Time
	if sub.CancelAtPeriodEnd && sub.CancelAt != nil && !sub.CancelAt.After(periodEnd) && !sub.CancelAt.After(now) {
		effectiveAt = sub.CancelAt
	}
	if sub.EndDate != nil && sub.EndDate.Equal(periodEnd) && !sub.EndDate.After(now) && (effectiveAt == nil || sub.EndDate.Before(*effectiveAt)) {
		effectiveAt = sub.EndDate
	}
	return effectiveAt
}

func (s *subscriptionService) completeCancellationPause(ctx context.Context, sub *subscription.Subscription) error {
	if sub.SubscriptionStatus != types.SubscriptionStatusPaused || sub.ActivePauseID == nil {
		return nil
	}
	pause, err := s.SubRepo.GetPause(ctx, *sub.ActivePauseID)
	if err != nil {
		return err
	}
	if pause == nil || pause.ID != *sub.ActivePauseID || pause.SubscriptionID != sub.ID || pause.TenantID != types.GetTenantID(ctx) || pause.EnvironmentID != types.GetEnvironmentID(ctx) {
		return ierr.NewError("subscription pause returned invalid scope").Mark(ierr.ErrDatabase)
	}
	completed := subscription.NewSubscriptionPauseBuilder(pause).WithStatus(types.PauseStatusCompleted).WithUpdatedBy(types.GetUserID(ctx)).Build()
	return s.SubRepo.UpdatePause(ctx, completed)
}
