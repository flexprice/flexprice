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

	// Only return published connections
	if item.Status != types.StatusPublished {
		return nil, ierr.NewError("connection not found").
			WithHintf("Connection with ID %s was not found", id).
			WithReportableDetails(map[string]any{
				"connection_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	return copyConnection(item), nil
}

func (s *InMemoryConnectionStore) GetByProvider(ctx context.Context, provider types.SecretProvider) (*connection.Connection, error) {
	// Create a filter function that matches by provider_type, tenant_id, environment (from ctx), and published status
	filterFn := func(ctx context.Context, c *connection.Connection, _ interface{}) bool {
		return c.ProviderType == provider &&
			c.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, c.EnvironmentID) &&
			c.Status == types.StatusPublished
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
			WithHintf("Connection with provider %s was not found in this environment", provider).
			WithReportableDetails(map[string]any{
				"provider_type": provider,
				"tenant_id":     types.GetTenantID(ctx),
			}).
			Mark(ierr.ErrNotFound)
	}

	return copyConnection(connections[0]), nil
}

func (s *InMemoryConnectionStore) ListPublishedByProvider(ctx context.Context, provider types.SecretProvider) ([]*connection.Connection, error) {
	// Match by provider + published status across all tenants/environments.
	filterFn := func(_ context.Context, c *connection.Connection, _ interface{}) bool {
		return c.ProviderType == provider && c.Status == types.StatusPublished
	}
	connections, err := s.store.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list connections").
			Mark(ierr.ErrDatabase)
	}
	return lo.Map(connections, func(c *connection.Connection, _ int) *connection.Connection {
		return copyConnection(c)
	}), nil
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

	// Apply published status filter - only return published connections
	if c.Status != types.StatusPublished {
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

	return &connection.Connection{
		ID:                  c.ID,
		Name:                c.Name,
		ProviderType:        c.ProviderType,
		EncryptedSecretData: c.EncryptedSecretData,
		EnvironmentID:       c.EnvironmentID,
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
