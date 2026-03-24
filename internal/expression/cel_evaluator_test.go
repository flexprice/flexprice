package expression

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCELEvaluator_EvaluateQuantity(t *testing.T) {
	eval := NewCELEvaluator()

	tests := []struct {
		name       string
		expr       string
		properties map[string]interface{}
		want       decimal.Decimal
		wantErr    bool
	}{
		{
			name:       "product of fields",
			expr:       "token * duration * pixel",
			properties: map[string]interface{}{"token": 10, "duration": 3, "pixel": 100},
			want:       decimal.NewFromInt(3000),
		},
		{
			name:       "sum two fields",
			expr:       "input_tokens + output_tokens",
			properties: map[string]interface{}{"input_tokens": 100, "output_tokens": 50},
			want:       decimal.NewFromInt(150),
		},
		{
			name:       "division",
			expr:       "double(duration_ms) / 1000.0",
			properties: map[string]interface{}{"duration_ms": 5000},
			want:       decimal.NewFromFloat(5),
		},
		{
			name:       "missing key defaults to 0",
			expr:       "a + b",
			properties: map[string]interface{}{"a": 10},
			want:       decimal.NewFromInt(10),
		},
		{
			name:       "empty expression",
			expr:       "",
			properties: map[string]interface{}{"a": 1},
			wantErr:    true,
		},
		{
			name:       "invalid expression",
			expr:       "a + b +",
			properties: map[string]interface{}{"a": 1, "b": 2},
			wantErr:    true,
		},
		{
			name:       "nil properties",
			expr:       "a + b",
			properties: nil,
			want:       decimal.Zero,
		},
		{
			name:       "weighted sum",
			expr:       "input_tokens * 2 + output_tokens",
			properties: map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
			want:       decimal.NewFromInt(25),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := eval.EvaluateQuantity(tt.expr, tt.properties)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.True(t, tt.want.Equal(got), "expected %s, got %s", tt.want.String(), got.String())
		})
	}
}

func TestCELEvaluator_Cache(t *testing.T) {
	eval := NewCELEvaluator()

	// First call compiles
	got1, err := eval.EvaluateQuantity("a + b", map[string]interface{}{"a": 1, "b": 2})
	require.NoError(t, err)
	assert.True(t, decimal.NewFromInt(3).Equal(got1))

	// Second call uses cache
	got2, err := eval.EvaluateQuantity("a + b", map[string]interface{}{"a": 5, "b": 2})
	require.NoError(t, err)
	assert.True(t, decimal.NewFromInt(7).Equal(got2))
}
