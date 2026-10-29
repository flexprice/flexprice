package service

import (
	"context"
	"reflect"
	"slices"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainBillingTerms "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *subscriptionService) updateSubscription(ctx context.Context, id string, req dto.UpdateSubscriptionRequest) error {
	if ctx == nil {
		return ierr.Validation("subscription update requires an ID, tenant and environment")
	}
	contextValue := reflect.ValueOf(ctx)
	if contextValue.Kind() == reflect.Pointer && contextValue.IsNil() {
		return ierr.Validation("subscription update requires a context")
	}
	if strings.TrimSpace(id) == "" || types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" {
		return ierr.Validation("subscription update requires an ID, tenant and environment")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return ierr.NewError("subscription update is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	for _, dependency := range []any{s.DB, s.SubRepo, s.BillingTermsStore} {
		value := reflect.ValueOf(dependency)
		if !value.IsValid() || (value.Kind() == reflect.Pointer && value.IsNil()) {
			return ierr.NewError("subscription update is unavailable").Mark(ierr.ErrServiceUnavailable)
		}
	}
	// This entry point owns the row-lock prefix; unknown caller locks cannot precede it.
	if s.DB.TxFromContext(ctx) != nil || s.BillingTermsStore.InTransaction(ctx) {
		return ierr.NewError("subscription update requires its own transaction").
			WithHint("Update the subscription outside the caller transaction").Mark(ierr.ErrInvalidOperation)
	}

	return s.DB.WithTx(types.WithForceWriter(ctx), func(tx context.Context) error {
		rows, err := s.SubRepo.List(tx, &types.SubscriptionFilter{
			QueryFilter:        &types.QueryFilter{Limit: lo.ToPtr(2), Status: lo.ToPtr(types.StatusPublished)},
			SubscriptionIDs:    []string{id},
			SubscriptionStatus: types.AllSubscriptionStatuses(),
		})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return ierr.NewError("subscription not found").Mark(ierr.ErrNotFound)
		}
		if len(rows) != 1 || !subscriptionUpdateScoped(tx, rows[0], id) {
			return ierr.NewError("subscription discovery returned invalid scope").Mark(ierr.ErrDatabase)
		}
		previousParentID := lo.FromPtr(rows[0].ParentSubscriptionID)
		ids := []string{id}
		if previousParentID != "" {
			ids = append(ids, previousParentID)
		}
		if req.ParentSubscriptionID != nil && *req.ParentSubscriptionID != "" {
			ids = append(ids, *req.ParentSubscriptionID)
		}
		slices.Sort(ids)
		ids = slices.Compact(ids)
		for _, participantID := range ids {
			if strings.TrimSpace(participantID) == "" || len(participantID) > 50 {
				return ierr.Validation("subscription update requires valid subscription IDs of at most 50 bytes")
			}
		}

		locked := make(map[string]*subscription.Subscription, len(ids))
		scopes := make([]domainBillingTerms.EligibilityScope, 0, len(ids))
		for _, participantID := range ids {
			participant, err := s.SubRepo.GetForUpdateNowait(tx, participantID)
			if err != nil {
				return err
			}
			if !subscriptionUpdateScoped(tx, participant, participantID) {
				return ierr.NewError("subscription lock returned invalid scope").Mark(ierr.ErrDatabase)
			}
			locked[participantID] = participant
			scopes = append(scopes, domainBillingTerms.EligibilityScope{Type: "subscription", ID: participantID})
		}
		current := locked[id]
		if lo.FromPtr(current.ParentSubscriptionID) != previousParentID {
			return ierr.NewError("subscription membership changed; refresh and retry").
				WithReportableDetails(map[string]any{"subscription_id": id, "reason": "subscription_membership_changed"}).
				Mark(ierr.ErrVersionConflict)
		}
		if err := s.BillingTermsStore.LockEligibility(tx, scopes); err != nil {
			return err
		}
		updated, err := s.applySubscriptionUpdate(current, locked[lo.FromPtr(req.ParentSubscriptionID)], req)
		if err != nil {
			return err
		}
		return s.SubRepo.Update(tx, updated)
	})
}

func subscriptionUpdateScoped(ctx context.Context, sub *subscription.Subscription, id string) bool {
	return sub != nil && sub.ID == id && sub.Status == types.StatusPublished &&
		sub.TenantID == types.GetTenantID(ctx) && sub.EnvironmentID == types.GetEnvironmentID(ctx)
}

func (s *subscriptionService) applySubscriptionUpdate(current, parent *subscription.Subscription, req dto.UpdateSubscriptionRequest) (*subscription.Subscription, error) {
	builder := subscription.NewSubscriptionBuilder(current)
	if req.ParentSubscriptionID != nil {
		parentID := *req.ParentSubscriptionID
		if parentID != "" {
			if current.SubscriptionType != types.SubscriptionTypeStandalone {
				return nil, ierr.NewError("parent_subscription_id can only be set on standalone subscriptions").
					WithHint("Convert the subscription to standalone before assigning a parent").Mark(ierr.ErrInvalidOperation)
			}
			if parentID == current.ID {
				return nil, ierr.Validation("subscription cannot be its own parent")
			}
			if parent == nil || parent.SubscriptionStatus != types.SubscriptionStatusActive {
				return nil, ierr.Validation("parent subscription must be active")
			}
			builder.WithParentSubscriptionID(&parentID)
		} else {
			builder.WithParentSubscriptionID(nil)
		}
	}
	if req.Status != "" && req.Status != current.SubscriptionStatus && current.ActivePauseID != nil {
		return nil, ierr.NewError("cannot change the status of a paused subscription").
			WithHint("Resume or cancel the subscription instead").Mark(ierr.ErrValidation)
	}
	if req.Status != "" {
		builder.WithStatus(req.Status)
	}
	if req.CancelAt != nil {
		if !req.CancelAt.After(current.CurrentPeriodStart) {
			return nil, ierr.NewError("cancel_at must be after current period start").
				WithHint("cancel_at cannot be at or before the current period start; it would leave the subscription with an inverted billing period").
				Mark(ierr.ErrValidation)
		}
		naturalEnd, err := s.naturalPeriodEnd(current)
		if err != nil {
			return nil, err
		}
		periodEnd := naturalEnd
		if req.CancelAt.Before(periodEnd) {
			periodEnd = *req.CancelAt
		}
		builder.WithCancelAt(req.CancelAt).WithEndDate(req.CancelAt).WithCurrentPeriodEnd(periodEnd)
	}
	if req.CancelAtPeriodEnd != nil {
		builder.WithCancelAtPeriodEnd(*req.CancelAtPeriodEnd)
		if !*req.CancelAtPeriodEnd && req.CancelAt == nil && current.CancelAt != nil {
			naturalEnd, err := s.naturalPeriodEnd(current)
			if err != nil {
				return nil, err
			}
			builder.WithCancelAt(nil).WithEndDate(nil).WithCurrentPeriodEnd(naturalEnd)
		}
	}
	return builder.Build(), nil
}
