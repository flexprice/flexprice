package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryEntitlementStore implements entitlement.Repository
type InMemoryEntitlementStore struct {
	*InMemoryStore[*entitlement.Entitlement]
}

// NewInMemoryEntitlementStore creates a new in-memory entitlement store
func NewInMemoryEntitlementStore() *InMemoryEntitlementStore {
	return &InMemoryEntitlementStore{
		InMemoryStore: NewInMemoryStore[*entitlement.Entitlement](),
	}
}

// entitlementFilterFn implements filtering logic for entitlements
func entitlementFilterFn(ctx context.Context, e *entitlement.Entitlement, filter interface{}) bool {
	if e == nil {
		return false
	}

	f, ok := filter.(*types.EntitlementFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if e.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, e.EnvironmentID) {
		return false
	}

	// Filter by entity IDs
	if len(f.EntityIDs) > 0 {
		found := false
		for _, id := range f.EntityIDs {
			if e.EntityID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Filter by feature IDs
	if len(f.FeatureIDs) > 0 {
		found := false
		for _, id := range f.FeatureIDs {
			if e.FeatureID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Filter by feature type
	if f.FeatureType != nil && e.FeatureType != *f.FeatureType {
		return false
	}

	// Filter by is_enabled
	if f.IsEnabled != nil && e.IsEnabled != *f.IsEnabled {
		return false
	}

	// Filter by time range
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && e.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && e.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

// entitlementSortFn implements sorting logic for entitlements
func entitlementSortFn(i, j *entitlement.Entitlement) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemoryEntitlementStore) Create(ctx context.Context, e *entitlement.Entitlement) (*entitlement.Entitlement, error) {
	if e == nil {
		return nil, errors.NewError("entitlement cannot be nil").Mark(errors.ErrValidation)
	}

	// Set environment ID from context if not already set
	if e.EnvironmentID == "" {
		e.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	err := s.InMemoryStore.Create(ctx, e.ID, e)
	if err != nil {
		return nil, errors.WithError(err).
			WithHint("Failed to create entitlement").
			WithReportableDetails(map[string]interface{}{
				"id":         e.ID,
				"entity_id":  e.EntityID,
				"feature_id": e.FeatureID,
			}).
			Mark(errors.ErrDatabase)
	}
	return e, nil
}

func (s *InMemoryEntitlementStore) Get(ctx context.Context, id string) (*entitlement.Entitlement, error) {
	e, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, errors.WithError(err).
			WithHint("Entitlement not found").
			WithReportableDetails(map[string]interface{}{
				"id": id,
			}).
			Mark(errors.ErrNotFound)
	}
	return e, nil
}

func (s *InMemoryEntitlementStore) List(ctx context.Context, filter *types.EntitlementFilter) ([]*entitlement.Entitlement, error) {
	entitlements, err := s.InMemoryStore.List(ctx, filter, entitlementFilterFn, entitlementSortFn)
	if err != nil {
		return nil, errors.WithError(err).
			WithHint("Failed to list entitlements").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(errors.ErrDatabase)
	}
	return entitlements, nil
}

func (s *InMemoryEntitlementStore) Count(ctx context.Context, filter *types.EntitlementFilter) (int, error) {
	count, err := s.InMemoryStore.Count(ctx, filter, entitlementFilterFn)
	if err != nil {
		return 0, errors.WithError(err).
			WithHint("Failed to count entitlements").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(errors.ErrDatabase)
	}
	return count, nil
}

func (s *InMemoryEntitlementStore) Update(ctx context.Context, e *entitlement.Entitlement) (*entitlement.Entitlement, error) {
	if e == nil {
		return nil, errors.NewError("entitlement cannot be nil").Mark(errors.ErrValidation)
	}
	err := s.InMemoryStore.Update(ctx, e.ID, e)
	if err != nil {
		return nil, errors.WithError(err).
			WithHint("Failed to update entitlement").
			WithReportableDetails(map[string]interface{}{
				"id": e.ID,
			}).
			Mark(errors.ErrDatabase)
	}
	return e, nil
}

func (s *InMemoryEntitlementStore) Delete(ctx context.Context, id string) error {
	err := s.InMemoryStore.Delete(ctx, id)
	if err != nil {
		return errors.WithError(err).
			WithHint("Failed to delete entitlement").
			WithReportableDetails(map[string]interface{}{
				"id": id,
			}).
			Mark(errors.ErrDatabase)
	}
	return nil
}

func (s *InMemoryEntitlementStore) CreateBulk(ctx context.Context, entitlements []*entitlement.Entitlement) ([]*entitlement.Entitlement, error) {
	environmentID := types.GetEnvironmentID(ctx)

	for _, e := range entitlements {
		if e == nil {
			return nil, errors.NewError("entitlement cannot be nil").Mark(errors.ErrValidation)
		}

		// Set environment ID from context if not already set
		if e.EnvironmentID == "" {
			e.EnvironmentID = environmentID
		}

		if err := s.InMemoryStore.Create(ctx, e.ID, e); err != nil {
			return nil, errors.WithError(err).
				WithHint("Failed to create entitlement in bulk").
				WithReportableDetails(map[string]interface{}{
					"id": e.ID,
				}).
				Mark(errors.ErrDatabase)
		}
	}
	return entitlements, nil
}

func (s *InMemoryEntitlementStore) DeleteBulk(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := s.InMemoryStore.Delete(ctx, id); err != nil {
			return errors.WithError(err).
				WithHint("Failed to delete entitlement in bulk").
				WithReportableDetails(map[string]interface{}{
					"id": id,
				}).
				Mark(errors.ErrDatabase)
		}
	}
	return nil
}

// Clear clears the entitlement store
func (s *InMemoryEntitlementStore) Clear() {
	s.InMemoryStore.Clear()
}

// ListByPlanIDs retrieves all entitlements for the given plan IDs
func (s *InMemoryEntitlementStore) ListByPlanIDs(ctx context.Context, planIDs []string) ([]*entitlement.Entitlement, error) {
	if len(planIDs) == 0 {
		return []*entitlement.Entitlement{}, nil
	}

	// Create a filter with entity IDs (for plan entitlements)
	entityType := types.ENTITLEMENT_ENTITY_TYPE_PLAN
	filter := &types.EntitlementFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  &entityType,
		EntityIDs:   planIDs,
	}

	// Use the existing List method
	return s.List(ctx, filter)
}

// ListByFeatureIDs retrieves all entitlements for the given feature IDs
func (s *InMemoryEntitlementStore) ListByFeatureIDs(ctx context.Context, featureIDs []string) ([]*entitlement.Entitlement, error) {
	if len(featureIDs) == 0 {
		return []*entitlement.Entitlement{}, nil
	}

	// Create a filter with feature IDs
	filter := &types.EntitlementFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		FeatureIDs:  featureIDs,
	}

	// Use the existing List method
	return s.List(ctx, filter)
}

// ListByAddonIDs retrieves all entitlements for the given addon IDs
func (s *InMemoryEntitlementStore) ListByAddonIDs(ctx context.Context, addonIDs []string) ([]*entitlement.Entitlement, error) {
	if len(addonIDs) == 0 {
		return []*entitlement.Entitlement{}, nil
	}

	// Create a filter with entity IDs (for addon entitlements)
	entityType := types.ENTITLEMENT_ENTITY_TYPE_ADDON
	filter := &types.EntitlementFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  &entityType,
		EntityIDs:   addonIDs,
	}

	// Use the existing List method
	return s.List(ctx, filter)
}
