package proration

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalculator_Calculate(t *testing.T) {
	tests := []struct {
		name          string
		params        ProrationParams
		expected      *ProrationResult
		expectedError string
	}{
		{
			name: "basic_upgrade_immediate",
			params: ProrationParams{
				Action:             types.ProrationActionUpgrade,
				OldPriceID:         "price_old",
				NewPriceID:         "price_new",
				OldQuantity:        decimal.NewFromInt(1),
				NewQuantity:        decimal.NewFromInt(1),
				OldPricePerUnit:    decimal.NewFromInt(10),
				NewPricePerUnit:    decimal.NewFromInt(20),
				ProrationDate:      time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
				CustomerTimezone:   "UTC",
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				ProrationStrategy:  types.StrategyDayBased,
				PlanPayInAdvance:   true,
			},
			expected: &ProrationResult{
				NetAmount:     decimal.NewFromFloat(5.49), // (20 * 17/31) - (10 * 17/31) = 10.97 - 5.48 = 5.49
				Action:        types.ProrationActionUpgrade,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
			},
		},
		{
			name: "downgrade_with_credit_cap",
			params: ProrationParams{
				Action:             types.ProrationActionDowngrade,
				OldPriceID:         "price_old",
				NewPriceID:         "price_new",
				OldQuantity:        decimal.NewFromInt(2),
				NewQuantity:        decimal.NewFromInt(1),
				OldPricePerUnit:    decimal.NewFromInt(50),
				NewPricePerUnit:    decimal.NewFromInt(30),
				ProrationDate:      time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
				CustomerTimezone:   "UTC",
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				ProrationStrategy:  types.StrategyDayBased,
				PlanPayInAdvance:   true,
				OriginalAmountPaid: decimal.NewFromInt(100),
				TerminationReason:  types.TerminationReasonDowngrade,
			},
			expected: &ProrationResult{
				NetAmount:     decimal.NewFromFloat(-38.71), // (30 * 17/31) - (100 * 17/31) = 16.45 - 54.84 = -38.71
				Action:        types.ProrationActionDowngrade,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
			},
		},
		{
			name: "quantity_change_increase",
			params: ProrationParams{
				Action:             types.ProrationActionQuantityChange,
				OldPriceID:         "price_same",
				NewPriceID:         "price_same",
				OldQuantity:        decimal.NewFromInt(5),
				NewQuantity:        decimal.NewFromInt(10),
				OldPricePerUnit:    decimal.NewFromInt(10),
				NewPricePerUnit:    decimal.NewFromInt(10),
				ProrationDate:      time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
				CustomerTimezone:   "UTC",
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				ProrationStrategy:  types.StrategyDayBased,
				PlanPayInAdvance:   true,
			},
			expected: &ProrationResult{
				NetAmount:     decimal.NewFromFloat(27.42), // (10-5) * 10 * 17/31 = 5 * 10 * 0.5484 = 27.42
				Action:        types.ProrationActionQuantityChange,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
			},
		},
		{
			name: "add_item_no_credit",
			params: ProrationParams{
				Action:             types.ProrationActionAddItem,
				NewPriceID:         "price_new",
				NewQuantity:        decimal.NewFromInt(1),
				NewPricePerUnit:    decimal.NewFromInt(25),
				ProrationDate:      time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
				CustomerTimezone:   "UTC",
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				ProrationStrategy:  types.StrategyDayBased,
				PlanPayInAdvance:   true,
			},
			expected: &ProrationResult{
				NetAmount:     decimal.NewFromFloat(13.71), // 25 * 17/31 = 13.71
				Action:        types.ProrationActionAddItem,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
			},
		},
		{
			name: "remove_item_with_credit",
			params: ProrationParams{
				Action:             types.ProrationActionRemoveItem,
				OldPriceID:         "price_old",
				OldQuantity:        decimal.NewFromInt(1),
				OldPricePerUnit:    decimal.NewFromInt(40),
				ProrationDate:      time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
				CustomerTimezone:   "UTC",
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				ProrationStrategy:  types.StrategyDayBased,
				PlanPayInAdvance:   true,
				OriginalAmountPaid: decimal.NewFromInt(40),
				TerminationReason:  types.TerminationReasonCancellation,
			},
			expected: &ProrationResult{
				NetAmount:     decimal.NewFromFloat(-21.94), // -40 * 17/31 = -21.94
				Action:        types.ProrationActionRemoveItem,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
			},
		},
		{
			name: "timezone_aware_calculation",
			params: ProrationParams{
				Action:             types.ProrationActionUpgrade,
				OldPriceID:         "price_old",
				NewPriceID:         "price_new",
				OldQuantity:        decimal.NewFromInt(1),
				NewQuantity:        decimal.NewFromInt(1),
				OldPricePerUnit:    decimal.NewFromInt(10),
				NewPricePerUnit:    decimal.NewFromInt(20),
				ProrationDate:      time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC),
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
				CustomerTimezone:   "America/New_York",
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				ProrationStrategy:  types.StrategyDayBased,
				PlanPayInAdvance:   true,
			},
			expected: &ProrationResult{
				NetAmount:     decimal.NewFromFloat(5.49), // Same calculation as basic_upgrade_immediate
				Action:        types.ProrationActionUpgrade,
				ProrationDate: time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
			},
		},
		{
			name: "validation_error_missing_timezone",
			params: ProrationParams{
				Action:             types.ProrationActionUpgrade,
				OldPriceID:         "price_old",
				NewPriceID:         "price_new",
				OldQuantity:        decimal.NewFromInt(1),
				NewQuantity:        decimal.NewFromInt(1),
				OldPricePerUnit:    decimal.NewFromInt(10),
				NewPricePerUnit:    decimal.NewFromInt(20),
				ProrationDate:      time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
				ProrationBehavior:  types.ProrationBehaviorCreateProrations,
				ProrationStrategy:  types.StrategyDayBased,
				PlanPayInAdvance:   true,
			},
			expectedError: "customer timezone is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewCalculator().Calculate(context.Background(), tt.params)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, result)

			if tt.expected != nil {
				assert.Equal(t, tt.expected.NetAmount.String(), result.NetAmount.String())
				assert.Equal(t, tt.expected.Action, result.Action)
				assert.Equal(t, tt.expected.ProrationDate, result.ProrationDate)
				assert.Equal(t, tt.expected.Currency, result.Currency)
				assert.Equal(t, tt.expected.IsPreview, result.IsPreview)
			}
		})
	}
}

func TestCalculator_DaysInDurationWithDST(t *testing.T) {
	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
		timezone string
		expected int
	}{
		{
			name:     "regular_days",
			start:    time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 5, 5, 0, 0, 0, 0, time.UTC),
			timezone: "UTC",
			expected: 4,
		},
		{
			name:     "dst_spring_forward",
			start:    time.Date(2024, 3, 9, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 3, 11, 0, 0, 0, 0, time.UTC),
			timezone: "America/New_York",
			expected: 2, // Should count as 2 days despite 23-hour day
		},
		{
			name:     "dst_fall_back",
			start:    time.Date(2024, 11, 2, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 11, 4, 0, 0, 0, 0, time.UTC),
			timezone: "America/New_York",
			expected: 2, // Should count as 2 days despite 25-hour day
		},
		{
			name:     "one_day",
			start:    time.Date(2024, 7, 15, 12, 30, 0, 0, time.UTC),
			end:      time.Date(2024, 7, 16, 8, 15, 0, 0, time.UTC),
			timezone: "Asia/Tokyo",
			expected: 1, // Even with time differences, should count as 1 day
		},
		{
			name:     "crossing_months",
			start:    time.Date(2024, 7, 30, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 8, 2, 0, 0, 0, 0, time.UTC),
			timezone: "Europe/London",
			expected: 3,
		},
		{
			name:     "crossing_years",
			start:    time.Date(2024, 12, 30, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
			timezone: "Europe/Paris",
			expected: 3,
		},
		{
			name:     "same_day",
			start:    time.Date(2024, 5, 1, 8, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 5, 1, 17, 0, 0, 0, time.UTC),
			timezone: "Europe/Berlin",
			expected: 0, // Same day should return 0 days
		},
		{
			name:     "southern_hemisphere_dst",
			start:    time.Date(2024, 4, 6, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 4, 8, 0, 0, 0, 0, time.UTC),
			timezone: "Australia/Sydney",
			expected: 2, // Southern hemisphere DST ends in April
		},
		{
			name:     "different_hour_midnight",
			start:    time.Date(2024, 8, 1, 23, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 8, 3, 1, 0, 0, 0, time.UTC),
			timezone: "America/Los_Angeles",
			expected: 2, // Test boundary conditions with time of day
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc, err := time.LoadLocation(tt.timezone)
			print(loc)
			require.NoError(t, err)

			days := daysInDurationWithDST(tt.start, tt.end, loc)
			assert.Equal(t, tt.expected, days)
		})
	}
}

func TestCalculator_CapCreditAmount(t *testing.T) {
	tests := []struct {
		name            string
		potentialCredit decimal.Decimal
		originalAmount  decimal.Decimal
		previousCredits decimal.Decimal
		expectedCredit  decimal.Decimal
	}{
		{
			name:            "normal_credit",
			potentialCredit: decimal.NewFromInt(100),
			originalAmount:  decimal.NewFromInt(200),
			previousCredits: decimal.NewFromInt(50),
			expectedCredit:  decimal.NewFromInt(100), // 100 capped at (200-50)
		},
		{
			name:            "credit_exceeds_original",
			potentialCredit: decimal.NewFromInt(300),
			originalAmount:  decimal.NewFromInt(200),
			previousCredits: decimal.NewFromInt(50),
			expectedCredit:  decimal.NewFromInt(150), // Capped at (200-50)
		},
		{
			name:            "previous_credits_limit",
			potentialCredit: decimal.NewFromInt(100),
			originalAmount:  decimal.NewFromInt(200),
			previousCredits: decimal.NewFromInt(180),
			expectedCredit:  decimal.NewFromInt(20), // Only 20 left to credit
		},
		{
			name:            "no_credit_available",
			potentialCredit: decimal.NewFromInt(100),
			originalAmount:  decimal.NewFromInt(200),
			previousCredits: decimal.NewFromInt(200),
			expectedCredit:  decimal.Zero, // No credit available
		},
		{
			name:            "negative_potential_credit",
			potentialCredit: decimal.NewFromInt(-50),
			originalAmount:  decimal.NewFromInt(200),
			previousCredits: decimal.NewFromInt(50),
			expectedCredit:  decimal.Zero, // No negative credits
		},
		{
			name:            "previous_credits_exceed_original",
			potentialCredit: decimal.NewFromInt(100),
			originalAmount:  decimal.NewFromInt(200),
			previousCredits: decimal.NewFromInt(250),
			expectedCredit:  decimal.Zero, // No credit available
		},
		{
			name:            "zero_original_amount",
			potentialCredit: decimal.NewFromInt(100),
			originalAmount:  decimal.Zero,
			previousCredits: decimal.Zero,
			expectedCredit:  decimal.Zero, // No credit available when nothing was paid
		},
		{
			name:            "zero_potential_credit",
			potentialCredit: decimal.Zero,
			originalAmount:  decimal.NewFromInt(200),
			previousCredits: decimal.NewFromInt(50),
			expectedCredit:  decimal.Zero, // Zero in, zero out
		},
	}

	calculator := NewCalculator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculator.(*calculatorImpl).capCreditAmount(
				tt.potentialCredit,
				tt.originalAmount,
				tt.previousCredits,
			)
			assert.True(t, tt.expectedCredit.Equal(result),
				"Expected credit %s but got %s", tt.expectedCredit, result)
		})
	}
}

// TestCalculator_Descriptions tests the description generation for credits and charges
func TestCalculator_Descriptions(t *testing.T) {
	tests := []struct {
		name           string
		params         ProrationParams
		expectedCredit string
		expectedCharge string
	}{
		{
			name: "upgrade_descriptions",
			params: ProrationParams{
				Action: types.ProrationActionUpgrade,
			},
			expectedCredit: "Credit for unused time on previous plan",
			expectedCharge: "Charge for remaining time on new plan",
		},
		{
			name: "downgrade_descriptions",
			params: ProrationParams{
				Action: types.ProrationActionDowngrade,
			},
			expectedCredit: "Credit for unused time on previous plan",
			expectedCharge: "Charge for remaining time on new plan",
		},
		{
			name: "quantity_change_descriptions",
			params: ProrationParams{
				Action: types.ProrationActionQuantityChange,
			},
			expectedCredit: "Credit for unused time on previous quantity",
			expectedCharge: "Charge for remaining time with new quantity",
		},
		{
			name: "remove_item_descriptions",
			params: ProrationParams{
				Action: types.ProrationActionRemoveItem,
			},
			expectedCredit: "Credit for unused time on removed item",
			expectedCharge: "", // No charge description for removal
		},
		{
			name: "add_item_descriptions",
			params: ProrationParams{
				Action: types.ProrationActionAddItem,
			},
			expectedCredit: "", // No credit description for addition
			expectedCharge: "Charge for new item",
		},
	}

	calculator := NewCalculator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creditDesc := calculator.(*calculatorImpl).generateCreditDescription(tt.params)
			chargeDesc := calculator.(*calculatorImpl).generateChargeDescription(tt.params)

			assert.Equal(t, tt.expectedCredit, creditDesc)
			assert.Equal(t, tt.expectedCharge, chargeDesc)
		})
	}
}
