package service

import (
	"context"
	"reflect"
	"slices"
	"strings"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

func (s *subscriptionService) validateSubscriptionMembership(ctx context.Context, id string) error {
	if ctx == nil || (reflect.ValueOf(ctx).Kind() == reflect.Pointer && reflect.ValueOf(ctx).IsNil()) {
		return ierr.Validation("subscription membership requires a context")
	}
	if types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" || strings.TrimSpace(id) == "" || len(id) > 50 {
		return ierr.Validation("subscription membership requires tenant, environment and subscription")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return ierr.NewError("subscription membership is unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	for _, dependency := range []any{s.DB, s.SubRepo, s.BillingTermsStore} {
		value := reflect.ValueOf(dependency)
		if !value.IsValid() || (value.Kind() == reflect.Pointer && value.IsNil()) {
			return ierr.NewError("subscription membership is unavailable").Mark(ierr.ErrServiceUnavailable)
		}
	}
	return nil
}

func (s *subscriptionService) lockGroupedMembership(ctx context.Context, parentID string, childIDs []string) (map[string]*subscription.Subscription, error) {
	if len(childIDs) == 0 || len(childIDs) > domain.MaxEligibilityScopes {
		return nil, ierr.Validation("grouped membership requires 1 to 500 children")
	}
	if err := s.validateSubscriptionMembership(ctx, childIDs[0]); err != nil {
		return nil, err
	}
	if s == nil || s.DB == nil || s.DB.TxFromContext(ctx) == nil || s.BillingTermsStore == nil {
		return nil, ierr.NewError("grouped membership requires a scoped transaction and eligibility store").Mark(ierr.ErrServiceUnavailable)
	}
	ids := append([]string(nil), childIDs...)
	if parentID != "" {
		ids = append(ids, parentID)
	}
	discovered := make(map[string]*subscription.Subscription)
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || len(id) > 50 {
			return nil, ierr.Validation("grouped membership requires valid subscription IDs")
		}
		if _, exists := discovered[id]; exists {
			return nil, ierr.Validation("grouped membership participants must be distinct")
		}
		sub, err := s.lifecycleSubscription(ctx, id)
		if err != nil {
			return nil, err
		}
		discovered[id] = sub
	}
	for _, id := range childIDs {
		if parentID != "" && discovered[id].ParentSubscriptionID != nil {
			return nil, ierr.Validation("child subscription already has a parent")
		}
		if oldParent := lo.FromPtr(discovered[id].ParentSubscriptionID); oldParent != "" {
			if _, exists := discovered[oldParent]; !exists {
				sub, err := s.lifecycleSubscription(ctx, oldParent)
				if err != nil {
					return nil, err
				}
				discovered[oldParent] = sub
				ids = append(ids, oldParent)
			}
		}
	}
	if len(ids) > domain.MaxEligibilityScopes {
		return nil, ierr.Validation("grouped membership supports at most 500 participants")
	}
	slices.Sort(ids)
	scopes := make([]domain.EligibilityScope, 0, len(ids))
	for _, id := range ids {
		locked, err := s.SubRepo.GetForUpdateNowait(ctx, id)
		if err != nil {
			return nil, err
		}
		if !subscriptionUpdateScoped(ctx, locked, id) {
			return nil, ierr.NewError("grouped membership lock returned invalid scope").Mark(ierr.ErrDatabase)
		}
		if locked.SubscriptionType != discovered[id].SubscriptionType || lo.FromPtr(locked.ParentSubscriptionID) != lo.FromPtr(discovered[id].ParentSubscriptionID) {
			return nil, lifecycleMembershipChanged()
		}
		scopes = append(scopes, domain.EligibilityScope{Type: "subscription", ID: id})
	}
	store, ok := s.BillingTermsStore.(domain.NonblockingEligibilityStore)
	if !ok {
		return nil, ierr.NewError("grouped membership requires nonblocking eligibility locks").Mark(ierr.ErrServiceUnavailable)
	}
	if err := store.TryLockEligibility(ctx, scopes); err != nil {
		return nil, err
	}
	for _, id := range ids {
		fresh, err := s.lifecycleSubscription(ctx, id)
		if err != nil {
			return nil, err
		}
		if fresh.SubscriptionType != discovered[id].SubscriptionType || lo.FromPtr(fresh.ParentSubscriptionID) != lo.FromPtr(discovered[id].ParentSubscriptionID) {
			return nil, lifecycleMembershipChanged()
		}
		discovered[id] = fresh
	}
	return discovered, nil
}

func (s *subscriptionService) lockCreationParent(ctx context.Context, id string) (*subscription.Subscription, error) {
	participants, err := s.lockGroupedMembership(ctx, "", []string{id})
	if err != nil {
		return nil, err
	}
	parent := participants[id]
	if parent.ParentSubscriptionID != nil || parent.SubscriptionType == types.SubscriptionTypeGroupedInvoicing || parent.SubscriptionType == types.SubscriptionTypeInherited {
		return nil, ierr.Validation("subscription cannot accept child subscriptions")
	}
	children, err := s.lifecycleChildren(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(children) >= domain.MaxEligibilityScopes-1 {
		return nil, ierr.Validation("subscription hierarchy supports at most 500 participants")
	}
	return parent, nil
}
