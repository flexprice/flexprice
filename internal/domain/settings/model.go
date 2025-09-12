package settings

import (
	"encoding/json"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Setting represents a tenant and environment specific configuration setting
type Setting struct {
	// ID is the unique identifier for the setting
	ID string `json:"id"`

	// Key is the setting key
	Key string `json:"key"`

	// Value is the JSON value of the setting
	Value map[string]interface{} `json:"value"`

	// EnvironmentID is the environment identifier for the setting
	EnvironmentID string `json:"environment_id"`

	types.BaseModel
}

// FromEnt converts an ent setting to a domain setting
func FromEnt(s *ent.Settings) *Setting {
	if s == nil {
		return nil
	}

	// The value is now directly map[string]interface{} from Ent
	value := s.Value
	if value == nil {
		value = make(map[string]interface{})
	}

	return &Setting{
		ID:            s.ID,
		Key:           s.Key,
		Value:         value,
		EnvironmentID: s.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  s.TenantID,
			Status:    types.Status(s.Status),
			CreatedAt: s.CreatedAt,
			UpdatedAt: s.UpdatedAt,
			CreatedBy: s.CreatedBy,
			UpdatedBy: s.UpdatedBy,
		},
	}
}

// FromEntList converts a list of ent settings to domain settings
func FromEntList(settings []*ent.Settings) []*Setting {
	if settings == nil {
		return nil
	}

	result := make([]*Setting, len(settings))
	for i, s := range settings {
		result[i] = FromEnt(s)
	}

	return result
}

// GetValue retrieves a value by key and unmarshals it into the target
func (s *Setting) GetValue(key string, target interface{}) error {
	if s.Value == nil {
		return ierr.NewErrorf("no value found for key '%s'", key).
			Mark(ierr.ErrNotFound)
	}

	value, exists := s.Value[key]
	if !exists {
		return ierr.NewErrorf("key '%s' not found in setting", key).
			Mark(ierr.ErrNotFound)
	}

	// Marshal and unmarshal to convert interface{} to target type
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return ierr.WithError(err).
			WithHintf("failed to marshal value for key '%s'", key).
			Mark(ierr.ErrValidation)
	}

	err = json.Unmarshal(jsonBytes, target)
	if err != nil {
		return ierr.WithError(err).
			WithHintf("failed to unmarshal value for key '%s'", key).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// SetValue sets a value for a specific key
func (s *Setting) SetValue(key string, value interface{}) {
	if s.Value == nil {
		s.Value = make(map[string]interface{})
	}
	s.Value[key] = value
}

// Validate validates the setting
func (s *Setting) Validate() error {
	if s.Key == "" {
		return ierr.NewError("setting key is required").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ConvertValue converts the setting value to the specified type T
// This method uses JSON marshal/unmarshal to leverage existing JSON tags
// and provides automatic type conversion and validation
func (s *Setting) ConvertValue(target interface{}) error {
	if s.Value == nil {
		return ierr.NewError("setting value is nil").
			Mark(ierr.ErrValidation)
	}

	// Marshal the map to JSON
	jsonBytes, err := json.Marshal(s.Value)
	if err != nil {
		return ierr.WithError(err).
			WithHint("failed to marshal setting value to JSON").
			WithReportableDetails(map[string]any{
				"key": s.Key,
			}).
			Mark(ierr.ErrValidation)
	}

	// Unmarshal JSON into target struct
	err = json.Unmarshal(jsonBytes, target)
	if err != nil {
		return ierr.WithError(err).
			WithHint("failed to unmarshal JSON to target type").
			WithReportableDetails(map[string]any{
				"key": s.Key,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ConvertValueWithDefaults converts the setting value to the specified type T,
// merging with default values first to ensure all required fields are present
func (s *Setting) ConvertValueWithDefaults(target interface{}, defaults map[string]interface{}) error {
	if s.Value == nil {
		return ierr.NewError("setting value is nil").
			Mark(ierr.ErrValidation)
	}

	// Merge defaults with actual values (actual values take precedence)
	mergedValue := make(map[string]interface{})

	// First, copy defaults
	for k, v := range defaults {
		mergedValue[k] = v
	}

	// Then, override with actual values
	for k, v := range s.Value {
		mergedValue[k] = v
	}

	// Marshal the merged map to JSON
	jsonBytes, err := json.Marshal(mergedValue)
	if err != nil {
		return ierr.WithError(err).
			WithHint("failed to marshal merged setting value to JSON").
			WithReportableDetails(map[string]any{
				"key": s.Key,
			}).
			Mark(ierr.ErrValidation)
	}

	// Unmarshal JSON into target struct
	err = json.Unmarshal(jsonBytes, target)
	if err != nil {
		return ierr.WithError(err).
			WithHint("failed to unmarshal JSON to target type").
			WithReportableDetails(map[string]any{
				"key": s.Key,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ToInvoiceConfig converts the setting value to InvoiceConfig with proper defaults
func (s *Setting) ToInvoiceConfig() (*types.InvoiceConfig, error) {
	if s.Key != types.SettingKeyInvoiceConfig.String() {
		return nil, ierr.NewErrorf("setting key '%s' is not an invoice config setting", s.Key).
			WithHint("This method should only be used with invoice config settings").
			Mark(ierr.ErrValidation)
	}

	// Get default values for invoice config
	defaultSettings := types.GetDefaultSettings()
	defaults := defaultSettings[types.SettingKeyInvoiceConfig].DefaultValue

	var config types.InvoiceConfig
	err := s.ConvertValueWithDefaults(&config, defaults)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

// ToSubscriptionConfig converts the setting value to SubscriptionConfig with proper defaults
func (s *Setting) ToSubscriptionConfig() (*types.SubscriptionConfig, error) {
	if s.Key != types.SettingKeySubscriptionConfig.String() {
		return nil, ierr.NewErrorf("setting key '%s' is not a subscription config setting", s.Key).
			WithHint("This method should only be used with subscription config settings").
			Mark(ierr.ErrValidation)
	}

	// Get default values for subscription config
	defaultSettings := types.GetDefaultSettings()
	defaults := defaultSettings[types.SettingKeySubscriptionConfig].DefaultValue

	var config types.SubscriptionConfig
	err := s.ConvertValueWithDefaults(&config, defaults)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
