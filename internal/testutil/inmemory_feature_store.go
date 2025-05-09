package testutil

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/feature"
	ierr "github.com/flexprice/flexprice/internal/errors"
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

	// Check environment ID
	if !CheckEnvironmentFilter(ctx, f.EnvironmentID) {
		return false
	}

	// Filter by meter IDs
	if len(filter_.MeterIDs) > 0 {
		if !lo.Contains(filter_.MeterIDs, f.MeterID) {
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

	// Filter by name contains
	if filter_.NameContains != "" {
		if !strings.Contains(strings.ToLower(f.Name), strings.ToLower(filter_.NameContains)) {
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
		return ierr.NewError("feature cannot be nil").
			WithHint("Feature data is required").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if f.EnvironmentID == "" {
		f.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	err := s.InMemoryStore.Create(ctx, f.ID, f)
	if err != nil {
		if err.Error() == "item already exists" {
			return ierr.WithError(err).
				WithHint("A feature with this ID already exists").
				WithReportableDetails(map[string]any{
					"feature_id": f.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create feature").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryFeatureStore) Get(ctx context.Context, id string) (*feature.Feature, error) {
	feature, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		if err.Error() == "item not found" {
			return nil, ierr.WithError(err).
				WithHintf("Feature with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"feature_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHintf("Failed to get feature with ID %s", id).
			Mark(ierr.ErrDatabase)
	}
	return feature, nil
}

func (s *InMemoryFeatureStore) List(ctx context.Context, filter *types.FeatureFilter) ([]*feature.Feature, error) {
	features, err := s.InMemoryStore.List(ctx, filter, featureFilterFn, featureSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list features").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}
	return features, nil
}

func (s *InMemoryFeatureStore) Count(ctx context.Context, filter *types.FeatureFilter) (int, error) {
	count, err := s.InMemoryStore.Count(ctx, filter, featureFilterFn)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count features").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (s *InMemoryFeatureStore) Update(ctx context.Context, f *feature.Feature) error {
	if f == nil {
		return ierr.NewError("feature cannot be nil").
			WithHint("Feature data is required").
			Mark(ierr.ErrValidation)
	}

	err := s.InMemoryStore.Update(ctx, f.ID, f)
	if err != nil {
		if err.Error() == "item not found" {
			return ierr.WithError(err).
				WithHintf("Feature with ID %s was not found", f.ID).
				WithReportableDetails(map[string]any{
					"feature_id": f.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHintf("Failed to update feature with ID %s", f.ID).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryFeatureStore) Delete(ctx context.Context, id string) error {
	err := s.InMemoryStore.Delete(ctx, id)
	if err != nil {
		if err.Error() == "item not found" {
			return ierr.WithError(err).
				WithHintf("Feature with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"feature_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHintf("Failed to delete feature with ID %s", id).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// ListAll returns all features without pagination
func (s *InMemoryFeatureStore) ListAll(ctx context.Context, filter *types.FeatureFilter) ([]*feature.Feature, error) {
	if filter == nil {
		filter = types.NewNoLimitFeatureFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}

	if !filter.IsUnlimited() {
		filter.QueryFilter.Limit = nil
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	// Create an unlimited filter
	unlimitedFilter := &types.FeatureFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		TimeRangeFilter: filter.TimeRangeFilter,
		FeatureIDs:      filter.FeatureIDs,
		LookupKey:       filter.LookupKey,
		NameContains:    filter.NameContains,
	}

	return s.List(ctx, unlimitedFilter)
}

func (s *InMemoryFeatureStore) CreateBulk(ctx context.Context, features []*feature.Feature) ([]*feature.Feature, error) {
	environmentID := types.GetEnvironmentID(ctx)

	for _, f := range features {
		if f == nil {
			return nil, ierr.NewError("feature cannot be nil").
				WithHint("Feature data is required").
				Mark(ierr.ErrValidation)
		}

		// Set environment ID from context if not already set
		if f.EnvironmentID == "" {
			f.EnvironmentID = environmentID
		}

		if err := s.Create(ctx, f); err != nil {
			return nil, err
		}
	}
	return features, nil
}

func (s *InMemoryFeatureStore) DeleteBulk(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := s.Delete(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// Clear clears the feature store
func (s *InMemoryFeatureStore) Clear() {
	s.InMemoryStore.Clear()
}

// ListByIDs retrieves features by their IDs
func (s *InMemoryFeatureStore) ListByIDs(ctx context.Context, featureIDs []string) ([]*feature.Feature, error) {
	if len(featureIDs) == 0 {
		return []*feature.Feature{}, nil
	}

	// Create a filter with feature IDs
	filter := &types.FeatureFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		FeatureIDs:  featureIDs,
	}

	// Use the existing List method
	return s.List(ctx, filter)
}
