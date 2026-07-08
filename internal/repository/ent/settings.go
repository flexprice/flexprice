package ent

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/settings"
	"github.com/flexprice/flexprice/internal/cache"
	domainSettings "github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/lib/pq"
)

type settingsRepository struct {
	client     postgres.IClient
	log        *logger.Logger
	redisCache cache.RedisCache
}

func NewSettingsRepository(client postgres.IClient, log *logger.Logger, redisCache cache.RedisCache) domainSettings.Repository {
	return &settingsRepository{
		client:     client,
		log:        log,
		redisCache: redisCache,
	}
}

func (r *settingsRepository) Create(ctx context.Context, s *domainSettings.Setting) error {
	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "creating setting",
		"setting_id", s.ID,
		"tenant_id", s.TenantID,
		"key", s.Key,
	)

	setting, err := client.Settings.Create().
		SetID(s.ID).
		SetTenantID(s.TenantID).
		SetKey(string(s.Key)).
		SetValue(s.Value).
		SetStatus(string(s.Status)).
		SetCreatedAt(s.CreatedAt).
		SetUpdatedAt(s.UpdatedAt).
		SetCreatedBy(s.CreatedBy).
		SetUpdatedBy(s.UpdatedBy).
		SetEnvironmentID(s.EnvironmentID).
		Save(ctx)

	if err != nil {
		if ent.IsConstraintError(err) {
			if pqErr, ok := err.(*pq.Error); ok {
				if strings.Contains(pqErr.Message, "tenant_id_environment_id_key") {
					return ierr.WithError(err).
						WithHint("A setting with this key already exists for this tenant and environment").
						WithReportableDetails(map[string]any{
							"key": s.Key,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
			}
			return ierr.WithError(err).
				WithHint("Failed to create setting").
				WithReportableDetails(map[string]any{
					"key": s.Key,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create setting").
			Mark(ierr.ErrDatabase)
	}

	*s = *domainSettings.FromEnt(setting)
	return nil
}

func (r *settingsRepository) Update(ctx context.Context, s *domainSettings.Setting) error {
	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "updating setting",
		"setting_id", s.ID,
		"tenant_id", s.TenantID,
		"key", s.Key,
	)

	// For tenant_config, use NULL environment_id (tenant-level)
	// Build the WHERE clause based on whether it's tenant_config or not
	_, err := client.Settings.Update().
		Where(
			settings.ID(s.ID),
			settings.TenantID(s.TenantID),
			settings.Status(string(types.StatusPublished)),
		).
		SetValue(s.Value).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Setting with ID %s was not found", s.ID).
				WithReportableDetails(map[string]any{
					"setting_id": s.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update setting").
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, s)
	return nil
}

func (r *settingsRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "deleting setting",
		"setting_id", id,
		"tenant_id", types.GetTenantID(ctx),
		"environment_id", types.GetEnvironmentID(ctx),
	)

	_, err := client.Settings.Update().
		Where(
			settings.ID(id),
			settings.TenantID(types.GetTenantID(ctx)),
			settings.EnvironmentID(types.GetEnvironmentID(ctx)),
			settings.Status(string(types.StatusPublished)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Setting with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"setting_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete setting").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *settingsRepository) Get(ctx context.Context, id string) (*domainSettings.Setting, error) {
	// Try to get from cache first
	if cachedSetting := r.GetCache(ctx, id); cachedSetting != nil {
		return cachedSetting, nil
	}

	client := r.client.Reader(ctx)
	r.log.Debug(ctx, "getting setting", "id", id)

	s, err := client.Settings.Query().
		Where(
			settings.ID(id),
			settings.TenantID(types.GetTenantID(ctx)),
			settings.EnvironmentID(types.GetEnvironmentID(ctx)),
			settings.Status(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Setting with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get setting").
			Mark(ierr.ErrDatabase)
	}

	setting := domainSettings.FromEnt(s)

	// Set cache
	r.SetCache(ctx, setting)
	return setting, nil
}

func (r *settingsRepository) GetByKey(ctx context.Context, key types.SettingKey) (*domainSettings.Setting, error) {

	client := r.client.Reader(ctx)
	r.log.Debug(ctx, "getting setting by key", "key", key)

	s, err := client.Settings.Query().
		Where(
			settings.Key(string(key)),
			settings.TenantID(types.GetTenantID(ctx)),
			settings.EnvironmentID(types.GetEnvironmentID(ctx)),
			settings.Status(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Setting with key %s was not found", string(key)).
				WithReportableDetails(map[string]any{
					"key": string(key),
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get setting by key").
			Mark(ierr.ErrDatabase)
	}

	setting := domainSettings.FromEnt(s)
	return setting, nil
}

// GetTenantLevelSettingByKey retrieves a tenant-level setting by key (without environment_id)
// This is for settings that apply tenant-wide across all environments
func (r *settingsRepository) GetTenantLevelSettingByKey(ctx context.Context, key types.SettingKey) (*domainSettings.Setting, error) {
	client := r.client.Reader(ctx)
	r.log.Debug(ctx, "getting tenant-level setting by key", "key", key)

	s, err := client.Settings.Query().
		Where(
			settings.Key(string(key)),
			settings.TenantID(types.GetTenantID(ctx)),
			settings.EnvironmentID(""),
			settings.Status(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Setting with key %s was not found", string(key)).
				WithReportableDetails(map[string]any{
					"key": string(key),
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get tenant-level setting by key").
			Mark(ierr.ErrDatabase)
	}

	setting := domainSettings.FromEnt(s)
	return setting, nil
}

func (r *settingsRepository) DeleteByKey(ctx context.Context, key types.SettingKey) error {
	// Get the setting first for cache invalidation
	setting, err := r.GetByKey(ctx, key)
	if err != nil {
		return err
	}

	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "deleting setting by key", "key", string(key))

	_, err = client.Settings.Update().
		Where(
			settings.Key(string(key)),
			settings.TenantID(types.GetTenantID(ctx)),
			settings.EnvironmentID(types.GetEnvironmentID(ctx)),
			settings.Status(string(types.StatusPublished)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Setting with key %s was not found", string(key)).
				WithReportableDetails(map[string]any{
					"key": string(key),
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete setting by key").
			Mark(ierr.ErrDatabase)
	}

	// Delete from cache
	r.DeleteCache(ctx, setting)
	return nil
}

func (r *settingsRepository) DeleteTenantLevelSettingByKey(ctx context.Context, key types.SettingKey) error {
	// Get the tenant-level setting first for cache invalidation
	setting, err := r.GetTenantLevelSettingByKey(ctx, key)
	if err != nil {
		return err
	}

	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "deleting tenant-level setting by key", "key", string(key))

	_, err = client.Settings.Update().
		Where(
			settings.Key(string(key)),
			settings.TenantID(types.GetTenantID(ctx)),
			settings.EnvironmentID(""),
			settings.Status(string(types.StatusPublished)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Tenant-level setting with key %s was not found", string(key)).
				WithReportableDetails(map[string]any{
					"key": string(key),
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete tenant-level setting by key").
			Mark(ierr.ErrDatabase)
	}

	// Delete from cache
	r.DeleteCache(ctx, setting)
	return nil
}

func (r *settingsRepository) SetCache(ctx context.Context, setting *domainSettings.Setting) {
	span, ctx := cache.StartRedisCacheSpan(ctx, "settings", "set", map[string]interface{}{
		"setting_id": setting.ID,
		"key":        setting.Key,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixSettings, setting.ID)
	r.redisCache.Set(ctx, cacheKey, setting, cache.ExpiryDefaultRedis)
}

func (r *settingsRepository) GetCache(ctx context.Context, id string) *domainSettings.Setting {
	span, ctx := cache.StartRedisCacheSpan(ctx, "settings", "get", map[string]interface{}{
		"key": id,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixSettings, id)
	value, found := r.redisCache.Get(ctx, cacheKey)
	if !found {
		return nil
	}
	s, ok := cache.UnmarshalCacheValue[domainSettings.Setting](value)
	if !ok {
		return nil
	}
	return s
}

func (r *settingsRepository) DeleteCache(ctx context.Context, setting *domainSettings.Setting) {
	span, ctx := cache.StartRedisCacheSpan(ctx, "settings", "delete", map[string]interface{}{
		"setting_id": setting.ID,
		"key":        setting.Key,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixSettings, setting.ID)
	r.redisCache.Delete(ctx, cacheKey)
}

// ListAllTenantEnvSettingsByKey returns all settings for a given key across all tenants and environments
func (r *settingsRepository) ListAllTenantEnvSettingsByKey(ctx context.Context, key types.SettingKey) ([]*types.TenantEnvConfig, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}

	client := r.client.Reader(ctx)

	// Query all settings for the given key
	settings, err := client.Settings.Query().
		Where(
			settings.Key(string(key)),
			settings.Status(string(types.StatusPublished)),
		).All(ctx)

	if err != nil {
		return nil, ierr.WithError(err).
			WithHintf("Failed to list settings for key %s", string(key)).
			Mark(ierr.ErrDatabase)
	}

	// Return basic config map for all settings
	configs := make([]*types.TenantEnvConfig, 0, len(settings))
	for _, setting := range settings {
		config := &types.TenantEnvConfig{
			TenantID:      setting.TenantID,
			EnvironmentID: setting.EnvironmentID,
			Config:        setting.Value,
		}
		configs = append(configs, config)
	}

	return configs, nil
}

// GetAllTenantEnvSubscriptionSettings returns all subscription configs across all tenants and environments
// Uses simple stateless conversion from map to struct
func (r *settingsRepository) GetAllTenantEnvSubscriptionSettings(ctx context.Context) ([]*types.TenantEnvSubscriptionConfig, error) {
	// Get all configs for subscription key
	configs, err := r.ListAllTenantEnvSettingsByKey(ctx, types.SettingKeySubscriptionConfig)
	if err != nil {
		return nil, err
	}

	// Convert to subscription configs using simple stateless conversion
	subscriptionConfigs := make([]*types.TenantEnvSubscriptionConfig, 0, len(configs))
	for _, config := range configs {
		// Simple conversion: map -> typed struct
		subscriptionConfig, err := utils.ToStruct[types.SubscriptionConfig](config.Config)
		if err != nil {
			r.log.Info(ctx, "failed to convert subscription config",
				"tenant_id", config.TenantID,
				"environment_id", config.EnvironmentID,
				"error", err)
			continue
		}

		r.log.Debug(ctx, "processing subscription config",
			"tenant_id", config.TenantID,
			"environment_id", config.EnvironmentID,
			"auto_cancellation_enabled", subscriptionConfig.AutoCancellationEnabled,
			"grace_period_days", subscriptionConfig.GracePeriodDays)

		// Only include if auto-cancellation is enabled
		if subscriptionConfig.AutoCancellationEnabled {
			subscriptionConfigs = append(subscriptionConfigs, &types.TenantEnvSubscriptionConfig{
				TenantID:           config.TenantID,
				EnvironmentID:      config.EnvironmentID,
				SubscriptionConfig: &subscriptionConfig,
			})
		} else {
			r.log.Info(ctx, "skipping subscription config - auto-cancellation disabled",
				"tenant_id", config.TenantID,
				"environment_id", config.EnvironmentID)
		}
	}

	return subscriptionConfigs, nil
}
