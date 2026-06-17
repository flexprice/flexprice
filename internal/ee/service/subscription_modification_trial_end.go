package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// validateTrialEndRequest validates the subscription state and, for scheduled_date actions,
// the new trial end date. Combines subscription-level and date-level checks in one pass.
func (s *subscriptionModificationService) validateTrialEndRequest(
	sub *subscription.Subscription,
	subscriptionID string,
	params *dto.SubModifyTrialEndRequest,
) error {
	if sub.SubscriptionStatus != types.SubscriptionStatusTrialing {
		return ierr.NewError("subscription is not in trialing status").
			WithHint("Only trialing subscriptions can have their trial period modified").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID, "status": sub.SubscriptionStatus}).
			Mark(ierr.ErrValidation)
	}
	if sub.TrialStart == nil || sub.TrialEnd == nil {
		return ierr.NewError("subscription is missing trial bounds").
			WithHint("Subscription must have both trial_start and trial_end set").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID}).
			Mark(ierr.ErrValidation)
	}
	if sub.SubscriptionType == types.SubscriptionTypeInherited {
		return ierr.NewError("cannot modify trial on inherited subscription").
			WithHint("Modify the parent subscription's trial instead").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID}).
			Mark(ierr.ErrValidation)
	}

	// Date-specific validation only applies to scheduled_date action.
	if params.Action == dto.TrialEndActionScheduledDate {
		newTrialEnd := params.NewTrialEnd.UTC()
		now := time.Now().UTC()
		if !newTrialEnd.After(now) {
			return ierr.NewError("new_trial_end must be in the future").
				WithHint("To end the trial immediately, use action 'immediate'").
				WithReportableDetails(map[string]interface{}{"new_trial_end": newTrialEnd, "now": now}).
				Mark(ierr.ErrValidation)
		}
		if sub.EndDate != nil && newTrialEnd.After(lo.FromPtr(sub.EndDate)) {
			return ierr.NewError("new_trial_end cannot be after the subscription end date").
				WithReportableDetails(map[string]interface{}{
					"new_trial_end":         newTrialEnd,
					"subscription_end_date": lo.FromPtr(sub.EndDate),
				}).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// ─────────────────────────────────────────────
// Execute: trial end
// ─────────────────────────────────────────────

func (s *subscriptionModificationService) executeTrialEnd(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyTrialEndRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	if err := s.validateTrialEndRequest(sub, subscriptionID, params); err != nil {
		return nil, err
	}

	switch params.Action {
	case dto.TrialEndActionImmediate:
		return s.executeTrialEndNow(ctx, sub)
	case dto.TrialEndActionScheduledDate:
		return s.executeTrialEndModifyDate(ctx, sub, params.NewTrialEnd.UTC())
	default:
		return nil, ierr.NewError("unknown trial end action: " + string(params.Action)).
			Mark(ierr.ErrValidation)
	}
}

// executeTrialEndNow ends the trial immediately by delegating to the existing
// processSubscriptionTrialEnd logic (now exposed via ProcessSingleSubscriptionTrialEnd).
func (s *subscriptionModificationService) executeTrialEndNow(
	ctx context.Context,
	sub *subscription.Subscription,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	subSvc := NewSubscriptionService(sp)
	now := time.Now().UTC()
	sub.TrialEnd = lo.ToPtr(now)
	invRes, err := subSvc.ProcessSingleSubscriptionTrialEnd(ctx, sub, now)
	if err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, sub.ID)

	changedSubs := []dto.ChangedSubscription{{
		ID:               sub.ID,
		Action:           dto.ChangedSubscriptionActionUpdated,
		Status:           types.SubscriptionStatusIncomplete,
		TrialEnd:         lo.ToPtr(now),
		CurrentPeriodEnd: lo.ToPtr(now),
	}}

	changedInvoice := []dto.ChangedInvoice{
		{
			ID:      invRes.ID,
			Action:  dto.ChangedInvoiceActionCreated,
			Invoice: invRes,
		},
	}
	return &dto.SubscriptionModifyResponse{
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
			Invoices:      changedInvoice,
		},
	}, nil
}

// executeTrialEndModifyDate extends or reduces the trial end date.
func (s *subscriptionModificationService) executeTrialEndModifyDate(
	ctx context.Context,
	sub *subscription.Subscription,
	newTrialEnd time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	sub.TrialEnd = lo.ToPtr(newTrialEnd)
	// During trialing, CurrentPeriodEnd is aligned with TrialEnd.
	sub.CurrentPeriodEnd = newTrialEnd

	err := sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := sp.SubRepo.Update(txCtx, sub); err != nil {
			return err
		}
		return s.cascadeTrialEndModifyToInherited(txCtx, sub, newTrialEnd)
	})
	if err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, sub.ID)

	changedSubs := []dto.ChangedSubscription{{
		ID:               sub.ID,
		Action:           dto.ChangedSubscriptionActionUpdated,
		Status:           sub.SubscriptionStatus,
		TrialEnd:         lo.ToPtr(newTrialEnd),
		CurrentPeriodEnd: lo.ToPtr(newTrialEnd),
	}}

	return &dto.SubscriptionModifyResponse{
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
		},
	}, nil
}

// ─────────────────────────────────────────────
// Preview: trial end
// ─────────────────────────────────────────────

func (s *subscriptionModificationService) previewTrialEnd(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyTrialEndRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	if err := s.validateTrialEndRequest(sub, subscriptionID, params); err != nil {
		return nil, err
	}

	status := types.SubscriptionStatusIncomplete
	endDate := time.Now().UTC()
	if params.Action == dto.TrialEndActionScheduledDate {
		status = types.SubscriptionStatusTrialing
		endDate = params.NewTrialEnd.UTC()
	}

	changedSubs := []dto.ChangedSubscription{{
		ID:               sub.ID,
		Action:           dto.ChangedSubscriptionActionUpdated,
		Status:           status,
		TrialEnd:         lo.ToPtr(endDate),
		CurrentPeriodEnd: lo.ToPtr(endDate),
	}}

	// For inherited children, show them as preview-updated too.
	if sub.SubscriptionType == types.SubscriptionTypeParent {
		children, err := s.getInheritedSubscriptions(ctx, sub.ID)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			changedSubs = append(changedSubs, dto.ChangedSubscription{
				ID:               child.ID,
				Action:           dto.ChangedSubscriptionActionUpdated,
				Status:           status,
				TrialEnd:         lo.ToPtr(endDate),
				CurrentPeriodEnd: lo.ToPtr(endDate),
			})
		}
	}

	return &dto.SubscriptionModifyResponse{
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
		},
	}, nil
}

// ─────────────────────────────────────────────
// Cascade helpers
// ─────────────────────────────────────────────

// cascadeTrialEndModifyToInherited propagates the updated trial end date to inherited children.
func (s *subscriptionModificationService) cascadeTrialEndModifyToInherited(ctx context.Context, parentSub *subscription.Subscription, newTrialEnd time.Time) error {
	if parentSub.SubscriptionType != types.SubscriptionTypeParent {
		return nil
	}
	children, err := s.getInheritedSubscriptions(ctx, parentSub.ID)
	if err != nil {
		return err
	}
	for _, child := range children {
		child.TrialEnd = lo.ToPtr(newTrialEnd)
		child.CurrentPeriodEnd = newTrialEnd
		if err := s.serviceParams.SubRepo.Update(ctx, child); err != nil {
			return err
		}
	}
	return nil
}
