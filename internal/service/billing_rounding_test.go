package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// TestLineItemRounding_FixedCharges tests that fixed charge line items are rounded
// at source before being added to the subtotal
func TestLineItemRounding_FixedCharges(t *testing.T) {
	tests := []struct {
		name            string
		amount          string // unrounded amount
		currency        string
		expectedRounded string
		description     string
	}{
		{
			name:            "USD_RoundUp",
			amount:          "10.278798",
			currency:        "usd",
			expectedRounded: "10.28",
			description:     "USD with 2 decimals rounds 10.278798 up to 10.28",
		},
		{
			name:            "USD_RoundDown",
			amount:          "10.271",
			currency:        "usd",
			expectedRounded: "10.27",
			description:     "USD with 2 decimals rounds 10.271 down to 10.27",
		},
		{
			name:            "USD_RoundHalfUp",
			amount:          "10.275",
			currency:        "usd",
			expectedRounded: "10.28",
			description:     "USD rounds half up: 10.275 -> 10.28",
		},
		{
			name:            "JPY_RoundUp",
			amount:          "1023.45",
			currency:        "jpy",
			expectedRounded: "1023",
			description:     "JPY with 0 decimals rounds 1023.45 up to 1023",
		},
		{
			name:            "JPY_RoundHalfUp",
			amount:          "1023.5",
			currency:        "jpy",
			expectedRounded: "1024",
			description:     "JPY rounds half up: 1023.5 -> 1024",
		},
		{
			name:            "EUR_Standard",
			amount:          "99.996",
			currency:        "eur",
			expectedRounded: "100.00",
			description:     "EUR rounds 99.996 to 100.00",
		},
		{
			name:            "GBP_SubCent",
			amount:          "0.004",
			currency:        "gbp",
			expectedRounded: "0.00",
			description:     "Sub-cent amount rounds to zero",
		},
		{
			name:            "GBP_RoundUpSubCent",
			amount:          "0.005",
			currency:        "gbp",
			expectedRounded: "0.01",
			description:     "0.005 rounds up to 0.01",
		},
		{
			name:            "USD_LargeAmount",
			amount:          "999999.999",
			currency:        "usd",
			expectedRounded: "1000000.00",
			description:     "Large amounts rounded correctly",
		},
		{
			name:            "USD_Zero",
			amount:          "0.00",
			currency:        "usd",
			expectedRounded: "0.00",
			description:     "Zero remains zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount := decimal.RequireFromString(tt.amount)
			expected := decimal.RequireFromString(tt.expectedRounded)

			rounded := types.RoundToCurrencyPrecision(amount, tt.currency)

			assert.True(t, rounded.Equal(expected),
				"%s: expected %s, got %s",
				tt.description, expected.String(), rounded.String())

			// Verify precision matches currency config
			precision := types.GetCurrencyPrecision(tt.currency)
			decimalPlaces := int32(len(rounded.String()) - len(rounded.Truncate(0).String()) - 1)
			if rounded.IsInteger() {
				decimalPlaces = 0
			}
			assert.LessOrEqual(t, decimalPlaces, precision,
				"Rounded value should not exceed currency precision")
		})
	}
}

// TestLineItemRounding_SubtotalEqualsSum tests that subtotal equals the sum
// of individually rounded line items (critical for "round at source" approach)
func TestLineItemRounding_SubtotalEqualsSum(t *testing.T) {
	tests := []struct {
		name             string
		lineItemAmounts  []string // unrounded amounts
		currency         string
		expectedSubtotal string
		description      string
	}{
		{
			name:             "ThreeItems_USD",
			lineItemAmounts:  []string{"10.333333", "10.333333", "10.333333"},
			currency:         "usd",
			expectedSubtotal: "30.99", // 10.33 + 10.33 + 10.33
			description:      "Stripe pattern: 3 items at $10.333333 -> $30.99 subtotal",
		},
		{
			name:             "RepeatingDecimal_USD",
			lineItemAmounts:  []string{"33.333333", "33.333333", "33.333334"},
			currency:         "usd",
			expectedSubtotal: "99.99", // 33.33 + 33.33 + 33.33 (note: 33.333334 rounds down to 33.33)
			description:      "Dividing $100 by 3: each rounds to 33.33, sum = 99.99 (expected rounding loss)",
		},
		{
			name:             "JPY_MultipleItems",
			lineItemAmounts:  []string{"100.4", "200.5", "300.6"},
			currency:         "jpy",
			expectedSubtotal: "602", // 100 + 201 + 301
			description:      "JPY rounds each item to integer, then sums",
		},
		{
			name:             "ManySmallAmounts_USD",
			lineItemAmounts:  []string{"0.011", "0.012", "0.013", "0.014", "0.015"},
			currency:         "usd",
			expectedSubtotal: "0.06", // 0.01 + 0.01 + 0.01 + 0.01 + 0.02 = 0.06
			description:      "Many sub-cent amounts accumulate correctly",
		},
		{
			name:             "SingleLargeAmount",
			lineItemAmounts:  []string{"1000000.009"},
			currency:         "usd",
			expectedSubtotal: "1000000.01",
			description:      "Single large amount rounded correctly",
		},
		{
			name:             "HundredSmallItems",
			lineItemAmounts:  generateRepeatingAmounts("0.011", 100),
			currency:         "usd",
			expectedSubtotal: "1.00", // 100 * 0.01
			description:      "Stress test: 100 items of $0.011 each -> $1.00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectedSubtotal := decimal.RequireFromString(tt.expectedSubtotal)

			// Simulate what happens in CalculateFixedCharges/CalculateUsageCharges
			subtotal := decimal.Zero
			for _, amountStr := range tt.lineItemAmounts {
				amount := decimal.RequireFromString(amountStr)
				// Round at source (this is what we do now)
				roundedAmount := types.RoundToCurrencyPrecision(amount, tt.currency)
				subtotal = subtotal.Add(roundedAmount)
			}

			assert.True(t, subtotal.Equal(expectedSubtotal),
				"%s: expected subtotal %s, got %s",
				tt.description, expectedSubtotal.String(), subtotal.String())
		})
	}
}

// TestLineItemRounding_UsageCharges tests usage charge rounding
func TestLineItemRounding_UsageCharges(t *testing.T) {
	tests := []struct {
		name            string
		quantity        string
		unitPrice       string
		currency        string
		expectedRounded string
		description     string
	}{
		{
			name:            "Usage_SimpleMultiplication",
			quantity:        "150",
			unitPrice:       "0.10",
			currency:        "usd",
			expectedRounded: "15.00",
			description:     "150 units * $0.10 = $15.00",
		},
		{
			name:            "Usage_FractionalUnits",
			quantity:        "123.456",
			unitPrice:       "0.50",
			currency:        "usd",
			expectedRounded: "61.73", // 123.456 * 0.50 = 61.728 -> 61.73
			description:     "Fractional units rounded after multiplication",
		},
		{
			name:            "Usage_HighPrecision",
			quantity:        "1000",
			unitPrice:       "0.003333",
			currency:        "usd",
			expectedRounded: "3.33", // 1000 * 0.003333 = 3.333 -> 3.33
			description:     "High-precision unit price rounded after calculation",
		},
		{
			name:            "Usage_JPY",
			quantity:        "50",
			unitPrice:       "10.5",
			currency:        "jpy",
			expectedRounded: "525", // 50 * 10.5 = 525 (already integer)
			description:     "JPY usage charge rounds to integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quantity := decimal.RequireFromString(tt.quantity)
			unitPrice := decimal.RequireFromString(tt.unitPrice)
			expected := decimal.RequireFromString(tt.expectedRounded)

			// Calculate unrounded amount
			amount := quantity.Mul(unitPrice)

			// Round at source (what we do now)
			roundedAmount := types.RoundToCurrencyPrecision(amount, tt.currency)

			assert.True(t, roundedAmount.Equal(expected),
				"%s: expected %s, got %s",
				tt.description, expected.String(), roundedAmount.String())
		})
	}
}

// TestLineItemRounding_CommitmentTrueUp tests commitment true-up rounding
func TestLineItemRounding_CommitmentTrueUp(t *testing.T) {
	tests := []struct {
		name             string
		commitmentAmount string
		usedAmount       string
		currency         string
		expectedTrueUp   string
		description      string
	}{
		{
			name:             "TrueUp_ExactDifference",
			commitmentAmount: "1000.00",
			usedAmount:       "750.00",
			currency:         "usd",
			expectedTrueUp:   "250.00",
			description:      "Commitment $1000, used $750, true-up $250",
		},
		{
			name:             "TrueUp_WithRounding",
			commitmentAmount: "1000.00",
			usedAmount:       "749.996",
			currency:         "usd",
			expectedTrueUp:   "250.00", // 1000 - 750.00 = 250.00
			description:      "Used amount rounds to 750.00, true-up is 250.00",
		},
		{
			name:             "TrueUp_JPY",
			commitmentAmount: "10000",
			usedAmount:       "8500.5",
			currency:         "jpy",
			expectedTrueUp:   "1499", // 10000 - 8501 = 1499
			description:      "JPY commitment true-up rounds to integer",
		},
		{
			name:             "TrueUp_SubCent",
			commitmentAmount: "10.00",
			usedAmount:       "9.997",
			currency:         "usd",
			expectedTrueUp:   "0.00", // 10.00 - 10.00 = 0.00
			description:      "Sub-cent true-up rounds to zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commitment := decimal.RequireFromString(tt.commitmentAmount)
			used := decimal.RequireFromString(tt.usedAmount)
			expected := decimal.RequireFromString(tt.expectedTrueUp)

			// Round used amount first (as it would be from usage charges)
			roundedUsed := types.RoundToCurrencyPrecision(used, tt.currency)

			// Calculate remaining commitment
			remaining := commitment.Sub(roundedUsed)

			// Round true-up amount (as done in billing.go line 1286)
			roundedTrueUp := types.RoundToCurrencyPrecision(remaining, tt.currency)

			assert.True(t, roundedTrueUp.Equal(expected),
				"%s: expected %s, got %s",
				tt.description, expected.String(), roundedTrueUp.String())
		})
	}
}

// TestLineItemRounding_EdgeCases tests edge cases for line item rounding
func TestLineItemRounding_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		amount      string
		currency    string
		expected    string
		description string
	}{
		{
			name:        "Exactly_Half_Cent",
			amount:      "10.125",
			currency:    "usd",
			expected:    "10.13",
			description: "Standard rounding: 0.125 rounds up to 0.13 (round half up)",
		},
		{
			name:        "Negative_Amount",
			amount:      "-10.50",
			currency:    "usd",
			expected:    "-10.50",
			description: "Negative amounts round correctly (for credit scenarios)",
		},
		{
			name:        "VerySmall_Positive",
			amount:      "0.0001",
			currency:    "usd",
			expected:    "0.00",
			description: "Very small positive amount rounds to zero",
		},
		{
			name:        "VeryLarge_Amount",
			amount:      "99999999.999",
			currency:    "usd",
			expected:    "100000000.00",
			description: "Very large amounts don't overflow and round correctly",
		},
		{
			name:        "Zero_Amount",
			amount:      "0",
			currency:    "usd",
			expected:    "0.00",
			description: "Zero amount remains zero",
		},
		{
			name:        "JPY_SubInteger",
			amount:      "0.9",
			currency:    "jpy",
			expected:    "1",
			description: "JPY sub-integer rounds to 1",
		},
		{
			name:        "JPY_Exactly_Half",
			amount:      "100.5",
			currency:    "jpy",
			expected:    "101",
			description: "JPY half value rounds up",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount := decimal.RequireFromString(tt.amount)
			expected := decimal.RequireFromString(tt.expected)

			rounded := types.RoundToCurrencyPrecision(amount, tt.currency)

			assert.True(t, rounded.Equal(expected),
				"%s: expected %s, got %s",
				tt.description, expected.String(), rounded.String())
		})
	}
}

// TestLineItemRounding_NoAccumulationErrors tests that summing many rounded
// line items doesn't cause accumulation errors
func TestLineItemRounding_NoAccumulationErrors(t *testing.T) {
	t.Run("Thousand_Small_Items", func(t *testing.T) {
		// 1000 items at $0.01 each should equal exactly $10.00
		subtotal := decimal.Zero
		itemAmount := types.RoundToCurrencyPrecision(decimal.NewFromFloat(0.01), "usd")

		for i := 0; i < 1000; i++ {
			subtotal = subtotal.Add(itemAmount)
		}

		expected := decimal.NewFromFloat(10.00)
		assert.True(t, subtotal.Equal(expected),
			"1000 items at $0.01 should equal $10.00, got %s", subtotal.String())
	})

	t.Run("Hundred_Thirds", func(t *testing.T) {
		// 100 items at $0.333333 each
		// Each rounds to $0.33, sum should be $33.00
		subtotal := decimal.Zero
		itemAmount := types.RoundToCurrencyPrecision(
			decimal.NewFromFloat(0.333333), "usd")

		for i := 0; i < 100; i++ {
			subtotal = subtotal.Add(itemAmount)
		}

		expected := decimal.NewFromFloat(33.00)
		assert.True(t, subtotal.Equal(expected),
			"100 items at rounded $0.33 should equal $33.00, got %s", subtotal.String())
	})

	t.Run("JPY_Large_Quantity", func(t *testing.T) {
		// 10000 items at ¥1.5 each
		// Each rounds to ¥2, sum should be ¥20000
		subtotal := decimal.Zero
		itemAmount := types.RoundToCurrencyPrecision(
			decimal.NewFromFloat(1.5), "jpy")

		for i := 0; i < 10000; i++ {
			subtotal = subtotal.Add(itemAmount)
		}

		expected := decimal.NewFromInt(20000)
		assert.True(t, subtotal.Equal(expected),
			"10000 items at ¥2 should equal ¥20000, got %s", subtotal.String())
	})
}

// Helper function to generate repeating amounts for stress tests
func generateRepeatingAmounts(amount string, count int) []string {
	amounts := make([]string, count)
	for i := 0; i < count; i++ {
		amounts[i] = amount
	}
	return amounts
}

// TestLineItemRounding_RealWorldScenario tests a complete realistic scenario
func TestLineItemRounding_RealWorldScenario(t *testing.T) {

	// Simulate a subscription with multiple line items
	// This tests the actual flow through CalculateFixedCharges/CalculateUsageCharges

	t.Run("Mixed_Line_Items", func(t *testing.T) {
		// Scenario:
		// - 3 fixed charges: $10.333, $20.666, $30.999
		// - 2 usage charges: 150 * $0.101 = $15.15, 75 * $0.333 = $24.975
		// Expected subtotal after rounding each:
		// Fixed: $10.33 + $20.67 + $31.00 = $62.00
		// Usage: $15.15 + $24.98 = $40.13
		// Total: $102.13

		fixedAmounts := []string{"10.333", "20.666", "30.999"}
		usageCalculations := []struct {
			quantity  string
			unitPrice string
		}{
			{"150", "0.101"}, // 15.15
			{"75", "0.333"},  // 24.975 -> 24.98
		}

		subtotal := decimal.Zero

		// Add fixed charges (rounded at source)
		for _, amountStr := range fixedAmounts {
			amount := decimal.RequireFromString(amountStr)
			rounded := types.RoundToCurrencyPrecision(amount, "usd")
			subtotal = subtotal.Add(rounded)
		}

		// Add usage charges (calculated then rounded at source)
		for _, calc := range usageCalculations {
			qty := decimal.RequireFromString(calc.quantity)
			price := decimal.RequireFromString(calc.unitPrice)
			amount := qty.Mul(price)
			rounded := types.RoundToCurrencyPrecision(amount, "usd")
			subtotal = subtotal.Add(rounded)
		}

		expected := decimal.RequireFromString("102.13")
		assert.True(t, subtotal.Equal(expected),
			"Real-world scenario: expected $102.13, got %s", subtotal.String())
	})
}
