package types

import (
	"errors"
	"fmt"
	"strings"
	"time"
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
		return ValidateInvoiceConfig(value)
	case SettingKeySubscriptionConfig:
		return ValidateSubscriptionConfig(value)
	default:
		return fmt.Errorf("unknown setting key: %s", key)
	}
}

// ValidateInvoiceConfig validates invoice configuration settings
func ValidateInvoiceConfig(value map[string]interface{}) error {
	if value == nil {
		return errors.New("invoice_config value cannot be nil")
	}

	// Validate prefix
	prefixRaw, exists := value["prefix"]
	if !exists {
		return errors.New("invoice_config: 'prefix' is required")
	}
	prefix, ok := prefixRaw.(string)
	if !ok {
		return fmt.Errorf("invoice_config: 'prefix' must be a string, got %T", prefixRaw)
	}
	if strings.TrimSpace(prefix) == "" {
		return errors.New("invoice_config: 'prefix' cannot be empty")
	}

	// Validate format
	formatRaw, exists := value["format"]
	if !exists {
		return errors.New("invoice_config: 'format' is required")
	}
	formatStr, ok := formatRaw.(string)
	if !ok {
		return fmt.Errorf("invoice_config: 'format' must be a string, got %T", formatRaw)
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
		return fmt.Errorf("invoice_config: 'format' must be one of %v, got %s", validFormats, formatStr)
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
			return errors.New("invoice_config: 'start_sequence' must be a whole number")
		}
		startSeq = int(v)
	default:
		return fmt.Errorf("invoice_config: 'start_sequence' must be an integer, got %T", startSeqRaw)
	}

	if startSeq < 0 {
		return errors.New("invoice_config: 'start_sequence' must be greater than or equal to 0")
	}

	// Validate timezone
	timezoneRaw, exists := value["timezone"]
	if !exists {
		return errors.New("invoice_config: 'timezone' is required")
	}
	timezone, ok := timezoneRaw.(string)
	if !ok {
		return fmt.Errorf("invoice_config: 'timezone' must be a string, got %T", timezoneRaw)
	}
	if strings.TrimSpace(timezone) == "" {
		return errors.New("invoice_config: 'timezone' cannot be empty")
	}

	// Validate timezone by trying to load it (support both IANA names and common abbreviations)
	if err := validateTimezone(timezone); err != nil {
		return fmt.Errorf("invoice_config: invalid timezone '%s': %v", timezone, err)
	}

	// Validate separator
	separatorRaw, exists := value["separator"]
	if !exists {
		return errors.New("invoice_config: 'separator' is required")
	}
	_, separatorOk := separatorRaw.(string)
	if !separatorOk {
		return fmt.Errorf("invoice_config: 'separator' must be a string, got %T", separatorRaw)
	}
	// Note: Empty separator ("") is allowed to generate invoice numbers without separators

	// Validate suffix_length
	suffixLengthRaw, exists := value["suffix_length"]
	if !exists {
		return errors.New("invoice_config: 'suffix_length' is required")
	}

	var suffixLength int
	switch v := suffixLengthRaw.(type) {
	case int:
		suffixLength = v
	case float64:
		if v != float64(int(v)) {
			return errors.New("invoice_config: 'suffix_length' must be a whole number")
		}
		suffixLength = int(v)
	default:
		return fmt.Errorf("invoice_config: 'suffix_length' must be an integer, got %T", suffixLengthRaw)
	}

	if suffixLength < 1 || suffixLength > 10 {
		return errors.New("invoice_config: 'suffix_length' must be between 1 and 10")
	}

	return nil
}

func ValidateSubscriptionConfig(value map[string]interface{}) error {
	if value == nil {
		return errors.New("subscription_config value cannot be nil")
	}

	// Validate grace_period_days
	gracePeriodDaysRaw, exists := value["grace_period_days"]
	if !exists {
		return errors.New("subscription_config: 'grace_period_days' is required")
	}

	var gracePeriodDays int
	switch v := gracePeriodDaysRaw.(type) {
	case int:
		gracePeriodDays = v
	case float64:
		if v != float64(int(v)) {
			return errors.New("subscription_config: 'grace_period_days' must be a whole number")
		}
		gracePeriodDays = int(v)
	default:
		return fmt.Errorf("subscription_config: 'grace_period_days' must be an integer, got %T", gracePeriodDaysRaw)
	}

	if gracePeriodDays < 1 {
		return errors.New("subscription_config: 'grace_period_days' must be greater than or equal to 1")
	}

	// Validate auto_cancellation_enabled (optional)
	if autoCancellationEnabledRaw, exists := value["auto_cancellation_enabled"]; exists {
		autoCancellationEnabled, ok := autoCancellationEnabledRaw.(bool)
		if !ok {
			return fmt.Errorf("subscription_config: 'auto_cancellation_enabled' must be a boolean, got %T", autoCancellationEnabledRaw)
		}
		// Store the validated value back for consistency
		value["auto_cancellation_enabled"] = autoCancellationEnabled
	} else {
		// If auto_cancellation_enabled doesn't exist in existing value, set it to false
		if _, exists := value["auto_cancellation_enabled"]; !exists {
			value["auto_cancellation_enabled"] = false
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
