package testutil

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	domainSettings "github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemorySettingsStore implements an in-memory settings repository for testing
type InMemorySettingsStore struct {
	*InMemoryStore[*domainSettings.Setting]
}

// NewInMemorySettingsStore creates a new in-memory settings store
func NewInMemorySettingsStore() *InMemorySettingsStore {
	return &InMemorySettingsStore{
		InMemoryStore: NewInMemoryStore[*domainSettings.Setting](),
	}
}

// Create creates a new setting
func (s *InMemorySettingsStore) Create(ctx context.Context, setting *domainSettings.Setting) error {
	return s.InMemoryStore.Create(ctx, setting.ID, setting)
}

// Update updates an existing setting
func (s *InMemorySettingsStore) Update(ctx context.Context, setting *domainSettings.Setting) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.items[setting.ID]; !exists {
		return ierr.NewError("setting not found").
			WithHintf("Setting with ID %s was not found", setting.ID).
			WithReportableDetails(map[string]any{
				"id": setting.ID,
			}).
			Mark(ierr.ErrNotFound)
	}

	s.items[setting.ID] = setting
	return nil
}

// Get retrieves a setting by ID
func (s *InMemorySettingsStore) Get(ctx context.Context, id string) (*domainSettings.Setting, error) {
	return s.InMemoryStore.Get(ctx, id)
}

// GetByKey retrieves a setting by key for a specific tenant and environment
func (s *InMemorySettingsStore) GetByKey(ctx context.Context, key types.SettingKey) (*domainSettings.Setting, error) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Find setting by tenant, environment, and key
	for _, setting := range s.items {
		if setting.TenantID == tenantID &&
			setting.EnvironmentID == environmentID &&
			setting.Key == key &&
			setting.Status == types.StatusPublished {
			return setting, nil
		}
	}

	return nil, ierr.WithError(&ent.NotFoundError{}).
		WithHintf("Setting with key %s was not found", key).
		WithReportableDetails(map[string]any{
			"key": key,
		}).
		Mark(ierr.ErrNotFound)
}

// GetTenantLevelSettingByKey retrieves a tenant-level setting by key (without environment_id)
// This is for settings that apply tenant-wide across all environments
func (s *InMemorySettingsStore) GetTenantLevelSettingByKey(ctx context.Context, key types.SettingKey) (*domainSettings.Setting, error) {
	tenantID := types.GetTenantID(ctx)

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Find setting by tenant, empty environment_id, and key
	for _, setting := range s.items {
		if setting.TenantID == tenantID &&
			setting.EnvironmentID == "" &&
			setting.Key == key &&
			setting.Status == types.StatusPublished {
			return setting, nil
		}
	}

	return nil, ierr.WithError(&ent.NotFoundError{}).
		WithHintf("Setting with key %s was not found", key).
		WithReportableDetails(map[string]any{
			"key": key,
		}).
		Mark(ierr.ErrNotFound)
}

// DeleteByKey deletes a setting by key for a specific tenant and environment
func (s *InMemorySettingsStore) DeleteByKey(ctx context.Context, key types.SettingKey) error {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Find and delete setting by tenant, environment, and key
	for id, setting := range s.items {
		if setting.TenantID == tenantID &&
			setting.EnvironmentID == environmentID &&
			setting.Key == key &&
			setting.Status == types.StatusPublished {
			delete(s.items, id)
			return nil
		}
	}

	return ierr.NewError("setting not found").
		WithHintf("Setting with key %s was not found", key).
		WithReportableDetails(map[string]any{
			"key":            key,
			"tenant_id":      tenantID,
			"environment_id": environmentID,
		}).
		Mark(ierr.ErrNotFound)
}

// DeleteTenantLevelSettingByKey deletes a tenant-level setting by key (without environment_id)
func (s *InMemorySettingsStore) DeleteTenantLevelSettingByKey(ctx context.Context, key types.SettingKey) error {
	tenantID := types.GetTenantID(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Find and delete tenant-level setting by tenant, empty environment_id, and key
	for id, setting := range s.items {
		if setting.TenantID == tenantID &&
			setting.EnvironmentID == "" &&
			setting.Key == key &&
			setting.Status == types.StatusPublished {
			delete(s.items, id)
			return nil
		}
	}

	return ierr.NewError("setting not found").
		WithHintf("Tenant-level setting with key %s was not found", key).
		WithReportableDetails(map[string]any{
			"key":       key,
			"tenant_id": tenantID,
		}).
		Mark(ierr.ErrNotFound)
}

// Clear removes all settings from the store
func (s *InMemorySettingsStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[string]*domainSettings.Setting)
}

// ListAllTenantEnvSettingsByKey returns all settings for a given key across all tenants and environments
func (s *InMemorySettingsStore) ListAllTenantEnvSettingsByKey(ctx context.Context, key types.SettingKey) ([]*types.TenantEnvConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var configs []*types.TenantEnvConfig

	for _, setting := range s.items {
		if setting.Key == key && setting.Status == types.StatusPublished {
			config := &types.TenantEnvConfig{
				TenantID:      setting.TenantID,
				EnvironmentID: setting.EnvironmentID,
				Config:        setting.Value,
			}
			configs = append(configs, config)
		}
	}

	return configs, nil
}

// GetAllTenantEnvSubscriptionSettings returns all subscription configs across all tenants and environments
func (s *InMemorySettingsStore) GetAllTenantEnvSubscriptionSettings(ctx context.Context) ([]*types.TenantEnvSubscriptionConfig, error) {
	configs, err := s.ListAllTenantEnvSettingsByKey(ctx, types.SettingKeySubscriptionConfig)
	if err != nil {
		return nil, err
	}

	var subscriptionConfigs []*types.TenantEnvSubscriptionConfig
	for _, config := range configs {
		subscriptionConfig := &types.TenantEnvSubscriptionConfig{
			TenantID:      config.TenantID,
			EnvironmentID: config.EnvironmentID,
			SubscriptionConfig: &types.SubscriptionConfig{
				GracePeriodDays:         config.Config["grace_period_days"].(int),
				AutoCancellationEnabled: config.Config["auto_cancellation_enabled"].(bool),
			},
		}
		subscriptionConfigs = append(subscriptionConfigs, subscriptionConfig)
	}

	return subscriptionConfigs, nil
}
