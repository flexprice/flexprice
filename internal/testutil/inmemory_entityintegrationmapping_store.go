package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryEntityIntegrationMappingStore implements entityintegrationmapping.Repository for testing
type InMemoryEntityIntegrationMappingStore struct {
	*InMemoryStore[*entityintegrationmapping.EntityIntegrationMapping]
}

// NewInMemoryEntityIntegrationMappingStore creates a new in-memory entity integration mapping store
func NewInMemoryEntityIntegrationMappingStore() entityintegrationmapping.Repository {
	return &InMemoryEntityIntegrationMappingStore{
		InMemoryStore: NewInMemoryStore[*entityintegrationmapping.EntityIntegrationMapping](),
	}
}

// Helper to copy entity integration mapping
func copyEntityIntegrationMapping(m *entityintegrationmapping.EntityIntegrationMapping) *entityintegrationmapping.EntityIntegrationMapping {
	if m == nil {
		return nil
	}

	// Deep copy of entity integration mapping
	m = &entityintegrationmapping.EntityIntegrationMapping{
		ID:               m.ID,
		EntityID:         m.EntityID,
		EntityType:       m.EntityType,
		ProviderType:     m.ProviderType,
		ProviderEntityID: m.ProviderEntityID,
		Metadata:         lo.Assign(map[string]interface{}{}, m.Metadata),
		EnvironmentID:    m.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  m.TenantID,
			Status:    m.Status,
			CreatedAt: m.CreatedAt,
			UpdatedAt: m.UpdatedAt,
			CreatedBy: m.CreatedBy,
			UpdatedBy: m.UpdatedBy,
		},
	}

	return m
}

// Create adds a new entity integration mapping to the store
func (s *InMemoryEntityIntegrationMappingStore) Create(ctx context.Context, mapping *entityintegrationmapping.EntityIntegrationMapping) error {
	// Set environment ID from context if not already set
	if mapping.EnvironmentID == "" {
		mapping.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	return s.InMemoryStore.Create(ctx, mapping.ID, copyEntityIntegrationMapping(mapping))
}

// Get retrieves an entity integration mapping by ID
func (s *InMemoryEntityIntegrationMappingStore) Get(ctx context.Context, id string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	mapping, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return copyEntityIntegrationMapping(mapping), nil
}

// List retrieves entity integration mappings based on filter
func (s *InMemoryEntityIntegrationMappingStore) List(ctx context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	mappings, err := s.InMemoryStore.List(ctx, filter, entityIntegrationMappingFilterFn, entityIntegrationMappingSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list entity integration mappings").
			Mark(ierr.ErrDatabase)
	}
	return lo.Map(mappings, func(m *entityintegrationmapping.EntityIntegrationMapping, _ int) *entityintegrationmapping.EntityIntegrationMapping {
		return copyEntityIntegrationMapping(m)
	}), nil
}

// Count counts entity integration mappings based on filter
func (s *InMemoryEntityIntegrationMappingStore) Count(ctx context.Context, filter *types.EntityIntegrationMappingFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, entityIntegrationMappingFilterFn)
}

// Update updates an entity integration mapping
func (s *InMemoryEntityIntegrationMappingStore) Update(ctx context.Context, mapping *entityintegrationmapping.EntityIntegrationMapping) error {
	return s.InMemoryStore.Update(ctx, mapping.ID, copyEntityIntegrationMapping(mapping))
}

// Delete removes an entity integration mapping
func (s *InMemoryEntityIntegrationMappingStore) Delete(ctx context.Context, mapping *entityintegrationmapping.EntityIntegrationMapping) error {
	return s.InMemoryStore.Delete(ctx, mapping.ID)
}

// GetByEntityAndProvider retrieves an entity integration mapping by entity and provider
func (s *InMemoryEntityIntegrationMappingStore) GetByEntityAndProvider(ctx context.Context, entityID string, entityType types.IntegrationEntityType, providerType string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	// Create a filter function that matches by entity and provider
	filterFn := func(ctx context.Context, m *entityintegrationmapping.EntityIntegrationMapping, _ interface{}) bool {
		return m.EntityID == entityID &&
			m.EntityType == entityType &&
			m.ProviderType == providerType &&
			m.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, m.EnvironmentID)
	}

	// List all mappings with our filter
	mappings, err := s.InMemoryStore.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list entity integration mappings").
			Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		return nil, ierr.NewError("entity integration mapping not found").
			WithHint("No mapping found for the specified entity and provider").
			WithReportableDetails(map[string]any{
				"entity_id":     entityID,
				"entity_type":   entityType,
				"provider_type": providerType,
			}).
			Mark(ierr.ErrNotFound)
	}

	return copyEntityIntegrationMapping(mappings[0]), nil
}

// GetByProviderEntity retrieves an entity integration mapping by provider entity
func (s *InMemoryEntityIntegrationMappingStore) GetByProviderEntity(ctx context.Context, providerType, providerEntityID string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	// Create a filter function that matches by provider entity
	filterFn := func(ctx context.Context, m *entityintegrationmapping.EntityIntegrationMapping, _ interface{}) bool {
		return m.ProviderType == providerType &&
			m.ProviderEntityID == providerEntityID &&
			m.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, m.EnvironmentID)
	}

	// List all mappings with our filter
	mappings, err := s.InMemoryStore.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list entity integration mappings").
			Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		return nil, ierr.NewError("entity integration mapping not found").
			WithHint("No mapping found for the specified provider entity").
			WithReportableDetails(map[string]any{
				"provider_type":      providerType,
				"provider_entity_id": providerEntityID,
			}).
			Mark(ierr.ErrNotFound)
	}

	return copyEntityIntegrationMapping(mappings[0]), nil
}

// ListByEntity retrieves entity integration mappings by entity
func (s *InMemoryEntityIntegrationMappingStore) ListByEntity(ctx context.Context, entityID string, entityType types.IntegrationEntityType) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	// Create a filter function that matches by entity
	filterFn := func(ctx context.Context, m *entityintegrationmapping.EntityIntegrationMapping, _ interface{}) bool {
		return m.EntityID == entityID &&
			m.EntityType == entityType &&
			m.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, m.EnvironmentID)
	}

	// List all mappings with our filter
	mappings, err := s.InMemoryStore.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list entity integration mappings").
			Mark(ierr.ErrDatabase)
	}

	return lo.Map(mappings, func(m *entityintegrationmapping.EntityIntegrationMapping, _ int) *entityintegrationmapping.EntityIntegrationMapping {
		return copyEntityIntegrationMapping(m)
	}), nil
}

// ListByProvider retrieves entity integration mappings by provider
func (s *InMemoryEntityIntegrationMappingStore) ListByProvider(ctx context.Context, providerType string) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	// Create a filter function that matches by provider
	filterFn := func(ctx context.Context, m *entityintegrationmapping.EntityIntegrationMapping, _ interface{}) bool {
		return m.ProviderType == providerType &&
			m.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, m.EnvironmentID)
	}

	// List all mappings with our filter
	mappings, err := s.InMemoryStore.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list entity integration mappings").
			Mark(ierr.ErrDatabase)
	}

	return lo.Map(mappings, func(m *entityintegrationmapping.EntityIntegrationMapping, _ int) *entityintegrationmapping.EntityIntegrationMapping {
		return copyEntityIntegrationMapping(m)
	}), nil
}

// entityIntegrationMappingFilterFn implements filtering logic for entity integration mappings
func entityIntegrationMappingFilterFn(ctx context.Context, m *entityintegrationmapping.EntityIntegrationMapping, filter interface{}) bool {
	f, ok := filter.(*types.EntityIntegrationMappingFilter)
	if !ok {
		return false
	}

	// Apply tenant filter
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" && m.TenantID != tenantID {
		return false
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, m.EnvironmentID) {
		return false
	}

	// Apply entity ID filter
	if f.EntityID != "" && m.EntityID != f.EntityID {
		return false
	}

	// Apply entity type filter
	if f.EntityType != "" && m.EntityType != f.EntityType {
		return false
	}

	// Apply provider types filter (plural)
	if len(f.ProviderTypes) > 0 && !lo.Contains(f.ProviderTypes, m.ProviderType) {
		return false
	}

	// Apply provider entity IDs filter (plural)
	if len(f.ProviderEntityIDs) > 0 && !lo.Contains(f.ProviderEntityIDs, m.ProviderEntityID) {
		return false
	}

	// Apply entity IDs filter
	if len(f.EntityIDs) > 0 && !lo.Contains(f.EntityIDs, m.EntityID) {
		return false
	}

	// Retain plural filters (already handled above); nothing more to do here

	// Apply time range filter if present
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && m.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && m.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

// entityIntegrationMappingSortFn implements sorting logic for entity integration mappings
func entityIntegrationMappingSortFn(i, j *entityintegrationmapping.EntityIntegrationMapping) bool {
	// Default sort by created_at desc
	return i.CreatedAt.After(j.CreatedAt)
}

// ListByFilter retrieves entity integration mappings based on filter
func (s *InMemoryEntityIntegrationMappingStore) ListByFilter(ctx context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	return s.List(ctx, filter)
}

// CountByFilter counts entity integration mappings based on filter
func (s *InMemoryEntityIntegrationMappingStore) CountByFilter(ctx context.Context, filter *types.EntityIntegrationMappingFilter) (int, error) {
	return s.Count(ctx, filter)
}
