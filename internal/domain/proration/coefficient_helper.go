package proration

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// calculateProrationCoefficient is a shared helper function that calculates the proration coefficient
// based on the strategy (second-based or day-based). This eliminates code duplication between
// price proration and entitlement proration.
func calculateProrationCoefficient(
	periodStart time.Time,
	periodEnd time.Time,
	prorationDate time.Time,
	loc *time.Location,
	strategy types.ProrationStrategy,
) (decimal.Decimal, error) {
	switch strategy {
	case types.StrategySecondBased:
		totalSeconds := periodEnd.Sub(periodStart).Seconds()
		if totalSeconds <= 0 {
			return decimal.Zero, ierr.NewError("invalid billing period").
				WithHintf("total seconds is zero or negative (%v to %v)", periodStart, periodEnd).
				Mark(ierr.ErrValidation)
		}

		remainingSeconds := periodEnd.Sub(prorationDate).Seconds()
		if remainingSeconds < 0 {
			remainingSeconds = 0
		}
		return decimal.NewFromFloat(remainingSeconds).Div(decimal.NewFromFloat(totalSeconds)), nil

	case types.StrategyDayBased:
		totalDaysRaw := daysInDurationWithDST(periodStart, periodEnd, loc)
		totalDays := totalDaysRaw + 1 // Add 1 to make it inclusive of both start and end dates
		if totalDays <= 0 {
			return decimal.Zero, ierr.NewError("invalid billing period").
				WithHintf("total days is zero or negative (%v to %v)", periodStart, periodEnd).
				Mark(ierr.ErrValidation)
		}

		remainingDaysRaw := daysInDurationWithDST(prorationDate, periodEnd, loc)
		remainingDays := remainingDaysRaw + 1 // Add 1 to make it inclusive of both start and end dates
		if remainingDays < 0 {
			remainingDays = 0
		}

		decimalTotalDays := decimal.NewFromInt(int64(totalDays))
		decimalRemainingDays := decimal.NewFromInt(int64(remainingDays))
		if decimalTotalDays.GreaterThan(decimal.Zero) {
			return decimalRemainingDays.Div(decimalTotalDays), nil
		}
		return decimal.Zero, nil

	default:
		return decimal.Zero, ierr.NewError("invalid proration strategy").
			WithHintf("invalid proration strategy: %s", strategy).
			Mark(ierr.ErrValidation)
	}
}
