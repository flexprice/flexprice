package billingterms

import (
	"math"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Calendar supplies the owner's next aligned boundary, clipped to end.
type Calendar = domain.Calendar

type calendar struct {
	anchor   time.Time
	timezone string
}

// NewCalendar constructs an independent calendar anchored in a validated timezone.
func NewCalendar(anchor time.Time, timezone string) (Calendar, error) {
	if !validDate(anchor) {
		return nil, ierr.Validation("calendar requires a finite anchor")
	}
	if !validTimezone(timezone) {
		return nil, ierr.Validation("calendar requires a valid IANA timezone")
	}
	return &calendar{anchor: billingDate(anchor), timezone: timezone}, nil
}

func validTimezone(timezone string) bool {
	if timezone == "" || timezone == "Local" {
		return false
	}
	_, err := time.LoadLocation(timezone)
	return err == nil
}

func (c *calendar) Next(start, end time.Time, interval types.BillingPeriod, count int) (time.Time, error) {
	if c == nil || !validDate(start) || !validDate(end) || !start.Before(end) {
		return time.Time{}, ierr.Validation("calendar requires an ordered finite range")
	}
	if !validInterval(interval) || !validCount(count) {
		return time.Time{}, ierr.Validation("calendar requires a valid frequency and positive 32-bit count")
	}
	// Larger strides exceed every RFC3339 term; avoid overflowing calendar arithmetic.
	limit := 120000
	switch interval {
	case types.BILLING_PERIOD_DAILY:
		limit = 4000000
	case types.BILLING_PERIOD_WEEKLY:
		limit = 4000000 / 7
	case types.BILLING_PERIOD_QUARTER:
		limit /= 3
	case types.BILLING_PERIOD_HALF_YEAR:
		limit /= 6
	case types.BILLING_PERIOD_ANNUAL:
		limit /= 12
	}
	if count > limit {
		return end.UTC(), nil
	}
	return types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: start, BillingAnchor: c.anchor,
		Unit: count, Period: interval, SubscriptionEndDate: &end, Timezone: c.timezone,
	})
}

func validCount(count int) bool { return count > 0 && count <= math.MaxInt32 }

func validInterval(interval types.BillingPeriod) bool {
	switch interval {
	case types.BILLING_PERIOD_DAILY, types.BILLING_PERIOD_WEEKLY, types.BILLING_PERIOD_MONTHLY,
		types.BILLING_PERIOD_QUARTER, types.BILLING_PERIOD_HALF_YEAR, types.BILLING_PERIOD_ANNUAL:
		return true
	default:
		return false
	}
}
