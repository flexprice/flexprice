package testutil

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/addon"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryAddonStore implements addon.Repository
type InMemoryAddonStore struct {
	*InMemoryStore[*addon.Addon]
}

// NewInMemoryAddonStore creates a new in-memory addon store
func NewInMemoryAddonStore() *InMemoryAddonStore {
	return &InMemoryAddonStore{
		InMemoryStore: NewInMemoryStore[*addon.Addon](),
	}
}

// Helper to copy addon
func copyAddon(a *addon.Addon) *addon.Addon {
	if a == nil {
		return nil
	}

	// Deep copy of addon
	copied := &addon.Addon{
		ID:            a.ID,
		EnvironmentID: a.EnvironmentID,
		LookupKey:     a.LookupKey,
		Name:          a.Name,
		Description:   a.Description,
		Type:          a.Type,
		Metadata:      lo.Assign(map[string]interface{}{}, a.Metadata),
		BaseModel: types.BaseModel{
			TenantID:  a.TenantID,
			Status:    a.Status,
			CreatedAt: a.CreatedAt,
			UpdatedAt: a.UpdatedAt,
			CreatedBy: a.CreatedBy,
			UpdatedBy: a.UpdatedBy,
		},
	}

	return copied
}

func (s *InMemoryAddonStore) Create(ctx context.Context, a *addon.Addon) error {
	if a == nil {
		return ierr.NewError("addon cannot be nil").
			WithHint("Addon cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if a.EnvironmentID == "" {
		a.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return s.InMemoryStore.Create(ctx, a.ID, copyAddon(a))
}

func (s *InMemoryAddonStore) GetByID(ctx context.Context, id string) (*addon.Addon, error) {
	a, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, ierr.NewError("addon not found").
			WithHint("Addon not found").
			WithReportableDetails(map[string]interface{}{
				"id": id,
			}).
			Mark(ierr.ErrNotFound)
	}
	return copyAddon(a), nil
}

func (s *InMemoryAddonStore) GetByLookupKey(ctx context.Context, lookupKey string) (*addon.Addon, error) {
	// Create a filter function that matches by lookup_key
	filterFn := func(ctx context.Context, a *addon.Addon, _ interface{}) bool {
		return a.LookupKey == lookupKey &&
			a.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, a.EnvironmentID)
	}

	addons, err := s.InMemoryStore.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, err
	}

	if len(addons) == 0 {
		return nil, ierr.NewError("addon not found").
			WithHint("Addon not found").
			WithReportableDetails(map[string]interface{}{
				"lookup_key": lookupKey,
			}).
			Mark(ierr.ErrNotFound)
	}

	return copyAddon(addons[0]), nil
}

func (s *InMemoryAddonStore) Update(ctx context.Context, a *addon.Addon) error {
	if a == nil {
		return ierr.NewError("addon cannot be nil").
			WithHint("Addon cannot be nil").
			Mark(ierr.ErrValidation)
	}

	return s.InMemoryStore.Update(ctx, a.ID, copyAddon(a))
}

func (s *InMemoryAddonStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
}

func (s *InMemoryAddonStore) List(ctx context.Context, filter *types.AddonFilter) ([]*addon.Addon, error) {
	if filter == nil {
		filter = types.NewAddonFilter()
	}

	return s.InMemoryStore.List(ctx, filter, addonFilterFn, addonSortFn)
}

func (s *InMemoryAddonStore) Count(ctx context.Context, filter *types.AddonFilter) (int, error) {
	if filter == nil {
		filter = types.NewAddonFilter()
	}

	return s.InMemoryStore.Count(ctx, filter, addonFilterFn)
}

func addonFilterFn(ctx context.Context, a *addon.Addon, filter interface{}) bool {
	f, ok := filter.(*types.AddonFilter)
	if !ok {
		return true
	}

	// Check tenant and environment
	if a.TenantID != types.GetTenantID(ctx) {
		return false
	}

	if !CheckEnvironmentFilter(ctx, a.EnvironmentID) {
		return false
	}

	// Apply filter conditions
	for _, condition := range f.Filters {
		if !applyAddonFilterCondition(a, condition) {
			return false
		}
	}

	// Check specific filters
	if len(f.AddonIDs) > 0 {
		if !lo.Contains(f.AddonIDs, a.ID) {
			return false
		}
	}

	if f.AddonType != "" {
		if a.Type != f.AddonType {
			return false
		}
	}

	if len(f.LookupKeys) > 0 {
		if !lo.Contains(f.LookupKeys, a.LookupKey) {
			return false
		}
	}

	return true
}

func applyAddonFilterCondition(a *addon.Addon, condition *types.FilterCondition) bool {
	if condition.Field == nil {
		return true
	}

	switch *condition.Field {
	case "id":
		if condition.Value != nil && condition.Value.String != nil {
			return strings.Contains(strings.ToLower(a.ID), strings.ToLower(*condition.Value.String))
		}
	case "name":
		if condition.Value != nil && condition.Value.String != nil {
			return strings.Contains(strings.ToLower(a.Name), strings.ToLower(*condition.Value.String))
		}
	case "lookup_key":
		if condition.Value != nil && condition.Value.String != nil {
			return strings.Contains(strings.ToLower(a.LookupKey), strings.ToLower(*condition.Value.String))
		}
	case "type":
		if condition.Value != nil && condition.Value.String != nil {
			return string(a.Type) == *condition.Value.String
		}
	case "status":
		if condition.Value != nil && condition.Value.String != nil {
			return string(a.Status) == *condition.Value.String
		}
	case "environment_id":
		if condition.Value != nil && condition.Value.String != nil {
			return a.EnvironmentID == *condition.Value.String
		}
	default:
		return true
	}

	return true
}

func addonSortFn(i, j *addon.Addon) bool {
	// Default sort by created_at desc
	return i.CreatedAt.After(j.CreatedAt)
} 