package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/types"
)

// SettingsService defines the interface for managing settings operations
type SettingsService interface {

	// Key-based operations
	GetSettingByKey(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error)
	UpdateSettingByKey(ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error)
	DeleteSettingByKey(ctx context.Context, key types.SettingKey) error

	// Get setting with field-level defaults
	GetSettingWithDefaults(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error)
}

type settingsService struct {
	ServiceParams
}

func NewSettingsService(params ServiceParams) SettingsService {
	return &settingsService{
		ServiceParams: params,
	}
}

func (s *settingsService) GetSettingByKey(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error) {
	// For env_config, use tenant-level query (no environment_id)
	var setting *settings.Setting
	var err error

	isEnvConfig := key == types.SettingKeyEnvConfig
	if isEnvConfig {
		setting, err = s.SettingsRepo.GetTenantSettingByKey(ctx, key)
	} else {
		setting, err = s.SettingsRepo.GetByKey(ctx, key)
	}

	if err != nil {
		// If setting not found, check if we should return default values
		if ent.IsNotFound(err) {
			// Check if this key has default values
			if defaultSetting, exists := types.GetDefaultSettings()[key]; exists {
				// Create and return a setting with default values

				defaultSettingModel := &settings.Setting{
					ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
					Key:           defaultSetting.Key.String(),
					Value:         defaultSetting.DefaultValue,
					EnvironmentID: types.GetEnvironmentID(ctx),
					BaseModel:     types.GetDefaultBaseModel(ctx),
				}
				if isEnvConfig {
					defaultSettingModel.EnvironmentID = ""
				} else {
					defaultSettingModel.EnvironmentID = types.GetEnvironmentID(ctx)
				}
				// env_config: EnvironmentID remains empty (zero value), repository will set to NULL
				return dto.SettingFromDomain(defaultSettingModel), nil
			}
		}
		return nil, err
	}

	return dto.SettingFromDomain(setting), nil
}

func (s *settingsService) createSetting(ctx context.Context, req *dto.CreateSettingRequest) (*dto.SettingResponse, error) {
	setting := req.ToSetting(ctx)

	err := s.SettingsRepo.Create(ctx, setting)
	if err != nil {
		return nil, err
	}

	return dto.SettingFromDomain(setting), nil
}

func (s *settingsService) updateSetting(ctx context.Context, setting *settings.Setting) (*dto.SettingResponse, error) {
	err := s.SettingsRepo.Update(ctx, setting)
	if err != nil {
		return nil, err
	}

	return dto.SettingFromDomain(setting), nil
}

func (s *settingsService) UpdateSettingByKey(ctx context.Context, key types.SettingKey, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error) {
	// STEP 1: Validate the request
	if err := req.Validate(key); err != nil {
		return nil, err
	}

	// STEP 2: Check if the setting exists
	// For env_config, use tenant-level query (no environment_id)
	var setting *settings.Setting
	var err error
	if key == types.SettingKeyEnvConfig {
		setting, err = s.SettingsRepo.GetTenantSettingByKey(ctx, key)
	} else {
		setting, err = s.SettingsRepo.GetByKey(ctx, key)
	}

	if ent.IsNotFound(err) {
		createReq := &dto.CreateSettingRequest{
			Key:   key,
			Value: req.Value,
		}
		if err := createReq.Validate(); err != nil {
			return nil, err
		}
		return s.createSetting(ctx, createReq)
	}

	if err != nil {
		return nil, err
	}

	// Merge request values with existing values (don't replace completely)
	if setting.Value == nil {
		setting.Value = make(map[string]interface{})
	}

	// Merge the request values into existing values
	for key, value := range req.Value {
		setting.Value[key] = value
	}

	// For env_config, ensure environment_id is not set (will be stored as NULL)
	// Don't set it - repository will handle NULL conversion

	return s.updateSetting(ctx, setting)
}

func (s *settingsService) DeleteSettingByKey(ctx context.Context, key types.SettingKey) error {
	// For env_config, delete tenant-level setting (empty environment_id)
	if key == types.SettingKeyEnvConfig {
		return s.SettingsRepo.DeleteTenantSettingByKey(ctx, key)
	}

	return s.SettingsRepo.DeleteByKey(ctx, key)
}

// GetSettingWithDefaults retrieves a setting by key and merges it with provided default values
// If the setting doesn't exist in the database, it returns the default values
// If the setting exists, it merges the default values with the stored values,
// giving preference to stored values for fields that exist in both
func (s *settingsService) GetSettingWithDefaults(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error) {
	// First, get the setting using GetSettingByKey which handles defaults for non-existent settings

	setting, err := s.GetSettingByKey(ctx, key)
	if err != nil {
		return nil, err
	}

	// If we get here, the setting exists in the database
	// Now we need to merge it with default values
	defaultSetting, exists := types.GetDefaultSettings()[key]
	if !exists {
		// No default values to merge, return the existing setting as-is
		return setting, nil
	}

	// Create a new map to store the final merged values
	mergedValues := make(map[string]interface{})

	// First, copy all default values to the merged map
	for k, v := range defaultSetting.DefaultValue {
		mergedValues[k] = v
	}

	// Then, override with stored values (giving preference to stored values)
	for k, v := range setting.Value {
		mergedValues[k] = v
	}

	// Normalize types for known setting keys to ensure consistent typing
	if err := s.normalizeSettingTypes(key, mergedValues); err != nil {
		return nil, err
	}

	for k, v := range mergedValues {
		if v, ok := v.(int); ok {
			mergedValues[k] = v
		}
		if v, ok := v.(float64); ok {
			mergedValues[k] = int(v)
		}
	}

	// Create a response with the merged values
	settingModel := &settings.Setting{
		ID:            setting.ID,
		Key:           setting.Key,
		Value:         mergedValues,
		EnvironmentID: setting.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  setting.TenantID,
			Status:    types.Status(setting.Status),
			CreatedAt: setting.CreatedAt,
			UpdatedAt: setting.UpdatedAt,
			CreatedBy: setting.CreatedBy,
			UpdatedBy: setting.UpdatedBy,
		},
	}

	return dto.SettingFromDomain(settingModel), nil
}

// normalizeSettingTypes normalizes types for known setting keys to ensure consistent typing
func (s *settingsService) normalizeSettingTypes(key types.SettingKey, values map[string]interface{}) error {
	switch key {
	case types.SettingKeyInvoicePDFConfig:
		return s.normalizeInvoicePDFConfigTypes(values)
	default:
		// No normalization needed for other setting keys
		return nil
	}
}

// normalizeInvoicePDFConfigTypes normalizes types for invoice PDF config settings
func (s *settingsService) normalizeInvoicePDFConfigTypes(values map[string]interface{}) error {
	// Normalize group_by field
	if groupByRaw, exists := values["group_by"]; exists {
		switch v := groupByRaw.(type) {
		case []string:
			// Already correct type - no change needed
		case []interface{}:
			// Convert []interface{} to []string
			groupByParams := make([]string, len(v))
			for i, item := range v {
				if str, ok := item.(string); ok {
					groupByParams[i] = str
				} else {
					return fmt.Errorf("group_by element %d must be a string, got %T", i, item)
				}
			}
			values["group_by"] = groupByParams
		default:
			return fmt.Errorf("group_by must be an array of strings, got %T", groupByRaw)
		}
	}

	// Normalize template_name field
	if templateNameRaw, exists := values["template_name"]; exists {
		switch v := templateNameRaw.(type) {
		case string:
			// Already correct type - no change needed
		case types.TemplateName:
			// Convert to string
			values["template_name"] = string(v)
		default:
			return fmt.Errorf("template_name must be a string, got %T", templateNameRaw)
		}
	}

	return nil
}
