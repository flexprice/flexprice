package testutil

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/entitlement"
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

	// Filter by plan IDs
	if len(f.PlanIDs) > 0 {
		found := false
		for _, id := range f.PlanIDs {
			if e.PlanID == id {
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
		return nil, fmt.Errorf("entitlement cannot be nil")
	}
	err := s.InMemoryStore.Create(ctx, e.ID, e)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (s *InMemoryEntitlementStore) Get(ctx context.Context, id string) (*entitlement.Entitlement, error) {
	return s.InMemoryStore.Get(ctx, id)
}

func (s *InMemoryEntitlementStore) List(ctx context.Context, filter *types.EntitlementFilter) ([]*entitlement.Entitlement, error) {
	return s.InMemoryStore.List(ctx, filter, entitlementFilterFn, entitlementSortFn)
}

func (s *InMemoryEntitlementStore) Count(ctx context.Context, filter *types.EntitlementFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, entitlementFilterFn)
}

func (s *InMemoryEntitlementStore) Update(ctx context.Context, e *entitlement.Entitlement) (*entitlement.Entitlement, error) {
	if e == nil {
		return nil, fmt.Errorf("entitlement cannot be nil")
	}
	err := s.InMemoryStore.Update(ctx, e.ID, e)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (s *InMemoryEntitlementStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
}

func (s *InMemoryEntitlementStore) CreateBulk(ctx context.Context, entitlements []*entitlement.Entitlement) ([]*entitlement.Entitlement, error) {
	for _, e := range entitlements {
		if e == nil {
			return nil, fmt.Errorf("entitlement cannot be nil")
		}
		if err := s.InMemoryStore.Create(ctx, e.ID, e); err != nil {
			return nil, err
		}
	}
	return entitlements, nil
}

func (s *InMemoryEntitlementStore) DeleteBulk(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := s.InMemoryStore.Delete(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// Clear clears the entitlement store
func (s *InMemoryEntitlementStore) Clear() {
	s.InMemoryStore.Clear()
}
