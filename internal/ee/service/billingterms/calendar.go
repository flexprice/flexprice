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
		if (interval == types.BILLING_PERIOD_DAILY || interval == types.BILLING_PERIOD_WEEKLY) && c.anchor.After(start) && c.anchor.Before(end) {
			return c.anchor.UTC(), nil
		}
		return end.UTC(), nil
	}
	if interval == types.BILLING_PERIOD_DAILY || interval == types.BILLING_PERIOD_WEEKLY {
		days := count
		if interval == types.BILLING_PERIOD_WEEKLY {
			days *= 7
		}
		return c.nextDayBoundary(start, end, days), nil
	}
	return types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart: start, BillingAnchor: c.anchor,
		Unit: count, Period: interval, SubscriptionEndDate: &end, Timezone: c.timezone,
	})
}

func (c *calendar) nextDayBoundary(start, end time.Time, days int) time.Time {
	location, _ := time.LoadLocation(c.timezone)
	anchor, current := c.anchor.In(location), start.In(location)
	anchorDay := time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, time.UTC)
	currentDay := time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, time.UTC)
	// Compare civil days without time.Duration overflow or DST-length assumptions.
	distance := (currentDay.Unix() - anchorDay.Unix()) / 86400
	step := int64(days)
	index := distance / step
	if distance%step < 0 {
		index--
	}
	next := time.Date(anchor.Year(), anchor.Month(), anchor.Day()+int(index*step), anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), location)
	if !next.After(start) {
		next = time.Date(anchor.Year(), anchor.Month(), anchor.Day()+int((index+1)*step), anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), location)
	}
	if next.After(end) {
		return end.UTC()
	}
	return next.UTC()
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
