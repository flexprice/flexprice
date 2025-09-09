package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemorySubscriptionLineItemStore implements subscription.LineItemRepository
type InMemorySubscriptionLineItemStore struct {
	*InMemoryStore[*subscription.SubscriptionLineItem]
}

// NewInMemorySubscriptionLineItemStore creates a new in-memory subscription line item store
func NewInMemorySubscriptionLineItemStore() *InMemorySubscriptionLineItemStore {
	return &InMemorySubscriptionLineItemStore{
		InMemoryStore: NewInMemoryStore[*subscription.SubscriptionLineItem](),
	}
}

// lineItemFilterFn implements filtering logic for subscription line items
func lineItemFilterFn(ctx context.Context, item *subscription.SubscriptionLineItem, filter interface{}) bool {
	if item == nil {
		return false
	}

	f, ok := filter.(*types.SubscriptionLineItemFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if item.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, item.EnvironmentID) {
		return false
	}

	// Filter by subscription IDs
	if len(f.SubscriptionIDs) > 0 && !lo.Contains(f.SubscriptionIDs, item.SubscriptionID) {
		return false
	}

	// Filter by entity IDs
	if len(f.EntityIDs) > 0 {
		// Check if the item's EntityID matches any of the plan IDs and EntityType is plan
		if item.EntityType != types.SubscriptionLineItemEntityTypePlan || !lo.Contains(f.EntityIDs, item.EntityID) {
			return false
		}
	}

	// Filter by price IDs
	if len(f.PriceIDs) > 0 && !lo.Contains(f.PriceIDs, item.PriceID) {
		return false
	}

	// Filter by meter IDs
	if len(f.MeterIDs) > 0 && !lo.Contains(f.MeterIDs, item.MeterID) {
		return false
	}

	// Filter by currencies
	if len(f.Currencies) > 0 && !lo.Contains(f.Currencies, item.Currency) {
		return false
	}

	// Filter by billing periods
	if len(f.BillingPeriods) > 0 && !lo.Contains(f.BillingPeriods, string(item.BillingPeriod)) {
		return false
	}

	return true
}

// lineItemSortFn implements sorting logic for subscription line items
func lineItemSortFn(i, j *subscription.SubscriptionLineItem) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

// Create creates a new subscription line item
func (s *InMemorySubscriptionLineItemStore) Create(ctx context.Context, item *subscription.SubscriptionLineItem) error {
	if item == nil {
		return ierr.NewError("subscription line item cannot be nil").
			WithHint("Subscription line item data is required").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if item.EnvironmentID == "" {
		item.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	err := s.InMemoryStore.Create(ctx, item.ID, item)
	if err != nil {
		if ierr.IsAlreadyExists(err) {
			return ierr.WithError(err).
				WithHint("A subscription line item with this ID already exists").
				WithReportableDetails(map[string]interface{}{
					"line_item_id": item.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create subscription line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": item.ID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// Get retrieves a subscription line item by ID
func (s *InMemorySubscriptionLineItemStore) Get(ctx context.Context, id string) (*subscription.SubscriptionLineItem, error) {
	item, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Subscription line item not found").
				WithReportableDetails(map[string]interface{}{
					"line_item_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve subscription line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}
	return item, nil
}

// Update updates a subscription line item
func (s *InMemorySubscriptionLineItemStore) Update(ctx context.Context, item *subscription.SubscriptionLineItem) error {
	if item == nil {
		return ierr.NewError("subscription line item cannot be nil").
			WithHint("Subscription line item data is required").
			Mark(ierr.ErrValidation)
	}
	err := s.InMemoryStore.Update(ctx, item.ID, item)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Subscription line item not found").
				WithReportableDetails(map[string]interface{}{
					"line_item_id": item.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update subscription line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": item.ID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// Delete deletes a subscription line item
func (s *InMemorySubscriptionLineItemStore) Delete(ctx context.Context, id string) error {
	err := s.InMemoryStore.Delete(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Subscription line item not found").
				WithReportableDetails(map[string]interface{}{
					"line_item_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete subscription line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// CreateBulk creates multiple subscription line items in bulk
func (s *InMemorySubscriptionLineItemStore) CreateBulk(ctx context.Context, items []*subscription.SubscriptionLineItem) error {
	if len(items) == 0 {
		return nil
	}

	for _, item := range items {
		if err := s.Create(ctx, item); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create subscription line items in bulk").
				WithReportableDetails(map[string]interface{}{
					"count": len(items),
				}).
				Mark(ierr.ErrDatabase)
		}
	}
	return nil
}

// ListBySubscription retrieves all line items for a subscription
func (s *InMemorySubscriptionLineItemStore) ListBySubscription(ctx context.Context, sub *subscription.Subscription) ([]*subscription.SubscriptionLineItem, error) {
	filter := &types.SubscriptionLineItemFilter{
		SubscriptionIDs: []string{sub.ID},
	}
	return s.List(ctx, filter)
}

// List retrieves subscription line items based on filter
func (s *InMemorySubscriptionLineItemStore) List(ctx context.Context, filter *types.SubscriptionLineItemFilter) ([]*subscription.SubscriptionLineItem, error) {
	items, err := s.InMemoryStore.List(ctx, filter, lineItemFilterFn, lineItemSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscription line items").
			Mark(ierr.ErrDatabase)
	}
	return items, nil
}

// Count counts subscription line items based on filter
func (s *InMemorySubscriptionLineItemStore) Count(ctx context.Context, filter *types.SubscriptionLineItemFilter) (int, error) {
	count, err := s.InMemoryStore.Count(ctx, filter, lineItemFilterFn)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count subscription line items").
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

// Clear clears all subscription line items from the store
func (s *InMemorySubscriptionLineItemStore) Clear() {
	s.InMemoryStore.Clear()
}

// ListByCustomer retrieves all line items for a customer
func (s *InMemorySubscriptionLineItemStore) ListByCustomer(ctx context.Context, customerID string) ([]*subscription.SubscriptionLineItem, error) {
	filter := &types.SubscriptionLineItemFilter{
		EntityIDs: []string{customerID},
	}
	return s.List(ctx, filter)
}

// GetByPriceID retrieves all line items for a price
func (s *InMemorySubscriptionLineItemStore) GetByPriceID(ctx context.Context, priceID string) ([]*subscription.SubscriptionLineItem, error) {
	filter := &types.SubscriptionLineItemFilter{
		PriceIDs: []string{priceID},
	}
	return s.List(ctx, filter)
}

// GetByPlanID retrieves all line items for a plan
func (s *InMemorySubscriptionLineItemStore) GetByPlanID(ctx context.Context, planID string) ([]*subscription.SubscriptionLineItem, error) {
	filter := &types.SubscriptionLineItemFilter{
		EntityIDs: []string{planID},
	}
	return s.List(ctx, filter)
}
