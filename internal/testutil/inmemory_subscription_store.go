package testutil

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemorySubscriptionStore implements subscription.Repository
type InMemorySubscriptionStore struct {
	*InMemoryStore[*subscription.Subscription]
}

func NewInMemorySubscriptionStore() *InMemorySubscriptionStore {
	return &InMemorySubscriptionStore{
		InMemoryStore: NewInMemoryStore[*subscription.Subscription](),
	}
}

// subscriptionFilterFn implements filtering logic for subscriptions
func subscriptionFilterFn(ctx context.Context, sub *subscription.Subscription, filter interface{}) bool {
	if sub == nil {
		return false
	}

	f, ok := filter.(*types.SubscriptionFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if sub.TenantID != tenantID {
			return false
		}
	}

	// Filter by customer ID
	if f.CustomerID != "" && sub.CustomerID != f.CustomerID {
		return false
	}

	// Filter by plan ID
	if f.PlanID != "" && sub.PlanID != f.PlanID {
		return false
	}

	// Filter by subscription status
	if len(f.SubscriptionStatus) > 0 && !lo.Contains(f.SubscriptionStatus, sub.SubscriptionStatus) {
		return false
	}

	// Filter by invoice cadence
	if len(f.InvoiceCadence) > 0 && !lo.Contains(f.InvoiceCadence, sub.InvoiceCadence) {
		return false
	}

	// Filter by billing cadence
	if len(f.BillingCadence) > 0 && !lo.Contains(f.BillingCadence, sub.BillingCadence) {
		return false
	}

	// Filter by billing period
	if len(f.BillingPeriod) > 0 && !lo.Contains(f.BillingPeriod, sub.BillingPeriod) {
		return false
	}

	// Filter by time range
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && sub.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && sub.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	// Filter by active at
	if f.ActiveAt != nil {
		if sub.SubscriptionStatus != types.SubscriptionStatusActive {
			return false
		}
		if sub.StartDate.After(*f.ActiveAt) {
			return false
		}
		if sub.EndDate != nil && sub.EndDate.Before(*f.ActiveAt) {
			return false
		}
	}

	return true
}

// subscriptionSortFn implements sorting logic for subscriptions
func subscriptionSortFn(i, j *subscription.Subscription) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemorySubscriptionStore) Create(ctx context.Context, sub *subscription.Subscription) error {
	if sub == nil {
		return fmt.Errorf("subscription cannot be nil")
	}
	return s.InMemoryStore.Create(ctx, sub.ID, sub)
}

func (s *InMemorySubscriptionStore) Get(ctx context.Context, id string) (*subscription.Subscription, error) {
	return s.InMemoryStore.Get(ctx, id)
}

func (s *InMemorySubscriptionStore) List(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	return s.InMemoryStore.List(ctx, filter, subscriptionFilterFn, subscriptionSortFn)
}

func (s *InMemorySubscriptionStore) Count(ctx context.Context, filter *types.SubscriptionFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, subscriptionFilterFn)
}

func (s *InMemorySubscriptionStore) Update(ctx context.Context, sub *subscription.Subscription) error {
	if sub == nil {
		return fmt.Errorf("subscription cannot be nil")
	}
	return s.InMemoryStore.Update(ctx, sub.ID, sub)
}

func (s *InMemorySubscriptionStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
}

// ListAll returns all subscriptions without pagination
func (s *InMemorySubscriptionStore) ListAll(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	// Create an unlimited filter
	unlimitedFilter := &types.SubscriptionFilter{
		QueryFilter:        types.NewNoLimitQueryFilter(),
		TimeRangeFilter:    filter.TimeRangeFilter,
		CustomerID:         filter.CustomerID,
		PlanID:             filter.PlanID,
		SubscriptionStatus: filter.SubscriptionStatus,
		InvoiceCadence:     filter.InvoiceCadence,
		BillingCadence:     filter.BillingCadence,
		BillingPeriod:      filter.BillingPeriod,
		IncludeCanceled:    filter.IncludeCanceled,
		ActiveAt:           filter.ActiveAt,
	}

	return s.List(ctx, unlimitedFilter)
}

// ListAllTenant returns all subscriptions across all tenants
// NOTE: This is a potentially expensive operation and to be used only for CRONs
func (s *InMemorySubscriptionStore) ListAllTenant(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	return s.ListAll(ctx, filter)
}
