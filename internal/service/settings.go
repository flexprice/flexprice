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

// GetSetting retrieves a setting and returns it as a typed struct
// Simple: DB map -> typed struct using stateless conversion
func GetSetting[T any](
	s *settingsService,
	ctx context.Context,
	key types.SettingKey,
) (T, error) {
	var zero T

	var setting *settings.Setting
	var err error

	if key == types.SettingKeyEnvConfig {
		setting, err = s.SettingsRepo.GetTenantLevelSettingByKey(ctx, key)
	} else {
		setting, err = s.SettingsRepo.GetByKey(ctx, key)
	}

	if ent.IsNotFound(err) {
		// Return default value
		return getDefaultValue[T](key)
	}
	if err != nil {
		return zero, err
	}

	// Simple conversion: map -> typed struct
	typedValue, err := typesSettings.ToStruct[T](setting.Value)
	if err != nil {
		return zero, ierr.WithError(err).
			WithHintf("Failed to convert setting %s", key).
			Mark(ierr.ErrValidation)
	}

	return typedValue, nil
}

// getDefaultValue returns the default value for a setting key
func getDefaultValue[T any](key types.SettingKey) (T, error) {
	var zero T

	defaults := types.GetDefaultSettings()
	defaultSetting, exists := defaults[key]
	if !exists {
		return zero, ierr.NewErrorf("unknown setting key: %s", key).Mark(ierr.ErrValidation)
	}

	// Convert default map to typed struct
	return typesSettings.ToStruct[T](defaultSetting.DefaultValue)
}

func getSettingByKey[T any](s *settingsService, ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error) {
	// Get typed config
	config, err := GetSetting[T](s, ctx, key)
	if err != nil {
		return nil, err
	}

	// Convert typed struct -> map for response
	valueMap, err := typesSettings.ToMap(config)
	if err != nil {
		return nil, err
	}

	return &dto.SettingResponse{Key: key.String(), Value: valueMap}, nil
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
		return nil, ierr.NewErrorf("unknown setting key: %s", key).Mark(ierr.ErrValidation)
	}
}

// UpdateSetting updates a setting value
// Simple: typed struct -> map for DB storage
func UpdateSetting[T types.SettingConfig](
	s *settingsService,
	ctx context.Context,
	key types.SettingKey,
	value T,
) error {
	// Validate the typed struct
	if err := value.Validate(); err != nil {
		return ierr.WithError(err).
			WithHintf("Validation failed for setting %s", key).
			Mark(ierr.ErrValidation)
	}

	// Convert typed struct -> map for DB
	valueMap, err := typesSettings.ToMap(value)
	if err != nil {
		return ierr.WithError(err).
			WithHintf("Failed to convert setting %s", key).
			Mark(ierr.ErrValidation)
	}

	var setting *settings.Setting
	isEnvConfig := key == types.SettingKeyEnvConfig
	if isEnvConfig {
		setting, err = s.SettingsRepo.GetTenantLevelSettingByKey(ctx, key)
	} else {
		setting, err = s.SettingsRepo.GetByKey(ctx, key)
	}

	if ent.IsNotFound(err) {
		// Create new setting
		newSetting := &settings.Setting{
			ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
			BaseModel: types.GetDefaultBaseModel(ctx),
			Key:       key.String(),
			Value:     valueMap,
		}
		if !isEnvConfig {
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

	// Convert current to map
	currentMap, err := typesSettings.ToMap(current)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHintf("Failed to convert current setting %s to map for merging", key).
			Mark(ierr.ErrValidation)
	}

	// Merge with request values
	for k, v := range req.Value {
		currentMap[k] = v
	}

	// Convert merged map back to typed struct
	merged, err := typesSettings.ToStruct[T](currentMap)
	if err != nil {
		return nil, err
	}

	// Update with merged typed struct
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
		return nil, ierr.NewErrorf("unknown setting key: %s", key).Mark(ierr.ErrValidation)
	}
}

func DeleteSetting[T any](
	s *settingsService,
	ctx context.Context,
	key types.SettingKey,
) error {
	var err error
	if key == types.SettingKeyEnvConfig {
		_, err = s.SettingsRepo.GetTenantLevelSettingByKey(ctx, key)
	} else {
		_, err = s.SettingsRepo.GetByKey(ctx, key)
	}

	if ent.IsNotFound(err) {
		return ierr.NewErrorf("setting with key '%s' not found", key).Mark(ierr.ErrNotFound)
	}
	if err != nil {
		return err
	}

	if key == types.SettingKeyEnvConfig {
		return s.SettingsRepo.DeleteTenantLevelSettingByKey(ctx, key)
	}
	return s.SettingsRepo.DeleteByKey(ctx, key)
}

func (s *settingsService) DeleteSettingByKey(ctx context.Context, key types.SettingKey) error {
	switch key {
	case types.SettingKeyInvoiceConfig:
		return DeleteSetting[types.InvoiceConfig](s, ctx, key)
	case types.SettingKeySubscriptionConfig:
		return DeleteSetting[types.SubscriptionConfig](s, ctx, key)
	case types.SettingKeyInvoicePDFConfig:
		return DeleteSetting[types.InvoicePDFConfig](s, ctx, key)
	case types.SettingKeyEnvConfig:
		return DeleteSetting[types.EnvConfig](s, ctx, key)
	default:
		return ierr.NewErrorf("unknown setting key: %s", key).Mark(ierr.ErrValidation)
	}
}
