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

	// Filter by plan IDs
	if len(f.PlanIDs) > 0 {
		if !lo.Contains(f.PlanIDs, p.PlanID) {
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
		PlanIDs:         filter.PlanIDs,
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

// Clear clears the price store
func (s *InMemoryPriceStore) Clear() {
	s.InMemoryStore.Clear()
}
