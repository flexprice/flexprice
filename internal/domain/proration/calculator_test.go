package proration

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
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
				OriginalAmountPaid: decimal.NewFromInt(10),
				Currency:           "USD",
			},
			expected: &ProrationResult{
				NetAmount:     decimal.NewFromFloat(5.49), // (20 * 17/31) - (10 * 17/31) = 10.97 - 5.48 = 5.49
				Action:        types.ProrationActionUpgrade,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
				CreditItems: []ProrationLineItem{
					{
						Amount:    decimal.NewFromFloat(-5.48), // -(10 * 17/31)
						StartDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
						EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
						Quantity:  decimal.NewFromInt(1),
						PriceID:   "price_old",
						IsCredit:  true,
					},
				},
				ChargeItems: []ProrationLineItem{
					{
						Amount:    decimal.NewFromFloat(10.97), // (20 * 17/31)
						StartDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
						EndDate:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
						Quantity:  decimal.NewFromInt(1),
						PriceID:   "price_new",
						IsCredit:  false,
					},
				},
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
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
				Currency:           "USD",
			},
			expected: &ProrationResult{
				NetAmount:     decimal.NewFromFloat(-38.39), // (30 * 17/31) - (100 * 17/31) = 16.45 - 54.84 = -38.39
				Action:        types.ProrationActionDowngrade,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
				CreditItems: []ProrationLineItem{
					{
						Description: "Credit for unused time on previous plan before downgrade",
						Amount:      decimal.NewFromFloat(-54.84), // -(100 * 17/31)
						StartDate:   time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
						EndDate:     time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
						Quantity:    decimal.NewFromInt(2),
						PriceID:     "price_old",
						IsCredit:    true,
					},
				},
				ChargeItems: []ProrationLineItem{
					{
						Description: "Prorated charge for downgrade",
						Amount:      decimal.NewFromFloat(16.45), // (30 * 17/31)
						StartDate:   time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
						EndDate:     time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
						Quantity:    decimal.NewFromInt(1),
						PriceID:     "price_new",
						IsCredit:    false,
					},
				},
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
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
				Currency:           "USD",
			},
			expected: &ProrationResult{
				NetAmount:     decimal.NewFromFloat(27.42), // (10-5) * 10 * 17/31 = 5 * 10 * 0.5484 = 27.42
				Action:        types.ProrationActionQuantityChange,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
				CreditItems: []ProrationLineItem{
					{
						Description: "Credit for unused time on previous quantity",
						Amount:      decimal.NewFromFloat(-27.42), // -(5 * 10 * 17/31)
						StartDate:   time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
						EndDate:     time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
						Quantity:    decimal.NewFromInt(5),
						PriceID:     "price_same",
						IsCredit:    true,
					},
				},
				ChargeItems: []ProrationLineItem{
					{
						Description: "Prorated charge for quantity change",
						Amount:      decimal.NewFromFloat(54.84), // (10 * 10 * 17/31)
						StartDate:   time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
						EndDate:     time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
						Quantity:    decimal.NewFromInt(10),
						PriceID:     "price_same",
						IsCredit:    false,
					},
				},
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
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
				Currency:           "USD",
			},
			expected: &ProrationResult{
				NetAmount:     decimal.NewFromFloat(13.71), // 25 * 17/31 = 13.71
				Action:        types.ProrationActionAddItem,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
				CreditItems:   []ProrationLineItem{}, // No credits for add_item
				ChargeItems: []ProrationLineItem{
					{
						Description: "Prorated charge for new item",
						Amount:      decimal.NewFromFloat(13.71),
						StartDate:   time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
						EndDate:     time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
						Quantity:    decimal.NewFromInt(1),
						PriceID:     "price_new",
						IsCredit:    false,
					},
				},
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
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
				Currency:           "USD",
			},
			expected: &ProrationResult{
				NetAmount:     decimal.NewFromFloat(-21.94), // -40 * 17/31 = -21.94
				Action:        types.ProrationActionRemoveItem,
				ProrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
				Currency:      "USD",
				IsPreview:     false,
				CreditItems: []ProrationLineItem{
					{
						Description: "Credit for unused time on removed item",
						Amount:      decimal.NewFromFloat(-21.94),
						StartDate:   time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
						EndDate:     time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
						Quantity:    decimal.NewFromInt(1),
						PriceID:     "price_old",
						IsCredit:    true,
					},
				},
				ChargeItems:        []ProrationLineItem{}, // No charges for remove_item
				CurrentPeriodStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
				CurrentPeriodEnd:   time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC),
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
				Currency:           "USD",
			},
			expectedError: "customer timezone is required",
		},
	}

	logger, err := logger.NewLogger(config.GetDefaultConfig())
	require.NoError(t, err)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewCalculator(logger).Calculate(context.Background(), tt.params)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, result)

			if tt.expected != nil {
				// Round all decimal values to currency precision (2 for USD)
				precision := types.GetCurrencyPrecision(tt.params.Currency)
				expectedNetAmount := tt.expected.NetAmount.Round(precision)
				actualNetAmount := result.NetAmount.Round(precision)

				assert.Equal(t, expectedNetAmount.String(), actualNetAmount.String())
				assert.Equal(t, tt.expected.Action, result.Action)
				assert.Equal(t, tt.expected.ProrationDate, result.ProrationDate)
				assert.Equal(t, tt.expected.Currency, result.Currency)
				assert.Equal(t, tt.expected.IsPreview, result.IsPreview)
				assert.Equal(t, tt.expected.CurrentPeriodStart, result.CurrentPeriodStart)
				assert.Equal(t, tt.expected.CurrentPeriodEnd, result.CurrentPeriodEnd)

				// Check credit items
				assert.Equal(t, len(tt.expected.CreditItems), len(result.CreditItems))
				for i, expectedCredit := range tt.expected.CreditItems {
					expectedAmount := expectedCredit.Amount.Round(precision)
					actualAmount := result.CreditItems[i].Amount.Round(precision)
					assert.Equal(t, expectedAmount.String(), actualAmount.String(), "Credit amount mismatch")
					assert.Equal(t, expectedCredit.StartDate, result.CreditItems[i].StartDate, "Credit start date mismatch")
					assert.Equal(t, expectedCredit.EndDate, result.CreditItems[i].EndDate, "Credit end date mismatch")
					assert.Equal(t, expectedCredit.Quantity.String(), result.CreditItems[i].Quantity.String(), "Credit quantity mismatch")
					assert.Equal(t, expectedCredit.PriceID, result.CreditItems[i].PriceID, "Credit price ID mismatch")
					assert.Equal(t, expectedCredit.IsCredit, result.CreditItems[i].IsCredit, "Credit type mismatch")
				}

				// Check charge items
				assert.Equal(t, len(tt.expected.ChargeItems), len(result.ChargeItems))
				for i, expectedCharge := range tt.expected.ChargeItems {
					expectedAmount := expectedCharge.Amount.Round(precision)
					actualAmount := result.ChargeItems[i].Amount.Round(precision)
					assert.Equal(t, expectedAmount.String(), actualAmount.String(), "Charge amount mismatch")
					assert.Equal(t, expectedCharge.StartDate, result.ChargeItems[i].StartDate, "Charge start date mismatch")
					assert.Equal(t, expectedCharge.EndDate, result.ChargeItems[i].EndDate, "Charge end date mismatch")
					assert.Equal(t, expectedCharge.Quantity.String(), result.ChargeItems[i].Quantity.String(), "Charge quantity mismatch")
					assert.Equal(t, expectedCharge.PriceID, result.ChargeItems[i].PriceID, "Charge price ID mismatch")
					assert.Equal(t, expectedCharge.IsCredit, result.ChargeItems[i].IsCredit, "Charge type mismatch")
				}
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

	logger, err := logger.NewLogger(config.GetDefaultConfig())
	require.NoError(t, err)
	calculator := NewCalculator(logger)
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
