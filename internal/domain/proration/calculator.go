package proration

import (
	"context"
	"fmt"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// NOTE: This file assumes that the `Calculator` interface is defined in another file within this package (e.g., models.go or service.go)
// For example:
// type Calculator interface {
// 	 Calculate(ctx context.Context, params ProrationParams) (*ProrationResult, error)
// }
//
// It also assumes that `ProrationParams` (from models.go) has a field `ProrationStrategy ProrationStrategy`
// and that `StrategySecondBased`, `StrategyDayBased` constants are defined (likely in models.go).

// NewCalculator creates a proration calculator.
// The calculator determines the proration logic (day-based, second-based)
// based on the ProrationStrategy provided in ProrationParams.
func NewCalculator(logger *logger.Logger) Calculator {
	return &calculatorImpl{
		logger: logger,
	}
}

// calculatorImpl implements proration logic, supporting multiple strategies.
type calculatorImpl struct {
	logger *logger.Logger
}

func (c *calculatorImpl) Calculate(ctx context.Context, params ProrationParams) (*ProrationResult, error) {
	if params.ProrationBehavior == types.ProrationBehaviorNone {
		return nil, nil
	}

	if err := validateParams(params); err != nil {
		return nil, ierr.WithError(err).
			WithHintf("invalid proration params: %+v", err).
			Mark(ierr.ErrValidation)
	}

	// Load customer timezone
	loc, err := time.LoadLocation(params.CustomerTimezone)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHintf("failed to load customer timezone '%s': %v", params.CustomerTimezone, err).
			Mark(ierr.ErrSystem)
	}

	// Convert times to customer timezone
	prorationDateInTZ := params.ProrationDate.In(loc)
	periodStartInTZ := params.CurrentPeriodStart.In(loc)
	periodEndInTZ := params.CurrentPeriodEnd.In(loc)

	var prorationCoefficient decimal.Decimal

	switch params.ProrationStrategy {
	case types.StrategySecondBased:
		totalSeconds := periodEndInTZ.Sub(periodStartInTZ).Seconds()
		if totalSeconds <= 0 {
			return nil, ierr.NewError("invalid billing period").
				WithHintf("total seconds is zero or negative (%v to %v)", periodStartInTZ, periodEndInTZ).
				Mark(ierr.ErrValidation)
		}

		remainingSeconds := periodEndInTZ.Sub(prorationDateInTZ).Seconds()
		if remainingSeconds < 0 {
			remainingSeconds = 0
		}
		prorationCoefficient = decimal.NewFromFloat(remainingSeconds).Div(decimal.NewFromFloat(totalSeconds))

	case types.StrategyDayBased:
		totalDaysRaw := daysInDurationWithDST(periodStartInTZ, periodEndInTZ, loc)
		totalDays := totalDaysRaw + 1
		if totalDays <= 0 {
			return nil, ierr.NewError("invalid billing period").
				WithHintf("total days is zero or negative (%v to %v)", params.CurrentPeriodStart, params.CurrentPeriodEnd).
				Mark(ierr.ErrValidation)
		}

		remainingDaysRaw := daysInDurationWithDST(prorationDateInTZ, periodEndInTZ, loc)
		remainingDays := remainingDaysRaw + 1
		if remainingDays < 0 {
			remainingDays = 0
		}

		decimalTotalDays := decimal.NewFromInt(int64(totalDays))
		decimalRemainingDays := decimal.NewFromInt(int64(remainingDays))
		prorationCoefficient = decimal.Zero
		if decimalTotalDays.GreaterThan(decimal.Zero) {
			prorationCoefficient = decimalRemainingDays.Div(decimalTotalDays)
		}
	default:
		return nil, ierr.NewError("invalid proration strategy").
			WithHintf("invalid proration strategy: %s", params.ProrationStrategy).
			Mark(ierr.ErrValidation)
	}

	result := &ProrationResult{
		NetAmount:          decimal.Zero,
		Action:             params.Action,
		ProrationDate:      params.ProrationDate,
		LineItemID:         params.LineItemID,
		IsPreview:          params.ProrationBehavior == types.ProrationBehaviorNone,
		CreditItems:        []ProrationLineItem{},
		ChargeItems:        []ProrationLineItem{},
		Currency:           params.Currency,
		CurrentPeriodStart: params.CurrentPeriodStart,
		CurrentPeriodEnd:   params.CurrentPeriodEnd,
	}

	billingMode := types.BillingModeInArrears
	if params.PlanPayInAdvance {
		billingMode = types.BillingModeInAdvance
	}

	shouldIssueCredit := c.shouldIssueCreditForAction(params, billingMode)

	precision := types.GetCurrencyPrecision(params.Currency)

	if shouldIssueCredit {
		oldItemTotal := params.OldPricePerUnit.Mul(params.OldQuantity)
		potentialCredit := oldItemTotal.Mul(prorationCoefficient)

		// Only cap credit for non-quantity changes
		var creditAmount decimal.Decimal
		if params.Action == types.ProrationActionQuantityChange {
			creditAmount = potentialCredit
		} else {
			creditAmount = c.capCreditAmount(potentialCredit, params.OriginalAmountPaid, params.PreviousCreditsIssued)
		}

		if creditAmount.GreaterThan(decimal.Zero) {
			creditItem := ProrationLineItem{
				Description: c.generateCreditDescription(params),
				Amount:      creditAmount.Neg().Round(precision),
				StartDate:   params.ProrationDate,
				EndDate:     params.CurrentPeriodEnd,
				Quantity:    params.OldQuantity,
				PriceID:     params.OldPriceID,
				IsCredit:    true,
			}
			result.CreditItems = append(result.CreditItems, creditItem)
			result.NetAmount = result.NetAmount.Add(creditItem.Amount)
		}
	}

	shouldIssueCharge := params.Action == types.ProrationActionAddItem ||
		params.Action == types.ProrationActionUpgrade ||
		params.Action == types.ProrationActionDowngrade ||
		params.Action == types.ProrationActionQuantityChange

	if shouldIssueCharge {
		newItemTotal := params.NewPricePerUnit.Mul(params.NewQuantity)
		proratedCharge := newItemTotal.Mul(prorationCoefficient)

		if proratedCharge.GreaterThan(decimal.Zero) {
			chargeItem := ProrationLineItem{
				Description: c.generateChargeDescription(params),
				Amount:      proratedCharge.Round(precision),
				StartDate:   params.ProrationDate,
				EndDate:     params.CurrentPeriodEnd,
				Quantity:    params.NewQuantity,
				PriceID:     params.NewPriceID,
				IsCredit:    false,
			}
			result.ChargeItems = append(result.ChargeItems, chargeItem)
			result.NetAmount = result.NetAmount.Add(chargeItem.Amount)
		}
	}

	// Round the final net amount according to currency precision
	result.NetAmount = result.NetAmount.Round(precision)

	c.logger.Infof("proration net amount: %s", result.NetAmount)

	return result, nil
}

// daysInDurationWithDST counts calendar days between two dates while properly handling
// DST transitions. Unlike using time.Duration, which would incorrectly count 23 or 25 hour
// days during DST shifts as partial days, this ensures each calendar day is counted exactly
// once in the customer's timezone, which is essential for accurate billing calculations.
func daysInDurationWithDST(start, end time.Time, loc *time.Location) int {
	// Normalize times to midnight in customer timezone
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, loc)

	// If same day, return 0
	if startDay.Equal(endDay) {
		return 0
	}

	// Calculate days between (exclusive of end date)
	days := 0
	current := startDay
	for current.Before(endDay) {
		days++
		current = current.AddDate(0, 0, 1)
	}

	return days
}

// capCreditAmount ensures the credit amount doesn't exceed the original amount paid minus previous credits
func (c *calculatorImpl) capCreditAmount(
	potentialCredit decimal.Decimal,
	originalAmountPaid decimal.Decimal,
	previousCreditsIssued decimal.Decimal,
) decimal.Decimal {
	if potentialCredit.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}
	if potentialCredit.GreaterThan(originalAmountPaid) {
		potentialCredit = originalAmountPaid
	}
	availableCredit := originalAmountPaid.Sub(previousCreditsIssued)
	if potentialCredit.GreaterThan(availableCredit) {
		potentialCredit = availableCredit
	}
	if potentialCredit.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}
	return potentialCredit
}

// shouldIssueCreditForAction determines if a credit should be issued based on action and billing mode
func (c *calculatorImpl) shouldIssueCreditForAction(params ProrationParams, billingMode types.BillingMode) bool {
	// For cancellations, we need more sophisticated logic
	if params.Action == types.ProrationActionCancellation {
		return c.shouldIssueCreditForCancellation(params, billingMode)
	}

	// For other actions, use existing logic
	return (params.Action == types.ProrationActionUpgrade ||
		params.Action == types.ProrationActionDowngrade ||
		params.Action == types.ProrationActionQuantityChange ||
		params.Action == types.ProrationActionRemoveItem) &&
		billingMode == types.BillingModeInAdvance
}

// shouldIssueCreditForCancellation determines if credit should be issued for cancellation scenarios
func (c *calculatorImpl) shouldIssueCreditForCancellation(params ProrationParams, billingMode types.BillingMode) bool {
	// Only issue credits if customer is eligible and paid in advance
	if !params.RefundEligible || billingMode != types.BillingModeInAdvance {
		return false
	}

	switch params.CancellationType {
	case types.CancellationTypeImmediate:
		// Immediate cancellation with advance billing gets credit for unused time
		return true
	case types.CancellationTypeEndOfPeriod:
		// End of period cancellation typically doesn't get credit since they use full period
		// However, there might be edge cases where partial credit is warranted
		return false
	default:
		return false
	}
}

func (c *calculatorImpl) generateCreditDescription(params ProrationParams) string {
	switch params.Action {
	case types.ProrationActionCancellation:
		// Enhanced description for cancellation based on type
		switch params.CancellationType {
		case types.CancellationTypeImmediate:
			if params.CancellationReason != "" {
				return fmt.Sprintf("Credit for unused time - immediate cancellation (%s)", params.CancellationReason)
			}
			return "Credit for unused time - immediate cancellation"
		case types.CancellationTypeEndOfPeriod:
			return "Credit adjustment for end-of-period cancellation"
		default:
			return "Credit for unused time on cancelled subscription"
		}
	case types.ProrationActionDowngrade:
		return "Credit for unused time on previous plan before downgrade"
	case types.ProrationActionUpgrade:
		return "Credit for unused time on previous plan before upgrade"
	case types.ProrationActionQuantityChange:
		return "Credit for unused time on previous quantity"
	case types.ProrationActionRemoveItem:
		return "Credit for unused time on removed item"
	default:
		return "Credit for unused time"
	}
}

func (c *calculatorImpl) generateChargeDescription(params ProrationParams) string {
	// For subscription charges, use the plan name
	if params.PlanDisplayName != "" {
		return params.PlanDisplayName
	}

	// Fallback to generic descriptions if plan name is not available
	switch params.Action {
	case types.ProrationActionUpgrade:
		return "Prorated charge for upgrade"
	case types.ProrationActionDowngrade:
		return "Prorated charge for downgrade"
	case types.ProrationActionQuantityChange:
		return "Prorated charge for quantity change"
	case types.ProrationActionAddItem:
		return "Prorated charge for new item"
	default:
		return "Prorated charge"
	}
}

func validateParams(params ProrationParams) error {
	if params.ProrationDate.IsZero() {
		return fmt.Errorf("proration date is required")
	}
	if params.CurrentPeriodStart.IsZero() || params.CurrentPeriodEnd.IsZero() {
		return fmt.Errorf("billing period start and end dates are required")
	}
	if params.CurrentPeriodEnd.Before(params.CurrentPeriodStart) {
		return fmt.Errorf("billing period end date cannot be before start date")
	}
	if params.CustomerTimezone == "" {
		return fmt.Errorf("customer timezone is required")
	}

	switch params.Action {
	case types.ProrationActionAddItem:
		if params.NewPriceID == "" {
			return fmt.Errorf("new price ID is required for add_item action")
		}
		if params.NewQuantity.LessThan(decimal.Zero) {
			return fmt.Errorf("new quantity must be positive for add_item action")
		}
	case types.ProrationActionRemoveItem, types.ProrationActionCancellation:
		if params.OldPriceID == "" {
			return fmt.Errorf("old price ID is required for remove_item/cancellation action")
		}
		if params.OldQuantity.LessThan(decimal.Zero) {
			return fmt.Errorf("old quantity must be positive for remove_item/cancellation action")
		}
	case types.ProrationActionUpgrade, types.ProrationActionDowngrade:
		if params.OldPriceID == "" || params.NewPriceID == "" {
			return fmt.Errorf("both old and new price IDs are required for upgrade/downgrade action")
		}
		if params.OldQuantity.LessThan(decimal.Zero) || params.NewQuantity.LessThan(decimal.Zero) {
			return fmt.Errorf("both old and new quantities must be positive for upgrade/downgrade action")
		}
	case types.ProrationActionQuantityChange:
		if params.OldQuantity.Equal(params.NewQuantity) {
			return fmt.Errorf("old and new quantities cannot be equal for quantity_change action")
		}
		if params.OldQuantity.LessThan(decimal.Zero) || params.NewQuantity.LessThan(decimal.Zero) {
			return fmt.Errorf("both old and new quantities must be positive for quantity_change action")
		}
	default:
		return fmt.Errorf("invalid proration action: %s", params.Action)
	}
	return nil
}
