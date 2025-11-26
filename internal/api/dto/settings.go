package dto

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/internal/domain/settings"
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

func ConvertToInvoiceConfig(value map[string]interface{}) (*types.InvoiceConfig, error) {

	invoiceConfig := &types.InvoiceConfig{}
	if dueDateDaysRaw, exists := value["due_date_days"]; exists {
		switch v := dueDateDaysRaw.(type) {
		case int:
			dueDateDays := v
			invoiceConfig.DueDateDays = &dueDateDays
		case float64:
			dueDateDays := int(v)
			invoiceConfig.DueDateDays = &dueDateDays
		}
	} else {
		// Get default value and convert to pointer
		defaultDays := types.GetDefaultSettings()[types.SettingKeyInvoiceConfig].DefaultValue["due_date_days"].(int)
		invoiceConfig.DueDateDays = &defaultDays
	}

	if invoiceNumberPrefix, ok := value["prefix"].(string); ok {
		invoiceConfig.InvoiceNumberPrefix = invoiceNumberPrefix
	}
	if invoiceNumberFormat, ok := value["format"].(string); ok {
		invoiceConfig.InvoiceNumberFormat = types.InvoiceNumberFormat(invoiceNumberFormat)
	}

	if invoiceNumberTimezone, ok := value["timezone"].(string); ok {
		invoiceConfig.InvoiceNumberTimezone = invoiceNumberTimezone
	}
	if startSequenceRaw, exists := value["start_sequence"]; exists {
		switch v := startSequenceRaw.(type) {
		case int:
			invoiceConfig.InvoiceNumberStartSequence = v
		case float64:
			invoiceConfig.InvoiceNumberStartSequence = int(v)
		}
	}

	if invoiceNumberSeparator, ok := value["separator"].(string); ok {
		invoiceConfig.InvoiceNumberSeparator = invoiceNumberSeparator
	}
	if suffixLengthRaw, exists := value["suffix_length"]; exists {
		switch v := suffixLengthRaw.(type) {
		case int:
			invoiceConfig.InvoiceNumberSuffixLength = v
		case float64:
			invoiceConfig.InvoiceNumberSuffixLength = int(v)
		}
	}

	return invoiceConfig, nil
}

// CreateSettingRequest represents the request to create a new setting
type CreateSettingRequest struct {
	Key   types.SettingKey       `json:"key" validate:"required"`
	Value map[string]interface{} `json:"value,omitempty"`
}

func (r *CreateSettingRequest) Validate() error {
	if r.Key == "" {
		return errors.New("key is required and cannot be empty")
	}

	// Check if the key is a valid setting key
	if !types.IsValidSettingKey(r.Key.String()) {
		return errors.New("invalid setting key")
	}

	if err := types.ValidateSettingValue(r.Key.String(), r.Value); err != nil {
		return err
	}

	return nil
}

func (r *CreateSettingRequest) ToSetting(ctx context.Context) *settings.Setting {
	setting := &settings.Setting{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		BaseModel: types.GetDefaultBaseModel(ctx),
		Key:       r.Key.String(),
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
	if err := types.ValidateSettingValue(key.String(), r.Value); err != nil {
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
