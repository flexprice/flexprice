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
	// Note: DueDateDays is no longer set here - it should be retrieved from invoice_due_date_config setting

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

func ConvertToDueDateConfig(value map[string]interface{}) (*types.DueDateConfig, error) {
	dueDateConfig := &types.DueDateConfig{}

	if dueDateDaysRaw, exists := value["due_date_days"]; exists {
		switch v := dueDateDaysRaw.(type) {
		case int:
			dueDateConfig.DueDateDays = v
		case float64:
			dueDateConfig.DueDateDays = int(v)
		}
	} else {
		// Get default value
		defaultDays := types.GetDefaultSettings()[types.SettingKeyInvoiceDueDateConfig].DefaultValue["due_date_days"].(int)
		dueDateConfig.DueDateDays = defaultDays
	}

	return dueDateConfig, nil
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
