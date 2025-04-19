package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// NextBillingDate calculates the next billing date based on the current period start,
// billing anchor, billing period, and billing period unit.
// The billing anchor determines the reference point for billing cycles:
// - For MONTHLY periods, it sets the day of the month
// - For ANNUAL periods, it sets the month and day of the year
// - For WEEKLY/DAILY periods, it's used only for validation
func NextBillingDate(currentPeriodStart, billingAnchor time.Time, unit int, period BillingPeriod) (time.Time, error) {
	if unit <= 0 {
		return currentPeriodStart, ierr.NewError("billing period unit must be a positive integer").
			WithHint("Billing period unit must be a positive integer").
			WithReportableDetails(
				map[string]any{
					"unit": unit,
				},
			).
			Mark(ierr.ErrValidation)
	}

	// Before the billing anchor hits, the billing anchor is the next billing date
	if currentPeriodStart.Before(billingAnchor) {
		return billingAnchor, nil
	}

	// For daily and weekly periods, we can use simple addition
	switch period {
	case BILLING_PERIOD_DAILY:
		return currentPeriodStart.AddDate(0, 0, unit), nil
	case BILLING_PERIOD_WEEKLY:
		anchorWeekday := billingAnchor.Weekday()
		currentWeekday := currentPeriodStart.Weekday()

		daysToAdd := int(anchorWeekday - currentWeekday)
		if daysToAdd < 0 {
			daysToAdd += 7
		}
		daysToAdd += (unit - 1) * 7
		if anchorWeekday == currentWeekday {
			daysToAdd = unit * 7
		}

		anchorHour, anchorMin, anchorSec := billingAnchor.Clock()
		return time.Date(currentPeriodStart.Year(), currentPeriodStart.Month(),
			currentPeriodStart.Day()+daysToAdd,
			anchorHour, anchorMin, anchorSec, 0, currentPeriodStart.Location()), nil
	}

	// For monthly and annual periods, calculate the target year and month
	var years, months int
	switch period {
	case BILLING_PERIOD_MONTHLY:
		months = unit
	case BILLING_PERIOD_ANNUAL:
		years = unit
	case BILLING_PERIOD_QUARTER:
		months = unit * 3
	case BILLING_PERIOD_HALF_YEAR:
		months = unit * 6
	default:
		return currentPeriodStart, ierr.NewError("invalid billing period type").
			WithHint("Invalid billing period type").
			WithReportableDetails(
				map[string]any{
					"period": period,
				},
			).
			Mark(ierr.ErrValidation)
	}

	// Get the current year and month
	y, m, _ := currentPeriodStart.Date()
	h, min, sec := currentPeriodStart.Clock()

	// Calculate the target year and month
	targetY := y + years
	targetM := time.Month(int(m) + months)

	// Adjust for month overflow/underflow
	for targetM > 12 {
		targetM -= 12
		targetY++
	}
	for targetM < 1 {
		targetM += 12
		targetY--
	}

	// For annual billing, preserve the billing anchor month
	if period == BILLING_PERIOD_ANNUAL {
		targetM = billingAnchor.Month()
	}

	// Get the target day from the billing anchor
	targetD := billingAnchor.Day()

	// Find the last day of the target month
	lastDayOfMonth := time.Date(targetY, targetM+1, 0, 0, 0, 0, 0, currentPeriodStart.Location()).Day()

	// Special handling for month-end dates and February
	if targetD > lastDayOfMonth {
		targetD = lastDayOfMonth
	}

	// Special case for February 29th in leap years
	if period == BILLING_PERIOD_ANNUAL &&
		billingAnchor.Month() == time.February &&
		billingAnchor.Day() == 29 &&
		!isLeapYear(targetY) {
		targetD = 28
	}

	return time.Date(targetY, targetM, targetD, h, min, sec, 0, currentPeriodStart.Location()), nil
}

// isLeapYear returns true if the given year is a leap year
func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}
