package settings

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToType_Success(t *testing.T) {
	defaultConfig := types.SubscriptionConfig{
		GracePeriodDays:         3,
		AutoCancellationEnabled: false,
	}

	value := map[string]interface{}{
		"grace_period_days":         5,
		"auto_cancellation_enabled": true,
	}

	result, err := ConvertToType(value, defaultConfig)
	require.NoError(t, err)
	assert.Equal(t, 5, result.GracePeriodDays)
	assert.Equal(t, true, result.AutoCancellationEnabled)
}

func TestConvertToType_NilValue_ReturnsDefaults(t *testing.T) {
	defaultConfig := types.SubscriptionConfig{
		GracePeriodDays:         3,
		AutoCancellationEnabled: false,
	}

	result, err := ConvertToType[types.SubscriptionConfig](nil, defaultConfig)
	require.NoError(t, err)
	assert.Equal(t, 3, result.GracePeriodDays)
	assert.Equal(t, false, result.AutoCancellationEnabled)
}

func TestConvertToType_Float64ToInt(t *testing.T) {
	defaultConfig := types.SubscriptionConfig{
		GracePeriodDays: 3,
	}

	// JSON unmarshaling converts numbers to float64
	value := map[string]interface{}{
		"grace_period_days": 7.0, // float64
	}

	result, err := ConvertToType(value, defaultConfig)
	require.NoError(t, err)
	assert.Equal(t, 7, result.GracePeriodDays) // Should be int
}

func TestConvertToType_PartialUpdate_MergesDefaults(t *testing.T) {
	defaultConfig := types.SubscriptionConfig{
		GracePeriodDays:         3,
		AutoCancellationEnabled: false,
	}

	// Only update one field
	value := map[string]interface{}{
		"grace_period_days": 10,
	}

	result, err := ConvertToType(value, defaultConfig)
	require.NoError(t, err)
	assert.Equal(t, 10, result.GracePeriodDays)            // Updated
	assert.Equal(t, false, result.AutoCancellationEnabled) // From defaults
}

func TestConvertToType_PointerField(t *testing.T) {
	dueDateDays := 5
	defaultConfig := types.InvoiceConfig{
		InvoiceNumberPrefix:        "INV",
		InvoiceNumberFormat:        types.InvoiceNumberFormatYYYYMM,
		InvoiceNumberStartSequence: 1,
		InvoiceNumberTimezone:      "UTC",
		InvoiceNumberSeparator:     "-",
		InvoiceNumberSuffixLength:  5,
		DueDateDays:                &dueDateDays,
	}

	value := map[string]interface{}{
		"prefix":         "BILL",
		"due_date_days":  7.0, // float64
		"start_sequence": 100.0,
	}

	result, err := ConvertToType(value, defaultConfig)
	require.NoError(t, err)
	assert.Equal(t, "BILL", result.InvoiceNumberPrefix)
	require.NotNil(t, result.DueDateDays)
	assert.Equal(t, 7, *result.DueDateDays)
	assert.Equal(t, 100, result.InvoiceNumberStartSequence)
}

func TestConvertToType_ArrayHandling(t *testing.T) {
	defaultConfig := types.InvoicePDFConfig{
		TemplateName: types.TemplateInvoiceDefault,
		GroupBy:      []string{},
	}

	value := map[string]interface{}{
		"template_name": "custom.typ",
		"group_by":      []interface{}{"meter", "feature"},
	}

	result, err := ConvertToType(value, defaultConfig)
	require.NoError(t, err)
	assert.Equal(t, types.TemplateName("custom.typ"), result.TemplateName)
	assert.Equal(t, []string{"meter", "feature"}, result.GroupBy)
}

func TestConvertToType_EmptyArray(t *testing.T) {
	defaultConfig := types.InvoicePDFConfig{
		TemplateName: types.TemplateInvoiceDefault,
		GroupBy:      []string{"default"},
	}

	value := map[string]interface{}{
		"group_by": []interface{}{}, // Empty array
	}

	result, err := ConvertToType(value, defaultConfig)
	require.NoError(t, err)
	assert.Empty(t, result.GroupBy) // Should be empty, not default
}

func TestConvertToType_NilArrayVsEmptyArray(t *testing.T) {
	defaultConfig := types.InvoicePDFConfig{
		TemplateName: types.TemplateInvoiceDefault,
		GroupBy:      []string{"default"},
	}

	// Test with explicit empty array
	value1 := map[string]interface{}{
		"group_by": []interface{}{},
	}
	result1, err := ConvertToType(value1, defaultConfig)
	require.NoError(t, err)
	assert.NotNil(t, result1.GroupBy)
	assert.Empty(t, result1.GroupBy)

	// Test with nil (field omitted) - should use defaults
	value2 := map[string]interface{}{
		"template_name": "test.typ",
		// group_by omitted
	}
	result2, err := ConvertToType(value2, defaultConfig)
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, result2.GroupBy)
}

func TestConvertFromType_Success(t *testing.T) {
	config := types.SubscriptionConfig{
		GracePeriodDays:         5,
		AutoCancellationEnabled: true,
	}

	result, err := ConvertFromType(config)
	require.NoError(t, err)
	assert.Equal(t, float64(5), result["grace_period_days"]) // JSON converts int to float64
	assert.Equal(t, true, result["auto_cancellation_enabled"])
}

func TestConvertFromType_WithPointers(t *testing.T) {
	dueDateDays := 7
	config := types.InvoiceConfig{
		InvoiceNumberPrefix: "INV",
		InvoiceNumberFormat: types.InvoiceNumberFormatYYYYMM,
		DueDateDays:         &dueDateDays,
	}

	result, err := ConvertFromType(config)
	require.NoError(t, err)
	assert.Equal(t, "INV", result["prefix"])
	assert.Equal(t, float64(7), result["due_date_days"])
}

func TestConvertFromType_EmptyStruct(t *testing.T) {
	config := types.SubscriptionConfig{}

	result, err := ConvertFromType(config)
	require.NoError(t, err)
	assert.Equal(t, float64(0), result["grace_period_days"])
	assert.Equal(t, false, result["auto_cancellation_enabled"])
}

func TestMergeWithDefaults(t *testing.T) {
	defaultConfig := types.SubscriptionConfig{
		GracePeriodDays:         3,
		AutoCancellationEnabled: false,
	}

	value := map[string]interface{}{
		"grace_period_days": 10,
	}

	result := mergeWithDefaults(value, defaultConfig)
	assert.Equal(t, 10, result["grace_period_days"])            // From value
	assert.Equal(t, false, result["auto_cancellation_enabled"]) // From defaults
}

func TestMergeWithDefaults_EmptyValue(t *testing.T) {
	defaultConfig := types.SubscriptionConfig{
		GracePeriodDays:         3,
		AutoCancellationEnabled: false,
	}

	value := map[string]interface{}{}

	result := mergeWithDefaults(value, defaultConfig)
	assert.Equal(t, float64(3), result["grace_period_days"])
	assert.Equal(t, false, result["auto_cancellation_enabled"])
}

func TestMergeWithDefaults_ValueOverridesDefaults(t *testing.T) {
	defaultConfig := types.SubscriptionConfig{
		GracePeriodDays:         3,
		AutoCancellationEnabled: false,
	}

	value := map[string]interface{}{
		"grace_period_days":         7,
		"auto_cancellation_enabled": true,
	}

	result := mergeWithDefaults(value, defaultConfig)
	assert.Equal(t, 7, result["grace_period_days"])
	assert.Equal(t, true, result["auto_cancellation_enabled"])
}

func TestConvertRoundTrip(t *testing.T) {
	original := types.SubscriptionConfig{
		GracePeriodDays:         5,
		AutoCancellationEnabled: true,
	}

	// Convert to map
	valueMap, err := ConvertFromType(original)
	require.NoError(t, err)

	// Convert back to struct
	result, err := ConvertToType(valueMap, types.SubscriptionConfig{})
	require.NoError(t, err)

	assert.Equal(t, original.GracePeriodDays, result.GracePeriodDays)
	assert.Equal(t, original.AutoCancellationEnabled, result.AutoCancellationEnabled)
}

func TestConvertToType_InvalidJSON(t *testing.T) {
	defaultConfig := types.SubscriptionConfig{}

	// Create a map with a type that can't be JSON marshaled
	value := map[string]interface{}{
		"grace_period_days": make(chan int), // Channels can't be JSON marshaled
	}

	_, err := ConvertToType(value, defaultConfig)
	assert.Error(t, err)
	// The error message from json.Marshal includes "unsupported type"
	assert.Contains(t, err.Error(), "unsupported type")
}

func TestConvertToType_AllFieldTypes(t *testing.T) {
	envConfig := types.EnvConfig{
		Production:  1,
		Development: 2,
	}

	value := map[string]interface{}{
		"production":  5.0,
		"development": 10.0,
	}

	result, err := ConvertToType(value, envConfig)
	require.NoError(t, err)
	assert.Equal(t, 5, result.Production)
	assert.Equal(t, 10, result.Development)
}
