package testutil

import (
	"context"

	domainAnalytics "github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryAnalyticsViewStore implements analytics.Repository for testing.
type InMemoryAnalyticsViewStore struct {
	*InMemoryStore[*domainAnalytics.View]
}

// NewInMemoryAnalyticsViewStore creates a new in-memory analytics view store.
func NewInMemoryAnalyticsViewStore() *InMemoryAnalyticsViewStore {
	return &InMemoryAnalyticsViewStore{
		InMemoryStore: NewInMemoryStore[*domainAnalytics.View](),
	}
}

func (s *InMemoryAnalyticsViewStore) Create(ctx context.Context, v *domainAnalytics.View) error {
	if v.TenantID == "" {
		v.TenantID = types.GetTenantID(ctx)
	}
	if v.EnvironmentID == "" {
		v.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	if v.Status == "" {
		v.Status = types.StatusPublished
	}
	return s.InMemoryStore.Create(ctx, v.ID, v)
}

func (s *InMemoryAnalyticsViewStore) Get(ctx context.Context, id string) (*domainAnalytics.View, error) {
	v, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Mirror the real ent repo's Get: scoped to tenant + environment + status=published.
	if !CheckTenantFilter(ctx, v.TenantID) || !CheckEnvironmentFilter(ctx, v.EnvironmentID) || v.Status != types.StatusPublished {
		return nil, ierr.NewError("item not found").
			WithHintf("Item with ID %s was not found", id).
			WithReportableDetails(map[string]any{
				"id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	return v, nil
}

func (s *InMemoryAnalyticsViewStore) List(ctx context.Context) ([]*domainAnalytics.View, error) {
	return s.InMemoryStore.List(ctx, nil, func(ctx context.Context, v *domainAnalytics.View, _ interface{}) bool {
		return CheckTenantFilter(ctx, v.TenantID) && CheckEnvironmentFilter(ctx, v.EnvironmentID) && v.Status == types.StatusPublished
	}, nil)
}
