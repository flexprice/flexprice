package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// TestTaxRounding_PercentageTax tests that percentage tax calculations
// are rounded at source before being aggregated
func TestTaxRounding_PercentageTax(t *testing.T) {
	tests := []struct {
		name          string
		taxableAmount string
		taxPercentage string
		currency      string
		expectedTax   string
		description   string
	}{
		{
			name:          "Simple_10Percent_USD",
			taxableAmount: "100.00",
			taxPercentage: "10",
			currency:      "usd",
			expectedTax:   "10.00",
			description:   "10% tax on $100 = $10.00",
		},
		{
			name:          "Sales_Tax_8.875_Percent",
			taxableAmount: "100.00",
			taxPercentage: "8.875",
			currency:      "usd",
			expectedTax:   "8.88", // 100 * 0.08875 = 8.875 -> 8.88
			description:   "NY sales tax 8.875% on $100 rounds to $8.88",
		},
		{
			name:          "VAT_20_Percent_EUR",
			taxableAmount: "99.99",
			taxPercentage: "20",
			currency:      "eur",
			expectedTax:   "20.00", // 99.99 * 0.20 = 19.998 -> 20.00
			description:   "EU VAT 20% on €99.99 rounds to €20.00",
		},
		{
			name:          "Fractional_Tax_Rate",
			taxableAmount: "123.45",
			taxPercentage: "7.25",
			currency:      "usd",
			expectedTax:   "8.95", // 123.45 * 0.0725 = 8.950125 -> 8.95
			description:   "7.25% tax with rounding",
		},
		{
			name:          "Repeating_Decimal_Tax",
			taxableAmount: "100.00",
			taxPercentage: "33.333",
			currency:      "usd",
			expectedTax:   "33.33", // 100 * 0.33333 = 33.333 -> 33.33
			description:   "Repeating decimal tax rate",
		},
		{
			name:          "JPY_Tax",
			taxableAmount: "1000",
			taxPercentage: "10",
			currency:      "jpy",
			expectedTax:   "100",
			description:   "10% tax on ¥1000 = ¥100 (no decimals)",
		},
		{
			name:          "JPY_Tax_WithRounding",
			taxableAmount: "999",
			taxPercentage: "10.5",
			currency:      "jpy",
			expectedTax:   "105", // 999 * 0.105 = 104.895 -> 105
			description:   "JPY tax rounds to nearest integer",
		},
		{
			name:          "Very_Small_Tax_Rate",
			taxableAmount: "100.00",
			taxPercentage: "0.01",
			currency:      "usd",
			expectedTax:   "0.01", // 100 * 0.0001 = 0.01
			description:   "0.01% tax rate",
		},
		{
			name:          "SubCent_Tax_Result",
			taxableAmount: "1.00",
			taxPercentage: "0.1",
			currency:      "usd",
			expectedTax:   "0.00", // 1.00 * 0.001 = 0.001 -> 0.00
			description:   "Sub-cent tax rounds to zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taxableAmount := decimal.RequireFromString(tt.taxableAmount)
			taxPercentage := decimal.RequireFromString(tt.taxPercentage)
			expectedTax := decimal.RequireFromString(tt.expectedTax)

			// Calculate tax (as done in tax.go calculateTaxAmount)
			taxAmount := taxableAmount.Mul(taxPercentage).Div(decimal.NewFromInt(100))

			// Round at source (as done in tax.go ApplyTaxesOnInvoice)
			roundedTax := types.RoundToCurrencyPrecision(taxAmount, tt.currency)

			assert.True(t, roundedTax.Equal(expectedTax),
				"%s: expected %s, got %s",
				tt.description, expectedTax.String(), roundedTax.String())

			// Verify precision
			precision := types.GetCurrencyPrecision(tt.currency)
			assert.Equal(t, roundedTax.Round(precision), roundedTax,
				"Tax should be rounded to currency precision")
		})
	}
}

// TestTaxRounding_FixedTax tests fixed tax amounts
func TestTaxRounding_FixedTax(t *testing.T) {
	tests := []struct {
		name        string
		fixedAmount string
		currency    string
		expectedTax string
		description string
	}{
		{
			name:        "Fixed_10_USD",
			fixedAmount: "10.00",
			currency:    "usd",
			expectedTax: "10.00",
			description: "Fixed $10 tax",
		},
		{
			name:        "Fixed_JPY",
			fixedAmount: "100",
			currency:    "jpy",
			expectedTax: "100",
			description: "Fixed ¥100 tax",
		},
		{
			name:        "Fixed_SubCent",
			fixedAmount: "0.50",
			currency:    "usd",
			expectedTax: "0.50",
			description: "Fixed $0.50 tax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixedAmount := decimal.RequireFromString(tt.fixedAmount)
			expectedTax := decimal.RequireFromString(tt.expectedTax)

			// Fixed taxes should already be in correct precision
			// But we still round to be safe
			roundedTax := types.RoundToCurrencyPrecision(fixedAmount, tt.currency)

			assert.True(t, roundedTax.Equal(expectedTax),
				"%s: expected %s, got %s",
				tt.description, expectedTax.String(), roundedTax.String())
		})
	}
}

// TestTaxRounding_MultipleTaxRates tests that multiple taxes on the same
// invoice are each rounded individually before being summed
func TestTaxRounding_MultipleTaxRates(t *testing.T) {
	t.Run("Two_Tax_Rates_USD", func(t *testing.T) {
		// Scenario: $100 taxable amount with 8% state tax and 2% county tax
		// State tax: 100 * 0.08 = 8.00
		// County tax: 100 * 0.02 = 2.00
		// Total: 10.00

		taxableAmount := decimal.NewFromFloat(100.00)

		stateTaxRate := decimal.NewFromFloat(8)
		stateTax := taxableAmount.Mul(stateTaxRate).Div(decimal.NewFromInt(100))
		roundedStateTax := types.RoundToCurrencyPrecision(stateTax, "usd")

		countyTaxRate := decimal.NewFromFloat(2)
		countyTax := taxableAmount.Mul(countyTaxRate).Div(decimal.NewFromInt(100))
		roundedCountyTax := types.RoundToCurrencyPrecision(countyTax, "usd")

		totalTax := roundedStateTax.Add(roundedCountyTax)

		expectedTotal := decimal.NewFromFloat(10.00)
		assert.True(t, totalTax.Equal(expectedTotal),
			"Total tax should equal sum of rounded individual taxes")
	})

	t.Run("Three_Tax_Rates_With_Rounding", func(t *testing.T) {
		// Scenario: $99.99 with 8.875% state, 2.125% county, 0.5% special
		// State: 99.99 * 0.08875 = 8.87412 -> 8.87
		// County: 99.99 * 0.02125 = 2.12478 -> 2.12
		// Special: 99.99 * 0.005 = 0.49995 -> 0.50
		// Total: 11.49

		taxableAmount := decimal.NewFromFloat(99.99)

		taxes := []struct {
			rate     string
			expected string
		}{
			{"8.875", "8.87"},
			{"2.125", "2.12"},
			{"0.5", "0.50"},
		}

		totalTax := decimal.Zero
		for _, tax := range taxes {
			rate := decimal.RequireFromString(tax.rate)
			amount := taxableAmount.Mul(rate).Div(decimal.NewFromInt(100))
			rounded := types.RoundToCurrencyPrecision(amount, "usd")

			expected := decimal.RequireFromString(tax.expected)
			assert.True(t, rounded.Equal(expected),
				"Tax rate %s should round to %s, got %s",
				tax.rate, expected.String(), rounded.String())

			totalTax = totalTax.Add(rounded)
		}

		expectedTotal := decimal.NewFromFloat(11.49)
		assert.True(t, totalTax.Equal(expectedTotal),
			"Total tax should be $11.49, got %s", totalTax.String())
	})

	t.Run("JPY_Multiple_Taxes", func(t *testing.T) {
		// Scenario: ¥1000 with 8% consumption tax and 2% local tax
		// Consumption: 1000 * 0.08 = 80
		// Local: 1000 * 0.02 = 20
		// Total: 100

		taxableAmount := decimal.NewFromInt(1000)

		consumptionRate := decimal.NewFromFloat(8)
		consumptionTax := taxableAmount.Mul(consumptionRate).Div(decimal.NewFromInt(100))
		roundedConsumption := types.RoundToCurrencyPrecision(consumptionTax, "jpy")

		localRate := decimal.NewFromFloat(2)
		localTax := taxableAmount.Mul(localRate).Div(decimal.NewFromInt(100))
		roundedLocal := types.RoundToCurrencyPrecision(localTax, "jpy")

		totalTax := roundedConsumption.Add(roundedLocal)

		expectedTotal := decimal.NewFromInt(100)
		assert.True(t, totalTax.Equal(expectedTotal))
	})
}

// TestTaxRounding_TaxOnDiscountedAmount tests that tax is calculated on
// the discounted amount (discount-first approach)
func TestTaxRounding_TaxOnDiscountedAmount(t *testing.T) {
	t.Run("Discount_Then_Tax", func(t *testing.T) {
		// Scenario: $100 item with $10 discount, then 10% tax
		// Subtotal: $100
		// Discount: $10
		// Taxable: $90
		// Tax: $90 * 10% = $9.00
		// Total: $90 + $9 = $99

		subtotal := decimal.NewFromFloat(100.00)
		discount := decimal.NewFromFloat(10.00)
		taxableAmount := subtotal.Sub(discount)

		taxRate := decimal.NewFromFloat(10)
		taxAmount := taxableAmount.Mul(taxRate).Div(decimal.NewFromInt(100))
		roundedTax := types.RoundToCurrencyPrecision(taxAmount, "usd")

		expectedTax := decimal.NewFromFloat(9.00)
		assert.True(t, roundedTax.Equal(expectedTax),
			"Tax on discounted amount should be $9.00")

		total := taxableAmount.Add(roundedTax)
		expectedTotal := decimal.NewFromFloat(99.00)
		assert.True(t, total.Equal(expectedTotal))
	})

	t.Run("Large_Discount_Small_Tax", func(t *testing.T) {
		// $100 item with $90 discount, then 10% tax
		// Taxable: $10
		// Tax: $10 * 10% = $1.00

		subtotal := decimal.NewFromFloat(100.00)
		discount := decimal.NewFromFloat(90.00)
		taxableAmount := subtotal.Sub(discount)

		taxRate := decimal.NewFromFloat(10)
		taxAmount := taxableAmount.Mul(taxRate).Div(decimal.NewFromInt(100))
		roundedTax := types.RoundToCurrencyPrecision(taxAmount, "usd")

		expectedTax := decimal.NewFromFloat(1.00)
		assert.True(t, roundedTax.Equal(expectedTax))
	})
}

// TestTaxRounding_NoAccumulationErrors tests that summing many tax amounts
// doesn't cause accumulation errors
func TestTaxRounding_NoAccumulationErrors(t *testing.T) {
	t.Run("Hundred_Small_Taxes", func(t *testing.T) {
		// 100 tax calculations at 1% each on $1.00
		// Each tax: $1.00 * 1% = $0.01
		// Total: 100 * $0.01 = $1.00

		taxableAmount := decimal.NewFromFloat(1.00)
		taxRate := decimal.NewFromFloat(1)

		totalTax := decimal.Zero
		for i := 0; i < 100; i++ {
			tax := taxableAmount.Mul(taxRate).Div(decimal.NewFromInt(100))
			roundedTax := types.RoundToCurrencyPrecision(tax, "usd")
			totalTax = totalTax.Add(roundedTax)
		}

		expected := decimal.NewFromFloat(1.00)
		assert.True(t, totalTax.Equal(expected),
			"100 small taxes should total $1.00, got %s", totalTax.String())
	})

	t.Run("Many_Fractional_Taxes", func(t *testing.T) {
		// 50 tax calculations at 0.1% on $100.00
		// Each: $100 * 0.1% = $0.10
		// Total: 50 * $0.10 = $5.00

		taxableAmount := decimal.NewFromFloat(100.00)
		taxRate := decimal.NewFromFloat(0.1)

		totalTax := decimal.Zero
		for i := 0; i < 50; i++ {
			tax := taxableAmount.Mul(taxRate).Div(decimal.NewFromInt(100))
			roundedTax := types.RoundToCurrencyPrecision(tax, "usd")
			totalTax = totalTax.Add(roundedTax)
		}

		expected := decimal.NewFromFloat(5.00)
		assert.True(t, totalTax.Equal(expected))
	})
}

// TestTaxRounding_EdgeCases tests edge cases for tax rounding
func TestTaxRounding_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		taxableAmount string
		taxRate       string
		currency      string
		expectedTax   string
		description   string
	}{
		{
			name:          "Zero_Taxable_Amount",
			taxableAmount: "0.00",
			taxRate:       "10",
			currency:      "usd",
			expectedTax:   "0.00",
			description:   "Tax on zero is zero",
		},
		{
			name:          "Very_Large_Amount",
			taxableAmount: "999999.99",
			taxRate:       "10",
			currency:      "usd",
			expectedTax:   "100000.00", // 999999.99 * 0.10 = 99999.999 -> 100000.00
			description:   "Large amount tax rounds correctly",
		},
		{
			name:          "Tiny_Tax_Rate",
			taxableAmount: "100.00",
			taxRate:       "0.001",
			currency:      "usd",
			expectedTax:   "0.00", // 100 * 0.00001 = 0.001 -> 0.00
			description:   "Tiny tax rate rounds to zero",
		},
		{
			name:          "Exactly_Half_Cent",
			taxableAmount: "10.00",
			taxRate:       "12.5",
			currency:      "usd",
			expectedTax:   "1.25", // 10 * 0.125 = 1.25 (exact)
			description:   "Exact half-cent result",
		},
		{
			name:          "Just_Over_Half_Cent",
			taxableAmount: "10.00",
			taxRate:       "12.51",
			currency:      "usd",
			expectedTax:   "1.25", // 10 * 0.1251 = 1.251 -> 1.25
			description:   "Just over half cent rounds down",
		},
		{
			name:          "Just_Under_Half_Cent",
			taxableAmount: "10.00",
			taxRate:       "12.49",
			currency:      "usd",
			expectedTax:   "1.25", // 10 * 0.1249 = 1.249 -> 1.25
			description:   "Just under half cent rounds down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taxableAmount := decimal.RequireFromString(tt.taxableAmount)
			taxRate := decimal.RequireFromString(tt.taxRate)
			expectedTax := decimal.RequireFromString(tt.expectedTax)

			tax := taxableAmount.Mul(taxRate).Div(decimal.NewFromInt(100))
			roundedTax := types.RoundToCurrencyPrecision(tax, tt.currency)

			assert.True(t, roundedTax.Equal(expectedTax),
				"%s: expected %s, got %s",
				tt.description, expectedTax.String(), roundedTax.String())
		})
	}
}
