package testutil

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryFeatureStore implements feature.Repository
type InMemoryFeatureStore struct {
	*InMemoryStore[*feature.Feature]
}

// NewInMemoryFeatureStore creates a new in-memory feature store
func NewInMemoryFeatureStore() *InMemoryFeatureStore {
	return &InMemoryFeatureStore{
		InMemoryStore: NewInMemoryStore[*feature.Feature](),
	}
}

// featureFilterFn implements filtering logic for features
func featureFilterFn(ctx context.Context, f *feature.Feature, filter interface{}) bool {
	if f == nil {
		return false
	}

	filter_, ok := filter.(*types.FeatureFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if f.TenantID != tenantID {
			return false
		}
	}

	// Filter by feature IDs
	if len(filter_.FeatureIDs) > 0 {
		if !lo.Contains(filter_.FeatureIDs, f.ID) {
			return false
		}
	}

	// Filter by lookup key
	if filter_.LookupKey != "" {
		if f.LookupKey != filter_.LookupKey {
			return false
		}
	}

	// Filter by status - if no status is specified, only show published features
	if filter_.Status != nil {
		if f.Status != *filter_.Status {
			return false
		}
	} else if f.Status == types.StatusArchived {
		return false
	}

	// Filter by time range
	if filter_.TimeRangeFilter != nil {
		if filter_.StartTime != nil && f.CreatedAt.Before(*filter_.StartTime) {
			return false
		}
		if filter_.EndTime != nil && f.CreatedAt.After(*filter_.EndTime) {
			return false
		}
	}

	return true
}

// featureSortFn implements sorting logic for features
func featureSortFn(i, j *feature.Feature) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemoryFeatureStore) Create(ctx context.Context, f *feature.Feature) error {
	if f == nil {
		return fmt.Errorf("feature cannot be nil")
	}
	return s.InMemoryStore.Create(ctx, f.ID, f)
}

func (s *InMemoryFeatureStore) Get(ctx context.Context, id string) (*feature.Feature, error) {
	return s.InMemoryStore.Get(ctx, id)
}

func (s *InMemoryFeatureStore) List(ctx context.Context, filter *types.FeatureFilter) ([]*feature.Feature, error) {
	return s.InMemoryStore.List(ctx, filter, featureFilterFn, featureSortFn)
}

func (s *InMemoryFeatureStore) Count(ctx context.Context, filter *types.FeatureFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, featureFilterFn)
}

func (s *InMemoryFeatureStore) Update(ctx context.Context, f *feature.Feature) error {
	if f == nil {
		return fmt.Errorf("feature cannot be nil")
	}
	return s.InMemoryStore.Update(ctx, f.ID, f)
}

func (s *InMemoryFeatureStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
}

// ListAll returns all features without pagination
func (s *InMemoryFeatureStore) ListAll(ctx context.Context, filter *types.FeatureFilter) ([]*feature.Feature, error) {
	// Create an unlimited filter
	unlimitedFilter := &types.FeatureFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		TimeRangeFilter: filter.TimeRangeFilter,
		FeatureIDs:      filter.FeatureIDs,
		LookupKey:       filter.LookupKey,
	}

	return s.List(ctx, unlimitedFilter)
}

func (s *InMemoryFeatureStore) CreateBulk(ctx context.Context, features []*feature.Feature) ([]*feature.Feature, error) {
	for _, f := range features {
		if f == nil {
			return nil, fmt.Errorf("feature cannot be nil")
		}
		if err := s.InMemoryStore.Create(ctx, f.ID, f); err != nil {
			return nil, err
		}
	}
	return features, nil
}

func (s *InMemoryFeatureStore) DeleteBulk(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := s.InMemoryStore.Delete(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// Clear clears the feature store
func (s *InMemoryFeatureStore) Clear() {
	s.InMemoryStore.Clear()
}
