package settings

import (
	"encoding/json"
	"strings"
	"time"

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

	if len(s.Key) > 255 {
		return ierr.NewError("setting key cannot exceed 255 characters").
			Mark(ierr.ErrValidation)
	}

	if s.Value == nil {
		return ierr.NewError("value cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Validate the value based on the key
	switch types.SettingKey(s.Key) {
	case types.SettingKeyInvoiceConfig:
		return s.validateInvoiceConfig()
	case types.SettingKeySubscriptionConfig:
		return s.validateSubscriptionConfig()
	default:
		return ierr.NewErrorf("unknown setting key: %s", s.Key).
			WithHintf("Unknown setting key: %s", s.Key).
			Mark(ierr.ErrValidation)
	}
}

// validateInvoiceConfig validates invoice configuration settings
func (s *Setting) validateInvoiceConfig() error {
	if s.Value == nil {
		return ierr.NewError("invoice_config value cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Check if this is a due_date_days only update
	if dueDateDaysRaw, exists := s.Value["due_date_days"]; exists {
		var dueDateDays int
		switch v := dueDateDaysRaw.(type) {
		case int:
			dueDateDays = v
		case float64:
			if v != float64(int(v)) {
				return ierr.NewError("invoice_config: 'due_date_days' must be a whole number").
					Mark(ierr.ErrValidation)
			}
			dueDateDays = int(v)
		default:
			return ierr.NewErrorf("invoice_config: 'due_date_days' must be an integer, got %T", dueDateDaysRaw).
				WithHintf("Invoice config due date days must be an integer, got %T", dueDateDaysRaw).
				Mark(ierr.ErrValidation)
		}

		if dueDateDays < 0 {
			return ierr.NewError("invoice_config: 'due_date_days' must be greater than or equal to 0").
				Mark(ierr.ErrValidation)
		}
		return nil
	}

	// If not a due_date_days only update, validate all required fields
	// Validate prefix
	prefixRaw, exists := s.Value["prefix"]
	if !exists {
		return ierr.NewError("invoice_config: 'prefix' is required").
			WithHint("Invoice config prefix is required").
			Mark(ierr.ErrValidation)
	}
	prefix, ok := prefixRaw.(string)
	if !ok {
		return ierr.NewErrorf("invoice_config: 'prefix' must be a string, got %T", prefixRaw).
			WithHintf("Invoice config prefix must be a string, got %T", prefixRaw).
			Mark(ierr.ErrValidation)
	}
	if len(strings.TrimSpace(prefix)) == 0 {
		return ierr.NewError("invoice_config: 'prefix' cannot be empty").
			WithHint("Invoice config prefix cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Validate format
	formatRaw, exists := s.Value["format"]
	if !exists {
		return ierr.NewError("invoice_config: 'format' is required").
			WithHint("Invoice config format is required").
			Mark(ierr.ErrValidation)
	}
	formatStr, ok := formatRaw.(string)
	if !ok {
		return ierr.NewErrorf("invoice_config: 'format' must be a string, got %T", formatRaw).
			WithHintf("Invoice config format must be a string, got %T", formatRaw).
			Mark(ierr.ErrValidation)
	}

	// Validate against enum values
	format := types.InvoiceNumberFormat(formatStr)
	validFormats := []types.InvoiceNumberFormat{
		types.InvoiceNumberFormatYYYYMM,
		types.InvoiceNumberFormatYYYYMMDD,
		types.InvoiceNumberFormatYYMMDD,
		types.InvoiceNumberFormatYY,
		types.InvoiceNumberFormatYYYY,
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
	startSeqRaw, exists := s.Value["start_sequence"]
	if !exists {
		return ierr.NewError("invoice_config: 'start_sequence' is required").
			Mark(ierr.ErrValidation)
	}

	var startSeq int
	switch v := startSeqRaw.(type) {
	case int:
		startSeq = v
	case float64:
		if v != float64(int(v)) {
			return ierr.NewError("invoice_config: 'start_sequence' must be a whole number").
				WithHint("Invoice config start sequence must be a whole number").
				Mark(ierr.ErrValidation)
		}
		startSeq = int(v)
	default:
		return ierr.NewErrorf("invoice_config: 'start_sequence' must be an integer, got %T", startSeqRaw).
			WithHintf("Invoice config start sequence must be an integer, got %T", startSeqRaw).
			Mark(ierr.ErrValidation)
	}

	if startSeq < 0 {
		return ierr.NewError("invoice_config: 'start_sequence' must be greater than or equal to 0").
			WithHint("Invoice config start sequence must be greater than or equal to 0").
			Mark(ierr.ErrValidation)
	}

	// Validate timezone
	timezoneRaw, exists := s.Value["timezone"]
	if !exists {
		return ierr.NewError("invoice_config: 'timezone' is required").
			WithHint("Invoice config timezone is required").
			Mark(ierr.ErrValidation)
	}
	timezone, ok := timezoneRaw.(string)
	if !ok {
		return ierr.NewErrorf("invoice_config: 'timezone' must be a string, got %T", timezoneRaw).
			WithHintf("Invoice config timezone must be a string, got %T", timezoneRaw).
			Mark(ierr.ErrValidation)
	}
	if len(strings.TrimSpace(timezone)) == 0 {
		return ierr.NewError("invoice_config: 'timezone' cannot be empty").
			WithHint("Invoice config timezone cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Validate timezone by trying to load it (support both IANA names and common abbreviations)
	if err := s.validateTimezone(timezone); err != nil {
		return ierr.NewErrorf("invoice_config: invalid timezone '%s': %v", timezone, err).
			WithHintf("Invoice config invalid timezone '%s': %v", timezone, err).
			Mark(ierr.ErrValidation)
	}

	// Validate separator
	separatorRaw, exists := s.Value["separator"]
	if !exists {
		return ierr.NewError("invoice_config: 'separator' is required").
			Mark(ierr.ErrValidation)
	}
	_, separatorOk := separatorRaw.(string)
	if !separatorOk {
		return ierr.NewErrorf("invoice_config: 'separator' must be a string, got %T", separatorRaw).
			WithHintf("Invoice config separator must be a string, got %T", separatorRaw).
			Mark(ierr.ErrValidation)
	}
	// Note: Empty separator ("") is allowed to generate invoice numbers without separators

	// Validate suffix_length
	suffixLengthRaw, exists := s.Value["suffix_length"]
	if !exists {
		return ierr.NewError("invoice_config: 'suffix_length' is required").
			WithHint("Invoice config suffix length is required").
			Mark(ierr.ErrValidation)
	}

	var suffixLength int
	switch v := suffixLengthRaw.(type) {
	case int:
		suffixLength = v
	case float64:
		if v != float64(int(v)) {
			return ierr.NewError("invoice_config: 'suffix_length' must be a whole number").
				WithHint("Invoice config suffix length must be a whole number").
				Mark(ierr.ErrValidation)
		}
		suffixLength = int(v)
	default:
		return ierr.NewErrorf("invoice_config: 'suffix_length' must be an integer, got %T", suffixLengthRaw).
			WithHintf("Invoice config suffix length must be an integer, got %T", suffixLengthRaw).
			Mark(ierr.ErrValidation)
	}

	if suffixLength < 1 || suffixLength > 10 {
		return ierr.NewError("invoice_config: 'suffix_length' must be between 1 and 10").
			WithHint("Invoice config suffix length must be between 1 and 10").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// validateSubscriptionConfig validates subscription configuration settings
func (s *Setting) validateSubscriptionConfig() error {
	if s.Value == nil {
		return ierr.NewError("subscription_config value cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Validate grace_period_days if provided
	if gracePeriodDaysRaw, exists := s.Value["grace_period_days"]; exists {
		var gracePeriodDays int
		switch v := gracePeriodDaysRaw.(type) {
		case int:
			gracePeriodDays = v
		case float64:
			if v != float64(int(v)) {
				return ierr.NewError("subscription_config: 'grace_period_days' must be a whole number").
					WithHint("Subscription config grace period days must be a whole number").
					Mark(ierr.ErrValidation)
			}
			gracePeriodDays = int(v)
		default:
			return ierr.NewErrorf("subscription_config: 'grace_period_days' must be an integer, got %T", gracePeriodDaysRaw).
				WithHintf("Subscription config grace period days must be an integer, got %T", gracePeriodDaysRaw).
				Mark(ierr.ErrValidation)
		}

		if gracePeriodDays < 1 {
			return ierr.NewError("subscription_config: 'grace_period_days' must be greater than or equal to 1").
				WithHint("Subscription config grace period days must be greater than or equal to 1").
				Mark(ierr.ErrValidation)
		}
	}

	// Validate auto_cancellation_enabled if provided
	if autoCancellationEnabledRaw, exists := s.Value["auto_cancellation_enabled"]; exists {
		autoCancellationEnabled, ok := autoCancellationEnabledRaw.(bool)
		if !ok {
			return ierr.NewErrorf("subscription_config: 'auto_cancellation_enabled' must be a boolean, got %T", autoCancellationEnabledRaw).
				WithHintf("Subscription config auto cancellation enabled must be a boolean, got %T", autoCancellationEnabledRaw).
				Mark(ierr.ErrValidation)
		}
		// Store the validated value back for consistency
		s.Value["auto_cancellation_enabled"] = autoCancellationEnabled
	}

	return nil
}

// validateTimezone validates a timezone by converting abbreviations and checking with time.LoadLocation
func (s *Setting) validateTimezone(timezone string) error {
	resolvedTimezone := types.ResolveTimezone(timezone)
	_, err := time.LoadLocation(resolvedTimezone)
	return err
}

// ToInvoiceConfig converts the setting to InvoiceConfig if it's an invoice_config setting
func (s *Setting) ToInvoiceConfig() (*types.InvoiceConfig, error) {
	if s.Key != types.SettingKeyInvoiceConfig.String() {
		return nil, ierr.NewErrorf("setting key '%s' is not invoice_config", s.Key).
			Mark(ierr.ErrValidation)
	}

	if s.Value == nil {
		return nil, ierr.NewError("invoice config value is nil").
			Mark(ierr.ErrValidation)
	}

	config := &types.InvoiceConfig{}

	// Extract prefix
	if prefix, ok := s.Value["prefix"].(string); ok {
		config.InvoiceNumberPrefix = prefix
	} else {
		config.InvoiceNumberPrefix = "INV" // default
	}

	// Extract format
	if format, ok := s.Value["format"].(string); ok {
		config.InvoiceNumberFormat = types.InvoiceNumberFormat(format)
	} else {
		config.InvoiceNumberFormat = types.InvoiceNumberFormatYYYYMM // default
	}

	// Extract start_sequence
	if startSeqRaw, exists := s.Value["start_sequence"]; exists {
		switch v := startSeqRaw.(type) {
		case int:
			config.InvoiceNumberStartSequence = v
		case float64:
			config.InvoiceNumberStartSequence = int(v)
		default:
			config.InvoiceNumberStartSequence = 1 // default
		}
	} else {
		config.InvoiceNumberStartSequence = 1 // default
	}

	// Extract timezone
	if timezone, ok := s.Value["timezone"].(string); ok {
		config.InvoiceNumberTimezone = timezone
	} else {
		config.InvoiceNumberTimezone = "UTC" // default
	}

	// Extract separator
	if separator, ok := s.Value["separator"].(string); ok {
		config.InvoiceNumberSeparator = separator
	} else {
		config.InvoiceNumberSeparator = "-" // default
	}

	// Extract suffix_length
	if suffixLenRaw, exists := s.Value["suffix_length"]; exists {
		switch v := suffixLenRaw.(type) {
		case int:
			config.InvoiceNumberSuffixLength = v
		case float64:
			config.InvoiceNumberSuffixLength = int(v)
		default:
			config.InvoiceNumberSuffixLength = 5 // default
		}
	} else {
		config.InvoiceNumberSuffixLength = 5 // default
	}

	// Extract due_date_days
	if dueDateDaysRaw, exists := s.Value["due_date_days"]; exists {
		switch v := dueDateDaysRaw.(type) {
		case int:
			days := v
			config.DueDateDays = &days
		case float64:
			days := int(v)
			config.DueDateDays = &days
		}
	}

	return config, nil
}
