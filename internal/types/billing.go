package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// BillingImpactDetails provides detailed information about the financial impact of subscription actions
type BillingImpactDetails struct {
	// The amount that will be adjusted for the current period
	// Positive value indicates a charge to the customer
	// Negative value indicates a credit to the customer
	PeriodAdjustmentAmount decimal.Decimal `json:"period_adjustment_amount,omitempty"`

	// The date when the next invoice will be generated
	// For paused subscriptions, this will be after the pause ends
	NextBillingDate *time.Time `json:"next_billing_date,omitempty"`

	// The amount that will be charged on the next billing date
	// This may be prorated if resuming mid-period
	NextBillingAmount decimal.Decimal `json:"next_billing_amount,omitempty"`

	// The original billing cycle dates before pause
	OriginalPeriodStart *time.Time `json:"original_period_start,omitempty"`
	OriginalPeriodEnd   *time.Time `json:"original_period_end,omitempty"`

	// The adjusted billing cycle dates after pause
	AdjustedPeriodStart *time.Time `json:"adjusted_period_start,omitempty"`
	AdjustedPeriodEnd   *time.Time `json:"adjusted_period_end,omitempty"`

	// The total pause duration in days
	PauseDurationDays int `json:"pause_duration_days,omitempty"`
}

// InvoiceReferencePoint indicates the point in time relative to a billing period
// that determines which charges to include in an invoice
type InvoiceReferencePoint string

const (
	// ReferencePointPeriodStart indicates invoice creation at the beginning of a period (for advance charges)
	ReferencePointPeriodStart InvoiceReferencePoint = "period_start"
	// ReferencePointPeriodEnd indicates invoice creation at the end of a period (for arrear charges)
	ReferencePointPeriodEnd InvoiceReferencePoint = "period_end"
	// ReferencePointPreview indicates a preview invoice that should include all charges
	ReferencePointPreview InvoiceReferencePoint = "preview"
	// ReferencePointCancel indicates invoice creation at the end of a period (for arrear charges)
	ReferencePointCancel InvoiceReferencePoint = "cancel"
)

func (r InvoiceReferencePoint) String() string {
	return string(r)
}

func (r InvoiceReferencePoint) Validate() error {
	allowedValues := []InvoiceReferencePoint{
		ReferencePointPeriodStart,
		ReferencePointPeriodEnd,
		ReferencePointPreview,
		ReferencePointCancel,
	}

	if !lo.Contains(allowedValues, r) {
		return ierr.NewError("invalid invoice reference point").
			WithHint("Invalid invoice reference point").
			WithReportableDetails(map[string]any{
				"allowed_values": allowedValues,
				"provided_value": r,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// BillingCycle is the cycle of the billing anchor.
// This is used to determine the billing anchor for the subscription.
// It can be either anniversary or calendar.
// If it's anniversary, the billing anchor will be the start date of the subscription.
// If it's calendar, the billing anchor will be the appropriate date based on the billing period.
type BillingCycle string

const (
	BillingCycleAnniversary BillingCycle = "anniversary"
	BillingCycleCalendar    BillingCycle = "calendar"
)

func (b BillingCycle) Validate() error {
	allowedValues := []BillingCycle{
		BillingCycleAnniversary,
		BillingCycleCalendar,
	}

	if !lo.Contains(allowedValues, b) {
		return ierr.NewError("invalid billing cycle").
			WithHint("Invalid billing cycle").
			WithReportableDetails(map[string]any{
				"allowed_values": allowedValues,
				"provided_value": b,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

func CalculateCalendarBillingAnchor(startDate time.Time, billingPeriod BillingPeriod) time.Time {
	now := startDate.UTC()

	switch billingPeriod {
	case BILLING_PERIOD_DAILY:
		// Start of next day: 00:00:00
		return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)

	case BILLING_PERIOD_WEEKLY:
		// Start of next week (Monday)
		daysUntilMonday := (8 - int(now.Weekday())) % 7
		if daysUntilMonday == 0 {
			daysUntilMonday = 7
		}
		return time.Date(now.Year(), now.Month(), now.Day()+daysUntilMonday, 0, 0, 0, 0, time.UTC)

	case BILLING_PERIOD_MONTHLY:
		// Start of next month
		return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)

	case BILLING_PERIOD_QUARTER:
		// Start of next quarter
		quarter := (int(now.Month())-1)/3 + 1
		startNextQuarterMonth := time.Month(quarter*3 + 1)
		if startNextQuarterMonth > 12 {
			startNextQuarterMonth -= 12
			return time.Date(now.Year()+1, startNextQuarterMonth, 1, 0, 0, 0, 0, time.UTC)
		}
		return time.Date(now.Year(), startNextQuarterMonth, 1, 0, 0, 0, 0, time.UTC)

	case BILLING_PERIOD_HALF_YEAR:
		// Start of next half-year
		halfYear := (int(now.Month())-1)/6 + 1
		startNextHalfYearMonth := time.Month(halfYear*6 + 1)
		if startNextHalfYearMonth > 12 {
			startNextHalfYearMonth -= 12
			return time.Date(now.Year()+1, startNextHalfYearMonth, 1, 0, 0, 0, 0, time.UTC)
		}
		return time.Date(now.Year(), startNextHalfYearMonth, 1, 0, 0, 0, 0, time.UTC)

	case BILLING_PERIOD_ANNUAL:
		// Start of next year
		return time.Date(now.Year()+1, 1, 1, 0, 0, 0, 0, time.UTC)

	default:
		return now
	}
}
