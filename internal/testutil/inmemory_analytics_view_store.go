package testutil

import (
	"context"
	"slices"
	"time"

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
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now()
	}
	v.CreatedBy = types.GetUserID(ctx)
	v.UpdatedBy = v.CreatedBy
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

func (s *InMemoryAnalyticsViewStore) Update(ctx context.Context, v *domainAnalytics.View) error {
	cur, err := s.Get(ctx, v.ID)
	if err != nil {
		return err
	}
	if cur.Version != v.Version-1 {
		return ierr.NewErrorf("analytics view %s changed concurrently", v.ID).
			WithHint("View was modified by someone else; reload and retry").
			Mark(ierr.ErrVersionConflict)
	}
	cur.Name, cur.Definition, cur.Version = v.Name, v.Definition, v.Version
	cur.UpdatedBy, cur.UpdatedAt = types.GetUserID(ctx), time.Now()
	return s.InMemoryStore.Update(ctx, cur.ID, cur)
}

func (s *InMemoryAnalyticsViewStore) Delete(ctx context.Context, id string) error {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	cur.Status, cur.UpdatedBy = types.StatusDeleted, types.GetUserID(ctx)
	return s.InMemoryStore.Update(ctx, id, cur)
}

func (s *InMemoryAnalyticsViewStore) List(ctx context.Context) ([]*domainAnalytics.View, error) {
	out, err := s.InMemoryStore.List(ctx, nil, func(ctx context.Context, v *domainAnalytics.View, _ interface{}) bool {
		return CheckTenantFilter(ctx, v.TenantID) && CheckEnvironmentFilter(ctx, v.EnvironmentID) && v.Status == types.StatusPublished
	}, nil)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b *domainAnalytics.View) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}
