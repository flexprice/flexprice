package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto" // pragma: allowlist secret
	"github.com/flexprice/flexprice/internal/domain/subscription" // pragma: allowlist secret
	ierr "github.com/flexprice/flexprice/internal/errors" // pragma: allowlist secret
	"github.com/flexprice/flexprice/internal/types" // pragma: allowlist secret
	"github.com/samber/lo"
)

// validateEndDateRequest checks eligibility and date rules for extending a subscription end date.
// Returns (isNoOp, error). isNoOp is true when new_end_date equals the current end (same UTC instant).
func (s *subscriptionModificationService) validateEndDateRequest(
	sub *subscription.Subscription,
	subscriptionID string,
	params *dto.SubModifyEndDateRequest,
) (bool, error) {
	switch sub.SubscriptionStatus {
	case types.SubscriptionStatusActive, types.SubscriptionStatusTrialing, types.SubscriptionStatusPaused:
		// allowed
	default:
		return false, ierr.NewError("cannot extend end date on subscription with status " + string(sub.SubscriptionStatus)).
			WithHint("Only active, trialing, or paused subscriptions can have their end date extended").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
				"status":          sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	if sub.SubscriptionType == types.SubscriptionTypeInherited {
		return false, ierr.NewError("cannot modify end date on inherited subscription").
			WithHint("Modify the parent subscription's end date instead").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID}).
			Mark(ierr.ErrValidation)
	}

	if sub.EndDate == nil {
		return false, ierr.NewError("subscription has no end_date to extend").
			WithHint("Extending an indefinite subscription is not supported; set an end_date at create time first").
			WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID}).
			Mark(ierr.ErrValidation)
	}

	newEnd := params.NewEndDate.UTC()
	oldEnd := sub.EndDate.UTC()
	now := time.Now().UTC()

	if newEnd.Equal(oldEnd) {
		return true, nil
	}

	if !newEnd.After(oldEnd) {
		return false, ierr.NewError("new_end_date must be after the current subscription end date").
			WithHint("This API only extends the term; use cancellation to shorten").
			WithReportableDetails(map[string]interface{}{
				"new_end_date":          newEnd,
				"subscription_end_date": oldEnd,
			}).
			Mark(ierr.ErrValidation)
	}

	if !newEnd.After(now) {
		return false, ierr.NewError("new_end_date must be in the future").
			WithHint("Provide a new_end_date strictly after the current UTC time").
			WithReportableDetails(map[string]interface{}{
				"new_end_date": newEnd,
				"now":          now,
			}).
			Mark(ierr.ErrValidation)
	}

	if sub.SubscriptionStatus == types.SubscriptionStatusTrialing && sub.TrialEnd != nil {
		if newEnd.Before(sub.TrialEnd.UTC()) {
			return false, ierr.NewError("new_end_date cannot be before trial_end").
				WithHint("The subscription term must cover the full trial period").
				WithReportableDetails(map[string]interface{}{
					"new_end_date": newEnd,
					"trial_end":    sub.TrialEnd.UTC(),
				}).
				Mark(ierr.ErrValidation)
		}
	}

	if sub.CancelAt != nil {
		cancelAt := sub.CancelAt.UTC()
		if !newEnd.After(cancelAt) {
			return false, ierr.NewError("new_end_date must be after the scheduled cancellation date").
				WithHint("Clear or cancel the scheduled cancellation first, or extend past cancel_at").
				WithReportableDetails(map[string]interface{}{
					"new_end_date": newEnd,
					"cancel_at":    cancelAt,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return false, nil
}

// selectLineItemsToExtend returns line items whose end should move to newEnd per the ERD §6 rules.
// When cancelBoundary is set (scheduled cancellation unwind), items terminated exactly at that
// boundary are also selected so they can be re-extended past the cancelled cancel_at.
func selectLineItemsToExtend(
	items []*subscription.SubscriptionLineItem,
	oldEnd time.Time,
	cancelBoundary *time.Time,
) []*subscription.SubscriptionLineItem {
	selected := make([]*subscription.SubscriptionLineItem, 0)
	for _, item := range items {
		if item == nil {
			continue
		}
		if item.Status != types.StatusPublished {
			continue
		}
		if item.IsOneTime() {
			continue
		}
		if item.EndDate.IsZero() || item.EndDate.Equal(oldEnd) {
			selected = append(selected, item)
			continue
		}
		if cancelBoundary != nil && item.EndDate.Equal(*cancelBoundary) {
			selected = append(selected, item)
			continue
		}
		// Earlier override / historical qty-change segment / earlier phase — skip.
	}
	return selected
}

// recomputePeriodEndAfterExtend recomputes CurrentPeriodEnd when it was clipped to the old subscription end.
func recomputePeriodEndAfterExtend(sub *subscription.Subscription, newEnd time.Time) error {
	oldEnd := lo.FromPtr(sub.EndDate)
	if !sub.CurrentPeriodEnd.Equal(oldEnd) {
		return nil
	}
	next, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart:  sub.CurrentPeriodStart,
		BillingAnchor:       sub.BillingAnchor,
		Unit:                sub.BillingPeriodCount,
		Period:              sub.BillingPeriod,
		SubscriptionEndDate: &newEnd,
		Timezone:            sub.Timezone,
	})
	if err != nil {
		return err
	}
	sub.CurrentPeriodEnd = next
	return nil
}

// ─────────────────────────────────────────────
// Execute: end date
// ─────────────────────────────────────────────

func (s *subscriptionModificationService) executeEndDate(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyEndDateRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	sub, lineItems, err := sp.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	isNoOp, err := s.validateEndDateRequest(sub, subscriptionID, params)
	if err != nil {
		return nil, err
	}

	subSvc := NewSubscriptionService(sp)
	if isNoOp {
		subResp, getErr := subSvc.GetSubscription(ctx, subscriptionID)
		if getErr != nil {
			return nil, getErr
		}
		return &dto.SubscriptionModifyResponse{
			Subscription:     subResp,
			ChangedResources: dto.ChangedResources{},
		}, nil
	}

	oldEnd := sub.EndDate.UTC()
	newEnd := params.NewEndDate.UTC()
	cancellationUnwind := false

	var (
		changedSubs    []dto.ChangedSubscription
		changedLIs     []dto.ChangedLineItem
		changedGrants  []dto.ChangedCreditGrant
		periodEndAfter *time.Time
	)

	err = sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		changedSubs = nil
		changedLIs = nil
		changedGrants = nil
		periodEndAfter = nil

		// Optimistic concurrency: re-read and ensure end has not changed mid-flight.
		fresh, freshLIs, getErr := sp.SubRepo.GetWithLineItems(txCtx, subscriptionID)
		if getErr != nil {
			return getErr
		}
		if fresh.EndDate == nil || !fresh.EndDate.UTC().Equal(oldEnd) {
			return ierr.NewError("subscription end_date changed concurrently").
				WithHint("Retry the end date extension").
				WithReportableDetails(map[string]interface{}{"subscription_id": subscriptionID}).
				Mark(ierr.ErrValidation)
		}
		sub = fresh
		lineItems = freshLIs

		var cancelBoundaryInTx *time.Time
		shouldUnwind := fresh.CancelAt != nil && newEnd.After(fresh.CancelAt.UTC())
		if shouldUnwind {
			cancelBoundaryInTx = lo.ToPtr(fresh.CancelAt.UTC())
			if err := s.unwindScheduledCancellationForExtend(txCtx, sub); err != nil {
				return err
			}
		}

		if err := recomputePeriodEndAfterExtend(sub, newEnd); err != nil {
			return err
		}
		sub.EndDate = lo.ToPtr(newEnd)
		if err := sp.SubRepo.Update(txCtx, sub); err != nil {
			return err
		}
		periodEndAfter = lo.ToPtr(sub.CurrentPeriodEnd)

		if err := s.extendLastPhaseMatchingEnd(txCtx, subscriptionID, oldEnd, newEnd); err != nil {
			return err
		}

		selected := selectLineItemsToExtend(lineItems, oldEnd, cancelBoundaryInTx)
		for _, li := range selected {
			li.EndDate = newEnd
			if err := sp.SubscriptionLineItemRepo.Update(txCtx, li); err != nil {
				return err
			}
			start := li.StartDate
			end := newEnd
			changedLIs = append(changedLIs, dto.ChangedLineItem{
				ID:           li.ID,
				PriceID:      li.PriceID,
				Quantity:     li.Quantity,
				StartDate:    &start,
				EndDate:      &end,
				ChangeAction: dto.ChangedLineItemActionUpdated,
			})
		}

		if err := s.extendAddonAssociationsMatchingEnd(txCtx, subscriptionID, oldEnd, newEnd, cancelBoundaryInTx); err != nil {
			return err
		}

		grants, err := s.extendCreditGrantsMatchingEnd(txCtx, subscriptionID, oldEnd, newEnd)
		if err != nil {
			return err
		}
		changedGrants = grants

		changedSubs = append(changedSubs, dto.ChangedSubscription{
			ID:               sub.ID,
			Action:           dto.ChangedSubscriptionActionUpdated,
			Status:           sub.SubscriptionStatus,
			EndDate:          lo.ToPtr(newEnd),
			CurrentPeriodEnd: periodEndAfter,
		})

		childSubs, childLIs, childGrants, err := s.cascadeEndDateToInherited(txCtx, sub, oldEnd, newEnd, shouldUnwind, cancelBoundaryInTx)
		if err != nil {
			return err
		}
		changedSubs = append(changedSubs, childSubs...)
		changedLIs = append(changedLIs, childLIs...)
		changedGrants = append(changedGrants, childGrants...)
		cancellationUnwind = shouldUnwind
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	sp.Logger.Info(ctx, "extended subscription end date",
		"subscription_id", subscriptionID,
		"old_end_date", oldEnd,
		"new_end_date", newEnd,
		"line_items_extended", len(changedLIs),
		"credit_grants_extended", len(changedGrants),
		"cancellation_unwound", cancellationUnwind,
	)

	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
			LineItems:     changedLIs,
			CreditGrants:  changedGrants,
		},
	}, nil
}

// ─────────────────────────────────────────────
// Preview: end date
// ─────────────────────────────────────────────

func (s *subscriptionModificationService) previewEndDate(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyEndDateRequest,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	sub, lineItems, err := sp.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	isNoOp, err := s.validateEndDateRequest(sub, subscriptionID, params)
	if err != nil {
		return nil, err
	}

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	if isNoOp {
		return &dto.SubscriptionModifyResponse{
			Subscription:     subResp,
			ChangedResources: dto.ChangedResources{},
		}, nil
	}

	oldEnd := sub.EndDate.UTC()
	newEnd := params.NewEndDate.UTC()

	// Project period end without mutating the loaded sub permanently for response.
	projectedPeriodEnd := sub.CurrentPeriodEnd
	if sub.CurrentPeriodEnd.Equal(oldEnd) {
		next, err := types.NextBillingDate(&types.NextBillingDateParams{
			CurrentPeriodStart:  sub.CurrentPeriodStart,
			BillingAnchor:       sub.BillingAnchor,
			Unit:                sub.BillingPeriodCount,
			Period:              sub.BillingPeriod,
			SubscriptionEndDate: &newEnd,
			Timezone:            sub.Timezone,
		})
		if err != nil {
			return nil, err
		}
		projectedPeriodEnd = next
	}

	changedSubs := []dto.ChangedSubscription{{
		ID:               sub.ID,
		Action:           dto.ChangedSubscriptionActionUpdated,
		Status:           sub.SubscriptionStatus,
		EndDate:          lo.ToPtr(newEnd),
		CurrentPeriodEnd: lo.ToPtr(projectedPeriodEnd),
	}}

	changedLIs := make([]dto.ChangedLineItem, 0)
	var cancelBoundary *time.Time
	if sub.CancelAt != nil && newEnd.After(sub.CancelAt.UTC()) {
		cancelBoundary = lo.ToPtr(sub.CancelAt.UTC())
	}
	for _, li := range selectLineItemsToExtend(lineItems, oldEnd, cancelBoundary) {
		start := li.StartDate
		end := newEnd
		changedLIs = append(changedLIs, dto.ChangedLineItem{
			ID:           li.ID,
			PriceID:      li.PriceID,
			Quantity:     li.Quantity,
			StartDate:    &start,
			EndDate:      &end,
			ChangeAction: dto.ChangedLineItemActionUpdated,
		})
	}

	changedGrants, err := s.previewCreditGrantsMatchingEnd(ctx, subscriptionID, oldEnd, newEnd)
	if err != nil {
		return nil, err
	}

	if sub.SubscriptionType == types.SubscriptionTypeParent {
		children, err := s.getInheritedSubscriptions(ctx, sub.ID)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			changedSubs = append(changedSubs, dto.ChangedSubscription{
				ID:               child.ID,
				Action:           dto.ChangedSubscriptionActionUpdated,
				Status:           child.SubscriptionStatus,
				EndDate:          lo.ToPtr(newEnd),
				CurrentPeriodEnd: lo.ToPtr(projectedPeriodEnd),
			})
			childLIs, err := sp.SubscriptionLineItemRepo.ListBySubscription(ctx, child)
			if err != nil {
				return nil, err
			}
			for _, li := range selectLineItemsToExtend(childLIs, oldEnd, cancelBoundary) {
				start := li.StartDate
				end := newEnd
				changedLIs = append(changedLIs, dto.ChangedLineItem{
					ID:           li.ID,
					PriceID:      li.PriceID,
					Quantity:     li.Quantity,
					StartDate:    &start,
					EndDate:      &end,
					ChangeAction: dto.ChangedLineItemActionUpdated,
				})
			}
			childGrants, err := s.previewCreditGrantsMatchingEnd(ctx, child.ID, oldEnd, newEnd)
			if err != nil {
				return nil, err
			}
			changedGrants = append(changedGrants, childGrants...)
		}
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			Subscriptions: changedSubs,
			LineItems:     changedLIs,
			CreditGrants:  changedGrants,
		},
	}, nil
}

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

func (s *subscriptionModificationService) unwindScheduledCancellationForExtend(
	ctx context.Context,
	sub *subscription.Subscription,
) error {
	sp := s.serviceParams

	pending, err := sp.SubScheduleRepo.GetPendingBySubscriptionAndType(
		ctx, sub.ID, types.SubscriptionScheduleChangeTypeCancellation,
	)
	if err != nil && !ierr.IsNotFound(err) {
		return err
	}
	if pending != nil && pending.CanBeCancelled() {
		now := time.Now().UTC()
		pending.Status = types.ScheduleStatusCancelled
		pending.CancelledAt = &now
		pending.UpdatedAt = now
		pending.UpdatedBy = types.GetUserID(ctx)
		if err := sp.SubScheduleRepo.Update(ctx, pending); err != nil {
			return err
		}
	}

	sub.CancelAt = nil
	sub.CancelAtPeriodEnd = false
	sub.CancelledAt = nil
	return nil
}

func (s *subscriptionModificationService) extendLastPhaseMatchingEnd(
	ctx context.Context,
	subscriptionID string,
	oldEnd, newEnd time.Time,
) error {
	sp := s.serviceParams
	if sp.SubscriptionPhaseRepo == nil {
		return nil
	}

	filter := types.NewNoLimitSubscriptionPhaseFilter()
	filter.SubscriptionIDs = []string{subscriptionID}
	phases, err := sp.SubscriptionPhaseRepo.List(ctx, filter)
	if err != nil {
		return err
	}
	if len(phases) == 0 {
		return nil
	}

	var last *subscription.SubscriptionPhase
	for _, phase := range phases {
		if phase.EndDate != nil && phase.EndDate.UTC().Equal(oldEnd) {
			if last == nil || phase.StartDate.After(last.StartDate) {
				last = phase
			}
		}
	}
	if last == nil {
		return nil
	}
	last.EndDate = lo.ToPtr(newEnd)
	return sp.SubscriptionPhaseRepo.Update(ctx, last)
}

func (s *subscriptionModificationService) extendAddonAssociationsMatchingEnd(
	ctx context.Context,
	subscriptionID string,
	oldEnd, newEnd time.Time,
	cancelBoundary *time.Time,
) error {
	sp := s.serviceParams
	if sp.AddonAssociationRepo == nil {
		return nil
	}

	filter := types.NewNoLimitAddonAssociationFilter()
	entityType := types.AddonAssociationEntityTypeSubscription
	filter.EntityType = &entityType
	filter.EntityIDs = []string{subscriptionID}
	assocs, err := sp.AddonAssociationRepo.List(ctx, filter)
	if err != nil {
		return err
	}
	for _, assoc := range assocs {
		if assoc == nil || assoc.EndDate == nil {
			continue
		}
		end := assoc.EndDate.UTC()
		match := end.Equal(oldEnd) || (cancelBoundary != nil && end.Equal(*cancelBoundary))
		if !match {
			continue
		}
		assoc.EndDate = lo.ToPtr(newEnd)
		if err := sp.AddonAssociationRepo.Update(ctx, assoc); err != nil {
			return err
		}
	}
	return nil
}

func (s *subscriptionModificationService) extendCreditGrantsMatchingEnd(
	ctx context.Context,
	subscriptionID string,
	oldEnd, newEnd time.Time,
) ([]dto.ChangedCreditGrant, error) {
	sp := s.serviceParams
	if sp.CreditGrantRepo == nil {
		return nil, nil
	}

	filter := types.NewNoLimitCreditGrantFilter().WithSubscriptionIDs([]string{subscriptionID})
	scope := types.CreditGrantScopeSubscription
	filter.Scope = &scope
	grants, err := sp.CreditGrantRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	changed := make([]dto.ChangedCreditGrant, 0)
	for _, grant := range grants {
		if grant == nil || grant.EndDate == nil {
			continue
		}
		if !grant.EndDate.UTC().Equal(oldEnd) {
			continue
		}
		grant.EndDate = lo.ToPtr(newEnd)
		updated, err := sp.CreditGrantRepo.Update(ctx, grant)
		if err != nil {
			return nil, err
		}
		changed = append(changed, dto.ChangedCreditGrant{
			ID:          updated.ID,
			Action:      dto.ChangedCreditGrantActionUpdated,
			EndDate:     lo.ToPtr(newEnd),
			CreditGrant: dto.FromCreditGrant(updated),
		})
	}
	return changed, nil
}

func (s *subscriptionModificationService) previewCreditGrantsMatchingEnd(
	ctx context.Context,
	subscriptionID string,
	oldEnd, newEnd time.Time,
) ([]dto.ChangedCreditGrant, error) {
	sp := s.serviceParams
	if sp.CreditGrantRepo == nil {
		return nil, nil
	}

	filter := types.NewNoLimitCreditGrantFilter().WithSubscriptionIDs([]string{subscriptionID})
	scope := types.CreditGrantScopeSubscription
	filter.Scope = &scope
	grants, err := sp.CreditGrantRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	changed := make([]dto.ChangedCreditGrant, 0)
	for _, grant := range grants {
		if grant == nil || grant.EndDate == nil {
			continue
		}
		if !grant.EndDate.UTC().Equal(oldEnd) {
			continue
		}
		preview := *grant
		preview.EndDate = lo.ToPtr(newEnd)
		changed = append(changed, dto.ChangedCreditGrant{
			ID:          grant.ID,
			Action:      dto.ChangedCreditGrantActionUpdated,
			EndDate:     lo.ToPtr(newEnd),
			CreditGrant: dto.FromCreditGrant(&preview),
		})
	}
	return changed, nil
}

func (s *subscriptionModificationService) cascadeEndDateToInherited(
	ctx context.Context,
	parent *subscription.Subscription,
	oldEnd, newEnd time.Time,
	unwindCancel bool,
	cancelBoundary *time.Time,
) ([]dto.ChangedSubscription, []dto.ChangedLineItem, []dto.ChangedCreditGrant, error) {
	if parent.SubscriptionType != types.SubscriptionTypeParent {
		return nil, nil, nil, nil
	}

	sp := s.serviceParams
	children, err := s.getInheritedSubscriptions(ctx, parent.ID)
	if err != nil {
		return nil, nil, nil, err
	}

	changedSubs := make([]dto.ChangedSubscription, 0, len(children))
	changedLIs := make([]dto.ChangedLineItem, 0)
	changedGrants := make([]dto.ChangedCreditGrant, 0)

	for _, child := range children {
		if unwindCancel {
			if err := s.unwindScheduledCancellationForExtend(ctx, child); err != nil {
				return nil, nil, nil, err
			}
		}

		if err := recomputePeriodEndAfterExtend(child, newEnd); err != nil {
			return nil, nil, nil, err
		}
		child.EndDate = lo.ToPtr(newEnd)
		if err := sp.SubRepo.Update(ctx, child); err != nil {
			return nil, nil, nil, err
		}

		changedSubs = append(changedSubs, dto.ChangedSubscription{
			ID:               child.ID,
			Action:           dto.ChangedSubscriptionActionUpdated,
			Status:           child.SubscriptionStatus,
			EndDate:          lo.ToPtr(newEnd),
			CurrentPeriodEnd: lo.ToPtr(child.CurrentPeriodEnd),
		})

		childLIs, err := sp.SubscriptionLineItemRepo.ListBySubscription(ctx, child)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, li := range selectLineItemsToExtend(childLIs, oldEnd, cancelBoundary) {
			li.EndDate = newEnd
			if err := sp.SubscriptionLineItemRepo.Update(ctx, li); err != nil {
				return nil, nil, nil, err
			}
			start := li.StartDate
			end := newEnd
			changedLIs = append(changedLIs, dto.ChangedLineItem{
				ID:           li.ID,
				PriceID:      li.PriceID,
				Quantity:     li.Quantity,
				StartDate:    &start,
				EndDate:      &end,
				ChangeAction: dto.ChangedLineItemActionUpdated,
			})
		}

		if err := s.extendAddonAssociationsMatchingEnd(ctx, child.ID, oldEnd, newEnd, cancelBoundary); err != nil {
			return nil, nil, nil, err
		}
		if err := s.extendLastPhaseMatchingEnd(ctx, child.ID, oldEnd, newEnd); err != nil {
			return nil, nil, nil, err
		}

		grants, err := s.extendCreditGrantsMatchingEnd(ctx, child.ID, oldEnd, newEnd)
		if err != nil {
			return nil, nil, nil, err
		}
		changedGrants = append(changedGrants, grants...)
	}

	return changedSubs, changedLIs, changedGrants, nil
}
