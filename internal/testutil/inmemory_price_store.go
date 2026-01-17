package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryPriceStore implements price.Repository
type InMemoryPriceStore struct {
	*InMemoryStore[*price.Price]
}

func NewInMemoryPriceStore() *InMemoryPriceStore {
	return &InMemoryPriceStore{
		InMemoryStore: NewInMemoryStore[*price.Price](),
	}
}

// priceFilterFn implements filtering logic for prices
func priceFilterFn(ctx context.Context, p *price.Price, filter interface{}) bool {
	if p == nil {
		return false
	}

	f, ok := filter.(*types.PriceFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if p.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, p.EnvironmentID) {
		return false
	}

	// Filter by entity type
	if f.EntityType != nil && p.EntityType != *f.EntityType {
		return false
	}

	// Filter by plan IDs
	if len(f.EntityIDs) > 0 {
		if !lo.Contains(f.EntityIDs, p.EntityID) {
			return false
		}
	}

	// filter by price ids
	if len(f.PriceIDs) > 0 {
		if !lo.Contains(f.PriceIDs, p.ID) {
			return false
		}
	}

	// Filter by status
	if f.Status != nil && p.Status != *f.Status {
		return false
	}

	// Filter by time range
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && p.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && p.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

// priceSortFn implements sorting logic for prices
func priceSortFn(i, j *price.Price) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemoryPriceStore) Create(ctx context.Context, p *price.Price) error {
	if p == nil {
		return ierr.NewError("price cannot be nil").
			WithHint("Price data is required").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if p.EnvironmentID == "" {
		p.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	err := s.InMemoryStore.Create(ctx, p.ID, p)
	if err != nil {
		if ierr.IsAlreadyExists(err) {
			return ierr.WithError(err).
				WithHint("A price with this identifier already exists").
				WithReportableDetails(map[string]any{
					"price_id": p.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create price").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryPriceStore) Get(ctx context.Context, id string) (*price.Price, error) {
	p, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Price with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"price_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get price").
			Mark(ierr.ErrDatabase)
	}
	return p, nil
}

func (s *InMemoryPriceStore) List(ctx context.Context, filter *types.PriceFilter) ([]*price.Price, error) {
	prices, err := s.InMemoryStore.List(ctx, filter, priceFilterFn, priceSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list prices").
			Mark(ierr.ErrDatabase)
	}
	return prices, nil
}

func (s *InMemoryPriceStore) Count(ctx context.Context, filter *types.PriceFilter) (int, error) {
	count, err := s.InMemoryStore.Count(ctx, filter, priceFilterFn)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count prices").
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (s *InMemoryPriceStore) Update(ctx context.Context, p *price.Price) error {
	if p == nil {
		return ierr.NewError("price cannot be nil").
			WithHint("Price data is required").
			Mark(ierr.ErrValidation)
	}

	err := s.InMemoryStore.Update(ctx, p.ID, p)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Price with ID %s was not found", p.ID).
				WithReportableDetails(map[string]any{
					"price_id": p.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update price").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryPriceStore) Delete(ctx context.Context, id string) error {
	err := s.InMemoryStore.Delete(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Price with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"price_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete price").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// ListAll returns all prices without pagination
func (s *InMemoryPriceStore) ListAll(ctx context.Context, filter *types.PriceFilter) ([]*price.Price, error) {
	// Create an unlimited filter
	unlimitedFilter := &types.PriceFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		TimeRangeFilter: filter.TimeRangeFilter,
		EntityIDs:       filter.EntityIDs,
	}

	return s.List(ctx, unlimitedFilter)
}

// CreateBulk creates multiple prices in bulk
func (s *InMemoryPriceStore) CreateBulk(ctx context.Context, prices []*price.Price) error {
	for _, p := range prices {
		if err := s.Create(ctx, p); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create prices in bulk").
				Mark(ierr.ErrDatabase)
		}
	}
	return nil
}

// DeleteBulk deletes multiple prices in bulk
func (s *InMemoryPriceStore) DeleteBulk(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := s.Delete(ctx, id); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to delete prices in bulk").
				Mark(ierr.ErrDatabase)
		}
	}
	return nil
}

func (s *InMemoryPriceStore) GetByPlanID(ctx context.Context, planID string) ([]*price.Price, error) {
	return s.InMemoryStore.List(ctx, types.NewPriceFilter().WithEntityIDs([]string{planID}), priceFilterFn, priceSortFn)
}

// Clear clears the price store
func (s *InMemoryPriceStore) Clear() {
	s.InMemoryStore.Clear()
}

func (s *InMemoryPriceStore) GetByGroupIDs(ctx context.Context, groupIDs []string) ([]*price.Price, error) {
	prices, err := s.InMemoryStore.List(ctx, nil, func(ctx context.Context, p *price.Price, filter interface{}) bool {
		if p == nil {
			return false
		}
		// Check tenant and environment filters
		if !CheckTenantFilter(ctx, p.TenantID) || !CheckEnvironmentFilter(ctx, p.EnvironmentID) {
			return false
		}
		// Check if price belongs to any of the specified groups
		if p.GroupID == "" {
			return false
		}
		return lo.Contains(groupIDs, p.GroupID) && p.Status == types.StatusPublished
	}, priceSortFn)

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get prices by group IDs").
			Mark(ierr.ErrDatabase)
	}
	return prices, nil
}

// ClearGroupIDsBulk clears the group ID for multiple prices
func (s *InMemoryPriceStore) ClearGroupIDsBulk(ctx context.Context, ids []string) error {
	for _, id := range ids {
		p, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		p.GroupID = ""
		if err := s.Update(ctx, p); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to clear group IDs in bulk").
				Mark(ierr.ErrDatabase)
		}
	}
	return nil
}

// ClearByGroupID clears the group ID for all prices in a specific group
func (s *InMemoryPriceStore) ClearByGroupID(ctx context.Context, groupID string) error {
	// Get all prices in the group
	prices, err := s.GetByGroupIDs(ctx, []string{groupID})
	if err != nil {
		return err
	}

	// Clear group ID for each price
	for _, p := range prices {
		p.GroupID = ""
		if err := s.Update(ctx, p); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to clear group ID by group ID").
				Mark(ierr.ErrDatabase)
		}
	}
	return nil
}

// GetByLookupKey retrieves a price by its lookup key
func (s *InMemoryPriceStore) GetByLookupKey(ctx context.Context, lookupKey string) (*price.Price, error) {
	if lookupKey == "" {
		return nil, ierr.NewError("lookup key is required").
			WithHint("Lookup key is required").
			Mark(ierr.ErrValidation)
	}

	// Get tenant and environment from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Search through all prices
	var foundPrice *price.Price
	s.InMemoryStore.mu.RLock()
	for _, p := range s.InMemoryStore.items {
		if p.LookupKey == lookupKey &&
			p.TenantID == tenantID &&
			p.EnvironmentID == environmentID &&
			p.Status == types.StatusPublished &&
			p.EndDate == nil { // Only return active prices (without end date)
			foundPrice = p
			break
		}
	}
	s.InMemoryStore.mu.RUnlock()

	if foundPrice == nil {
		return nil, ierr.NewError("price not found").
			WithHintf("Price with lookup key '%s' was not found", lookupKey).
			WithReportableDetails(map[string]interface{}{
				"lookup_key": lookupKey,
			}).
			Mark(ierr.ErrNotFound)
	}

	return foundPrice, nil
}
