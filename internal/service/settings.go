package service

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/types"
	typesSettings "github.com/flexprice/flexprice/internal/types/settings"
)

// SettingsService defines the interface for managing settings operations
type SettingsService interface {

	// Key-based operations
	GetSettingByKey(ctx context.Context, key string) (*dto.SettingResponse, error)
	UpdateSettingByKey(ctx context.Context, key string, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error)
	DeleteSettingByKey(ctx context.Context, key string) error
}

type settingsService struct {
	ServiceParams
}

func NewSettingsService(params ServiceParams) SettingsService {
	return &settingsService{
		ServiceParams: params,
	}
}

func (s *settingsService) GetSettingByKey(ctx context.Context, key string) (*dto.SettingResponse, error) {
	setting, err := s.SettingsRepo.GetByKey(ctx, key)
	if err != nil {
		// If setting not found, check if we should return default values
		if ent.IsNotFound(err) {
			// Check if this key has default values
			if defaultSetting, exists := typesSettings.GetDefaultSettings()[typesSettings.SettingKey(key)]; exists {
				// Create and return a setting with default values
				defaultSettingModel := &settings.Setting{
					ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
					Key:           string(defaultSetting.Key),
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

func (s *settingsService) UpdateSettingByKey(ctx context.Context, key string, req *dto.UpdateSettingRequest) (*dto.SettingResponse, error) {
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

func (s *settingsService) DeleteSettingByKey(ctx context.Context, key string) error {
	err := s.SettingsRepo.DeleteByKey(ctx, key)
	if err != nil {
		return err
	}
	return nil
}
