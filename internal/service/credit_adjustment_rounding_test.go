package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// TestCreditRounding_MultipleWallets tests that credits from multiple wallets
// are each rounded individually before being summed
func TestCreditRounding_MultipleWallets(t *testing.T) {
	t.Run("Two_Wallets_USD", func(t *testing.T) {
		// Scenario: $100 line item, two wallets with $40.333 and $30.666
		// Wallet 1: $40.333 -> $40.33
		// Wallet 2: $30.666 -> $30.67
		// Total credits: $71.00

		credit1 := decimal.RequireFromString("40.333")
		rounded1 := types.RoundToCurrencyPrecision(credit1, "usd")
		expected1 := decimal.RequireFromString("40.33")
		assert.True(t, rounded1.Equal(expected1))

		credit2 := decimal.RequireFromString("30.666")
		rounded2 := types.RoundToCurrencyPrecision(credit2, "usd")
		expected2 := decimal.RequireFromString("30.67")
		assert.True(t, rounded2.Equal(expected2))

		totalCredits := rounded1.Add(rounded2)
		expectedTotal := decimal.RequireFromString("71.00")
		assert.True(t, totalCredits.Equal(expectedTotal),
			"Total credits should be $71.00, got %s", totalCredits.String())
	})

	t.Run("Three_Wallets_JPY", func(t *testing.T) {
		// Scenario: ¥1000 line item, three wallets
		// Wallet 1: ¥333.3 -> ¥333
		// Wallet 2: ¥333.3 -> ¥333
		// Wallet 3: ¥333.4 -> ¥333
		// Total: ¥999

		walletCredits := []string{"333.3", "333.3", "333.4"}
		totalCredits := decimal.Zero

		for _, creditStr := range walletCredits {
			credit := decimal.RequireFromString(creditStr)
			rounded := types.RoundToCurrencyPrecision(credit, "jpy")
			totalCredits = totalCredits.Add(rounded)
		}

		expectedTotal := decimal.NewFromInt(999)
		assert.True(t, totalCredits.Equal(expectedTotal),
			"Total JPY credits should be ¥999")
	})
}

// TestCreditRounding_CreditApplication tests credit application scenarios including
// partial credits, capping at line item amounts, and distribution across multiple line items
func TestCreditRounding_CreditApplication(t *testing.T) {
	t.Run("Credit_Less_Than_LineItem", func(t *testing.T) {
		// $100 line item with $25.555 credit available
		// Credit applied: $25.56 (rounded)
		// Remaining on line item: $74.44

		lineItemAmount := decimal.NewFromFloat(100.00)
		creditAvailable := decimal.RequireFromString("25.555")

		roundedCredit := types.RoundToCurrencyPrecision(creditAvailable, "usd")
		expectedCredit := decimal.RequireFromString("25.56")
		assert.True(t, roundedCredit.Equal(expectedCredit))

		remaining := lineItemAmount.Sub(roundedCredit)
		expectedRemaining := decimal.RequireFromString("74.44")
		assert.True(t, remaining.Equal(expectedRemaining))
	})

	t.Run("Credit_Exceeds_LineItem", func(t *testing.T) {
		// $50 line item with $75.50 credit available
		// Only $50.00 can be applied (capped at line item amount)

		lineItemAmount := decimal.NewFromFloat(50.00)
		creditAvailable := decimal.NewFromFloat(75.50)

		appliedCredit := decimal.Min(creditAvailable, lineItemAmount)
		roundedCredit := types.RoundToCurrencyPrecision(appliedCredit, "usd")

		assert.True(t, roundedCredit.Equal(lineItemAmount),
			"Applied credit should be capped at line item amount")
	})

	t.Run("Credits_Across_Multiple_LineItems", func(t *testing.T) {
		// Scenario: 3 line items, $100 total credit
		// Item 1: $50, gets $33.333 credit -> $33.33
		// Item 2: $30, gets $33.333 credit -> $30.00 (capped)
		// Item 3: $20, gets $33.334 credit -> $20.00 (capped)
		// Total applied: $83.33

		lineItems := []struct {
			amount      string
			creditAlloc string // before capping
			expected    string // after capping and rounding
		}{
			{"50.00", "33.333", "33.33"},
			{"30.00", "33.333", "30.00"},
			{"20.00", "33.334", "20.00"},
		}

		totalApplied := decimal.Zero
		for _, item := range lineItems {
			amount := decimal.RequireFromString(item.amount)
			allocCredit := decimal.RequireFromString(item.creditAlloc)
			expected := decimal.RequireFromString(item.expected)

			// Cap at line item amount
			appliedCredit := decimal.Min(allocCredit, amount)
			// Round
			roundedCredit := types.RoundToCurrencyPrecision(appliedCredit, "usd")

			assert.True(t, roundedCredit.Equal(expected),
				"Line item %s should have credit %s", item.amount, expected.String())

			totalApplied = totalApplied.Add(roundedCredit)
		}

		expectedTotal := decimal.RequireFromString("83.33")
		assert.True(t, totalApplied.Equal(expectedTotal),
			"Total credits applied should be $83.33")
	})
}

// TestCreditRounding_AfterDiscounts tests that credits are applied to
// the discounted amount (amount after line-item and invoice-level discounts)
func TestCreditRounding_AfterDiscounts(t *testing.T) {
	t.Run("Credit_On_Discounted_Amount", func(t *testing.T) {
		// Line item: $100
		// Line-item discount: $10
		// Invoice-level discount: $5
		// Adjusted amount: $85
		// Credit: 50% of adjusted = $42.50

		lineItemAmount := decimal.NewFromFloat(100.00)
		lineItemDiscount := decimal.NewFromFloat(10.00)
		invoiceDiscount := decimal.NewFromFloat(5.00)

		adjustedAmount := lineItemAmount.Sub(lineItemDiscount).Sub(invoiceDiscount)
		expectedAdjusted := decimal.NewFromFloat(85.00)
		assert.True(t, adjustedAmount.Equal(expectedAdjusted))

		// Apply 50% credit on adjusted amount
		creditPercent := decimal.NewFromFloat(0.50)
		creditAmount := adjustedAmount.Mul(creditPercent)
		roundedCredit := types.RoundToCurrencyPrecision(creditAmount, "usd")

		expectedCredit := decimal.NewFromFloat(42.50)
		assert.True(t, roundedCredit.Equal(expectedCredit))
	})
}

// TestCreditRounding_NoAccumulationErrors tests that applying many small
// credits doesn't cause accumulation errors
func TestCreditRounding_NoAccumulationErrors(t *testing.T) {
	t.Run("Hundred_Small_Credits", func(t *testing.T) {
		// 100 credits of $0.011 each
		// Each rounds to $0.01
		// Total: $1.00

		totalCredits := decimal.Zero
		creditAmount := decimal.RequireFromString("0.011")

		for i := 0; i < 100; i++ {
			rounded := types.RoundToCurrencyPrecision(creditAmount, "usd")
			totalCredits = totalCredits.Add(rounded)
		}

		expected := decimal.NewFromFloat(1.00)
		assert.True(t, totalCredits.Equal(expected),
			"100 credits of $0.01 should total $1.00, got %s", totalCredits.String())
	})

	t.Run("Many_Fractional_Credits", func(t *testing.T) {
		// 50 credits of $0.333 each
		// Each rounds to $0.33
		// Total: $16.50

		totalCredits := decimal.Zero
		creditAmount := decimal.RequireFromString("0.333")

		for i := 0; i < 50; i++ {
			rounded := types.RoundToCurrencyPrecision(creditAmount, "usd")
			totalCredits = totalCredits.Add(rounded)
		}

		expected := decimal.NewFromFloat(16.50)
		assert.True(t, totalCredits.Equal(expected),
			"50 credits of $0.33 should total $16.50")
	})
}

// TestCreditRounding_EdgeCases tests edge cases and basic rounding scenarios for credit rounding
func TestCreditRounding_EdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		creditAmount   string
		currency       string
		expectedCredit string
		description    string
	}{
		{
			name:           "Zero_Credit",
			creditAmount:   "0.00",
			currency:       "usd",
			expectedCredit: "0.00",
			description:    "Zero credit remains zero",
		},
		{
			name:           "Exact_Credit_USD",
			creditAmount:   "50.00",
			currency:       "usd",
			expectedCredit: "50.00",
			description:    "Exact credit amount",
		},
		{
			name:           "Credit_WithRounding_USD",
			creditAmount:   "33.333",
			currency:       "usd",
			expectedCredit: "33.33",
			description:    "Credit 33.333 rounds to $33.33",
		},
		{
			name:           "Credit_RoundUp",
			creditAmount:   "3.335",
			currency:       "usd",
			expectedCredit: "3.34",
			description:    "Credit rounds up from 3.335",
		},
		{
			name:           "SubCent_Rounds_Down",
			creditAmount:   "0.004",
			currency:       "usd",
			expectedCredit: "0.00",
			description:    "$0.004 rounds to $0.00",
		},
		{
			name:           "SubCent_Rounds_Up",
			creditAmount:   "0.005",
			currency:       "usd",
			expectedCredit: "0.01",
			description:    "$0.005 rounds to $0.01",
		},
		{
			name:           "Exactly_Half_Cent",
			creditAmount:   "10.125",
			currency:       "usd",
			expectedCredit: "10.13",
			description:    "Half-cent rounds up (standard rounding)",
		},
		{
			name:           "Large_Credit",
			creditAmount:   "999999.999",
			currency:       "usd",
			expectedCredit: "1000000.00",
			description:    "Large credit rounds correctly",
		},
		{
			name:           "JPY_Credit",
			creditAmount:   "500.5",
			currency:       "jpy",
			expectedCredit: "501",
			description:    "JPY credit rounds to integer",
		},
		{
			name:           "JPY_Half",
			creditAmount:   "100.5",
			currency:       "jpy",
			expectedCredit: "101",
			description:    "JPY half value rounds up",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creditAmount := decimal.RequireFromString(tt.creditAmount)
			expectedCredit := decimal.RequireFromString(tt.expectedCredit)

			roundedCredit := types.RoundToCurrencyPrecision(creditAmount, tt.currency)

			assert.True(t, roundedCredit.Equal(expectedCredit),
				"%s: expected %s, got %s",
				tt.description, expectedCredit.String(), roundedCredit.String())

			// Verify precision for non-zero values
			if !roundedCredit.IsZero() {
				precision := types.GetCurrencyPrecision(tt.currency)
				assert.Equal(t, roundedCredit.Round(precision), roundedCredit,
					"Credit should be rounded to currency precision")
			}
		})
	}
}

// TestCreditRounding_RealWorldScenario tests a complete realistic credit scenario
func TestCreditRounding_RealWorldScenario(t *testing.T) {
	t.Run("Complex_Multi_Wallet_Scenario", func(t *testing.T) {
		// Scenario:
		// Line item 1 (usage): $150.00
		// Line item 2 (usage): $75.50
		// Total usage: $225.50
		//
		// Wallet 1: $100.333 available
		// Wallet 2: $80.666 available
		// Wallet 3: $50.000 available
		//
		// Application:
		// Item 1 gets: $100.33 (wallet 1) + $49.67 (wallet 2) = $150.00
		// Item 2 gets: $31.00 (wallet 2) + $44.50 (wallet 3) = $75.50
		// Total applied: $225.50

		// Simulate wallet applications with rounding at each step
		type walletApplication struct {
			amount   string
			currency string
		}

		applications := []walletApplication{
			{"100.333", "usd"}, // Wallet 1 to Item 1
			{"49.667", "usd"},  // Wallet 2 to Item 1
			{"31.000", "usd"},  // Wallet 2 to Item 2
			{"44.500", "usd"},  // Wallet 3 to Item 2
		}

		totalApplied := decimal.Zero
		for _, app := range applications {
			amount := decimal.RequireFromString(app.amount)
			rounded := types.RoundToCurrencyPrecision(amount, app.currency)
			totalApplied = totalApplied.Add(rounded)
		}

		// Expected: $100.33 + $49.67 + $31.00 + $44.50 = $225.50
		expected := decimal.RequireFromString("225.50")
		assert.True(t, totalApplied.Equal(expected),
			"Total applied credits should be $225.50, got %s", totalApplied.String())
	})
}

// TestCreditRounding_UsageOnlyPolicy tests that credits only apply to usage items
func TestCreditRounding_UsageOnlyPolicy(t *testing.T) {
	t.Run("Credits_Skip_Fixed_Items", func(t *testing.T) {
		// This test documents the policy that credits only apply to usage line items
		// Fixed items are skipped

		lineItems := []*invoice.InvoiceLineItem{
			{
				Amount:    decimal.NewFromFloat(100.00),
				PriceType: lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
				Currency:  "usd",
			},
			{
				Amount:    decimal.NewFromFloat(50.00),
				PriceType: lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
				Currency:  "usd",
			},
			{
				Amount:    decimal.NewFromFloat(75.00),
				PriceType: lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
				Currency:  "usd",
			},
		}

		// Simulate credit application (only to usage items)
		totalCreditsApplied := decimal.Zero
		availableCredit := decimal.NewFromFloat(100.00)

		for _, item := range lineItems {
			if item.PriceType != nil && *item.PriceType == string(types.PRICE_TYPE_USAGE) {
				// Apply credit to this usage item
				creditToApply := decimal.Min(availableCredit, item.Amount)
				roundedCredit := types.RoundToCurrencyPrecision(creditToApply, item.Currency)

				totalCreditsApplied = totalCreditsApplied.Add(roundedCredit)
				availableCredit = availableCredit.Sub(roundedCredit)
			}
		}

		// Total usage = $125, credit available = $100
		// Should apply $100 total (first usage item gets $50, second gets $50)
		expectedTotal := decimal.NewFromFloat(100.00)
		assert.True(t, totalCreditsApplied.Equal(expectedTotal),
			"Should apply $100 credit to usage items only")
	})
}
