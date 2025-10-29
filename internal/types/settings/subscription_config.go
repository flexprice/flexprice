package types

import (
	"errors"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// SubscriptionConfig represents the configuration for subscription auto-cancellation
type SubscriptionConfig struct {
	GracePeriodDays         int  `json:"grace_period_days"`
	AutoCancellationEnabled bool `json:"auto_cancellation_enabled"`
}

// ValidateSubscriptionConfig validates subscription configuration settings
func (s *SubscriptionConfig) ValidateSubscriptionConfig(value map[string]interface{}) error {
	if value == nil {
		return errors.New("subscription_config value cannot be nil")
	}

	// First, validate that only allowed fields are present
	allowedFields := map[string]bool{
		"grace_period_days":         true,
		"auto_cancellation_enabled": true,
	}

	var invalidFields []string
	for field := range value {
		if !allowedFields[field] {
			invalidFields = append(invalidFields, field)
		}
	}

	if len(invalidFields) > 0 {
		return ierr.NewErrorf("subscription_config: invalid fields %v", invalidFields).
			WithHintf("Subscription config does not support fields %v. Allowed fields: grace_period_days, auto_cancellation_enabled", invalidFields).
			Mark(ierr.ErrValidation)
	}

	// Use ConvertValue to parse the entire value map once
	var config SubscriptionConfig
	err := ConvertValue(value, &config)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to parse subscription config").
			Mark(ierr.ErrValidation)
	}

	// Validate grace_period_days if provided
	if _, exists := value["grace_period_days"]; exists {
		if config.GracePeriodDays < 1 {
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

	return nil
}

// ToSubscriptionConfig converts the setting value to SubscriptionConfig with proper defaults
func (s *SubscriptionConfig) ToSubscriptionConfig(value map[string]interface{}) (*SubscriptionConfig, error) {
	// Get default values for subscription config
	defaultSettings := GetDefaultSettings()
	defaults := defaultSettings[SettingKeySubscriptionConfig].DefaultValue

	var config SubscriptionConfig
	err := ConvertValueWithDefaults(value, &config, defaults)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
