package testutil

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/group"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryGroupStore implements group.Repository for tests.
type InMemoryGroupStore struct {
	*InMemoryStore[*group.Group]
}

// NewInMemoryGroupStore creates a new in-memory group store.
func NewInMemoryGroupStore() *InMemoryGroupStore {
	return &InMemoryGroupStore{
		InMemoryStore: NewInMemoryStore[*group.Group](),
	}
}

func groupFilterFn(ctx context.Context, g *group.Group, filter interface{}) bool {
	if g == nil {
		return false
	}
	f, ok := filter.(*types.GroupFilter)
	if !ok {
		return true
	}
	if !CheckTenantFilter(ctx, g.TenantID) {
		return false
	}
	if !CheckEnvironmentFilter(ctx, g.EnvironmentID) {
		return false
	}
	if g.Status == types.StatusArchived || g.Status == types.StatusDeleted {
		return false
	}
	if len(f.GroupIDs) > 0 && !lo.Contains(f.GroupIDs, g.ID) {
		return false
	}
	if f.EntityType != "" && string(g.EntityType) != f.EntityType {
		return false
	}
	if f.LookupKey != "" && g.LookupKey != f.LookupKey {
		return false
	}
	if f.Name != "" && !strings.Contains(strings.ToLower(g.Name), strings.ToLower(f.Name)) {
		return false
	}
	return true
}

func groupSortFn(i, j *group.Group) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemoryGroupStore) Create(ctx context.Context, g *group.Group) error {
	if g == nil {
		return ierr.NewError("group cannot be nil").Mark(ierr.ErrValidation)
	}
	return s.InMemoryStore.Create(ctx, g.ID, g)
}

func (s *InMemoryGroupStore) Get(ctx context.Context, id string) (*group.Group, error) {
	g, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if g.Status != types.StatusPublished {
		return nil, ierr.NewError("group not found").WithHint("Group not found").Mark(ierr.ErrNotFound)
	}
	return g, nil
}

func (s *InMemoryGroupStore) GetByLookupKey(ctx context.Context, lookupKey string) (*group.Group, error) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	list, err := s.InMemoryStore.List(ctx, &types.GroupFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		LookupKey:   lookupKey,
	}, groupFilterFn, groupSortFn)
	if err != nil {
		return nil, err
	}
	for _, g := range list {
		if g.LookupKey == lookupKey && g.TenantID == tenantID && g.EnvironmentID == environmentID && g.Status == types.StatusPublished {
			return g, nil
		}
	}
	return nil, ierr.NewError("group not found").WithHint("Group not found").Mark(ierr.ErrNotFound)
}

func (s *InMemoryGroupStore) Update(ctx context.Context, g *group.Group) error {
	if g == nil {
		return ierr.NewError("group cannot be nil").Mark(ierr.ErrValidation)
	}
	return s.InMemoryStore.Update(ctx, g.ID, g)
}

func (s *InMemoryGroupStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
}

func (s *InMemoryGroupStore) List(ctx context.Context, filter *types.GroupFilter) ([]*group.Group, error) {
	if filter == nil {
		filter = types.NewNoLimitGroupFilter()
	}
	return s.InMemoryStore.List(ctx, filter, groupFilterFn, groupSortFn)
}

func (s *InMemoryGroupStore) Count(ctx context.Context, filter *types.GroupFilter) (int, error) {
	if filter == nil {
		filter = types.NewNoLimitGroupFilter()
	}
	return s.InMemoryStore.Count(ctx, filter, groupFilterFn)
}
