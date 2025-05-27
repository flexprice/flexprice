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

		// will be 00:00:00 for calendar-aligned billing
		// otherwise it will be the start time of the first billing period
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
	// get the time always from anchor because
	// it's either 00:00:00 for calendar-aligned billing
	// or the start time of the first billing period
	h, min, sec := billingAnchor.Clock()

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

// PreviousBillingDate calculates the previous billing date by going backwards from the billing anchor
// by the specified period duration. This is useful for proration calculations where we need to determine
// the start of a full billing period that ends at the billing anchor.
func PreviousBillingDate(billingAnchor time.Time, unit int, period BillingPeriod) (time.Time, error) {
	if unit <= 0 {
		return billingAnchor, ierr.NewError("billing period unit must be a positive integer").
			WithHint("Billing period unit must be a positive integer").
			WithReportableDetails(
				map[string]any{
					"unit": unit,
				},
			).
			Mark(ierr.ErrValidation)
	}

	// For daily and weekly periods, we can use simple subtraction
	switch period {
	case BILLING_PERIOD_DAILY:
		return billingAnchor.AddDate(0, 0, -unit), nil
	case BILLING_PERIOD_WEEKLY:
		return billingAnchor.AddDate(0, 0, -unit*7), nil
	}

	// For monthly and annual periods, calculate the target year and month
	var years, months int
	switch period {
	case BILLING_PERIOD_MONTHLY:
		months = -unit
	case BILLING_PERIOD_ANNUAL:
		years = -unit
	case BILLING_PERIOD_QUARTER:
		months = -unit * 3
	case BILLING_PERIOD_HALF_YEAR:
		months = -unit * 6
	default:
		return billingAnchor, ierr.NewError("invalid billing period type").
			WithHint("Invalid billing period type").
			WithReportableDetails(
				map[string]any{
					"period": period,
				},
			).
			Mark(ierr.ErrValidation)
	}

	// Get the anchor year, month, and time components
	y, m, d := billingAnchor.Date()
	h, min, sec := billingAnchor.Clock()

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

	// For annual billing, preserve the billing anchor month and day
	if period == BILLING_PERIOD_ANNUAL {
		targetM = billingAnchor.Month()
	}

	// Get the target day from the billing anchor
	targetD := d

	// Find the last day of the target month
	lastDayOfMonth := time.Date(targetY, targetM+1, 0, 0, 0, 0, 0, billingAnchor.Location()).Day()

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

	return time.Date(targetY, targetM, targetD, h, min, sec, 0, billingAnchor.Location()), nil
}

// isLeapYear returns true if the given year is a leap year
func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// CalculatePeriodID determines the appropriate billing period start for an event timestamp
// and returns it as a uint64 epoch millisecond timestamp (for ClickHouse period_id column)
// It handles three cases:
// 1. Event timestamp falls within current billing period -> return current period start
// 2. Event timestamp is before current period start -> reject the event for now
// TODO: we can return the previous period start if we want to but need to rethink as
// if the current period is the switched to next period, then it means invoice is already created
// so maybe we should not process the event at all
// 3. Event timestamp is after current period end -> find appropriate future period
func CalculatePeriodID(
	eventTimestamp time.Time,
	subStart time.Time,
	currentPeriodStart time.Time,
	currentPeriodEnd time.Time,
	billingAnchor time.Time,
	periodUnit int,
	periodType BillingPeriod,
) (uint64, error) {
	// Case 1: Event falls within current billing period
	if isBetween(eventTimestamp, currentPeriodStart, currentPeriodEnd) {
		// Return the current period start as milliseconds since epoch
		return calculatePeriodID(currentPeriodStart), nil
	}

	if eventTimestamp.Before(subStart) {
		return 0, ierr.NewError("event timestamp is before subscription start date").
			WithHint("Event timestamp is before subscription start date").
			WithReportableDetails(
				map[string]any{
					"event_timestamp": eventTimestamp,
					"sub_start":       subStart,
				},
			).
			Mark(ierr.ErrValidation)
	}

	// Case 2: Event timestamp is before current period start
	if eventTimestamp.Before(currentPeriodStart) {
		return 0, ierr.NewError("event timestamp is before current period start").
			WithHint("Event timestamp is before current period start").
			WithReportableDetails(
				map[string]any{
					"event_timestamp":      eventTimestamp,
					"current_period_start": currentPeriodStart,
				},
			).
			Mark(ierr.ErrValidation)
	}

	// Case 3: Event timestamp is after current period end
	periodStart := currentPeriodStart
	periodEnd := currentPeriodEnd

	// Iterate forward until we find the period containing the event
	for i := 0; i < 100; i++ { // Limit to 100 iterations to prevent infinite loops
		nextPeriodStart, err := NextBillingDate(periodStart, billingAnchor, periodUnit, periodType)
		if err != nil {
			return 0, err
		}

		// Calculate the next period end
		nextPeriodEnd, err := NextBillingDate(nextPeriodStart, billingAnchor, periodUnit, periodType)
		if err != nil {
			return 0, err
		}

		// Check if event falls within this period
		if isBetween(eventTimestamp, nextPeriodStart, nextPeriodEnd) {
			return calculatePeriodID(nextPeriodStart), nil
		}

		// If this period doesn't contain the event and it's after the period end,
		// continue iterating forward
		periodStart = nextPeriodStart
		periodEnd = nextPeriodEnd
	}

	return 0, ierr.NewError("failed to find appropriate period for event timestamp").
		WithHint("Failed to find appropriate period for event timestamp").
		WithReportableDetails(
			map[string]any{
				"event_timestamp": eventTimestamp,
				"period_start":    periodStart,
				"period_end":      periodEnd,
				"billing_anchor":  billingAnchor,
				"period_unit":     periodUnit,
				"period_type":     periodType,
			},
		).
		Mark(ierr.ErrValidation)
}

func isBetween(eventTimestamp time.Time, periodStart time.Time, periodEnd time.Time) bool {
	return (eventTimestamp.Equal(periodStart) || eventTimestamp.After(periodStart)) &&
		eventTimestamp.Before(periodEnd)
}

func calculatePeriodID(periodStart time.Time) uint64 {
	return uint64(periodStart.Unix() * 1000)
}
