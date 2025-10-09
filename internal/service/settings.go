package service

import (
	"context"

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
	GetSettingWithDefaults(ctx context.Context, key types.SettingKey, defaultValues map[string]interface{}) (*dto.SettingResponse, error)
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
	setting, err := s.SettingsRepo.GetByKey(ctx, key)
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
	setting, err := s.SettingsRepo.GetByKey(ctx, key)
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
	return s.updateSetting(ctx, setting)
}

func (s *settingsService) DeleteSettingByKey(ctx context.Context, key types.SettingKey) error {
	err := s.SettingsRepo.DeleteByKey(ctx, key)
	if err != nil {
		return err
	}
	return nil
}

// GetSettingWithDefaults retrieves a setting by key and merges it with provided default values
// If the setting doesn't exist in the database, it returns the default values
// If the setting exists, it merges the default values with the stored values,
// giving preference to stored values for fields that exist in both
func (s *settingsService) GetSettingWithDefaults(ctx context.Context, key types.SettingKey, defaultValues map[string]interface{}) (*dto.SettingResponse, error) {
	// Try to get the setting from the database
	setting, err := s.SettingsRepo.GetByKey(ctx, key)

	// Create a new map to store the final merged values
	mergedValues := make(map[string]interface{})

	// First, copy all default values to the merged map
	for k, v := range defaultValues {
		mergedValues[k] = v
	}

	if err != nil {
		// Check for not found error first
		if ent.IsNotFound(err) {
			// If setting not found, create a new one with default values
			defaultSettingModel := &settings.Setting{
				ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
				Key:           key.String(),
				Value:         mergedValues,
				EnvironmentID: types.GetEnvironmentID(ctx),
				BaseModel:     types.GetDefaultBaseModel(ctx),
			}

			return dto.SettingFromDomain(defaultSettingModel), nil
		}
		// If there was any other error
		return nil, err
	}

	// If we get here, the setting exists and there was no error
	// Iterate through the stored values and override defaults
	for k, v := range setting.Value {
		// If the key exists in the stored values, use that instead of default
		mergedValues[k] = v
	}

	// Create a response with the merged values
	settingModel := &settings.Setting{
		ID:            setting.ID,
		Key:           setting.Key,
		Value:         mergedValues,
		EnvironmentID: setting.EnvironmentID,
		BaseModel:     setting.BaseModel,
	}

	return dto.SettingFromDomain(settingModel), nil
}
