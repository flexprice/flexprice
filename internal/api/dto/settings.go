package dto

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// SettingResponse represents a setting in API responses
type SettingResponse struct {
	ID            string                 `json:"id"`
	Key           string                 `json:"key"`
	Value         map[string]interface{} `json:"value"`
	EnvironmentID string                 `json:"environment_id"`
	TenantID      string                 `json:"tenant_id"`
	Status        string                 `json:"status"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	CreatedBy     string                 `json:"created_by,omitempty"`
	UpdatedBy     string                 `json:"updated_by,omitempty"`
}

// ConvertMapToStruct converts a map[string]interface{} to any struct type
// using JSON marshal/unmarshal for clean type conversion
func ConvertMapToStruct(value map[string]interface{}, target interface{}) error {
	if value == nil {
		return ierr.NewError("value map is nil").
			Mark(ierr.ErrValidation)
	}

	// Marshal the map to JSON
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return ierr.WithError(err).
			WithHint("failed to marshal value to JSON").
			Mark(ierr.ErrValidation)
	}

	// Unmarshal JSON into target struct
	err = json.Unmarshal(jsonBytes, target)
	if err != nil {
		return ierr.WithError(err).
			WithHint("failed to unmarshal JSON to target type").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ConvertMapToStructWithDefaults converts a map[string]interface{} to any struct type,
// merging with default values first
func ConvertMapToStructWithDefaults(value map[string]interface{}, target interface{}, defaults map[string]interface{}) error {
	if value == nil {
		return ierr.NewError("value map is nil").
			Mark(ierr.ErrValidation)
	}

	// Merge defaults with actual values (actual values take precedence)
	mergedValue := make(map[string]interface{})

	// First, copy defaults
	for k, v := range defaults {
		mergedValue[k] = v
	}

	// Then, override with actual values
	for k, v := range value {
		mergedValue[k] = v
	}

	return ConvertMapToStruct(mergedValue, target)
}

func ConvertToInvoiceConfig(value map[string]interface{}) (*types.InvoiceConfig, error) {
	// Get default values for invoice config
	defaultSettings := types.GetDefaultSettings()
	defaults := defaultSettings[types.SettingKeyInvoiceConfig].DefaultValue

	var invoiceConfig types.InvoiceConfig
	err := ConvertMapToStructWithDefaults(value, &invoiceConfig, defaults)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("failed to convert map to invoice config").
			Mark(ierr.ErrValidation)
	}

	return &invoiceConfig, nil
}

// CreateSettingRequest represents the request to create a new setting
type CreateSettingRequest struct {
	Key   string                 `json:"key" validate:"required,min=1,max=255"`
	Value map[string]interface{} `json:"value,omitempty"`
}

func (r *CreateSettingRequest) Validate() error {
	if r.Key == "" {
		return errors.New("key is required and cannot be empty")
	}

	if len(r.Key) > 255 {
		return errors.New("key cannot exceed 255 characters")
	}

	if err := types.ValidateSettingValue(r.Key, r.Value); err != nil {
		return err
	}

	return nil
}

func (r *CreateSettingRequest) ToSetting(ctx context.Context) *settings.Setting {
	return &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
		Key:           r.Key,
		Value:         r.Value,
	}
}

type UpdateSettingRequest struct {
	Value map[string]interface{} `json:"value,omitempty"`
}

// UpdateSettingRequest represents the request to update an existing setting
func (r *UpdateSettingRequest) Validate(key string) error {
	if err := types.ValidateSettingValue(key, r.Value); err != nil {
		return err
	}

	return nil
}

// SettingFromDomain converts a domain setting to DTO
func SettingFromDomain(s *settings.Setting) *SettingResponse {
	if s == nil {
		return nil
	}

	return &SettingResponse{
		ID:            s.ID,
		Key:           s.Key,
		Value:         s.Value,
		EnvironmentID: s.EnvironmentID,
		TenantID:      s.TenantID,
		Status:        string(s.Status),
		CreatedAt:     s.BaseModel.CreatedAt,
		UpdatedAt:     s.BaseModel.UpdatedAt,
		CreatedBy:     s.CreatedBy,
		UpdatedBy:     s.UpdatedBy,
	}
}
