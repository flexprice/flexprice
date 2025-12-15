package types

import (
	"testing"

	"github.com/flexprice/flexprice/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToStruct_SubscriptionConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected SubscriptionConfig
		wantErr  bool
	}{
		{
			name: "valid subscription config",
			input: map[string]interface{}{
				"grace_period_days":         7,
				"auto_cancellation_enabled": true,
			},
			expected: SubscriptionConfig{
				GracePeriodDays:         7,
				AutoCancellationEnabled: true,
			},
			wantErr: false,
		},
		{
			name: "with float64 type coercion",
			input: map[string]interface{}{
				"grace_period_days":         float64(5),
				"auto_cancellation_enabled": false,
			},
			expected: SubscriptionConfig{
				GracePeriodDays:         5,
				AutoCancellationEnabled: false,
			},
			wantErr: false,
		},
		{
			name:     "nil input returns zero value",
			input:    nil,
			expected: SubscriptionConfig{},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := utils.ToStruct[SubscriptionConfig](tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected.GracePeriodDays, result.GracePeriodDays)
				assert.Equal(t, tt.expected.AutoCancellationEnabled, result.AutoCancellationEnabled)
			}
		})
	}
}

func TestToStruct_InvoicePDFConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected InvoicePDFConfig
		wantErr  bool
	}{
		{
			name: "valid invoice PDF config",
			input: map[string]interface{}{
				"template_name": string(TemplateInvoiceDefault),
				"group_by":      []string{"customer", "date"},
			},
			expected: InvoicePDFConfig{
				TemplateName: TemplateInvoiceDefault,
				GroupBy:      []string{"customer", "date"},
			},
			wantErr: false,
		},
		{
			name: "empty group_by",
			input: map[string]interface{}{
				"template_name": string(TemplateInvoiceDefault),
				"group_by":      []string{},
			},
			expected: InvoicePDFConfig{
				TemplateName: TemplateInvoiceDefault,
				GroupBy:      []string{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := utils.ToStruct[InvoicePDFConfig](tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected.TemplateName, result.TemplateName)
				assert.Equal(t, tt.expected.GroupBy, result.GroupBy)
			}
		})
	}
}

func TestToStruct_EnvConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected EnvConfig
		wantErr  bool
	}{
		{
			name: "valid env config",
			input: map[string]interface{}{
				"production":  2,
				"development": 5,
			},
			expected: EnvConfig{
				Production:  2,
				Development: 5,
			},
			wantErr: false,
		},
		{
			name: "with float64 type coercion",
			input: map[string]interface{}{
				"production":  float64(1),
				"development": float64(3),
			},
			expected: EnvConfig{
				Production:  1,
				Development: 3,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := utils.ToStruct[EnvConfig](tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected.Production, result.Production)
				assert.Equal(t, tt.expected.Development, result.Development)
			}
		})
	}
}

func TestToMap_SubscriptionConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    SubscriptionConfig
		expected map[string]interface{}
		wantErr  bool
	}{
		{
			name: "valid subscription config",
			input: SubscriptionConfig{
				GracePeriodDays:         10,
				AutoCancellationEnabled: true,
			},
			expected: map[string]interface{}{
				"grace_period_days":         float64(10),
				"auto_cancellation_enabled": true,
			},
			wantErr: false,
		},
		{
			name: "disabled auto-cancellation",
			input: SubscriptionConfig{
				GracePeriodDays:         3,
				AutoCancellationEnabled: false,
			},
			expected: map[string]interface{}{
				"grace_period_days":         float64(3),
				"auto_cancellation_enabled": false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := utils.ToMap(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected["grace_period_days"], result["grace_period_days"])
				assert.Equal(t, tt.expected["auto_cancellation_enabled"], result["auto_cancellation_enabled"])
			}
		})
	}
}

func TestToMap_InvoicePDFConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    InvoicePDFConfig
		expected map[string]interface{}
		wantErr  bool
	}{
		{
			name: "valid invoice PDF config",
			input: InvoicePDFConfig{
				TemplateName: TemplateInvoiceDefault,
				GroupBy:      []string{"customer", "product"},
			},
			expected: map[string]interface{}{
				"template_name": string(TemplateInvoiceDefault),
				"group_by":      []interface{}{"customer", "product"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := utils.ToMap(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected["template_name"], result["template_name"])
				assert.NotNil(t, result["group_by"])
			}
		})
	}
}

func TestToMap_EnvConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    EnvConfig
		expected map[string]interface{}
		wantErr  bool
	}{
		{
			name: "valid env config",
			input: EnvConfig{
				Production:  1,
				Development: 2,
			},
			expected: map[string]interface{}{
				"production":  float64(1),
				"development": float64(2),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := utils.ToMap(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected["production"], result["production"])
				assert.Equal(t, tt.expected["development"], result["development"])
			}
		})
	}
}

// Test round-trip conversion: struct -> map -> struct
func TestRoundTrip_SubscriptionConfig(t *testing.T) {
	original := SubscriptionConfig{
		GracePeriodDays:         15,
		AutoCancellationEnabled: true,
	}

	// Convert to map
	asMap, err := utils.ToMap(original)
	require.NoError(t, err)

	// Convert back to struct
	result, err := utils.ToStruct[SubscriptionConfig](asMap)
	require.NoError(t, err)

	// Should be equal
	assert.Equal(t, original.GracePeriodDays, result.GracePeriodDays)
	assert.Equal(t, original.AutoCancellationEnabled, result.AutoCancellationEnabled)
}

func TestRoundTrip_InvoicePDFConfig(t *testing.T) {
	original := InvoicePDFConfig{
		TemplateName: TemplateInvoiceDefault,
		GroupBy:      []string{"customer", "date", "product"},
	}

	// Convert to map
	asMap, err := utils.ToMap(original)
	require.NoError(t, err)

	// Convert back to struct
	result, err := utils.ToStruct[InvoicePDFConfig](asMap)
	require.NoError(t, err)

	// Should be equal
	assert.Equal(t, original.TemplateName, result.TemplateName)
	assert.Equal(t, original.GroupBy, result.GroupBy)
}

func TestRoundTrip_EnvConfig(t *testing.T) {
	original := EnvConfig{
		Production:  3,
		Development: 10,
	}

	// Convert to map
	asMap, err := utils.ToMap(original)
	require.NoError(t, err)

	// Convert back to struct
	result, err := utils.ToStruct[EnvConfig](asMap)
	require.NoError(t, err)

	// Should be equal
	assert.Equal(t, original.Production, result.Production)
	assert.Equal(t, original.Development, result.Development)
}
