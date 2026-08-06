package testutil

import (
	"context"

	domainAlert "github.com/flexprice/flexprice/internal/domain/alert"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryAlertSettingsStore implements an in-memory alert_settings repository for testing
type InMemoryAlertSettingsStore struct {
	*InMemoryStore[*domainAlert.AlertSettings]
}

// NewInMemoryAlertSettingsStore creates a new in-memory alert settings store
func NewInMemoryAlertSettingsStore() *InMemoryAlertSettingsStore {
	return &InMemoryAlertSettingsStore{
		InMemoryStore: NewInMemoryStore[*domainAlert.AlertSettings](),
	}
}

// Create creates a new alert settings row
func (s *InMemoryAlertSettingsStore) Create(ctx context.Context, alertSettings *domainAlert.AlertSettings) error {
	if alertSettings == nil {
		return ierr.NewError("alert settings cannot be nil").
			WithHint("Alert settings data is required").
			Mark(ierr.ErrValidation)
	}

	if alertSettings.EnvironmentID == "" {
		alertSettings.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	if alertSettings.TenantID == "" {
		alertSettings.TenantID = types.GetTenantID(ctx)
	}

	return s.InMemoryStore.Create(ctx, alertSettings.ID, alertSettings)
}

// Get retrieves an alert settings row by ID
func (s *InMemoryAlertSettingsStore) Get(ctx context.Context, id string) (*domainAlert.AlertSettings, error) {
	alertSettings, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if alertSettings.Status == types.StatusArchived {
		return nil, ierr.NewError("alert settings not found").
			WithHint("Alert settings not found").
			Mark(ierr.ErrNotFound)
	}
	return alertSettings, nil
}

// Update updates an existing alert settings row
func (s *InMemoryAlertSettingsStore) Update(ctx context.Context, alertSettings *domainAlert.AlertSettings) error {
	if alertSettings == nil {
		return ierr.NewError("alert settings cannot be nil").
			WithHint("Alert settings data is required").
			Mark(ierr.ErrValidation)
	}
	return s.InMemoryStore.Update(ctx, alertSettings.ID, alertSettings)
}

// Delete soft deletes an alert settings row
func (s *InMemoryAlertSettingsStore) Delete(ctx context.Context, id string) error {
	alertSettings, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return err
	}
	alertSettings.Status = types.StatusArchived
	return s.InMemoryStore.Update(ctx, id, alertSettings)
}

// List retrieves alert settings with filtering and pagination
func (s *InMemoryAlertSettingsStore) List(ctx context.Context, filter *types.AlertSettingsFilter) ([]*domainAlert.AlertSettings, error) {
	if filter == nil {
		filter = types.NewNoLimitAlertSettingsFilter()
	}
	return s.InMemoryStore.List(ctx, filter, alertSettingsFilterFn, alertSettingsSortFn)
}

// Count returns the number of alert settings rows matching the filter
func (s *InMemoryAlertSettingsStore) Count(ctx context.Context, filter *types.AlertSettingsFilter) (int, error) {
	if filter == nil {
		filter = types.NewNoLimitAlertSettingsFilter()
	}
	return s.InMemoryStore.Count(ctx, filter, alertSettingsFilterFn)
}

// alertSettingsFilterFn implements filtering logic for alert settings
func alertSettingsFilterFn(ctx context.Context, alertSettings *domainAlert.AlertSettings, filter interface{}) bool {
	if alertSettings == nil {
		return false
	}

	f, ok := filter.(*types.AlertSettingsFilter)
	if !ok {
		return true
	}

	if !CheckTenantFilter(ctx, alertSettings.TenantID) {
		return false
	}
	if !CheckEnvironmentFilter(ctx, alertSettings.EnvironmentID) {
		return false
	}

	// Default status behaviour mirrors ApplyStatusFilter: no explicit status excludes archived rows.
	if f.GetStatus() == "" {
		if alertSettings.Status == types.StatusArchived {
			return false
		}
	} else if string(alertSettings.Status) != f.GetStatus() {
		return false
	}

	if f.EntityType != "" && alertSettings.EntityType != f.EntityType {
		return false
	}

	if f.EntityID != "" && alertSettings.EntityID != f.EntityID {
		return false
	}

	if len(f.EntityIDs) > 0 && !lo.Contains(f.EntityIDs, alertSettings.EntityID) {
		return false
	}

	if f.ParentEntityType != "" {
		if alertSettings.ParentEntityType == nil || *alertSettings.ParentEntityType != string(f.ParentEntityType) {
			return false
		}
	}

	if f.ParentEntityID != "" {
		if alertSettings.ParentEntityID == nil || *alertSettings.ParentEntityID != f.ParentEntityID {
			return false
		}
	}

	if len(f.ParentEntityIDs) > 0 {
		if alertSettings.ParentEntityID == nil || !lo.Contains(f.ParentEntityIDs, *alertSettings.ParentEntityID) {
			return false
		}
	}

	if f.Enabled != nil && alertSettings.Enabled != *f.Enabled {
		return false
	}

	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && alertSettings.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && alertSettings.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

// alertSettingsSortFn implements sorting logic for alert settings (newest first)
func alertSettingsSortFn(i, j *domainAlert.AlertSettings) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}
