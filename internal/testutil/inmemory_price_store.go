package testutil

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/price"
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
		return fmt.Errorf("price cannot be nil")
	}
	return s.InMemoryStore.Create(ctx, p.ID, p)
}

func (s *InMemoryPriceStore) Get(ctx context.Context, id string) (*price.Price, error) {
	return s.InMemoryStore.Get(ctx, id)
}

func (s *InMemoryPriceStore) List(ctx context.Context, filter *types.PriceFilter) ([]*price.Price, error) {
	return s.InMemoryStore.List(ctx, filter, priceFilterFn, priceSortFn)
}

func (s *InMemoryPriceStore) Count(ctx context.Context, filter *types.PriceFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, priceFilterFn)
}

func (s *InMemoryPriceStore) Update(ctx context.Context, p *price.Price) error {
	if p == nil {
		return fmt.Errorf("price cannot be nil")
	}
	return s.InMemoryStore.Update(ctx, p.ID, p)
}

func (s *InMemoryPriceStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
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
			return err
		}
	}
	return nil
}

// DeleteBulk deletes multiple prices in bulk
func (s *InMemoryPriceStore) DeleteBulk(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := s.Delete(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// Clear clears the price store
func (s *InMemoryPriceStore) Clear() {
	s.InMemoryStore.Clear()
}
