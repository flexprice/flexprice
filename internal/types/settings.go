package types

import (
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// SettingConfig defines the interface for setting configuration validation
type SettingConfig interface {
	Validate() error
}

type SettingKey string

const (
	SettingKeyInvoiceConfig            SettingKey = "invoice_config"
	SettingKeySubscriptionConfig       SettingKey = "subscription_config"
	SettingKeyInvoicePDFConfig         SettingKey = "invoice_pdf_config"
	SettingKeyEnvConfig                SettingKey = "env_config"
	SettingKeyCustomerOnboarding       SettingKey = "customer_onboarding"
	SettingKeyWalletBalanceAlertConfig SettingKey = "wallet_balance_alert_config"
)

func (s *SettingKey) Validate() error {

	allowedKeys := []SettingKey{
		SettingKeyInvoiceConfig,
		SettingKeySubscriptionConfig,
		SettingKeyInvoicePDFConfig,
		SettingKeyEnvConfig,
		SettingKeyCustomerOnboarding,
		SettingKeyWalletBalanceAlertConfig,
	}

	if !lo.Contains(allowedKeys, *s) {
		return ierr.NewErrorf("invalid setting key: %s", *s).
			WithHint("Please provide a valid setting key").
			WithReportableDetails(map[string]any{
				"allowed": allowedKeys,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// DefaultSettingValue represents a default setting configuration
type DefaultSettingValue struct {
	Key          SettingKey             `json:"key"`
	DefaultValue map[string]interface{} `json:"default_value"`
	Description  string                 `json:"description"`
}

// SubscriptionConfig represents the configuration for subscription auto-cancellation
type SubscriptionConfig struct {
	GracePeriodDays         int  `json:"grace_period_days" validate:"required,min=1"`
	AutoCancellationEnabled bool `json:"auto_cancellation_enabled"`
}

// Validate implements SettingConfig interface
func (c SubscriptionConfig) Validate() error {
	return validator.ValidateRequest(c)
}

// InvoicePDFConfig represents configuration for invoice PDF generation
type InvoicePDFConfig struct {
	TemplateName TemplateName `json:"template_name" validate:"required"`
	GroupBy      []string     `json:"group_by" validate:"omitempty,dive,required"`
}

// Validate implements SettingConfig interface
func (c InvoicePDFConfig) Validate() error {
	if err := validator.ValidateRequest(c); err != nil {
		return err
	}
	// Additional validation for TemplateName enum
	return c.TemplateName.Validate()
}

// EnvConfig represents environment creation limits configuration
type EnvConfig struct {
	Production  int `json:"production" validate:"required,min=0"`
	Development int `json:"development" validate:"required,min=0"`
}

// Validate implements SettingConfig interface
func (c EnvConfig) Validate() error {
	return validator.ValidateRequest(c)
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

// GetDefaultSettings returns the default settings configuration for all setting keys
// Uses typed structs and converts them to maps using ToMap utility from conversion.go
func GetDefaultSettings() (map[SettingKey]DefaultSettingValue, error) {
	// Define defaults as typed structs
	defaultInvoiceConfig := InvoiceConfig{
		InvoiceNumberPrefix:                    "INV",
		InvoiceNumberFormat:                    InvoiceNumberFormatYYYYMM,
		InvoiceNumberStartSequence:             1,
		InvoiceNumberTimezone:                  "UTC",
		InvoiceNumberSeparator:                 "-",
		InvoiceNumberSuffixLength:              5,
		DueDateDays:                            lo.ToPtr(1),
		AutoCompletePurchasedCreditTransaction: false,
	}

	defaultSubscriptionConfig := SubscriptionConfig{
		GracePeriodDays:         3,
		AutoCancellationEnabled: false,
	}

	defaultInvoicePDFConfig := InvoicePDFConfig{
		TemplateName: TemplateInvoiceDefault,
		GroupBy:      []string{},
	}

	defaultEnvConfig := EnvConfig{
		Production:  1,
		Development: 2,
	}

	// Note: WorkflowConfig is now defined in service package to avoid import cycles
	// We'll use a map for the default config to avoid importing service package here
	defaultCustomerOnboardingConfig := map[string]interface{}{
		"workflow_type": "customer_onboarding",
		"actions":       []interface{}{},
	}

	defaultWalletBalanceAlertConfig := AlertConfig{
		Threshold: &WalletAlertThreshold{
			Type:  AlertThresholdTypeAmount,
			Value: decimal.NewFromFloat(0.0),
		},
	}

	// Convert typed structs to maps using centralized utility
	invoiceConfigMap, err := utils.ToMap(defaultInvoiceConfig)
	if err != nil {
		return nil, err
	}
	subscriptionConfigMap, err := utils.ToMap(defaultSubscriptionConfig)
	if err != nil {
		return nil, err
	}
	invoicePDFConfigMap, err := utils.ToMap(defaultInvoicePDFConfig)
	if err != nil {
		return nil, err
	}
	envConfigMap, err := utils.ToMap(defaultEnvConfig)
	if err != nil {
		return nil, err
	}
	// Already a map, no conversion needed
	customerOnboardingConfigMap := defaultCustomerOnboardingConfig

	defaultWalletBalanceAlertConfigMap, err := utils.ToMap(defaultWalletBalanceAlertConfig)
	if err != nil {
		return nil, err
	}

	return map[SettingKey]DefaultSettingValue{
		SettingKeyInvoiceConfig: {
			Key:          SettingKeyInvoiceConfig,
			DefaultValue: invoiceConfigMap,
			Description:  "Default configuration for invoice generation and management",
		},
		SettingKeySubscriptionConfig: {
			Key:          SettingKeySubscriptionConfig,
			DefaultValue: subscriptionConfigMap,
			Description:  "Default configuration for subscription auto-cancellation (grace period and enabled flag)",
		},
		SettingKeyInvoicePDFConfig: {
			Key:          SettingKeyInvoicePDFConfig,
			DefaultValue: invoicePDFConfigMap,
			Description:  "Default configuration for invoice PDF generation",
		},
		SettingKeyEnvConfig: {
			Key:          SettingKeyEnvConfig,
			DefaultValue: envConfigMap,
			Description:  "Default configuration for environment creation limits (production and sandbox)",
		},
		SettingKeyCustomerOnboarding: {
			Key:          SettingKeyCustomerOnboarding,
			DefaultValue: customerOnboardingConfigMap,
			Description:  "Default configuration for customer onboarding workflow",
		},
		SettingKeyWalletBalanceAlertConfig: {
			Key:          SettingKeyWalletBalanceAlertConfig,
			DefaultValue: defaultWalletBalanceAlertConfigMap,
			Description:  "Default configuration for wallet balance alert configuration",
		},
	}, nil
}

// IsValidSettingKey checks if a setting key is valid
func IsValidSettingKey(key string) bool {
	defaults, err := GetDefaultSettings()
	if err != nil {
		return false
	}
	_, exists := defaults[SettingKey(key)]
	return exists
}

// ValidateSettingValue validates a setting value based on its key
// Uses centralized conversion (inline to avoid import cycle)
func ValidateSettingValue(key SettingKey, value map[string]interface{}) error {
	if err := key.Validate(); err != nil {
		return err
	}

	if value == nil {
		return ierr.NewErrorf("value cannot be nil").
			WithHint("Please provide a valid setting value").
			Mark(ierr.ErrValidation)
	}

	// Use ToStruct from conversion.go (same package, no import cycle)
	switch key {
	case SettingKeyInvoiceConfig:
		config, err := utils.ToStruct[InvoiceConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeySubscriptionConfig:
		config, err := utils.ToStruct[SubscriptionConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeyInvoicePDFConfig:
		config, err := utils.ToStruct[InvoicePDFConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeyEnvConfig:
		config, err := utils.ToStruct[EnvConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	case SettingKeyCustomerOnboarding:
		// WorkflowConfig validation is handled in the service layer
		// Here we just do basic structure validation
		if _, ok := value["workflow_type"]; !ok {
			return ierr.NewError("workflow_type is required").
				WithHint("Please provide a workflow_type").
				Mark(ierr.ErrValidation)
		}
		if _, ok := value["actions"]; !ok {
			return ierr.NewError("actions field is required").
				WithHint("Please provide an actions array").
				Mark(ierr.ErrValidation)
		}
		return nil

	case SettingKeyWalletBalanceAlertConfig:
		config, err := utils.ToStruct[AlertConfig](value)
		if err != nil {
			return err
		}
		return config.Validate()

	default:
		return ierr.NewErrorf("unknown setting key: %s", key).
			WithHintf("Unknown setting key: %s", key).
			Mark(ierr.ErrValidation)
	}
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

// ValidateTimezone validates a timezone by converting abbreviations and checking with time.LoadLocation
func ValidateTimezone(timezone string) error {
	resolvedTimezone := ResolveTimezone(timezone)
	_, err := time.LoadLocation(resolvedTimezone)
	return err
}
