package types

import (
	"errors"
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

type SettingKey string

const (
	SettingKeyInvoiceConfig      SettingKey = "invoice_config"
	SettingKeySubscriptionConfig SettingKey = "subscription_config"
	SettingKeyInvoicePDFConfig   SettingKey = "invoice_pdf_config"
	SettingKeyEnvConfig          SettingKey = "env_config"
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
		SettingKeyInvoicePDFConfig: {
			Key: SettingKeyInvoicePDFConfig,
			DefaultValue: map[string]interface{}{
				"template_name": TemplateInvoiceDefault,
				"group_by":      []string{},
			},
			Description: "Default configuration for invoice PDF generation",
			Required:    true,
		},
		SettingKeyEnvConfig: {
			Key: SettingKeyEnvConfig,
			DefaultValue: map[string]interface{}{
				string(EnvironmentProduction):  1,
				string(EnvironmentDevelopment): 2,
			},
			Description: "Default configuration for environment creation limits (production and sandbox)",
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
		return ValidateInvoiceConfig(value)
	case SettingKeySubscriptionConfig:
		return ValidateSubscriptionConfig(value)
	case SettingKeyInvoicePDFConfig:
		return ValidateInvoicePDFConfig(value)
	case SettingKeyEnvConfig:
		return ValidateEnvConfig(value)
	default:
		return ierr.NewErrorf("unknown setting key: %s", key).
			WithHintf("Unknown setting key: %s", key).
			Mark(ierr.ErrValidation)
	}
}

// ValidateInvoiceConfig validates invoice configuration settings
func ValidateInvoiceConfig(value map[string]interface{}) error {
	if value == nil {
		return errors.New("invoice_config value cannot be nil")
	}

	// Check if this is a due_date_days only update
	if dueDateDaysRaw, exists := value["due_date_days"]; exists {
		var dueDateDays int
		switch v := dueDateDaysRaw.(type) {
		case int:
			dueDateDays = v
		case float64:
			if v != float64(int(v)) {
				return errors.New("invoice_config: 'due_date_days' must be a whole number")
			}
			dueDateDays = int(v)
		default:
			return ierr.NewErrorf("invoice_config: 'due_date_days' must be an integer, got %T", dueDateDaysRaw).
				WithHintf("Invoice config due date days must be an integer, got %T", dueDateDaysRaw).
				Mark(ierr.ErrValidation)
		}

		if dueDateDays < 0 {
			return errors.New("invoice_config: 'due_date_days' must be greater than or equal to 0")
		}
		return nil
	}

	// If not a due_date_days only update, validate all required fields
	// Validate prefix
	prefixRaw, exists := value["prefix"]
	if !exists {
		return ierr.NewErrorf("invoice_config: 'prefix' is required").
			WithHintf("Invoice config prefix is required").
			Mark(ierr.ErrValidation)
	}
	prefix, ok := prefixRaw.(string)
	if !ok {
		return ierr.NewErrorf("invoice_config: 'prefix' must be a string, got %T", prefixRaw).
			WithHintf("Invoice config prefix must be a string, got %T", prefixRaw).
			Mark(ierr.ErrValidation)
	}
	if strings.TrimSpace(prefix) == "" {
		return ierr.NewErrorf("invoice_config: 'prefix' cannot be empty").
			WithHintf("Invoice config prefix cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Validate format
	formatRaw, exists := value["format"]
	if !exists {
		return ierr.NewErrorf("invoice_config: 'format' is required").
			WithHintf("Invoice config format is required").
			Mark(ierr.ErrValidation)
	}
	formatStr, ok := formatRaw.(string)
	if !ok {
		return ierr.NewErrorf("invoice_config: 'format' must be a string, got %T", formatRaw).
			WithHintf("Invoice config format must be a string, got %T", formatRaw).
			Mark(ierr.ErrValidation)
	}

	// Validate against enum values
	format := InvoiceNumberFormat(formatStr)
	validFormats := []InvoiceNumberFormat{
		InvoiceNumberFormatYYYYMM,
		InvoiceNumberFormatYYYYMMDD,
		InvoiceNumberFormatYYMMDD,
		InvoiceNumberFormatYY,
		InvoiceNumberFormatYYYY,
	}
	found := false
	for _, validFormat := range validFormats {
		if format == validFormat {
			found = true
			break
		}
	}
	if !found {
		return ierr.NewErrorf("invoice_config: 'format' must be one of %v, got %s", validFormats, formatStr).
			WithHintf("Invoice config format must be one of %v, got %s", validFormats, formatStr).
			Mark(ierr.ErrValidation)
	}

	// Validate start_sequence
	startSeqRaw, exists := value["start_sequence"]
	if !exists {
		return errors.New("invoice_config: 'start_sequence' is required")
	}

	var startSeq int
	switch v := startSeqRaw.(type) {
	case int:
		startSeq = v
	case float64:
		if v != float64(int(v)) {
			return ierr.NewErrorf("invoice_config: 'start_sequence' must be a whole number").
				WithHintf("Invoice config start sequence must be a whole number").
				Mark(ierr.ErrValidation)
		}
		startSeq = int(v)
	default:
		return ierr.NewErrorf("invoice_config: 'start_sequence' must be an integer, got %T", startSeqRaw).
			WithHintf("Invoice config start sequence must be an integer, got %T", startSeqRaw).
			Mark(ierr.ErrValidation)
	}

	if startSeq < 0 {
		return ierr.NewErrorf("invoice_config: 'start_sequence' must be greater than or equal to 0").
			WithHintf("Invoice config start sequence must be greater than or equal to 0").
			Mark(ierr.ErrValidation)
	}

	// Validate timezone
	timezoneRaw, exists := value["timezone"]
	if !exists {
		return ierr.NewErrorf("invoice_config: 'timezone' is required").
			WithHintf("Invoice config timezone is required").
			Mark(ierr.ErrValidation)
	}
	timezone, ok := timezoneRaw.(string)
	if !ok {
		return ierr.NewErrorf("invoice_config: 'timezone' must be a string, got %T", timezoneRaw).
			WithHintf("Invoice config timezone must be a string, got %T", timezoneRaw).
			Mark(ierr.ErrValidation)
	}
	if strings.TrimSpace(timezone) == "" {
		return ierr.NewErrorf("invoice_config: 'timezone' cannot be empty").
			WithHintf("Invoice config timezone cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Validate timezone by trying to load it (support both IANA names and common abbreviations)
	if err := validateTimezone(timezone); err != nil {
		return ierr.NewErrorf("invoice_config: invalid timezone '%s': %v", timezone, err).
			WithHintf("Invoice config invalid timezone '%s': %v", timezone, err).
			Mark(ierr.ErrValidation)
	}

	// Validate separator
	separatorRaw, exists := value["separator"]
	if !exists {
		return errors.New("invoice_config: 'separator' is required")
	}
	_, separatorOk := separatorRaw.(string)
	if !separatorOk {
		return ierr.NewErrorf("invoice_config: 'separator' must be a string, got %T", separatorRaw).
			WithHintf("Invoice config separator must be a string, got %T", separatorRaw).
			Mark(ierr.ErrValidation)
	}
	// Note: Empty separator ("") is allowed to generate invoice numbers without separators

	// Validate suffix_length
	suffixLengthRaw, exists := value["suffix_length"]
	if !exists {
		return ierr.NewErrorf("invoice_config: 'suffix_length' is required").
			WithHintf("Invoice config suffix length is required").
			Mark(ierr.ErrValidation)
	}

	var suffixLength int
	switch v := suffixLengthRaw.(type) {
	case int:
		suffixLength = v
	case float64:
		if v != float64(int(v)) {
			return ierr.NewErrorf("invoice_config: 'suffix_length' must be a whole number").
				WithHintf("Invoice config suffix length must be a whole number").
				Mark(ierr.ErrValidation)
		}
		suffixLength = int(v)
	default:
		return ierr.NewErrorf("invoice_config: 'suffix_length' must be an integer, got %T", suffixLengthRaw).
			WithHintf("Invoice config suffix length must be an integer, got %T", suffixLengthRaw).
			Mark(ierr.ErrValidation)
	}

	if suffixLength < 1 || suffixLength > 10 {
		return ierr.NewErrorf("invoice_config: 'suffix_length' must be between 1 and 10").
			WithHintf("Invoice config suffix length must be between 1 and 10").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func ValidateInvoicePDFConfig(value map[string]interface{}) error {

	if value == nil {
		return errors.New("invoice_pdf_config value cannot be nil")
	}

	// Validate template_name
	templateNameRaw, exists := value["template_name"]
	if !exists {
		return ierr.NewErrorf("invoice_pdf_config: 'template_name' is required").
			WithHintf("Invoice PDF config template name is required").
			Mark(ierr.ErrValidation)
	}

	// Validate template name
	templateName, ok := templateNameRaw.(string)
	if !ok {
		return ierr.NewErrorf("invoice_pdf_config: 'template_name' must be a string, got %T", templateNameRaw).
			WithHintf("Invoice PDF config template name must be a string, got %T", templateNameRaw).
			Mark(ierr.ErrValidation)
	}

	if err := TemplateName(templateName).Validate(); err != nil {
		return err
	}

	// Validate group_by

	// Validate group by if provided
	if groupByRaw, exists := value["group_by"]; exists {
		switch v := groupByRaw.(type) {
		case []string:
			// Already correct type - no validation needed
		case []interface{}:
			// Convert []interface{} to []string and validate each element
			for i, item := range v {
				if _, ok := item.(string); !ok {
					return ierr.NewErrorf("invoice_pdf_config: 'group_by' element %d must be a string, got %T", i, item).
						WithHintf("Invoice PDF config group by elements must be strings").
						Mark(ierr.ErrValidation)
				}
			}
		default:
			return ierr.NewErrorf("invoice_pdf_config: 'group_by' must be an array of strings, got %T", groupByRaw).
				WithHintf("Invoice PDF config group by must be an array of strings, got %T", groupByRaw).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

func ValidateSubscriptionConfig(value map[string]interface{}) error {
	if value == nil {
		return errors.New("subscription_config value cannot be nil")
	}

	// Validate grace_period_days if provided
	if gracePeriodDaysRaw, exists := value["grace_period_days"]; exists {
		var gracePeriodDays int
		switch v := gracePeriodDaysRaw.(type) {
		case int:
			gracePeriodDays = v
		case float64:
			if v != float64(int(v)) {
				return ierr.NewErrorf("subscription_config: 'grace_period_days' must be a whole number").
					WithHintf("Subscription config grace period days must be a whole number").
					Mark(ierr.ErrValidation)
			}
			gracePeriodDays = int(v)
		default:
			return ierr.NewErrorf("subscription_config: 'grace_period_days' must be an integer, got %T", gracePeriodDaysRaw).
				WithHintf("Subscription config grace period days must be an integer, got %T", gracePeriodDaysRaw).
				Mark(ierr.ErrValidation)
		}

		if gracePeriodDays < 1 {
			return ierr.NewErrorf("subscription_config: 'grace_period_days' must be greater than or equal to 1").
				WithHintf("Subscription config grace period days must be greater than or equal to 1").
				Mark(ierr.ErrValidation)
		}
	}

	// Validate auto_cancellation_enabled if provided
	if autoCancellationEnabledRaw, exists := value["auto_cancellation_enabled"]; exists {
		autoCancellationEnabled, ok := autoCancellationEnabledRaw.(bool)
		if !ok {
			return ierr.NewErrorf("subscription_config: 'auto_cancellation_enabled' must be a boolean, got %T", autoCancellationEnabledRaw).
				WithHintf("Subscription config auto cancellation enabled must be a boolean, got %T", autoCancellationEnabledRaw).
				Mark(ierr.ErrValidation)
		}
		// Store the validated value back for consistency
		value["auto_cancellation_enabled"] = autoCancellationEnabled
	}

	// If due_date_days is provided in full config, validate it
	if dueDateDaysRaw, exists := value["due_date_days"]; exists {
		var dueDateDays int
		switch v := dueDateDaysRaw.(type) {
		case int:
			dueDateDays = v
		case float64:
			if v != float64(int(v)) {
				return errors.New("invoice_config: 'due_date_days' must be a whole number")
			}
			dueDateDays = int(v)
		default:
			return ierr.NewErrorf("invoice_config: 'due_date_days' must be an integer, got %T", dueDateDaysRaw).
				WithHintf("Invoice config due date days must be an integer, got %T", dueDateDaysRaw).
				Mark(ierr.ErrValidation)
		}

		if dueDateDays < 0 {
			return errors.New("invoice_config: 'due_date_days' must be greater than or equal to 0")
		}
	}

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

// validateTimezone validates a timezone by converting abbreviations and checking with time.LoadLocation
func validateTimezone(timezone string) error {
	resolvedTimezone := ResolveTimezone(timezone)
	_, err := time.LoadLocation(resolvedTimezone)
	return err
}

// ValidateEnvConfig validates environment configuration settings
func ValidateEnvConfig(value map[string]interface{}) error {
	if value == nil {
		return errors.New("env_config value cannot be nil")
	}

	// Define valid keys - only production and development are allowed
	validKeys := map[string]bool{
		string(EnvironmentProduction):  true,
		string(EnvironmentDevelopment): true,
	}

	// Check for invalid keys
	for key := range value {
		if !validKeys[key] {
			return ierr.NewErrorf("env_config: invalid key '%s'. Only '%s' and '%s' are allowed", key, EnvironmentProduction, EnvironmentDevelopment).
				WithHintf("Environment config only accepts keys: '%s' and '%s'", EnvironmentProduction, EnvironmentDevelopment).
				Mark(ierr.ErrValidation)
		}
	}

	// Validate each environment type limit
	envTypes := []EnvironmentType{EnvironmentProduction, EnvironmentDevelopment}
	for _, envType := range envTypes {
		envTypeKey := string(envType)
		if limitRaw, exists := value[envTypeKey]; exists {
			if err := validateEnvLimit(envTypeKey, limitRaw); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateEnvLimit validates a single environment limit value
func validateEnvLimit(envTypeKey string, limitRaw interface{}) error {
	var limit int
	switch v := limitRaw.(type) {
	case int:
		limit = v
	case float64:
		if v != float64(int(v)) {
			return ierr.NewErrorf("env_config: '%s' must be a whole number", envTypeKey).
				WithHintf("Environment config for %s must be a whole number", envTypeKey).
				Mark(ierr.ErrValidation)
		}
		limit = int(v)
	default:
		return ierr.NewErrorf("env_config: '%s' must be an integer, got %T", envTypeKey, limitRaw).
			WithHintf("Environment config for %s must be an integer, got %T", envTypeKey, limitRaw).
			Mark(ierr.ErrValidation)
	}

	if limit < 0 {
		return ierr.NewErrorf("env_config: '%s' must be greater than or equal to 0", envTypeKey).
			WithHintf("Environment config for %s must be greater than or equal to 0", envTypeKey).
			Mark(ierr.ErrValidation)
	}

	return nil
}
