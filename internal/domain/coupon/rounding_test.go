package coupon

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// TestDiscountRounding_PercentageDiscounts tests that percentage discounts
// are rounded at source before being aggregated
func TestDiscountRounding_PercentageDiscounts(t *testing.T) {
	tests := []struct {
		name             string
		originalPrice    string
		percentageOff    string
		currency         string
		expectedDiscount string
		expectedFinal    string
		description      string
	}{
		{
			name:             "Simple_15Percent_USD",
			originalPrice:    "100.00",
			percentageOff:    "15",
			currency:         "usd",
			expectedDiscount: "15.00",
			expectedFinal:    "85.00",
			description:      "15% off $100.00 = $15.00 discount",
		},
		{
			name:             "Fractional_15.5Percent_USD",
			originalPrice:    "10.00",
			percentageOff:    "15.5",
			currency:         "usd",
			expectedDiscount: "1.55", // 10.00 * 0.155 = 1.55
			expectedFinal:    "8.45",
			description:      "15.5% off $10.00 = $1.55 discount (exact)",
		},
		{
			name:             "Rounding_33.333Percent_USD",
			originalPrice:    "10.00",
			percentageOff:    "33.333",
			currency:         "usd",
			expectedDiscount: "3.33", // 10.00 * 0.33333 = 3.3333 -> 3.33
			expectedFinal:    "6.67",
			description:      "33.333% off $10.00 rounds to $3.33 discount",
		},
		{
			name:             "Small_Percentage_USD",
			originalPrice:    "100.00",
			percentageOff:    "0.5",
			currency:         "usd",
			expectedDiscount: "0.50",
			expectedFinal:    "99.50",
			description:      "0.5% off $100.00 = $0.50 discount",
		},
		{
			name:             "JPY_Percentage",
			originalPrice:    "1000",
			percentageOff:    "15.5",
			currency:         "jpy",
			expectedDiscount: "155", // 1000 * 0.155 = 155
			expectedFinal:    "845",
			description:      "15.5% off ¥1000 = ¥155 discount (no decimals)",
		},
		{
			name:             "JPY_WithRounding",
			originalPrice:    "1000",
			percentageOff:    "33.333",
			currency:         "jpy",
			expectedDiscount: "333", // 1000 * 0.33333 = 333.33 -> 333
			expectedFinal:    "667",
			description:      "33.333% off ¥1000 rounds to ¥333",
		},
		{
			name:             "SubCent_Result",
			originalPrice:    "1.00",
			percentageOff:    "0.1",
			currency:         "usd",
			expectedDiscount: "0.00", // 1.00 * 0.001 = 0.001 -> 0.00
			expectedFinal:    "1.00",
			description:      "Sub-cent discount rounds to zero",
		},
		{
			name:             "High_Percentage_99",
			originalPrice:    "100.00",
			percentageOff:    "99",
			currency:         "usd",
			expectedDiscount: "99.00",
			expectedFinal:    "1.00",
			description:      "99% off leaves small remainder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalPrice := decimal.RequireFromString(tt.originalPrice)
			percentageOff := decimal.RequireFromString(tt.percentageOff)
			expectedDiscount := decimal.RequireFromString(tt.expectedDiscount)
			expectedFinal := decimal.RequireFromString(tt.expectedFinal)

			// Create coupon with percentage discount
			coupon := &Coupon{
				Type:          types.CouponTypePercentage,
				PercentageOff: &percentageOff,
			}

			// Apply discount with currency (our new implementation)
			result := coupon.ApplyDiscount(originalPrice, tt.currency)

			assert.True(t, result.Discount.Equal(expectedDiscount),
				"%s: expected discount %s, got %s",
				tt.description, expectedDiscount.String(), result.Discount.String())

			assert.True(t, result.FinalPrice.Equal(expectedFinal),
				"%s: expected final price %s, got %s",
				tt.description, expectedFinal.String(), result.FinalPrice.String())

			// Verify the discount is properly rounded for the currency
			precision := types.GetCurrencyPrecision(tt.currency)
			assert.Equal(t, result.Discount.Round(precision), result.Discount,
				"Discount should already be rounded to currency precision")
		})
	}
}

// TestDiscountRounding_FixedDiscounts tests that fixed amount discounts
// are handled correctly (they should already be in correct currency precision)
func TestDiscountRounding_FixedDiscounts(t *testing.T) {
	tests := []struct {
		name             string
		originalPrice    string
		amountOff        string
		currency         string
		expectedDiscount string
		expectedFinal    string
		description      string
	}{
		{
			name:             "Simple_Fixed_USD",
			originalPrice:    "100.00",
			amountOff:        "10.00",
			currency:         "usd",
			expectedDiscount: "10.00",
			expectedFinal:    "90.00",
			description:      "$10 off $100 = $90 final",
		},
		{
			name:             "Fixed_Exceeds_Price",
			originalPrice:    "10.00",
			amountOff:        "15.00",
			currency:         "usd",
			expectedDiscount: "10.00", // Clamped to original price
			expectedFinal:    "0.00",
			description:      "$15 discount on $10 item clamps to $10",
		},
		{
			name:             "Fixed_JPY",
			originalPrice:    "1000",
			amountOff:        "150",
			currency:         "jpy",
			expectedDiscount: "150",
			expectedFinal:    "850",
			description:      "¥150 off ¥1000 = ¥850",
		},
		{
			name:             "SubCent_Fixed",
			originalPrice:    "1.00",
			amountOff:        "0.99",
			currency:         "usd",
			expectedDiscount: "0.99",
			expectedFinal:    "0.01",
			description:      "$0.99 discount leaves $0.01",
		},
		{
			name:             "Exact_Match",
			originalPrice:    "50.00",
			amountOff:        "50.00",
			currency:         "usd",
			expectedDiscount: "50.00",
			expectedFinal:    "0.00",
			description:      "Discount exactly equals price",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalPrice := decimal.RequireFromString(tt.originalPrice)
			amountOff := decimal.RequireFromString(tt.amountOff)
			expectedDiscount := decimal.RequireFromString(tt.expectedDiscount)
			expectedFinal := decimal.RequireFromString(tt.expectedFinal)

			// Create coupon with fixed discount
			coupon := &Coupon{
				Type:      types.CouponTypeFixed,
				AmountOff: &amountOff,
			}

			// Apply discount with currency
			result := coupon.ApplyDiscount(originalPrice, tt.currency)

			assert.True(t, result.Discount.Equal(expectedDiscount),
				"%s: expected discount %s, got %s",
				tt.description, expectedDiscount.String(), result.Discount.String())

			assert.True(t, result.FinalPrice.Equal(expectedFinal),
				"%s: expected final price %s, got %s",
				tt.description, expectedFinal.String(), result.FinalPrice.String())

			// Verify final price is not negative
			assert.True(t, result.FinalPrice.GreaterThanOrEqual(decimal.Zero),
				"Final price should never be negative")
		})
	}
}

// TestDiscountRounding_MultiCurrency tests discounts across different currencies
func TestDiscountRounding_MultiCurrency(t *testing.T) {
	tests := []struct {
		name             string
		originalPrice    string
		percentageOff    string
		currency         string
		expectedDiscount string
		description      string
	}{
		{
			name:             "USD_2_Decimals",
			originalPrice:    "99.999",
			percentageOff:    "10",
			currency:         "usd",
			expectedDiscount: "10.00", // 99.999 * 0.10 = 9.9999 -> 10.00
			description:      "USD rounds to 2 decimals",
		},
		{
			name:             "EUR_2_Decimals",
			originalPrice:    "99.999",
			percentageOff:    "10",
			currency:         "eur",
			expectedDiscount: "10.00",
			description:      "EUR rounds to 2 decimals",
		},
		{
			name:             "GBP_2_Decimals",
			originalPrice:    "99.999",
			percentageOff:    "10",
			currency:         "gbp",
			expectedDiscount: "10.00",
			description:      "GBP rounds to 2 decimals",
		},
		{
			name:             "JPY_0_Decimals",
			originalPrice:    "999.9",
			percentageOff:    "10",
			currency:         "jpy",
			expectedDiscount: "100", // 999.9 * 0.10 = 99.99 -> 100
			description:      "JPY rounds to 0 decimals",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalPrice := decimal.RequireFromString(tt.originalPrice)
			percentageOff := decimal.RequireFromString(tt.percentageOff)
			expectedDiscount := decimal.RequireFromString(tt.expectedDiscount)

			coupon := &Coupon{
				Type:          types.CouponTypePercentage,
				PercentageOff: &percentageOff,
			}

			result := coupon.ApplyDiscount(originalPrice, tt.currency)

			assert.True(t, result.Discount.Equal(expectedDiscount),
				"%s: expected discount %s, got %s",
				tt.description, expectedDiscount.String(), result.Discount.String())

			// Verify currency precision
			precision := types.GetCurrencyPrecision(tt.currency)
			roundedDiscount := result.Discount.Round(precision)
			assert.True(t, roundedDiscount.Equal(result.Discount),
				"Discount should be rounded to currency precision")
		})
	}
}

// TestDiscountRounding_EdgeCases tests edge cases for discount rounding
func TestDiscountRounding_EdgeCases(t *testing.T) {
	t.Run("Zero_Price", func(t *testing.T) {
		price := decimal.Zero
		percentage := decimal.NewFromFloat(10)

		coupon := &Coupon{
			Type:          types.CouponTypePercentage,
			PercentageOff: &percentage,
		}

		// Note: In real usage, this would fail validation in the service layer
		// But the domain model should handle it gracefully
		result := coupon.ApplyDiscount(price, "usd")

		assert.True(t, result.Discount.Equal(decimal.Zero))
		assert.True(t, result.FinalPrice.Equal(decimal.Zero))
	})

	t.Run("100_Percent_Discount", func(t *testing.T) {
		price := decimal.NewFromFloat(100.00)
		percentage := decimal.NewFromFloat(100)

		coupon := &Coupon{
			Type:          types.CouponTypePercentage,
			PercentageOff: &percentage,
		}

		result := coupon.ApplyDiscount(price, "usd")

		assert.True(t, result.Discount.Equal(price))
		assert.True(t, result.FinalPrice.Equal(decimal.Zero))
	})

	t.Run("Over_100_Percent_Discount", func(t *testing.T) {
		price := decimal.NewFromFloat(100.00)
		percentage := decimal.NewFromFloat(150)

		coupon := &Coupon{
			Type:          types.CouponTypePercentage,
			PercentageOff: &percentage,
		}

		result := coupon.ApplyDiscount(price, "usd")

		// Should clamp to price
		assert.True(t, result.Discount.Equal(price))
		assert.True(t, result.FinalPrice.Equal(decimal.Zero))
	})

	t.Run("Very_Small_Percentage", func(t *testing.T) {
		price := decimal.NewFromFloat(1.00)
		percentage := decimal.NewFromFloat(0.01) // 0.01%

		coupon := &Coupon{
			Type:          types.CouponTypePercentage,
			PercentageOff: &percentage,
		}

		result := coupon.ApplyDiscount(price, "usd")

		// 1.00 * 0.0001 = 0.0001 -> rounds to 0.00
		assert.True(t, result.Discount.Equal(decimal.Zero))
		assert.True(t, result.FinalPrice.Equal(price))
	})

	t.Run("Repeating_Decimal_Percentage", func(t *testing.T) {
		// 100 / 3 = 33.333...% off
		price := decimal.NewFromFloat(100.00)
		percentage := decimal.NewFromFloat(33.333333333)

		coupon := &Coupon{
			Type:          types.CouponTypePercentage,
			PercentageOff: &percentage,
		}

		result := coupon.ApplyDiscount(price, "usd")

		// 100 * 0.33333... = 33.333... -> rounds to 33.33
		expectedDiscount := decimal.NewFromFloat(33.33)
		assert.True(t, result.Discount.Equal(expectedDiscount))

		expectedFinal := decimal.NewFromFloat(66.67)
		assert.True(t, result.FinalPrice.Equal(expectedFinal))
	})
}
