package types

import (
	"fmt"
	"time"
)

// BillingModel is the billing model for the price ex FLAT_FEE, PACKAGE, TIERED
type BillingModel string

// BillingPeriod is the billing period for the price ex MONTHLY, ANNUAL, WEEKLY, DAILY
type BillingPeriod string

// BillingCadence is the billing cadence for the price ex RECURRING, ONETIME
type BillingCadence string

// BillingTier when Billing model is TIERED defines how to
// calculate the price for a given quantity
type BillingTier string

type PriceType string

const (
	PRICE_TYPE_USAGE PriceType = "USAGE"
	PRICE_TYPE_FIXED PriceType = "FIXED"

	// Billing model for a flat fee per unit
	BILLING_MODEL_FLAT_FEE BillingModel = "FLAT_FEE"

	// Billing model for a package of units ex 1000 emails for $100
	BILLING_MODEL_PACKAGE BillingModel = "PACKAGE"

	// Billing model for a tiered pricing model
	// ex 1-100 emails for $100, 101-1000 emails for $90
	BILLING_MODEL_TIERED BillingModel = "TIERED"

	// For BILLING_CADENCE_RECURRING
	BILLING_PERIOD_MONTHLY BillingPeriod = "MONTHLY"
	BILLING_PERIOD_ANNUAL  BillingPeriod = "ANNUAL"
	BILLING_PERIOD_WEEKLY  BillingPeriod = "WEEKLY"
	BILLING_PERIOD_DAILY   BillingPeriod = "DAILY"

	BILLING_CADENCE_RECURRING BillingCadence = "RECURRING"
	BILLING_CADENCE_ONETIME   BillingCadence = "ONETIME"

	// BILLING_TIER_VOLUME means all units price based on final tier reached.
	BILLING_TIER_VOLUME BillingTier = "VOLUME"

	// BILLING_TIER_SLAB means Tiers apply progressively as quantity increases
	BILLING_TIER_SLAB BillingTier = "SLAB"
)

// NextBillingDate calculates the next billing date based on the given start time,
// billing period, and billing period unit (the frequency multiplier).
// For example:
// - If billing period is MONTHLY and unit is 2, we add two months.
// - If billing period is ANNUAL and unit is 1, we add one year.
// - If billing period is WEEKLY and unit is 3, we add 21 days (3 weeks).
// - If billing period is DAILY and unit is 10, we add 10 days.
// This function leverages time.AddDate, which properly handles leap years, month-boundary issues, etc.
func NextBillingDate(start time.Time, unit int, period BillingPeriod) (time.Time, error) {
	if unit <= 0 {
		return start, fmt.Errorf("billing period unit must be a positive integer, got %d", unit)
	}

	switch period {
	case BILLING_PERIOD_DAILY:
		// Add unit days
		return AddClampedDate(start, 0, 0, unit), nil
	case BILLING_PERIOD_WEEKLY:
		// 1 week = 7 days
		return AddClampedDate(start, 0, 0, 7*unit), nil
	case BILLING_PERIOD_MONTHLY:
		// Add 'unit' months
		return AddClampedDate(start, 0, unit, 0), nil
	case BILLING_PERIOD_ANNUAL:
		// Add 'unit' years
		return AddClampedDate(start, unit, 0, 0), nil
	default:
		return start, fmt.Errorf("invalid billing period type: %s", period)
	}
}

func AddClampedDate(t time.Time, years, months, days int) time.Time {
	y, m, d := t.Date()
	h, min, sec := t.Clock()

	// Calculate the proposed year and month
	newY := y + years
	newM := time.Month(int(m) + months)

	// If we move beyond December, it adjusts correctly,
	// for example adding 2 months to November will land on January next year.
	for newM > 12 {
		newM -= 12
		newY++
	}
	for newM < 1 {
		newM += 12
		newY--
	}

	// Find the last valid day of the new month
	firstOfNextMonth := time.Date(newY, newM+1, 1, 0, 0, 0, 0, t.Location())
	lastDay := firstOfNextMonth.Add(-24 * time.Hour).Day()

	newD := d + days
	if newD > lastDay {
		// Clamp to last valid day
		newD = lastDay
	} else if newD < 1 {
		// If we go backwards beyond the start of the month,
		// we might need similar logic. For simplicity, assume positive increments.
	}

	return time.Date(newY, newM, newD, h, min, sec, t.Nanosecond(), t.Location())
}
