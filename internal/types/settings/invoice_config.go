package types

import (
	"errors"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InvoiceConfig represents the configuration for invoice generation
type InvoiceConfig struct {
	Prefix        string `json:"prefix"`
	Format        string `json:"format"`
	StartSequence int    `json:"start_sequence"`
	Timezone      string `json:"timezone"`
	Separator     string `json:"separator"`
	SuffixLength  int    `json:"suffix_length"`
	DueDateDays   int    `json:"due_date_days"`
}

// ValidateInvoiceConfig validates invoice configuration settings
func (s *InvoiceConfig) ValidateInvoiceConfig(value map[string]interface{}) error {
	if value == nil {
		return errors.New("invoice_config value cannot be nil")
	}

	// First, validate that only allowed fields are present
	allowedFields := map[string]bool{
		"prefix":         true,
		"format":         true,
		"start_sequence": true,
		"timezone":       true,
		"separator":      true,
		"suffix_length":  true,
		"due_date_days":  true,
	}

	var invalidFields []string
	for field := range value {
		if !allowedFields[field] {
			invalidFields = append(invalidFields, field)
		}
	}

	if len(invalidFields) > 0 {
		return ierr.NewErrorf("invoice_config: invalid fields %v", invalidFields).
			WithHintf("Invoice config does not support fields %v. Allowed fields: prefix, format, start_sequence, timezone, separator, suffix_length, due_date_days", invalidFields).
			Mark(ierr.ErrValidation)
	}

	// Use ConvertValue to parse the entire value map once
	var config InvoiceConfig
	err := ConvertValue(value, &config)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to parse invoice config").
			Mark(ierr.ErrValidation)
	}

	// Check if this is a due_date_days only update
	if len(value) == 1 {
		if _, exists := value["due_date_days"]; exists {
			if config.DueDateDays < 0 {
				return errors.New("invoice_config: 'due_date_days' must be greater than or equal to 0")
			}
			return nil
		}
	}

	// If not a due_date_days only update, validate all required fields using the parsed config
	// Check required fields exist in the input
	requiredFields := []string{"prefix", "format", "start_sequence", "timezone", "separator", "suffix_length"}
	for _, field := range requiredFields {
		if _, exists := value[field]; !exists {
			return ierr.NewErrorf("invoice_config: '%s' is required", field).
				WithHintf("Invoice config %s is required", field).
				Mark(ierr.ErrValidation)
		}
	}

	// Validate prefix
	if strings.TrimSpace(config.Prefix) == "" {
		return ierr.NewErrorf("invoice_config: 'prefix' cannot be empty").
			WithHintf("Invoice config prefix cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Validate format against enum values
	format := types.InvoiceNumberFormat(config.Format)
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
		return ierr.NewErrorf("invoice_config: 'format' must be one of %v, got %s", validFormats, config.Format).
			WithHintf("Invoice config format must be one of %v, got %s", validFormats, config.Format).
			Mark(ierr.ErrValidation)
	}

	// Validate start_sequence
	if config.StartSequence < 0 {
		return ierr.NewErrorf("invoice_config: 'start_sequence' must be greater than or equal to 0").
			WithHintf("Invoice config start sequence must be greater than or equal to 0").
			Mark(ierr.ErrValidation)
	}

	// Validate timezone
	if strings.TrimSpace(config.Timezone) == "" {
		return ierr.NewErrorf("invoice_config: 'timezone' cannot be empty").
			WithHintf("Invoice config timezone cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Validate timezone by trying to load it (support both IANA names and common abbreviations)
	if err := types.ValidateTimezone(config.Timezone); err != nil {
		return ierr.NewErrorf("invoice_config: invalid timezone '%s': %v", config.Timezone, err).
			WithHintf("Invoice config invalid timezone '%s': %v", config.Timezone, err).
			Mark(ierr.ErrValidation)
	}

	// Validate suffix_length
	if config.SuffixLength < 1 || config.SuffixLength > 10 {
		return ierr.NewErrorf("invoice_config: 'suffix_length' must be between 1 and 10").
			WithHintf("Invoice config suffix length must be between 1 and 10").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ToInvoiceConfig converts the setting value to InvoiceConfig with proper defaults
func (s *InvoiceConfig) ToInvoiceConfig(value map[string]interface{}) (*InvoiceConfig, error) {
	// Get default values for invoice config
	defaultSettings := GetDefaultSettings()
	defaults := defaultSettings[SettingKeyInvoiceConfig].DefaultValue

	var config InvoiceConfig
	err := ConvertValueWithDefaults(value, &config, defaults)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
