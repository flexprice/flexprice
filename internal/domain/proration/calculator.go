package proration

import (
	"context"
	"fmt"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CalculatorType defines the type of proration calculation to use
type CalculatorType string

const (
	CalculatorTypeDay    CalculatorType = "day"
	CalculatorTypeSecond CalculatorType = "second"
)

// NewCalculator creates a proration calculator of the specified type.
func NewCalculator(calculatorType CalculatorType) Calculator {
	switch calculatorType {
	case CalculatorTypeSecond:
		return &secondBasedCalculator{}
	default:
		return &dayBasedCalculator{}
	}
}

// dayBasedCalculator implements the default day-based proration logic.
type dayBasedCalculator struct{}

func (c *dayBasedCalculator) Calculate(ctx context.Context, params ProrationParams) (*ProrationResult, error) {
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

	// Calculate total days in the period (inclusive start, exclusive end)
	totalDays := daysInDurationWithDST(periodStartInTZ, periodEndInTZ, loc)
	if totalDays <= 0 {
		return nil, ierr.NewError("invalid billing period").
			WithHintf("total days is zero or negative (%v to %v)", params.CurrentPeriodStart, params.CurrentPeriodEnd).
			Mark(ierr.ErrValidation)
	}

	// Calculate remaining days (inclusive proration date, exclusive end date)
	remainingDays := daysInDurationWithDST(prorationDateInTZ, periodEndInTZ, loc)
	if remainingDays < 0 {
		remainingDays = 0 // Change happened after period end
	}

	// Calculate proration coefficient
	decimalTotalDays := decimal.NewFromInt(int64(totalDays))
	decimalRemainingDays := decimal.NewFromInt(int64(remainingDays))
	prorationCoefficient := decimal.Zero
	if decimalTotalDays.GreaterThan(decimal.Zero) {
		prorationCoefficient = decimalRemainingDays.Div(decimalTotalDays)
	}

	result := &ProrationResult{
		NetAmount:     decimal.Zero,
		Action:        params.Action,
		ProrationDate: params.ProrationDate,
		LineItemID:    params.LineItemID,
		IsPreview:     params.ProrationBehavior == types.ProrationBehaviorNone,
		CreditItems:   []ProrationLineItem{},
		ChargeItems:   []ProrationLineItem{},
	}

	billingMode := types.BillingModeInArrears
	if params.PlanPayInAdvance {
		billingMode = types.BillingModeInAdvance
	}

	// Credits are issued for existing items that are being modified/removed when:
	// 1. The action involves an existing item (upgrade, downgrade, quantity change, remove, cancel)
	// 2. The billing mode is in advance (we've already collected payment)
	shouldIssueCredit := (params.Action == types.ProrationActionUpgrade ||
		params.Action == types.ProrationActionDowngrade ||
		params.Action == types.ProrationActionQuantityChange ||
		params.Action == types.ProrationActionRemoveItem ||
		params.Action == types.ProrationActionCancellation) &&
		billingMode == types.BillingModeInAdvance

	if shouldIssueCredit {
		oldItemTotal := params.OldPricePerUnit.Mul(params.OldQuantity)
		potentialCredit := oldItemTotal.Mul(prorationCoefficient)
		cappedCredit := c.capCreditAmount(potentialCredit, params.OriginalAmountPaid, params.PreviousCreditsIssued)

		if cappedCredit.GreaterThan(decimal.Zero) {
			creditItem := ProrationLineItem{
				Description: c.generateCreditDescription(params),
				Amount:      cappedCredit.Neg(),
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

	// Charges are issued for new/modified items when:
	// 1. The action involves adding or modifying an item (add, upgrade, downgrade, quantity change)
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
				Amount:      proratedCharge,
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

	return result, nil
}

// daysInDurationWithDST calculates the number of calendar days between two points in time,
// considering the given timezone for day boundaries and handling DST transitions.
func daysInDurationWithDST(start, end time.Time, loc *time.Location) int {
	// Normalize times to midnight in customer timezone
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, loc)

	// Count calendar days, handling DST transitions
	days := 0
	current := startDay
	for current.Before(endDay) {
		days++
		// Add 24 hours, then normalize to midnight to handle DST
		next := current.Add(24 * time.Hour)
		current = time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, loc)
	}

	return days
}

// capCreditAmount ensures credits do not exceed the original amount paid,
// considering any previous credits already issued for the same original payment.
func (c *dayBasedCalculator) capCreditAmount(
	potentialCredit decimal.Decimal,
	originalAmountPaid decimal.Decimal,
	previousCreditsIssued decimal.Decimal,
) decimal.Decimal {
	// Ensure non-negative potential credit
	if potentialCredit.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}

	// Cap at original amount paid
	if potentialCredit.GreaterThan(originalAmountPaid) {
		potentialCredit = originalAmountPaid
	}

	// Reduce by previous credits already issued against this original amount
	availableCredit := originalAmountPaid.Sub(previousCreditsIssued)
	if potentialCredit.GreaterThan(availableCredit) {
		potentialCredit = availableCredit
	}

	// Ensure non-negative final credit
	if potentialCredit.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}

	return potentialCredit
}

// generateCreditDescription generates a human-readable description for a credit line item.
func (c *dayBasedCalculator) generateCreditDescription(params ProrationParams) string {
	switch params.Action {
	case types.ProrationActionCancellation:
		return "Credit for unused time on cancelled subscription"
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

// generateChargeDescription generates a human-readable description for a charge line item.
func (c *dayBasedCalculator) generateChargeDescription(params ProrationParams) string {
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

// validateParams checks if essential parameters are provided.
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

	// Validate price and quantity based on action
	switch params.Action {
	case types.ProrationActionAddItem:
		if params.NewPriceID == "" {
			return fmt.Errorf("new price ID is required for add_item action")
		}
		if params.NewQuantity.LessThanOrEqual(decimal.Zero) {
			return fmt.Errorf("new quantity must be positive for add_item action")
		}
	case types.ProrationActionRemoveItem, types.ProrationActionCancellation:
		if params.OldPriceID == "" {
			return fmt.Errorf("old price ID is required for remove_item/cancellation action")
		}
		if params.OldQuantity.LessThanOrEqual(decimal.Zero) {
			return fmt.Errorf("old quantity must be positive for remove_item/cancellation action")
		}
	case types.ProrationActionUpgrade, types.ProrationActionDowngrade:
		if params.OldPriceID == "" || params.NewPriceID == "" {
			return fmt.Errorf("both old and new price IDs are required for upgrade/downgrade action")
		}
		if params.OldQuantity.LessThanOrEqual(decimal.Zero) || params.NewQuantity.LessThanOrEqual(decimal.Zero) {
			return fmt.Errorf("both old and new quantities must be positive for upgrade/downgrade action")
		}
	case types.ProrationActionQuantityChange:
		if params.OldQuantity.Equal(params.NewQuantity) {
			return fmt.Errorf("old and new quantities cannot be equal for quantity_change action")
		}
		if params.OldQuantity.LessThanOrEqual(decimal.Zero) || params.NewQuantity.LessThanOrEqual(decimal.Zero) {
			return fmt.Errorf("both old and new quantities must be positive for quantity_change action")
		}
	default:
		return fmt.Errorf("invalid proration action: %s", params.Action)
	}

	return nil
}

// secondBasedCalculator implements second-based proration logic
type secondBasedCalculator struct{}

func (c *secondBasedCalculator) Calculate(ctx context.Context, params ProrationParams) (*ProrationResult, error) {
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

	// Calculate total seconds in period
	totalSeconds := periodEndInTZ.Sub(periodStartInTZ).Seconds()
	if totalSeconds <= 0 {
		return nil, ierr.NewError("invalid billing period").
			WithHintf("total seconds is zero or negative (%v to %v)", periodStartInTZ, periodEndInTZ).
			Mark(ierr.ErrValidation)
	}

	// Calculate remaining seconds
	remainingSeconds := periodEndInTZ.Sub(prorationDateInTZ).Seconds()
	if remainingSeconds < 0 {
		remainingSeconds = 0
	}

	// Calculate proration coefficient with higher precision
	prorationCoefficient := decimal.NewFromFloat(remainingSeconds).Div(decimal.NewFromFloat(totalSeconds))

	result := &ProrationResult{
		NetAmount:     decimal.Zero,
		Action:        params.Action,
		ProrationDate: params.ProrationDate,
		LineItemID:    params.LineItemID,
		IsPreview:     params.ProrationBehavior == types.ProrationBehaviorNone,
		CreditItems:   []ProrationLineItem{},
		ChargeItems:   []ProrationLineItem{},
	}

	billingMode := types.BillingModeInArrears
	if params.PlanPayInAdvance {
		billingMode = types.BillingModeInAdvance
	}

	// Credits are issued for existing items that are being modified/removed when:
	// 1. The action involves an existing item (upgrade, downgrade, quantity change, remove, cancel)
	// 2. The billing mode is in advance (we've already collected payment)
	shouldIssueCredit := (params.Action == types.ProrationActionUpgrade ||
		params.Action == types.ProrationActionDowngrade ||
		params.Action == types.ProrationActionQuantityChange ||
		params.Action == types.ProrationActionRemoveItem ||
		params.Action == types.ProrationActionCancellation) &&
		billingMode == types.BillingModeInAdvance

	if shouldIssueCredit {
		oldItemTotal := params.OldPricePerUnit.Mul(params.OldQuantity)
		potentialCredit := oldItemTotal.Mul(prorationCoefficient)
		cappedCredit := c.capCreditAmount(potentialCredit, params.OriginalAmountPaid, params.PreviousCreditsIssued)

		if cappedCredit.GreaterThan(decimal.Zero) {
			creditItem := ProrationLineItem{
				Description: c.generateCreditDescription(params),
				Amount:      cappedCredit.Neg(),
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

	// Charges are issued for new/modified items when:
	// 1. The action involves adding or modifying an item (add, upgrade, downgrade, quantity change)
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
				Amount:      proratedCharge,
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

	return result, nil
}

// capCreditAmount ensures credits do not exceed the original amount paid for second-based calculator
func (c *secondBasedCalculator) capCreditAmount(
	potentialCredit decimal.Decimal,
	originalAmountPaid decimal.Decimal,
	previousCreditsIssued decimal.Decimal,
) decimal.Decimal {
	// Ensure non-negative potential credit
	if potentialCredit.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}

	// Cap at original amount paid
	if potentialCredit.GreaterThan(originalAmountPaid) {
		potentialCredit = originalAmountPaid
	}

	// Reduce by previous credits already issued against this original amount
	availableCredit := originalAmountPaid.Sub(previousCreditsIssued)
	if potentialCredit.GreaterThan(availableCredit) {
		potentialCredit = availableCredit
	}

	// Ensure non-negative final credit
	if potentialCredit.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}

	return potentialCredit
}

// generateCreditDescription generates a human-readable description for a credit line item
func (c *secondBasedCalculator) generateCreditDescription(params ProrationParams) string {
	switch params.Action {
	case types.ProrationActionCancellation:
		return "Credit for unused time on cancelled subscription"
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

// generateChargeDescription generates a human-readable description for a charge line item
func (c *secondBasedCalculator) generateChargeDescription(params ProrationParams) string {
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
