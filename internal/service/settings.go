package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	typesSettings "github.com/flexprice/flexprice/internal/types/settings"
)

// SettingsService defines the interface for managing settings operations
type SettingsService interface {
	GetSettingByKey(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error)
	UpdateSettingByKey(ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error)
	DeleteSettingByKey(ctx context.Context, key types.SettingKey) error
}

type settingsService struct {
	ServiceParams
	registry *typesSettings.SettingRegistry
}

func NewSettingsService(params ServiceParams) SettingsService {
	registry := typesSettings.NewSettingRegistry()

	// Register InvoiceConfig
	typesSettings.Register(
		registry,
		types.SettingKeyInvoiceConfig,
		types.InvoiceConfig{
			InvoiceNumberPrefix:        "INV",
			InvoiceNumberFormat:        types.InvoiceNumberFormatYYYYMM,
			InvoiceNumberStartSequence: 1,
			InvoiceNumberTimezone:      "UTC",
			InvoiceNumberSeparator:     "-",
			InvoiceNumberSuffixLength:  5,
			DueDateDays:                intPtr(1),
		},
		validateInvoiceConfig,
		"Invoice generation configuration",
	)

	// Register SubscriptionConfig
	typesSettings.Register(
		registry,
		types.SettingKeySubscriptionConfig,
		types.SubscriptionConfig{
			GracePeriodDays:         3,
			AutoCancellationEnabled: false,
		},
		validateSubscriptionConfig,
		"Subscription auto-cancellation configuration",
	)

	// Register InvoicePDFConfig
	typesSettings.Register(
		registry,
		types.SettingKeyInvoicePDFConfig,
		types.InvoicePDFConfig{
			TemplateName: types.TemplateInvoiceDefault,
			GroupBy:      []string{},
		},
		validateInvoicePDFConfig,
		"Invoice PDF generation configuration",
	)

	// Register EnvConfig
	typesSettings.Register(
		registry,
		types.SettingKeyEnvConfig,
		types.EnvConfig{
			Production:  1,
			Development: 2,
		},
		validateEnvConfig,
		"Environment creation limits",
	)

	return &settingsService{
		ServiceParams: params,
		registry:      registry,
	}
}

// Helper for pointer int
func intPtr(i int) *int {
	return &i
}

// Typed validators that wrap existing validation logic
func validateInvoiceConfig(config types.InvoiceConfig) error {
	// Delegate to existing ValidateInvoiceConfig after converting to map
	valueMap, err := typesSettings.ConvertFromType(config)
	if err != nil {
		return err
	}
	return types.ValidateInvoiceConfig(valueMap)
}

func validateSubscriptionConfig(config types.SubscriptionConfig) error {
	if config.GracePeriodDays < 1 {
		return fmt.Errorf("grace_period_days must be >= 1")
	}
	return nil
}

func validateInvoicePDFConfig(config types.InvoicePDFConfig) error {
	return config.TemplateName.Validate()
}

func validateEnvConfig(config types.EnvConfig) error {
	if config.Production < 0 || config.Development < 0 {
		return fmt.Errorf("environment limits must be >= 0")
	}
	return nil
}

// GetSetting retrieves a setting with compile-time type safety
func GetSetting[T any](
	s *settingsService,
	ctx context.Context,
	key types.SettingKey,
) (T, error) {
	var zero T

	// Get type definition from registry
	settingType, err := typesSettings.GetType[T](s.registry, key)
	if err != nil {
		return zero, ierr.WithError(err).
			WithHintf("Unknown setting type for key %s", key).
			Mark(ierr.ErrValidation)
	}

	// Handle tenant-level vs environment-level
	var setting *settings.Setting
	if key == types.SettingKeyEnvConfig {
		setting, err = s.SettingsRepo.GetTenantSettingByKey(ctx, key)
	} else {
		setting, err = s.SettingsRepo.GetByKey(ctx, key)
	}

	// If not found, return defaults
	if ent.IsNotFound(err) {
		return settingType.DefaultValue, nil
	}
	if err != nil {
		return zero, err
	}

	// Convert to typed value
	typedValue, err := typesSettings.ConvertToType[T](
		setting.Value,
		settingType.DefaultValue,
	)
	if err != nil {
		return zero, ierr.WithError(err).
			WithHintf("Failed to convert setting %s", key).
			Mark(ierr.ErrValidation)
	}

	// Validate
	if settingType.Validator != nil {
		if err := settingType.Validator(typedValue); err != nil {
			return zero, ierr.WithError(err).
				WithHintf("Validation failed for setting %s", key).
				Mark(ierr.ErrValidation)
		}
	}

	return typedValue, nil
}

// getSettingByKey is a generic helper that gets a setting and converts it to DTO
func getSettingByKey[T any](s *settingsService, ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error) {
	config, err := GetSetting[T](s, ctx, key)
	if err != nil {
		return nil, err
	}
	valueMap, err := typesSettings.ConvertFromType(config)
	if err != nil {
		return nil, err
	}
	return &dto.SettingResponse{Key: key.String(), Value: valueMap}, nil
}

func (s *settingsService) GetSettingByKey(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error) {
	// Use generic helper based on key type
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

// UpdateSetting updates a setting with compile-time type safety
func UpdateSetting[T any](
	s *settingsService,
	ctx context.Context,
	key types.SettingKey,
	value T,
) error {
	// Get type definition
	settingType, err := typesSettings.GetType[T](s.registry, key)
	if err != nil {
		return ierr.WithError(err).
			WithHintf("Unknown setting type for key %s", key).
			Mark(ierr.ErrValidation)
	}

	// Validate before conversion
	if settingType.Validator != nil {
		if err := settingType.Validator(value); err != nil {
			return ierr.WithError(err).
				WithHintf("Validation failed for setting %s", key).
				Mark(ierr.ErrValidation)
		}
	}

	// Convert to map for storage
	valueMap, err := typesSettings.ConvertFromType(value)
	if err != nil {
		return ierr.WithError(err).
			WithHintf("Failed to convert setting %s", key).
			Mark(ierr.ErrValidation)
	}

	// Check if setting exists
	var setting *settings.Setting
	isEnvConfig := key == types.SettingKeyEnvConfig
	if isEnvConfig {
		setting, err = s.SettingsRepo.GetTenantSettingByKey(ctx, key)
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

	// Merge with existing value (new value takes precedence)
	if setting.Value == nil {
		setting.Value = make(map[string]interface{})
	}
	// Merge: copy existing values, then override with new values
	mergedValue := make(map[string]interface{})
	for k, v := range setting.Value {
		mergedValue[k] = v
	}
	for k, v := range valueMap {
		mergedValue[k] = v
	}

	// Convert merged back to type for validation
	mergedTyped, err := typesSettings.ConvertToType[T](mergedValue, settingType.DefaultValue)
	if err != nil {
		return ierr.WithError(err).
			WithHintf("Failed to convert merged value for validation").
			Mark(ierr.ErrValidation)
	}

	// Validate merged value
	if settingType.Validator != nil {
		if err := settingType.Validator(mergedTyped); err != nil {
			return ierr.WithError(err).
				WithHintf("Validation failed for merged setting %s", key).
				Mark(ierr.ErrValidation)
		}
	}

	// Update setting with merged value
	setting.Value = mergedValue
	return s.SettingsRepo.Update(ctx, setting)
}

// updateSettingByKey is a generic helper that handles the update logic for any setting type
func updateSettingByKey[T any](s *settingsService, ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error) {
	// Get current config
	current, err := GetSetting[T](s, ctx, key)
	if err != nil {
		return nil, err
	}
	// Merge with request values
	currentMap, _ := typesSettings.ConvertFromType(current)
	for k, v := range req.Value {
		currentMap[k] = v
	}
	// Convert back to type
	var zero T
	merged, err := typesSettings.ConvertToType[T](currentMap, zero)
	if err != nil {
		return nil, err
	}
	// Update
	if err := UpdateSetting[T](s, ctx, key, merged); err != nil {
		return nil, err
	}
	// Return updated
	return s.GetSettingByKey(ctx, key)
}

func (s *settingsService) UpdateSettingByKey(ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error) {
	// Validate request
	if err := req.Validate(key); err != nil {
		return nil, err
	}

	// Use generic helper based on key type
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

// DeleteSetting deletes a setting with compile-time type safety
func DeleteSetting[T any](
	s *settingsService,
	ctx context.Context,
	key types.SettingKey,
) error {
	// Validate that key is registered
	_, err := typesSettings.GetType[T](s.registry, key)
	if err != nil {
		return ierr.WithError(err).
			WithHintf("Unknown setting type for key %s", key).
			Mark(ierr.ErrValidation)
	}

	// Check if setting exists
	if key == types.SettingKeyEnvConfig {
		_, err = s.SettingsRepo.GetTenantSettingByKey(ctx, key)
	} else {
		_, err = s.SettingsRepo.GetByKey(ctx, key)
	}

	if ent.IsNotFound(err) {
		return ierr.NewErrorf("setting with key '%s' not found", key).Mark(ierr.ErrNotFound)
	}
	if err != nil {
		return err
	}

	// Delete the setting
	if key == types.SettingKeyEnvConfig {
		return s.SettingsRepo.DeleteTenantSettingByKey(ctx, key)
	}
	return s.SettingsRepo.DeleteByKey(ctx, key)
}

func (s *settingsService) DeleteSettingByKey(ctx context.Context, key types.SettingKey) error {
	// Use generic method based on key type
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
