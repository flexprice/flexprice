package types

import (
	"strings"
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

// SubscriptionConfig represents the configuration for subscription auto-cancellation
type SubscriptionConfig struct {
	GracePeriodDays         int  `json:"grace_period_days"`
	AutoCancellationEnabled bool `json:"auto_cancellation_enabled"`
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
		SubscriptionConfig: extractSubscriptionConfigFromValue(config.Config),
	}
}

// Helper function to extract subscription config from setting value
func extractSubscriptionConfigFromValue(value map[string]interface{}) *SubscriptionConfig {
	// Get default values from central defaults
	defaultSettings := GetDefaultSettings()
	defaultConfig := defaultSettings[SettingKeySubscriptionConfig].DefaultValue

	config := &SubscriptionConfig{
		GracePeriodDays:         defaultConfig["grace_period_days"].(int),
		AutoCancellationEnabled: defaultConfig["auto_cancellation_enabled"].(bool),
	}

	// Extract grace_period_days
	if gracePeriodDaysRaw, exists := value["grace_period_days"]; exists {
		switch v := gracePeriodDaysRaw.(type) {
		case float64:
			config.GracePeriodDays = int(v)
		case int:
			config.GracePeriodDays = v
		}
	}

	// Extract auto_cancellation_enabled
	if autoCancellationEnabledRaw, exists := value["auto_cancellation_enabled"]; exists {
		if autoCancellationEnabled, ok := autoCancellationEnabledRaw.(bool); ok {
			config.AutoCancellationEnabled = autoCancellationEnabled
		}
	}

	return config
}

// GetDefaultSettings returns the default settings configuration for all setting keys
func GetDefaultSettings() map[SettingKey]DefaultSettingValue {
	return map[SettingKey]DefaultSettingValue{
		SettingKeyInvoiceConfig: {
			Key: SettingKeyInvoiceConfig,
			DefaultValue: map[string]interface{}{
				"prefix":         "INV",
				"format":         string(InvoiceNumberFormatYYYYMM),
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
// DEPRECATED: Use domain model validation instead
func ValidateSettingValue(key string, value map[string]interface{}) error {
	// Validation is now handled in the domain model
	return nil
}

// timezoneAbbreviationMap maps common three-letter timezone abbreviations to IANA timezone identifiers
var timezoneAbbreviationMap = map[string]string{
	// Indian Standard Time
	"IST": "Asia/Kolkata",

	// US Timezones
	"EST":  "America/New_York",    // Eastern Standard Time
	"CST":  "America/Chicago",     // Central Standard Time
	"MST":  "America/Denver",      // Mountain Standard Time
	"PST":  "America/Los_Angeles", // Pacific Standard Time
	"HST":  "Pacific/Honolulu",    // Hawaii Standard Time
	"AKST": "America/Anchorage",   // Alaska Standard Time

	// European Timezones
	"GMT": "Europe/London", // Greenwich Mean Time
	"CET": "Europe/Berlin", // Central European Time
	"EET": "Europe/Athens", // Eastern European Time
	"WET": "Europe/Lisbon", // Western European Time
	"BST": "Europe/London", // British Summer Time

	// Asia Pacific
	"JST":  "Asia/Tokyo",       // Japan Standard Time
	"KST":  "Asia/Seoul",       // Korea Standard Time
	"CCT":  "Asia/Shanghai",    // China Coast Time (avoiding CST conflict)
	"AEST": "Australia/Sydney", // Australian Eastern Standard Time
	"AWST": "Australia/Perth",  // Australian Western Standard Time

	// Others
	"MSK": "Europe/Moscow",  // Moscow Standard Time
	"CAT": "Africa/Harare",  // Central Africa Time
	"EAT": "Africa/Nairobi", // East Africa Time
	"WAT": "Africa/Lagos",   // West Africa Time
}

// ResolveTimezone converts timezone abbreviation to IANA identifier or returns the input if it's already valid
func ResolveTimezone(timezone string) string {
	// First check if it's a known abbreviation
	if ianaName, exists := timezoneAbbreviationMap[strings.ToUpper(timezone)]; exists {
		return ianaName
	}

	// If not an abbreviation, return as-is (might be IANA name already)
	return timezone
}
