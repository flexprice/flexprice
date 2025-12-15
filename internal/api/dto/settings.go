package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/types"
)

// SettingResponse represents a setting in API responses
type SettingResponse struct {
	*settings.Setting
}

func NewSettingResponse(s *settings.Setting) *SettingResponse {
	return &SettingResponse{Setting: s}
}

// CreateSettingRequest represents the request to create a new setting
type CreateSettingRequest struct {
	Key   types.SettingKey       `json:"key" validate:"required"`
	Value map[string]interface{} `json:"value,omitempty"`
}

func (r *CreateSettingRequest) Validate() error {
	// Check if the key is a valid setting key
	if err := r.Key.Validate(); err != nil {
		return err
	}

	if err := types.ValidateSettingValue(r.Key, r.Value); err != nil {
		return err
	}

	return nil
}

func (r *CreateSettingRequest) ToSetting(ctx context.Context) *settings.Setting {
	setting := &settings.Setting{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		BaseModel: types.GetDefaultBaseModel(ctx),
		Key:       r.Key,
		Value:     r.Value,
	}

	// For env_config, don't set environment_id (will be NULL in DB)
	// For other settings, use environment_id from context
	if r.Key != types.SettingKeyEnvConfig {
		setting.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	// env_config: EnvironmentID remains empty (zero value), repository will set to NULL

	return setting
}

type UpdateSettingRequest struct {
	Value map[string]interface{} `json:"value,omitempty"`
}

// UpdateSettingRequest represents the request to update an existing setting
func (r *UpdateSettingRequest) Validate(key types.SettingKey) error {
	if err := key.Validate(); err != nil {
		return err
	}

	if err := types.ValidateSettingValue(key, r.Value); err != nil {
		return err
	}

	return nil
}
