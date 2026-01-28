package service

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	workflowModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

type SettingsService interface {
	// GetSettingByKey returns a setting as a DTO response (for API endpoints)
	// Use this when you need the full Setting object with metadata (ID, timestamps, etc.)
	GetSettingByKey(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error)

	// UpdateSettingByKey updates a setting with partial values (merges with existing)
	// Use this for API endpoints that accept partial updates
	UpdateSettingByKey(ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error)

	// DeleteSettingByKey deletes a setting by key
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

// isTenantLevelSetting checks if a setting is tenant-level (no environment_id)
// Tenant-level settings apply across all environments for a tenant
// Currently only env_config is tenant-level
func isTenantLevelSetting(key types.SettingKey) bool {
	return key == types.SettingKeyEnvConfig
}

// fetchSetting fetches a setting from the repository
// Handles the distinction between tenant-level and environment-level settings
//
// WHEN TO USE:
//   - Use this helper instead of calling repository methods directly
//   - This ensures consistent handling of tenant-level vs environment-level settings
func (s *settingsService) fetchSetting(ctx context.Context, key types.SettingKey) (*settings.Setting, error) {
	if isTenantLevelSetting(key) {
		return s.SettingsRepo.GetTenantLevelSettingByKey(ctx, key)
	}
	return s.SettingsRepo.GetByKey(ctx, key)
}

// getDefaultValue returns the default value for a setting key as a typed struct
// Returns the default configuration when a setting doesn't exist in the database
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

	return utils.ToStruct[T](defaultSetting.DefaultValue)
}

// GetSetting retrieves a setting and returns it as a typed struct
//
// WHEN TO USE:
//   - Use this when you need the setting value as a typed struct in your business logic
//   - Use this in other services (e.g., subscription service needs InvoiceConfig)
//   - Returns default values if setting doesn't exist
//   - Use this for type-safe access to setting values
//
// WHEN NOT TO USE:
//   - Don't use for API responses (use GetSettingByKey instead)
//   - Don't use if you need the Setting object with metadata (ID, timestamps, etc.)
//   - Don't call repository methods directly - always use service methods
//
// Example:
//
//	config, err := service.GetSetting[types.InvoiceConfig](ctx, types.SettingKeyInvoiceConfig)
//	if err != nil {
//	    return err
//	}
//	prefix := config.InvoiceNumberPrefix  // Type-safe access
func GetSetting[T any](s *settingsService, ctx context.Context, key types.SettingKey) (T, error) {
	var zero T

	setting, err := s.fetchSetting(ctx, key)
	if ent.IsNotFound(err) {
		// Return default value if setting doesn't exist
		return getDefaultValue[T](key)
	}
	if err != nil {
		return zero, err
	}

	// Convert map to typed struct
	typedValue, err := utils.ToStruct[T](setting.Value)
	if err != nil {
		return zero, ierr.WithError(err).
			WithHintf("Failed to convert setting %s", key).
			Mark(ierr.ErrValidation)
	}

	return typedValue, nil
}

// UpdateSetting updates a setting value (creates if doesn't exist)
//
// WHEN TO USE:
//   - Use this when you have a complete typed struct and want to update the setting
//   - Use this in other services when you need to update settings programmatically
//   - Automatically creates the setting if it doesn't exist
//   - Use this for full replacement of setting values
//
// WHEN NOT TO USE:
//   - Don't use for API endpoints with partial updates (use UpdateSettingByKey instead)
//   - Don't use if you need to merge with existing values
//   - Don't call repository methods directly - always use service methods
//
// Example:
//
//	config := types.InvoiceConfig{
//	    InvoiceNumberPrefix: "INV",
//	    InvoiceNumberFormat: types.InvoiceNumberFormatYYYYMM,
//	    ... other fields
//	}
//	err := service.UpdateSetting(ctx, types.SettingKeyInvoiceConfig, config)
func UpdateSetting[T types.SettingConfig](s *settingsService, ctx context.Context, key types.SettingKey, value T) error {
	// Validate the typed struct
	if err := value.Validate(); err != nil {
		return ierr.WithError(err).
			WithHintf("Validation failed for setting %s", key).
			Mark(ierr.ErrValidation)
	}

	// Convert typed struct to map for database storage
	valueMap, err := utils.ToMap(value)
	if err != nil {
		return ierr.WithError(err).
			WithHintf("Failed to convert setting %s", key).
			Mark(ierr.ErrValidation)
	}

	// Fetch existing setting to check if it exists
	setting, err := s.fetchSetting(ctx, key)

	if ent.IsNotFound(err) {
		// Create new setting
		newSetting := &settings.Setting{
			ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
			BaseModel: types.GetDefaultBaseModel(ctx),
			Key:       key,
			Value:     valueMap,
		}
		// Set environment_id only for environment-level settings
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

// GetSettingByKey returns a setting as a DTO response for API endpoints
//
// WHEN TO USE:
//   - Use this for API endpoints (GET /api/v1/settings/{key})
//   - Returns the full Setting object with all metadata (ID, timestamps, etc.)
//   - Returns default values if setting doesn't exist (without ID)
//
// WHEN NOT TO USE:
//   - Don't use in business logic if you only need the typed struct (use GetSetting instead)
//   - Don't use if you need to work with the typed config directly
//   - Don't call repository methods directly - always use service methods
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
	case types.SettingKeyCustomerOnboarding:
		return getSettingByKey[*workflowModels.WorkflowConfig](s, ctx, key)
	case types.SettingKeyWalletBalanceAlertConfig:
		return getSettingByKey[types.AlertConfig](s, ctx, key)
	case types.SettingKeyPrepareProcessedEvents:
		return getSettingByKey[*workflowModels.WorkflowConfig](s, ctx, key)
	default:
		return nil, ierr.NewErrorf("unknown setting key: %s", key).
			WithHintf("Unknown setting key: %s", key).
			Mark(ierr.ErrValidation)
	}
}

// UpdateSettingByKey updates a setting with partial values (merges with existing)
//
// WHEN TO USE:
//   - Use this for API endpoints (PATCH /api/v1/settings/{key})
//   - Accepts partial updates and merges with existing values
//   - Validates the merged result before saving
//
// WHEN NOT TO USE:
//   - Don't use if you have a complete typed struct (use UpdateSetting instead)
//   - Don't use in business logic if you want to replace the entire setting
//   - Don't call repository methods directly - always use service methods
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
	case types.SettingKeyCustomerOnboarding:
		return updateSettingByKey[*workflowModels.WorkflowConfig](s, ctx, key, req)
	case types.SettingKeyWalletBalanceAlertConfig:
		return updateSettingByKey[types.AlertConfig](s, ctx, key, req)
	case types.SettingKeyPrepareProcessedEvents:
		return updateSettingByKey[*workflowModels.WorkflowConfig](s, ctx, key, req)
	default:
		return nil, ierr.NewErrorf("unknown setting key: %s", key).
			WithHintf("Unknown setting key: %s", key).
			Mark(ierr.ErrValidation)
	}
}

// DeleteSettingByKey deletes a setting by key
//
// WHEN TO USE:
//   - Use this for API endpoints (DELETE /api/v1/settings/{key})
//   - Handles both tenant-level and environment-level settings
//
// WHEN NOT TO USE:
//   - Don't call repository methods directly - always use service methods
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

	// Delete based on setting type (tenant-level vs environment-level)
	if isTenantLevelSetting(key) {
		return s.SettingsRepo.DeleteTenantLevelSettingByKey(ctx, key)
	}
	return s.SettingsRepo.DeleteByKey(ctx, key)
}

// getSettingByKey fetches a setting and returns it as a DTO response
// Internal helper used by GetSettingByKey to handle type-specific logic
func getSettingByKey[T any](s *settingsService, ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error) {
	setting, err := s.fetchSetting(ctx, key)

	if ent.IsNotFound(err) {
		// Setting doesn't exist, return default value
		config, err := getDefaultValue[T](key)
		if err != nil {
			return nil, err
		}

		// Convert typed struct to map for response
		valueMap, err := utils.ToMap(config)
		if err != nil {
			return nil, err
		}

		// Return default setting (no ID since it doesn't exist in DB)
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

	// Use the actual Setting object fetched from DB (with all metadata: ID, timestamps, etc.)
	return dto.NewSettingResponse(setting), nil
}

// updateSettingByKey updates a setting with partial values from a request
// Internal helper used by UpdateSettingByKey to handle type-specific logic
func updateSettingByKey[T types.SettingConfig](s *settingsService, ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error) {
	// Get current setting as typed struct
	current, err := GetSetting[T](s, ctx, key)
	if err != nil {
		return nil, err
	}

	// Convert current setting to map
	currentMap, err := utils.ToMap(current)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHintf("Failed to convert current setting %s to map", key).
			Mark(ierr.ErrValidation)
	}

	// Merge request values with current values
	for k, v := range req.Value {
		currentMap[k] = v
	}

	// Convert merged map back to typed struct for validation
	merged, err := utils.ToStruct[T](currentMap)
	if err != nil {
		return nil, err
	}

	// Update with merged and validated typed struct
	if err := UpdateSetting[T](s, ctx, key, merged); err != nil {
		return nil, err
	}

	// Return updated setting
	return s.GetSettingByKey(ctx, key)
}
