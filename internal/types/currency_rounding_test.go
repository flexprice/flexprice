package types

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// TestCurrencyRounding_AllPrecisions tests rounding for all configured currencies
func TestCurrencyRounding_AllPrecisions(t *testing.T) {
	tests := []struct {
		name        string
		amount      string
		currency    string
		expected    string
		precision   int32
		description string
	}{
		// 2-decimal currencies (most common)
		{
			name:        "USD_Standard",
			amount:      "10.275",
			currency:    "usd",
			expected:    "10.28",
			precision:   2,
			description: "USD rounds to 2 decimals",
		},
		{
			name:        "EUR_Standard",
			amount:      "10.275",
			currency:    "eur",
			expected:    "10.28",
			precision:   2,
			description: "EUR rounds to 2 decimals",
		},
		{
			name:        "GBP_Standard",
			amount:      "10.275",
			currency:    "gbp",
			expected:    "10.28",
			precision:   2,
			description: "GBP rounds to 2 decimals",
		},

		// 0-decimal currencies
		{
			name:        "JPY_NoDecimals",
			amount:      "1000.5",
			currency:    "jpy",
			expected:    "1001",
			precision:   0,
			description: "JPY rounds to 0 decimals (no fractional yen)",
		},
		{
			name:        "KRW_NoDecimals",
			amount:      "1000.5",
			currency:    "krw",
			expected:    "1001",
			precision:   0,
			description: "KRW rounds to 0 decimals (no fractional won)",
		},
		{
			name:        "VND_NoDecimals",
			amount:      "1000.5",
			currency:    "vnd",
			expected:    "1001",
			precision:   0,
			description: "VND rounds to 0 decimals (no fractional dong)",
		},
		{
			name:        "CLP_NoDecimals",
			amount:      "1000.5",
			currency:    "clp",
			expected:    "1001",
			precision:   0,
			description: "CLP rounds to 0 decimals (no fractional peso)",
		},

		// Other currencies
		{
			name:        "INR_TwoDecimals",
			amount:      "100.556",
			currency:    "inr",
			expected:    "100.56",
			precision:   2,
			description: "INR rounds to 2 decimals",
		},
		{
			name:        "SGD_TwoDecimals",
			amount:      "100.556",
			currency:    "sgd",
			expected:    "100.56",
			precision:   2,
			description: "SGD rounds to 2 decimals",
		},
		{
			name:        "AUD_TwoDecimals",
			amount:      "100.556",
			currency:    "aud",
			expected:    "100.56",
			precision:   2,
			description: "AUD rounds to 2 decimals",
		},
		{
			name:        "CAD_TwoDecimals",
			amount:      "100.556",
			currency:    "cad",
			expected:    "100.56",
			precision:   2,
			description: "CAD rounds to 2 decimals",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount := decimal.RequireFromString(tt.amount)
			expected := decimal.RequireFromString(tt.expected)

			// Test RoundToCurrencyPrecision function
			rounded := RoundToCurrencyPrecision(amount, tt.currency)

			assert.True(t, rounded.Equal(expected),
				"%s: expected %s, got %s",
				tt.description, expected.String(), rounded.String())

			// Verify precision matches config
			actualPrecision := GetCurrencyPrecision(tt.currency)
			assert.Equal(t, tt.precision, actualPrecision,
				"%s: currency precision should be %d", tt.currency, tt.precision)

			// Verify rounded value respects precision
			assert.Equal(t, rounded.Round(actualPrecision), rounded,
				"Rounded value should already be at currency precision")
		})
	}
}

// TestCurrencyRounding_EdgeCases tests edge cases for currency rounding
func TestCurrencyRounding_EdgeCases(t *testing.T) {
	t.Run("SubCent_USD", func(t *testing.T) {
		tests := []struct {
			amount   string
			expected string
		}{
			{"0.001", "0.00"},
			{"0.004", "0.00"},
			{"0.005", "0.01"}, // Round half up
			{"0.009", "0.01"},
			{"0.014", "0.01"},
			{"0.015", "0.02"},
		}

		for _, tt := range tests {
			amount := decimal.RequireFromString(tt.amount)
			expected := decimal.RequireFromString(tt.expected)
			rounded := RoundToCurrencyPrecision(amount, "usd")

			assert.True(t, rounded.Equal(expected),
				"USD %s should round to %s, got %s",
				tt.amount, tt.expected, rounded.String())
		}
	})

	t.Run("SubInteger_JPY", func(t *testing.T) {
		tests := []struct {
			amount   string
			expected string
		}{
			{"0.1", "0"},
			{"0.4", "0"},
			{"0.5", "1"}, // Round half up
			{"0.9", "1"},
			{"1.4", "1"},
			{"1.5", "2"},
			{"99.5", "100"},
		}

		for _, tt := range tests {
			amount := decimal.RequireFromString(tt.amount)
			expected := decimal.RequireFromString(tt.expected)
			rounded := RoundToCurrencyPrecision(amount, "jpy")

			assert.True(t, rounded.Equal(expected),
				"JPY %s should round to %s, got %s",
				tt.amount, tt.expected, rounded.String())
		}
	})

	t.Run("Exactly_Half", func(t *testing.T) {
		// Test standard rounding (round half up) behavior
		tests := []struct {
			amount   string
			currency string
			expected string
		}{
			{"10.125", "usd", "10.13"}, // 0.125 rounds up
			{"10.225", "usd", "10.23"},
			{"10.325", "usd", "10.33"},
			{"100.5", "jpy", "101"}, // 0.5 rounds up
			{"200.5", "jpy", "201"},
		}

		for _, tt := range tests {
			amount := decimal.RequireFromString(tt.amount)
			expected := decimal.RequireFromString(tt.expected)
			rounded := RoundToCurrencyPrecision(amount, tt.currency)

			assert.True(t, rounded.Equal(expected),
				"%s %s should round to %s (round half up), got %s",
				tt.currency, tt.amount, tt.expected, rounded.String())
		}
	})

	t.Run("Zero_Amount", func(t *testing.T) {
		currencies := []string{"usd", "eur", "gbp", "jpy", "krw"}

		for _, currency := range currencies {
			amount := decimal.Zero
			rounded := RoundToCurrencyPrecision(amount, currency)

			assert.True(t, rounded.Equal(decimal.Zero),
				"%s: zero should remain zero", currency)
		}
	})

	t.Run("Negative_Amounts", func(t *testing.T) {
		tests := []struct {
			amount   string
			currency string
			expected string
		}{
			{"-10.125", "usd", "-10.13"},
			{"-10.124", "usd", "-10.12"},
			{"-100.5", "jpy", "-101"},
			{"-100.4", "jpy", "-100"},
		}

		for _, tt := range tests {
			amount := decimal.RequireFromString(tt.amount)
			expected := decimal.RequireFromString(tt.expected)
			rounded := RoundToCurrencyPrecision(amount, tt.currency)

			assert.True(t, rounded.Equal(expected),
				"%s %s should round to %s, got %s",
				tt.currency, tt.amount, tt.expected, rounded.String())
		}
	})

	t.Run("Very_Large_Amounts", func(t *testing.T) {
		tests := []struct {
			amount   string
			currency string
			expected string
		}{
			{"999999999.999", "usd", "1000000000.00"},
			{"999999999.5", "jpy", "1000000000"},
			{"1000000000.001", "usd", "1000000000.00"},
		}

		for _, tt := range tests {
			amount := decimal.RequireFromString(tt.amount)
			expected := decimal.RequireFromString(tt.expected)
			rounded := RoundToCurrencyPrecision(amount, tt.currency)

			assert.True(t, rounded.Equal(expected),
				"Large amount %s %s should round to %s",
				tt.currency, tt.amount, tt.expected)
		}
	})

	t.Run("Repeating_Decimals", func(t *testing.T) {
		// Test 1/3, 2/3, etc.
		tests := []struct {
			amount   string
			currency string
			expected string
		}{
			{"0.333333333", "usd", "0.33"},
			{"0.666666666", "usd", "0.67"},
			{"10.333333333", "usd", "10.33"},
			{"10.666666666", "usd", "10.67"},
			{"333.333333", "jpy", "333"},
			{"666.666666", "jpy", "667"},
		}

		for _, tt := range tests {
			amount := decimal.RequireFromString(tt.amount)
			expected := decimal.RequireFromString(tt.expected)
			rounded := RoundToCurrencyPrecision(amount, tt.currency)

			assert.True(t, rounded.Equal(expected),
				"%s %s should round to %s, got %s",
				tt.currency, tt.amount, tt.expected, rounded.String())
		}
	})
}

// TestCurrencyRounding_PrecisionConfig tests the currency configuration
func TestCurrencyRounding_PrecisionConfig(t *testing.T) {
	t.Run("Verify_All_Configured_Currencies", func(t *testing.T) {
		expectedPrecisions := map[string]int32{
			"usd": 2, "eur": 2, "gbp": 2, "aud": 2, "cad": 2,
			"jpy": 0, "krw": 0, "vnd": 0, "clp": 0,
			"inr": 2, "idr": 2, "sgd": 2, "thb": 2, "myr": 2,
			"php": 2, "hkd": 2, "nzd": 2, "brl": 2, "chf": 2,
			"cny": 2, "czk": 2, "dkk": 2, "huf": 2, "ils": 2,
			"mxn": 2, "nok": 2, "pln": 2, "ron": 2, "rub": 2,
			"sar": 2, "sek": 2, "try": 2, "twd": 2, "zar": 2,
		}

		for currency, expectedPrecision := range expectedPrecisions {
			actualPrecision := GetCurrencyPrecision(currency)
			assert.Equal(t, expectedPrecision, actualPrecision,
				"%s should have precision %d, got %d",
				currency, expectedPrecision, actualPrecision)
		}
	})

	t.Run("Unknown_Currency_Uses_Default", func(t *testing.T) {
		unknownCurrency := "xxx"
		precision := GetCurrencyPrecision(unknownCurrency)

		assert.Equal(t, int32(DEFAULT_PRECISION), precision,
			"Unknown currency should use default precision %d", DEFAULT_PRECISION)
	})
}

// TestCurrencyRounding_Consistency tests that rounding is consistent
// across multiple operations
func TestCurrencyRounding_Consistency(t *testing.T) {
	t.Run("Idempotent_Rounding", func(t *testing.T) {
		// Rounding an already-rounded value should not change it
		amount := decimal.RequireFromString("10.27")

		rounded1 := RoundToCurrencyPrecision(amount, "usd")
		rounded2 := RoundToCurrencyPrecision(rounded1, "usd")

		assert.True(t, rounded1.Equal(rounded2),
			"Rounding twice should give same result")
		assert.True(t, rounded1.Equal(amount),
			"Already-rounded value should not change")
	})

	t.Run("Addition_Then_Round_vs_Round_Then_Add", func(t *testing.T) {
		// This test documents that order matters:
		// Adding then rounding != rounding each then adding

		a := decimal.RequireFromString("10.333")
		b := decimal.RequireFromString("10.333")

		// Method 1: Add then round
		sum1 := a.Add(b)
		result1 := RoundToCurrencyPrecision(sum1, "usd")
		expected1 := decimal.RequireFromString("20.67") // 20.666 -> 20.67

		// Method 2: Round each then add (our approach)
		roundedA := RoundToCurrencyPrecision(a, "usd")
		roundedB := RoundToCurrencyPrecision(b, "usd")
		result2 := roundedA.Add(roundedB)
		expected2 := decimal.RequireFromString("20.66") // 10.33 + 10.33

		assert.True(t, result1.Equal(expected1),
			"Add then round: %s", result1.String())
		assert.True(t, result2.Equal(expected2),
			"Round then add: %s", result2.String())

		// Document the difference
		assert.False(t, result1.Equal(result2),
			"Different approaches give different results: %s vs %s",
			result1.String(), result2.String())

		// Our system uses "round then add" (Method 2)
		t.Logf("Our approach: round each component first, then sum")
		t.Logf("Result: %s (each rounds to %s, then sum)",
			result2.String(), roundedA.String())
	})
}

// TestCurrencyRounding_StressTest tests rounding under stress conditions
func TestCurrencyRounding_StressTest(t *testing.T) {
	t.Run("Thousand_Small_Amounts", func(t *testing.T) {
		// Round 1000 small amounts and ensure consistency
		amount := decimal.RequireFromString("0.011")

		for i := 0; i < 1000; i++ {
			rounded := RoundToCurrencyPrecision(amount, "usd")
			expected := decimal.RequireFromString("0.01")

			assert.True(t, rounded.Equal(expected),
				"Iteration %d: rounding should be consistent", i)
		}
	})

	t.Run("Large_Scale_Accumulation", func(t *testing.T) {
		// Add 10000 rounded amounts and verify precision maintained
		total := decimal.Zero
		itemAmount := decimal.RequireFromString("0.01")

		for i := 0; i < 10000; i++ {
			total = total.Add(itemAmount)
		}

		// Total should be exactly $100.00
		expected := decimal.NewFromFloat(100.00)
		assert.True(t, total.Equal(expected),
			"10000 x $0.01 should equal $100.00, got %s", total.String())

		// Verify it's properly rounded
		rounded := RoundToCurrencyPrecision(total, "usd")
		assert.True(t, rounded.Equal(total),
			"Accumulated total should already be rounded")
	})
}
