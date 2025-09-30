package types

import (
	"encoding/json"
	"errors"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type SettingKey string

const (
	SettingKeyInvoiceConfig      SettingKey = "invoice_config"
	SettingKeySubscriptionConfig SettingKey = "subscription_config"
)

func (s SettingKey) String() string {
	return string(s)
}

// DefaultSettingValue represents a default setting configuration
type DefaultSettingValue struct {
	Key          SettingKey             `json:"key"`
	DefaultValue map[string]interface{} `json:"default_value"`
	Description  string                 `json:"description"`
	Required     bool                   `json:"required"`
}

// TenantEnvConfig represents a generic configuration for a specific tenant and environment
type TenantEnvConfig struct {
	TenantID      string                 `json:"tenant_id"`
	EnvironmentID string                 `json:"environment_id"`
	Config        map[string]interface{} `json:"config"`
}

// TenantSubscriptionConfig represents subscription configuration for a specific tenant and environment
type TenantEnvSubscriptionConfig struct {
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	*SubscriptionConfig
}

// ToTenantEnvConfig converts a TenantEnvSubscriptionConfig to a generic TenantEnvConfig
func (t *TenantEnvSubscriptionConfig) ToTenantEnvConfig() *TenantEnvConfig {
	return &TenantEnvConfig{
		TenantID:      t.TenantID,
		EnvironmentID: t.EnvironmentID,
		Config: map[string]interface{}{
			"grace_period_days":         t.GracePeriodDays,
			"auto_cancellation_enabled": t.AutoCancellationEnabled,
		},
	}
}

// FromTenantEnvConfig creates a TenantEnvSubscriptionConfig from a generic TenantEnvConfig
func TenantEnvSubscriptionConfigFromConfig(config *TenantEnvConfig) *TenantEnvSubscriptionConfig {
	return &TenantEnvSubscriptionConfig{
		TenantID:           config.TenantID,
		EnvironmentID:      config.EnvironmentID,
		SubscriptionConfig: ExtractSubscriptionConfigFromValue(config.Config),
	}
}

// ExtractSubscriptionConfigFromValue extracts subscription config from setting value
func ExtractSubscriptionConfigFromValue(value map[string]interface{}) *SubscriptionConfig {
	// Get default values from central defaults
	defaultSettings := GetDefaultSettings()
	defaults := defaultSettings[SettingKeySubscriptionConfig].DefaultValue

	var config SubscriptionConfig
	err := ConvertValueWithDefaults(value, &config, defaults)
	if err != nil {
		// Fallback to defaults if conversion fails
		return &SubscriptionConfig{
			GracePeriodDays:         defaults["grace_period_days"].(int),
			AutoCancellationEnabled: defaults["auto_cancellation_enabled"].(bool),
		}
	}

	return &config
}

// GetDefaultSettings returns the default settings configuration for all setting keys
func GetDefaultSettings() map[SettingKey]DefaultSettingValue {
	return map[SettingKey]DefaultSettingValue{
		SettingKeyInvoiceConfig: {
			Key: SettingKeyInvoiceConfig,
			DefaultValue: map[string]interface{}{
				"prefix":         "INV",
				"format":         types.InvoiceNumberFormatYYYYMM,
				"start_sequence": 1,
				"timezone":       "UTC",
				"separator":      "-",
				"suffix_length":  5,
				"due_date_days":  1, // Default to 1 day after period end
			},
			Description: "Default configuration for invoice generation and management",
			Required:    true,
		},
		SettingKeySubscriptionConfig: {
			Key: SettingKeySubscriptionConfig,
			DefaultValue: map[string]interface{}{
				"grace_period_days":         3,
				"auto_cancellation_enabled": false,
			},
			Description: "Default configuration for subscription auto-cancellation (grace period and enabled flag)",
			Required:    true,
		},
	}
}

// IsValidSettingKey checks if a setting key is valid
func IsValidSettingKey(key string) bool {
	_, exists := GetDefaultSettings()[SettingKey(key)]
	return exists
}

// ValidateSettingValue validates a setting value based on its key
func ValidateSettingValue(key string, value map[string]interface{}) error {
	if value == nil {
		return errors.New("value cannot be nil")
	}

	switch SettingKey(key) {
	case SettingKeyInvoiceConfig:
		// Create a temporary InvoiceConfig instance to call the validation method
		config := &InvoiceConfig{}
		return config.ValidateInvoiceConfig(value)
	case SettingKeySubscriptionConfig:
		// Create a temporary SubscriptionConfig instance to call the validation method
		config := &SubscriptionConfig{}
		return config.ValidateSubscriptionConfig(value)
	default:
		return ierr.NewErrorf("unknown setting key: %s", key).
			WithHintf("Unknown setting key: %s", key).
			Mark(ierr.ErrValidation)
	}
}

// ConvertValue converts the setting value to the specified type
// This function uses JSON marshal/unmarshal to leverage existing JSON tags
// and provides automatic type conversion and validation
func ConvertValue(value map[string]interface{}, target interface{}) error {
	if value == nil {
		return ierr.NewError("setting value is nil").
			Mark(ierr.ErrValidation)
	}

	// Marshal the map to JSON
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return ierr.WithError(err).
			WithHint("failed to marshal setting value to JSON").
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

// ConvertValueWithDefaults converts the setting value to the specified type,
// merging with default values first to ensure all required fields are present
func ConvertValueWithDefaults(value map[string]interface{}, target interface{}, defaults map[string]interface{}) error {
	if value == nil {
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
	for k, v := range value {
		mergedValue[k] = v
	}

	// Marshal the merged map to JSON
	jsonBytes, err := json.Marshal(mergedValue)
	if err != nil {
		return ierr.WithError(err).
			WithHint("failed to marshal merged setting value to JSON").
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
