package dto

import (
	"context"
	"time"

	domainSettings "github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/types"
	typesSettings "github.com/flexprice/flexprice/internal/types/settings"
	"github.com/flexprice/flexprice/internal/validator"
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

// func ConvertToInvoiceConfig(value map[string]interface{}) (*types.InvoiceConfig, error) {

// 	invoiceConfig := &types.InvoiceConfig{}
// 	if dueDateDaysRaw, exists := value["due_date_days"]; exists {
// 		switch v := dueDateDaysRaw.(type) {
// 		case int:
// 			dueDateDays := v
// 			invoiceConfig.DueDateDays = &dueDateDays
// 		case float64:
// 			dueDateDays := int(v)
// 			invoiceConfig.DueDateDays = &dueDateDays
// 		}
// 	} else {
// 		// Get default value and convert to pointer
// 		defaultDays := typesSettings.GetDefaultSettings()[typesSettings.SettingKeyInvoiceConfig].DefaultValue["due_date_days"].(int)
// 		invoiceConfig.DueDateDays = &defaultDays
// 	}

// 	if invoiceNumberPrefix, ok := value["prefix"].(string); ok {
// 		invoiceConfig.InvoiceNumberPrefix = invoiceNumberPrefix
// 	}
// 	if invoiceNumberFormat, ok := value["format"].(string); ok {
// 		invoiceConfig.InvoiceNumberFormat = types.InvoiceNumberFormat(invoiceNumberFormat)
// 	}

// 	if invoiceNumberTimezone, ok := value["timezone"].(string); ok {
// 		invoiceConfig.InvoiceNumberTimezone = invoiceNumberTimezone
// 	}
// 	if startSequenceRaw, exists := value["start_sequence"]; exists {
// 		switch v := startSequenceRaw.(type) {
// 		case int:
// 			invoiceConfig.InvoiceNumberStartSequence = v
// 		case float64:
// 			invoiceConfig.InvoiceNumberStartSequence = int(v)
// 		}
// 	}

// 	if invoiceNumberSeparator, ok := value["separator"].(string); ok {
// 		invoiceConfig.InvoiceNumberSeparator = invoiceNumberSeparator
// 	}
// 	if suffixLengthRaw, exists := value["suffix_length"]; exists {
// 		switch v := suffixLengthRaw.(type) {
// 		case int:
// 			invoiceConfig.InvoiceNumberSuffixLength = v
// 		case float64:
// 			invoiceConfig.InvoiceNumberSuffixLength = int(v)
// 		}
// 	}

// 	return invoiceConfig, nil
// }

// CreateSettingRequest represents the request to create a new setting
type CreateSettingRequest struct {
	Key   string                 `json:"key" validate:"required,min=1,max=255"`
	Value map[string]interface{} `json:"value,omitempty"`
}

func (r *CreateSettingRequest) Validate() error {
	// First validate the struct using validator tags
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Then validate the setting value based on the key
	if err := typesSettings.ValidateSettingValue(r.Key, r.Value); err != nil {
		return err
	}

	return nil
}

func (r *CreateSettingRequest) ToSetting(ctx context.Context) *domainSettings.Setting {
	return &domainSettings.Setting{
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
	// First validate the struct using validator tags
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Then validate the setting value based on the key
	if err := typesSettings.ValidateSettingValue(key, r.Value); err != nil {
		return err
	}

	return nil
}

// SettingFromDomain converts a domain setting to DTO
func SettingFromDomain(s *domainSettings.Setting) *SettingResponse {
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
