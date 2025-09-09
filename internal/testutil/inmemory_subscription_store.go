package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemorySubscriptionStore implements subscription.Repository
type InMemorySubscriptionStore struct {
	*InMemoryStore[*subscription.Subscription]
	lineItems map[string][]*subscription.SubscriptionLineItem // map[subscriptionID][]lineItems
	pauses    map[string][]*subscription.SubscriptionPause    // map[subscriptionID][]pauses
	pauseByID map[string]*subscription.SubscriptionPause      // map[pauseID]pause
}

func NewInMemorySubscriptionStore() *InMemorySubscriptionStore {
	return &InMemorySubscriptionStore{
		InMemoryStore: NewInMemoryStore[*subscription.Subscription](),
		lineItems:     make(map[string][]*subscription.SubscriptionLineItem),
		pauses:        make(map[string][]*subscription.SubscriptionPause),
		pauseByID:     make(map[string]*subscription.SubscriptionPause),
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

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, sub.EnvironmentID) {
		return false
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
		return ierr.NewError("subscription cannot be nil").
			WithHint("Subscription data is required").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if sub.EnvironmentID == "" {
		sub.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	err := s.InMemoryStore.Create(ctx, sub.ID, sub)
	if err != nil {
		if ierr.IsAlreadyExists(err) {
			return ierr.WithError(err).
				WithHint("A subscription with this ID already exists").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": sub.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemorySubscriptionStore) Get(ctx context.Context, id string) (*subscription.Subscription, error) {
	sub, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Subscription not found").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}
	// Attach line items if they exist
	if items, ok := s.lineItems[id]; ok {
		sub.LineItems = items
	}
	return sub, nil
}

func (s *InMemorySubscriptionStore) List(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	subs, err := s.InMemoryStore.List(ctx, filter, subscriptionFilterFn, subscriptionSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscriptions").
			Mark(ierr.ErrDatabase)
	}
	// Attach line items to each subscription
	for _, sub := range subs {
		if items, ok := s.lineItems[sub.ID]; ok {
			sub.LineItems = items
		}
	}
	return subs, nil
}

func (s *InMemorySubscriptionStore) ListByCustomerID(ctx context.Context, customerID string) ([]*subscription.Subscription, error) {
	// Create a filter with customer ID
	filter := &types.SubscriptionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		CustomerID:  customerID,
		SubscriptionStatus: []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
			types.SubscriptionStatusTrialing,
		},
	}

	// Use the existing List method
	return s.List(ctx, filter)
}

func (s *InMemorySubscriptionStore) ListByIDs(ctx context.Context, ids []string) ([]*subscription.Subscription, error) {
	allSubs, err := s.ListAll(ctx, &types.SubscriptionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscriptions").
			Mark(ierr.ErrDatabase)
	}

	// Filter the subscriptions by IDs
	return lo.Filter(allSubs, func(sub *subscription.Subscription, _ int) bool {
		return lo.Contains(ids, sub.ID)
	}), nil
}
func (s *InMemorySubscriptionStore) Count(ctx context.Context, filter *types.SubscriptionFilter) (int, error) {
	count, err := s.InMemoryStore.Count(ctx, filter, subscriptionFilterFn)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count subscriptions").
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (s *InMemorySubscriptionStore) Update(ctx context.Context, sub *subscription.Subscription) error {
	if sub == nil {
		return ierr.NewError("subscription cannot be nil").
			WithHint("Subscription data is required").
			Mark(ierr.ErrValidation)
	}
	err := s.InMemoryStore.Update(ctx, sub.ID, sub)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Subscription not found").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": sub.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemorySubscriptionStore) Delete(ctx context.Context, id string) error {
	// Delete line items first
	delete(s.lineItems, id)
	err := s.InMemoryStore.Delete(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Subscription not found").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// ListAll returns all subscriptions without pagination
func (s *InMemorySubscriptionStore) ListAll(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	// Create an unlimited filter
	unlimitedFilter := &types.SubscriptionFilter{
		QueryFilter:             types.NewNoLimitQueryFilter(),
		TimeRangeFilter:         filter.TimeRangeFilter,
		CustomerID:              filter.CustomerID,
		PlanID:                  filter.PlanID,
		SubscriptionStatus:      filter.SubscriptionStatus,
		BillingCadence:          filter.BillingCadence,
		BillingPeriod:           filter.BillingPeriod,
		SubscriptionStatusNotIn: filter.SubscriptionStatusNotIn,
		ActiveAt:                filter.ActiveAt,
	}

	return s.List(ctx, unlimitedFilter)
}

// ListAllTenant returns all subscriptions across all tenants
// NOTE: This is a potentially expensive operation and to be used only for CRONs
func (s *InMemorySubscriptionStore) ListAllTenant(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	return s.ListAll(ctx, filter)
}

// CreateWithLineItems creates a subscription with its line items
func (s *InMemorySubscriptionStore) CreateWithLineItems(ctx context.Context, sub *subscription.Subscription, items []*subscription.SubscriptionLineItem) error {
	if err := s.Create(ctx, sub); err != nil {
		return err
	}
	s.lineItems[sub.ID] = items
	sub.LineItems = items
	return nil
}

// GetWithLineItems gets a subscription with its line items
func (s *InMemorySubscriptionStore) GetWithLineItems(ctx context.Context, id string) (*subscription.Subscription, []*subscription.SubscriptionLineItem, error) {
	sub, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	items := s.lineItems[id]
	sub.LineItems = items
	return sub, items, nil
}

// CreatePause creates a new subscription pause
func (s *InMemorySubscriptionStore) CreatePause(ctx context.Context, pause *subscription.SubscriptionPause) error {
	if pause == nil {
		return ierr.NewError("pause cannot be nil").
			WithHint("Pause data is required").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if pause.EnvironmentID == "" {
		pause.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	// Store the pause
	s.pauseByID[pause.ID] = pause

	// Add to subscription's pauses
	s.pauses[pause.SubscriptionID] = append(s.pauses[pause.SubscriptionID], pause)

	return nil
}

// GetPause gets a subscription pause by ID
func (s *InMemorySubscriptionStore) GetPause(ctx context.Context, id string) (*subscription.SubscriptionPause, error) {
	pause, ok := s.pauseByID[id]
	if !ok {
		return nil, ierr.NewError("pause not found").
			WithHint("Pause not found").
			WithReportableDetails(map[string]interface{}{
				"id": id,
			}).
			Mark(ierr.ErrNotFound)
	}
	return pause, nil
}

// UpdatePause updates a subscription pause
func (s *InMemorySubscriptionStore) UpdatePause(ctx context.Context, pause *subscription.SubscriptionPause) error {
	if pause == nil {
		return ierr.NewError("pause cannot be nil").
			WithHint("Pause data is required").
			Mark(ierr.ErrValidation)
	}

	// Check if pause exists
	_, ok := s.pauseByID[pause.ID]
	if !ok {
		return ierr.NewError("pause not found").
			WithHint("Pause not found").
			WithReportableDetails(map[string]interface{}{
				"id": pause.ID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Update the pause
	s.pauseByID[pause.ID] = pause

	// Update in subscription's pauses
	for i, p := range s.pauses[pause.SubscriptionID] {
		if p.ID == pause.ID {
			s.pauses[pause.SubscriptionID][i] = pause
			break
		}
	}

	return nil
}

// ListPauses lists all pauses for a subscription
func (s *InMemorySubscriptionStore) ListPauses(ctx context.Context, subscriptionID string) ([]*subscription.SubscriptionPause, error) {
	pauses := s.pauses[subscriptionID]
	return pauses, nil
}

// GetWithPauses gets a subscription with its pauses
func (s *InMemorySubscriptionStore) GetWithPauses(ctx context.Context, id string) (*subscription.Subscription, []*subscription.SubscriptionPause, error) {
	sub, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	pauses := s.pauses[id]
	sub.Pauses = pauses
	return sub, pauses, nil
}

// ListSubscriptionsDueForRenewal retrieves all active subscriptions that are due for renewal in 24 hours
func (s *InMemorySubscriptionStore) ListSubscriptionsDueForRenewal(ctx context.Context) ([]*subscription.Subscription, error) {
	// Create a filter for active subscriptions
	filter := &types.SubscriptionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		SubscriptionStatus: []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
		},
		TimeRangeFilter: &types.TimeRangeFilter{
			EndTime: lo.ToPtr(time.Now().UTC().Add(24 * time.Hour)),
		},
	}

	return s.ListAll(ctx, filter)
}

// Clear removes all data from the store
func (s *InMemorySubscriptionStore) Clear() {
	// Clear the base subscription store
	s.InMemoryStore.Clear()

	// Clear line items
	s.lineItems = make(map[string][]*subscription.SubscriptionLineItem)

	// Clear pauses
	s.pauses = make(map[string][]*subscription.SubscriptionPause)
	s.pauseByID = make(map[string]*subscription.SubscriptionPause)
}
