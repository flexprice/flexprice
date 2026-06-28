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
			// Headline behavior: integer-literal division is real, not truncated.
			name:       "real division with integer literal",
			expr:       "total / 4",
			properties: map[string]interface{}{"total": 10},
			want:       decimal.NewFromFloat(2.5),
		},
		{
			name:       "real division of two fields",
			expr:       "numerator / denominator",
			properties: map[string]interface{}{"numerator": 7, "denominator": 2},
			want:       decimal.NewFromFloat(3.5),
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
		{
			name:       "numeric string coerced to number",
			expr:       "tokens * 3",
			properties: map[string]interface{}{"tokens": "2"},
			want:       decimal.NewFromInt(6),
		},
		{
			name:       "two numeric string fields",
			expr:       "price * qty",
			properties: map[string]interface{}{"price": "2", "qty": "3"},
			want:       decimal.NewFromInt(6),
		},
		{
			name:       "fractional string coerced to float",
			expr:       "amount * 1.0",
			properties: map[string]interface{}{"amount": "5.5"},
			want:       decimal.NewFromFloat(5.5),
		},
		{
			name:       "non-numeric string used in math errors",
			expr:       "a * b",
			properties: map[string]interface{}{"a": 2, "b": "abc"},
			wantErr:    true,
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

func TestCELEvaluator_DivideByZero(t *testing.T) {
	eval := NewCELEvaluator()

	// All arithmetic is double, so dividing by zero yields +Inf rather than a CEL
	// error. The non-finite guard in toDecimal must turn that into a clean error
	// instead of panicking decimal.NewFromFloat.
	cases := []struct {
		name       string
		expr       string
		properties map[string]interface{}
	}{
		{"missing divisor", "a / b", map[string]interface{}{"a": 10}},
		{"explicit zero divisor", "a / b", map[string]interface{}{"a": 10, "b": 0}},
		{"zero over zero (NaN)", "a / b", map[string]interface{}{"a": 0, "b": 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				_, err := eval.EvaluateQuantity(tc.expr, tc.properties)
				require.Error(t, err)
			})
		})
	}
}

func TestCELEvaluator_Validate(t *testing.T) {
	eval := NewCELEvaluator()

	t.Run("valid expression", func(t *testing.T) {
		require.NoError(t, eval.Validate("tokens * 2 + overage"))
	})
	t.Run("empty expression", func(t *testing.T) {
		require.Error(t, eval.Validate(""))
	})
	t.Run("syntax error", func(t *testing.T) {
		require.Error(t, eval.Validate("a + "))
	})
	t.Run("no variable identifiers", func(t *testing.T) {
		require.Error(t, eval.Validate("1 + 2"))
	})
	t.Run("non-numeric expression rejected at definition time", func(t *testing.T) {
		// Modulo has no double overload; the fully-typed env rejects it on compile
		// rather than letting it fail per-event at runtime.
		require.Error(t, eval.Validate("a % b"))
	})
}

func TestCELEvaluator_ValidateDoesNotCache(t *testing.T) {
	eval := NewCELEvaluator()

	// Use an expression unique to this test so the assertions hold whether the
	// evaluator is a fresh instance or a process-wide shared singleton.
	const expr = "vdnc_alpha + vdnc_beta"

	require.NoError(t, eval.Validate(expr))
	if _, cached := eval.cache.Load(expr); cached {
		t.Fatal("Validate must not populate the program cache")
	}

	// Sanity check: evaluation still caches.
	_, err := eval.EvaluateQuantity(expr, map[string]interface{}{"vdnc_alpha": 1, "vdnc_beta": 2})
	require.NoError(t, err)
	if _, cached := eval.cache.Load(expr); !cached {
		t.Fatal("EvaluateQuantity should cache the compiled expression")
	}
}
