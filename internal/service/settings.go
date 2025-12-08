package service

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	typesSettings "github.com/flexprice/flexprice/internal/types/settings"
)

type SettingsService interface {
	GetSettingByKey(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error)
	UpdateSettingByKey(ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error)
	DeleteSettingByKey(ctx context.Context, key types.SettingKey) error
}

type settingsService struct {
	ServiceParams
}

func NewSettingsService(params ServiceParams) SettingsService {
	return &settingsService{
		ServiceParams: params,
	}
}

// Helper: Check if setting is tenant-level (no environment_id)
func isTenantLevelSetting(key types.SettingKey) bool {
	return key == types.SettingKeyEnvConfig
}

// Helper: Fetch setting from repository (handles tenant-level vs environment-level)
func (s *settingsService) fetchSetting(ctx context.Context, key types.SettingKey) (*settings.Setting, error) {
	if isTenantLevelSetting(key) {
		return s.SettingsRepo.GetTenantLevelSettingByKey(ctx, key)
	}
	return s.SettingsRepo.GetByKey(ctx, key)
}

// Helper: Get default value for a setting key
func getDefaultValue[T any](key types.SettingKey) (T, error) {
	var zero T

	defaults, err := types.GetDefaultSettings()
	if err != nil {
		return zero, err
	}
	defaultSetting, exists := defaults[key]
	if !exists {
		return zero, ierr.NewErrorf("unknown setting key: %s", key).
			WithHintf("Unknown setting key: %s", key).
			Mark(ierr.ErrValidation)
	}

	return typesSettings.ToStruct[T](defaultSetting.DefaultValue)
}

// GetSetting retrieves a setting and returns it as a typed struct
func GetSetting[T any](s *settingsService, ctx context.Context, key types.SettingKey) (T, error) {
	var zero T

	setting, err := s.fetchSetting(ctx, key)
	if ent.IsNotFound(err) {
		return getDefaultValue[T](key)
	}
	if err != nil {
		return zero, err
	}

	typedValue, err := typesSettings.ToStruct[T](setting.Value)
	if err != nil {
		return zero, ierr.WithError(err).
			WithHintf("Failed to convert setting %s", key).
			Mark(ierr.ErrValidation)
	}

	return typedValue, nil
}

// getSettingByKey fetches setting and returns as DTO response
func getSettingByKey[T any](s *settingsService, ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error) {
	setting, err := s.fetchSetting(ctx, key)

	if ent.IsNotFound(err) {
		// Setting doesn't exist, return default value
		config, err := getDefaultValue[T](key)
		if err != nil {
			return nil, err
		}

		valueMap, err := typesSettings.ToMap(config)
		if err != nil {
			return nil, err
		}

		return dto.NewSettingResponse(&settings.Setting{
			ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
			BaseModel: types.GetDefaultBaseModel(ctx),
			Key:       key,
			Value:     valueMap,
		}), nil
	}
	if err != nil {
		return nil, err
	}

	// Use the actual Setting object fetched from DB (with all metadata)
	return dto.NewSettingResponse(setting), nil
}

func (s *settingsService) GetSettingByKey(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error) {
	switch key {
	case types.SettingKeyInvoiceConfig:
		return getSettingByKey[types.InvoiceConfig](s, ctx, key)
	case types.SettingKeySubscriptionConfig:
		return getSettingByKey[types.SubscriptionConfig](s, ctx, key)
	case types.SettingKeyInvoicePDFConfig:
		return getSettingByKey[types.InvoicePDFConfig](s, ctx, key)
	case types.SettingKeyEnvConfig:
		return getSettingByKey[types.EnvConfig](s, ctx, key)
	default:
		return nil, ierr.NewErrorf("unknown setting key: %s", key).
			WithHintf("Unknown setting key: %s", key).
			Mark(ierr.ErrValidation)
	}
}

// UpdateSetting updates a setting value (creates if doesn't exist)
func UpdateSetting[T types.SettingConfig](s *settingsService, ctx context.Context, key types.SettingKey, value T) error {
	// Validate
	if err := value.Validate(); err != nil {
		return ierr.WithError(err).
			WithHintf("Validation failed for setting %s", key).
			Mark(ierr.ErrValidation)
	}

	// Convert to map
	valueMap, err := typesSettings.ToMap(value)
	if err != nil {
		return ierr.WithError(err).
			WithHintf("Failed to convert setting %s", key).
			Mark(ierr.ErrValidation)
	}

	// Fetch existing setting
	setting, err := s.fetchSetting(ctx, key)

	if ent.IsNotFound(err) {
		// Create new setting
		newSetting := &settings.Setting{
			ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
			BaseModel: types.GetDefaultBaseModel(ctx),
			Key:       key,
			Value:     valueMap,
		}
		if !isTenantLevelSetting(key) {
			newSetting.EnvironmentID = types.GetEnvironmentID(ctx)
		}
		return s.SettingsRepo.Create(ctx, newSetting)
	}
	if err != nil {
		return err
	}

	// Update existing setting
	setting.Value = valueMap
	return s.SettingsRepo.Update(ctx, setting)
}

func updateSettingByKey[T types.SettingConfig](s *settingsService, ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error) {
	// Get current setting
	current, err := GetSetting[T](s, ctx, key)
	if err != nil {
		return nil, err
	}

	// Convert to map and merge with request
	currentMap, err := typesSettings.ToMap(current)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHintf("Failed to convert current setting %s to map", key).
			Mark(ierr.ErrValidation)
	}

	for k, v := range req.Value {
		currentMap[k] = v
	}

	// Convert merged map back to typed struct and update
	merged, err := typesSettings.ToStruct[T](currentMap)
	if err != nil {
		return nil, err
	}

	if err := UpdateSetting[T](s, ctx, key, merged); err != nil {
		return nil, err
	}

	return s.GetSettingByKey(ctx, key)
}

func (s *settingsService) UpdateSettingByKey(ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error) {
	if err := req.Validate(key); err != nil {
		return nil, err
	}

	switch key {
	case types.SettingKeyInvoiceConfig:
		return updateSettingByKey[types.InvoiceConfig](s, ctx, key, req)
	case types.SettingKeySubscriptionConfig:
		return updateSettingByKey[types.SubscriptionConfig](s, ctx, key, req)
	case types.SettingKeyInvoicePDFConfig:
		return updateSettingByKey[types.InvoicePDFConfig](s, ctx, key, req)
	case types.SettingKeyEnvConfig:
		return updateSettingByKey[types.EnvConfig](s, ctx, key, req)
	default:
		return nil, ierr.NewErrorf("unknown setting key: %s", key).
			WithHintf("Unknown setting key: %s", key).
			Mark(ierr.ErrValidation)
	}
}

func (s *settingsService) DeleteSettingByKey(ctx context.Context, key types.SettingKey) error {
	// Check if setting exists
	_, err := s.fetchSetting(ctx, key)
	if ent.IsNotFound(err) {
		return ierr.NewErrorf("setting with key '%s' not found", key).
			WithHintf("Setting with key %s not found", key).
			Mark(ierr.ErrNotFound)
	}
	if err != nil {
		return err
	}

	// Delete based on setting type
	if isTenantLevelSetting(key) {
		return s.SettingsRepo.DeleteTenantLevelSettingByKey(ctx, key)
	}
	return s.SettingsRepo.DeleteByKey(ctx, key)
}
