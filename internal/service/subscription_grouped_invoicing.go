package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// validateAddToGroupedInvoicing checks all 9 constraints required before a child subscription
// can be attached to a parent for grouped invoicing. It performs no writes.
func (s *subscriptionService) validateAddToGroupedInvoicing(
	ctx context.Context,
	parentSub *subscription.Subscription,
	child *subscription.Subscription,
) error {
	// 1. child must be standalone
	if child.SubscriptionType != types.SubscriptionTypeStandalone {
		return ierr.NewError("child subscription must be standalone").
			WithHint("Only standalone subscriptions can be added to grouped invoicing").
			WithReportableDetails(map[string]any{
				"child_subscription_id":   child.ID,
				"child_subscription_type": child.SubscriptionType,
			}).
			Mark(ierr.ErrValidation)
	}

	// 2. child status must be active or trialing
	if child.SubscriptionStatus != types.SubscriptionStatusActive &&
		child.SubscriptionStatus != types.SubscriptionStatusTrialing {
		return ierr.NewError("child subscription must be active or trialing").
			WithHint("Only active or trialing subscriptions can be added to grouped invoicing").
			WithReportableDetails(map[string]any{
				"child_subscription_id":     child.ID,
				"child_subscription_status": child.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// 3. child must not already have a parent
	if child.ParentSubscriptionID != nil {
		return ierr.NewError("child subscription already has a parent").
			WithHint("Subscription is already attached to a parent; detach it first").
			WithReportableDetails(map[string]any{
				"child_subscription_id": child.ID,
				"current_parent_sub_id": *child.ParentSubscriptionID,
			}).
			Mark(ierr.ErrValidation)
	}

	// 4. parent must have type "parent"
	if parentSub.SubscriptionType != types.SubscriptionTypeParent {
		return ierr.NewError("parent subscription must have type 'parent'").
			WithHint("The target parent subscription does not have the correct type").
			WithReportableDetails(map[string]any{
				"parent_subscription_id":   parentSub.ID,
				"parent_subscription_type": parentSub.SubscriptionType,
			}).
			Mark(ierr.ErrValidation)
	}

	// 5. parent status must be active or trialing
	if parentSub.SubscriptionStatus != types.SubscriptionStatusActive &&
		parentSub.SubscriptionStatus != types.SubscriptionStatusTrialing {
		return ierr.NewError("parent subscription must be active or trialing").
			WithHint("Only active or trialing parent subscriptions can accept grouped invoicing children").
			WithReportableDetails(map[string]any{
				"parent_subscription_id":     parentSub.ID,
				"parent_subscription_status": parentSub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// 6. billing periods must match
	if child.BillingPeriod != parentSub.BillingPeriod {
		return ierr.NewError("billing period mismatch between child and parent subscriptions").
			WithHint("Child and parent subscriptions must have the same billing period").
			WithReportableDetails(map[string]any{
				"child_billing_period":  child.BillingPeriod,
				"parent_billing_period": parentSub.BillingPeriod,
			}).
			Mark(ierr.ErrValidation)
	}

	// 7. billing period counts must match
	if child.BillingPeriodCount != parentSub.BillingPeriodCount {
		return ierr.NewError("billing period count mismatch between child and parent subscriptions").
			WithHint("Child and parent subscriptions must have the same billing period count").
			WithReportableDetails(map[string]any{
				"child_billing_period_count":  child.BillingPeriodCount,
				"parent_billing_period_count": parentSub.BillingPeriodCount,
			}).
			Mark(ierr.ErrValidation)
	}

	// 8. billing anchors must match exactly
	if !child.BillingAnchor.Equal(parentSub.BillingAnchor) {
		return ierr.NewError("billing anchor mismatch between child and parent subscriptions").
			WithHint("Child and parent subscriptions must share the same billing anchor").
			WithReportableDetails(map[string]any{
				"child_billing_anchor":  child.BillingAnchor,
				"parent_billing_anchor": parentSub.BillingAnchor,
			}).
			Mark(ierr.ErrValidation)
	}

	// 9. child start date must be >= parent start date
	if child.StartDate.Before(parentSub.StartDate) {
		return ierr.NewError("child subscription start date is before parent subscription start date").
			WithHint("Child subscription cannot start before its parent").
			WithReportableDetails(map[string]any{
				"child_start_date":  child.StartDate,
				"parent_start_date": parentSub.StartDate,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// addToGroupedInvoicing fetches the child by ID, validates all constraints, then persists the
// type and parent-link changes.
func (s *subscriptionService) addToGroupedInvoicing(
	ctx context.Context,
	parentSub *subscription.Subscription,
	childSubID string,
) error {
	child, err := s.SubRepo.Get(ctx, childSubID)
	if err != nil {
		return err
	}

	if err := s.validateAddToGroupedInvoicing(ctx, parentSub, child); err != nil {
		return err
	}

	child.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
	child.ParentSubscriptionID = lo.ToPtr(parentSub.ID)

	return s.SubRepo.Update(ctx, child)
}

// removeFromGroupedInvoicing fetches the child, verifies it is currently grouped_invoicing,
// resets it to standalone and clears the parent link.
func (s *subscriptionService) removeFromGroupedInvoicing(
	ctx context.Context,
	childSubID string,
) error {
	child, err := s.SubRepo.Get(ctx, childSubID)
	if err != nil {
		return err
	}

	if child.SubscriptionType != types.SubscriptionTypeGroupedInvoicing {
		return ierr.NewError("subscription is not of type grouped_invoicing").
			WithHint("Only grouped_invoicing subscriptions can be removed from grouped invoicing").
			WithReportableDetails(map[string]any{
				"subscription_id":   child.ID,
				"subscription_type": child.SubscriptionType,
			}).
			Mark(ierr.ErrValidation)
	}

	child.SubscriptionType = types.SubscriptionTypeStandalone
	child.ParentSubscriptionID = nil

	return s.SubRepo.Update(ctx, child)
}

// validateAddToGroupedInvoicingDryRun fetches the child and runs all constraints without
// writing anything to the database.
func (s *subscriptionService) validateAddToGroupedInvoicingDryRun(
	ctx context.Context,
	parentSub *subscription.Subscription,
	childSubID string,
) error {
	child, err := s.SubRepo.Get(ctx, childSubID)
	if err != nil {
		return err
	}

	return s.validateAddToGroupedInvoicing(ctx, parentSub, child)
}

// validateRemoveFromGroupedInvoicingDryRun fetches the child and verifies it is of type
// grouped_invoicing, without writing anything.
func (s *subscriptionService) validateRemoveFromGroupedInvoicingDryRun(
	ctx context.Context,
	childSubID string,
) error {
	child, err := s.SubRepo.Get(ctx, childSubID)
	if err != nil {
		return err
	}

	if child.SubscriptionType != types.SubscriptionTypeGroupedInvoicing {
		return ierr.NewError("subscription is not of type grouped_invoicing").
			WithHint("Only grouped_invoicing subscriptions can be removed from grouped invoicing").
			WithReportableDetails(map[string]any{
				"subscription_id":   child.ID,
				"subscription_type": child.SubscriptionType,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// groupedInvoicingChildrenFilter builds the canonical filter for listing active
// grouped_invoicing child subscriptions of the given parent.
func groupedInvoicingChildrenFilter(parentSubID string) *types.SubscriptionFilter {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	filter.ParentSubscriptionIDs = []string{parentSubID}
	filter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeGroupedInvoicing}
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusDraft,
	}
	return filter
}

// getGroupedInvoicingSubscriptions returns all active/trialing/draft child subscriptions
// of type grouped_invoicing that belong to the given parent subscription.
func (s *subscriptionService) getGroupedInvoicingSubscriptions(
	ctx context.Context,
	parentSubID string,
) ([]*subscription.Subscription, error) {
	return s.SubRepo.List(ctx, groupedInvoicingChildrenFilter(parentSubID))
}
