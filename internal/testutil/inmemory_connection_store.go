package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type InMemoryConnectionStore struct {
	store *InMemoryStore[*connection.Connection]
}

func NewInMemoryConnectionStore() *InMemoryConnectionStore {
	return &InMemoryConnectionStore{
		store: NewInMemoryStore[*connection.Connection](),
	}
}

func (s *InMemoryConnectionStore) Create(ctx context.Context, c *connection.Connection) error {
	return s.store.Create(ctx, c.ID, copyConnection(c))
}

func (s *InMemoryConnectionStore) Get(ctx context.Context, id string) (*connection.Connection, error) {
	item, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return copyConnection(item), nil
}

func (s *InMemoryConnectionStore) GetByEnvironmentAndProvider(ctx context.Context, environmentID string, provider types.SecretProvider) (*connection.Connection, error) {
	// Create a filter function that matches by environment_id, provider_type, tenant_id
	filterFn := func(ctx context.Context, c *connection.Connection, _ interface{}) bool {
		return c.EnvironmentID == environmentID &&
			c.ProviderType == provider &&
			c.TenantID == types.GetTenantID(ctx)
	}

	// List all connections with our filter
	connections, err := s.store.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list connections").
			Mark(ierr.ErrDatabase)
	}

	if len(connections) == 0 {
		return nil, ierr.NewError("connection not found").
			WithHintf("Connection with environment ID %s and provider %s was not found", environmentID, provider).
			WithReportableDetails(map[string]any{
				"environment_id": environmentID,
				"provider_type":  provider,
			}).
			Mark(ierr.ErrNotFound)
	}

	return copyConnection(connections[0]), nil
}

func (s *InMemoryConnectionStore) List(ctx context.Context, filter *types.ConnectionFilter) ([]*connection.Connection, error) {
	items, err := s.store.List(ctx, filter, connectionFilterFn, connectionSortFn)
	if err != nil {
		return nil, err
	}

	return lo.Map(items, func(c *connection.Connection, _ int) *connection.Connection {
		return copyConnection(c)
	}), nil
}

func (s *InMemoryConnectionStore) Count(ctx context.Context, filter *types.ConnectionFilter) (int, error) {
	return s.store.Count(ctx, filter, connectionFilterFn)
}

func (s *InMemoryConnectionStore) ListAll(ctx context.Context, filter *types.ConnectionFilter) ([]*connection.Connection, error) {
	f := *filter
	f.QueryFilter = types.NewNoLimitQueryFilter()
	return s.List(ctx, &f)
}

func (s *InMemoryConnectionStore) Update(ctx context.Context, c *connection.Connection) error {
	return s.store.Update(ctx, c.ID, copyConnection(c))
}

func (s *InMemoryConnectionStore) Delete(ctx context.Context, connection *connection.Connection) error {
	return s.store.Delete(ctx, connection.ID)
}

func (s *InMemoryConnectionStore) Clear() {
	s.store.Clear()
}

// connectionFilterFn implements filtering logic for connections
func connectionFilterFn(ctx context.Context, c *connection.Connection, filter interface{}) bool {
	f, ok := filter.(*types.ConnectionFilter)
	if !ok {
		return false
	}

	// Apply tenant filter
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" && c.TenantID != tenantID {
		return false
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, c.EnvironmentID) {
		return false
	}

	// Apply provider type filter
	if f.ProviderType != "" && c.ProviderType != f.ProviderType {
		return false
	}

	// Apply connection ID filter
	if len(f.ConnectionIDs) > 0 && !lo.Contains(f.ConnectionIDs, c.ID) {
		return false
	}

	// Apply time range filter if present
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && c.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && c.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

// connectionSortFn implements sorting logic for connections
func connectionSortFn(i, j *connection.Connection) bool {
	// Default sort by created_at desc
	return i.CreatedAt.After(j.CreatedAt)
}

// ListByFilter retrieves connections based on filter
func (s *InMemoryConnectionStore) ListByFilter(ctx context.Context, filter *types.ConnectionFilter) ([]*connection.Connection, error) {
	return s.List(ctx, filter)
}

// CountByFilter counts connections based on filter
func (s *InMemoryConnectionStore) CountByFilter(ctx context.Context, filter *types.ConnectionFilter) (int, error) {
	return s.Count(ctx, filter)
}

func copyConnection(c *connection.Connection) *connection.Connection {
	if c == nil {
		return nil
	}

	metadata := make(map[string]interface{})
	for k, v := range c.Metadata {
		metadata[k] = v
	}

	return &connection.Connection{
		ID:            c.ID,
		Name:          c.Name,
		ProviderType:  c.ProviderType,
		Metadata:      metadata,
		EnvironmentID: c.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  c.TenantID,
			Status:    c.Status,
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
			CreatedBy: c.CreatedBy,
			UpdatedBy: c.UpdatedBy,
		},
	}
}
