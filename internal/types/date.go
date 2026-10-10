package types

import (
	"math"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// loadTimezone resolves an IANA name (or known abbreviation) to a *time.Location.
//
// Timezone values are validated once at the API boundary (customer DTO), so an
// empty/unresolvable value here is unexpected. We fall back to UTC rather than
// erroring — callers downstream of validation trust the value and never re-validate.
func loadTimezone(tz string) *time.Location {
	if tz == "" || tz == DefaultTimezone {
		return time.UTC
	}
	if loc, err := time.LoadLocation(ResolveTimezone(tz)); err == nil {
		return loc
	}
	return time.UTC
}

// FloorToStartOfDay returns the start of the calendar day (00:00:00.000)
// containing t, evaluated in the given IANA timezone. Empty or unknown tz
// falls back to UTC (matching loadTimezone). The returned instant is UTC.
func FloorToStartOfDay(t time.Time, tz string) time.Time {
	loc := loadTimezone(tz)
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).UTC()
}

// FloorToStartOfHour returns the start of the hour containing t, evaluated
// in the given IANA timezone (empty → UTC). The returned instant is UTC.
// Note: half-hour and quarter-hour offset zones (IST +5:30, NPT +5:45) shift
// the top-of-hour instant relative to UTC — this is the desired behaviour when
// callers reason about "the hour the user sees in their timezone".
//
// On DST fall-back days a local hour repeats (e.g. 01:00 EDT and 01:00 EST both
// exist in New York on the fall-back Sunday). We floor by subtracting sub-hour
// parts from the ORIGINAL instant so the caller keeps the ambiguous-hour
// instance they passed in — reconstructing via time.Date always picks the first
// occurrence (EDT), which would silently rewind the second (EST) occurrence.
func FloorToStartOfHour(t time.Time, tz string) time.Time {
	loc := loadTimezone(tz)
	local := t.In(loc)
	return t.Add(
		-time.Duration(local.Minute())*time.Minute -
			time.Duration(local.Second())*time.Second -
			time.Duration(local.Nanosecond())*time.Nanosecond,
	).UTC()
}

// FloorToStartOfWeek returns Monday 00:00:00 of the ISO 8601 week containing t,
// evaluated in the given IANA timezone (empty → UTC). Monday-first is hardcoded;
// per-tenant/customer week-start configuration is a documented follow-up.
func FloorToStartOfWeek(t time.Time, tz string) time.Time {
	loc := loadTimezone(tz)
	local := t.In(loc)
	// Go's Weekday: Sunday=0, Monday=1, ..., Saturday=6.
	// Days since ISO Monday: Mon=0, Tue=1, ..., Sun=6.
	daysSinceMonday := (int(local.Weekday()) + 6) % 7
	startOfDay := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return startOfDay.AddDate(0, 0, -daysSinceMonday).UTC()
}

// AdvanceDays returns t plus n calendar days in the given timezone. Uses
// calendar arithmetic (time.Time.AddDate in the local zone) so DST transitions
// are handled correctly: adding "1 day" to a local midnight returns the next
// local midnight, even if the intervening calendar day is 23 or 25 hours long.
// Callers that add "24 * time.Hour" instead will drift by ±1h across DST.
func AdvanceDays(t time.Time, n int, tz string) time.Time {
	loc := loadTimezone(tz)
	return t.In(loc).AddDate(0, 0, n).UTC()
}

// NextBillingDateParams holds the inputs for NextBillingDate.
//
// The BillingAnchor determines the reference point for billing cycles:
//   - For month-based periods, billing dates are anchor + k periods, with the day clamped to
//     month end (Jan 31 -> Feb 28 -> Mar 31); the next date is the first one after the start
//   - For WEEKLY/DAILY periods, it sets the weekday and time of day
//
// If SubscriptionEndDate is provided, the result will be cliffed to not exceed it.
// Timezone is an IANA timezone name (e.g. "Asia/Kolkata"); empty or "UTC" computes in UTC.
type NextBillingDateParams struct {
	CurrentPeriodStart  time.Time
	BillingAnchor       time.Time
	Unit                int
	Period              BillingPeriod
	SubscriptionEndDate *time.Time
	Timezone            string
}

// NextBillingDate computes the next billing date in the customer's local timezone
// (when p.Timezone is set), returned as a UTC instant. Stored dates are always UTC
// instants — only the boundary computation happens in local time.
func NextBillingDate(p *NextBillingDateParams) (time.Time, error) {
	loc := loadTimezone(p.Timezone)

	// Re-locate the input times so that all time.Date() calls inside
	// nextBillingDateCore compute the boundary in local time.
	localStart := p.CurrentPeriodStart.In(loc)
	localAnchor := p.BillingAnchor.In(loc)

	result, err := nextBillingDateCore(localStart, localAnchor, p.Unit, p.Period, p.SubscriptionEndDate, p.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	return result.UTC(), nil
}

// nextBillingDateCore is the internal positional-arg implementation used by NextBillingDate
// and other helpers in this file. Callers must pass times already in the desired location.
func nextBillingDateCore(currentPeriodStart, billingAnchor time.Time, unit int, period BillingPeriod, subscriptionEndDate *time.Time, tz string) (time.Time, error) {
	grid, err := NewBillingPeriodGrid(billingAnchor, period, unit, tz)
	if err != nil {
		return currentPeriodStart, err
	}

	// For daily and weekly periods, we can use simple addition
	switch period {
	case BILLING_PERIOD_DAILY:
		// Use the anchor's time component (hour, min, sec) for calendar-aligned billing
		// For calendar billing, anchor is at 00:00:00, so next date will be at midnight
		// For anniversary billing, anchor has the same time as subscription start
		anchorHour, anchorMin, anchorSec := billingAnchor.Clock()
		nextDate := time.Date(
			currentPeriodStart.Year(), currentPeriodStart.Month(), currentPeriodStart.Day()+unit,
			anchorHour, anchorMin, anchorSec, 0, currentPeriodStart.Location(),
		)
		if subscriptionEndDate != nil && nextDate.After(*subscriptionEndDate) {
			return *subscriptionEndDate, nil
		}
		return nextDate, nil
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
		nextDate := time.Date(currentPeriodStart.Year(), currentPeriodStart.Month(),
			currentPeriodStart.Day()+daysToAdd,
			anchorHour, anchorMin, anchorSec, 0, currentPeriodStart.Location())
		if subscriptionEndDate != nil && nextDate.After(*subscriptionEndDate) {
			return *subscriptionEndDate, nil
		}
		return nextDate, nil
	}

	// index of the first billing date after start
	periodIndexNextToStartDatePeriod := grid.periodIndexAfter(currentPeriodStart)

	// resolve the date of that period index
	nextDate := grid.billingDateAtIndex(periodIndexNextToStartDatePeriod)

	// Cliff to subscription end date if provided
	if subscriptionEndDate != nil && nextDate.After(*subscriptionEndDate) {
		return *subscriptionEndDate, nil
	}

	return nextDate, nil
}

// Period represents a billing period with start and end times
type Period struct {
	Start time.Time
	End   time.Time
}

// CalculateBillingPeriodsParams holds the inputs for CalculateBillingPeriods.
//   - InitialPeriodStart: Start of the first period
//   - EndDate: Calculate periods until this date (nil means no limit, but use with caution)
//   - Anchor: Billing anchor for period calculations
//   - PeriodCount: Number of period units (e.g., 1 month, 2 weeks)
//   - BillingPeriod: Type of billing period (daily, weekly, monthly, etc.)
//   - Timezone: IANA timezone for local boundary computation (empty/"UTC" = UTC)
type CalculateBillingPeriodsParams struct {
	InitialPeriodStart time.Time
	EndDate            *time.Time
	Anchor             time.Time
	PeriodCount        int
	BillingPeriod      BillingPeriod
	Timezone           string
}

// CalculateBillingPeriods calculates all billing periods from an initial period start until an end date.
// It uses the same logic as subscription period processing to generate periods consistently.
//
// Returns an array of Period structs and an error if calculation fails.
// The function will stop generating periods when:
//   - The current period end reaches or exceeds the endDate
//   - The endDate is reached (when nextEnd equals currentEnd)
func CalculateBillingPeriods(p *CalculateBillingPeriodsParams) ([]Period, error) {
	// Calculate the initial period end from the start date
	initialPeriodEnd, err := NextBillingDate(&NextBillingDateParams{
		CurrentPeriodStart:  p.InitialPeriodStart,
		BillingAnchor:       p.Anchor,
		Unit:                p.PeriodCount,
		Period:              p.BillingPeriod,
		SubscriptionEndDate: p.EndDate,
		Timezone:            p.Timezone,
	})
	if err != nil {
		return nil, err
	}

	// Start with initial period
	var periods []Period
	periods = append(periods, Period{
		Start: p.InitialPeriodStart,
		End:   initialPeriodEnd,
	})

	currentEnd := initialPeriodEnd

	// Generate periods but respect end date
	// If endDate is nil, use current time
	boundaryEnd := p.EndDate
	if boundaryEnd == nil {
		boundaryEnd = lo.ToPtr(time.Now())
	}

	for currentEnd.Before(*boundaryEnd) {
		nextStart := currentEnd
		nextEnd, err := NextBillingDate(&NextBillingDateParams{
			CurrentPeriodStart:  nextStart,
			BillingAnchor:       p.Anchor,
			Unit:                p.PeriodCount,
			Period:              p.BillingPeriod,
			SubscriptionEndDate: p.EndDate,
			Timezone:            p.Timezone,
		})
		if err != nil {
			return nil, err
		}

		periods = append(periods, Period{
			Start: nextStart,
			End:   nextEnd,
		})

		// In case of end date reached or next end is equal to current end, we break the loop
		// nextEnd will be equal to currentEnd in case of end date reached
		if nextEnd.Equal(currentEnd) {
			break
		}

		currentEnd = nextEnd
	}

	return periods, nil
}

// PreviousBillingDateParams holds the inputs for PreviousBillingDate.
type PreviousBillingDateParams struct {
	BillingAnchor time.Time
	Unit          int
	Period        BillingPeriod
	Timezone      string
}

// PreviousBillingDate calculates the previous billing date by going backwards from the billing anchor
// by the specified period duration. This is useful for proration calculations where we need to determine
// the start of a full billing period that ends at the billing anchor.
func PreviousBillingDate(p *PreviousBillingDateParams) (time.Time, error) {
	grid, err := NewBillingPeriodGrid(p.BillingAnchor, p.Period, p.Unit, p.Timezone)
	if err != nil {
		return p.BillingAnchor, err
	}

	// since we are finding the previous billing date from anchor
	// we are already at the origin and just need to go back one unit distance
	return grid.billingDateAtIndex(-1).UTC(), nil
}

// FullBillingPeriod returns the regular billing period on the grid's schedule that contains t.
// Pk-1 ... t ... Pk, where k = periodIndexAfter(t)
// e.g. monthly grid anchored Feb 1: t = Jan 15 → [Jan 1, Feb 1); t = Feb 1 → [Feb 1, Mar 1).
func FullBillingPeriod(t time.Time, grid *billingPeriodGrid) Period {
	if grid == nil {
		return Period{}
	}

	k := grid.periodIndexAfter(t.In(grid.anchor.Location()))

	return Period{
		Start: grid.billingDateAtIndex(k - 1).UTC(),
		End:   grid.billingDateAtIndex(k).UTC(),
	}
}

// billingPeriodGrid is a billing schedule: the dates anchor + k periods, for any integer k.
// Think of it like a time with origin at anchor
// The next point at anchor + unit * period
// later period is resolve to either N * days or N * months
type billingPeriodGrid struct {
	anchor time.Time
	period BillingPeriod
	unit   int
}

// NewBillingPeriodGrid validates the period and unit and builds the schedule in the timezone tz.
func NewBillingPeriodGrid(anchor time.Time, period BillingPeriod, unit int, tz string) (*billingPeriodGrid, error) {
	if unit <= 0 {
		return nil, ierr.NewError("billing period unit must be a positive integer").
			WithHint("Billing period unit must be a positive integer").
			WithReportableDetails(
				map[string]any{
					"unit": unit,
				},
			).
			Mark(ierr.ErrValidation)
	}
	if period != BILLING_PERIOD_DAILY && period != BILLING_PERIOD_WEEKLY && monthsPerPeriod(period) == 0 {
		return nil, ierr.NewError("invalid billing period type").
			WithHint("Invalid billing period type").
			WithReportableDetails(
				map[string]any{
					"period": period,
				},
			).
			Mark(ierr.ErrValidation)
	}

	return &billingPeriodGrid{anchor: anchor.In(loadTimezone(tz)), period: period, unit: unit}, nil
}

// billingDateAtIndex returns the k-th billing date; k = 0 is the anchor and k may be negative.
func (g billingPeriodGrid) billingDateAtIndex(k int) time.Time {
	switch g.period {
	case BILLING_PERIOD_DAILY:
		return g.anchor.AddDate(0, 0, k*g.unit)
	case BILLING_PERIOD_WEEKLY:
		return g.anchor.AddDate(0, 0, k*g.unit*7)
	}

	// adding months on another month can lead to a date
	// that is not a an actual date hence Date package rolling it over
	// Eg: Jan 31 + 1 month => Feb 31 => Mar 3 but we want Feb 28
	// So we clamp the day to the last day of the target month
	// Possible only for months because they are of uneven number of days
	// Not possible for weeks or any other cadence
	return addMonthsClamped(g.anchor, k*g.unit*monthsPerPeriod(g.period))
}

// periodIndexAfter returns the smallest k whose billing date is strictly after t.
func (g billingPeriodGrid) periodIndexAfter(t time.Time) int {
	// Estimate k, then correct it: step back until dateAt(k) <= t, then forward to the first dateAt(k) > t.
	var k int
	if months := g.unit * monthsPerPeriod(g.period); months > 0 {
		k = ((t.Year()-g.anchor.Year())*12 + int(t.Month()) - int(g.anchor.Month())) / months
	} else {
		k = int(t.Sub(g.anchor).Hours()/24) / (g.unit * lo.Ternary(g.period == BILLING_PERIOD_WEEKLY, 7, 1))
	}

	// step back until the date is before t in case we estimated too high
	for g.billingDateAtIndex(k).After(t) {
		k--
	}

	// step forward till we are at first billing date after t
	for !g.billingDateAtIndex(k).After(t) {
		k++
	}

	return k
}

func monthsPerPeriod(period BillingPeriod) int {
	switch period {
	case BILLING_PERIOD_MONTHLY:
		return 1
	case BILLING_PERIOD_QUARTER:
		return 3
	case BILLING_PERIOD_HALF_YEAR:
		return 6
	case BILLING_PERIOD_ANNUAL:
		return 12
	}

	return 0
}

// addMonthsClamped moves t by n months, keeping its day-of-month but clamping it to the target month's end.
func addMonthsClamped(t time.Time, n int) time.Time {
	y, m, d := t.Date()
	h, mi, sec := t.Clock()

	// m+n => the target month
	// m+n+1 => the next month after target month
	// Day 0 of a month means the last day of the previous month.
	// so m+n+1 day 0 is m+n (target month's) last day
	lastDay := time.Date(y, m+time.Month(n)+1, 0, 0, 0, 0, 0, t.Location()).Day()

	// If Jan 31 + 1 month => Feb 28 (not 31)
	clampedDay := min(d, lastDay)

	return time.Date(y, m+time.Month(n), clampedDay, h, mi, sec, 0, t.Location())
}

// CalculatePeriodIDParams holds the inputs for CalculatePeriodID.
type CalculatePeriodIDParams struct {
	EventTimestamp     time.Time
	SubStart           time.Time
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   time.Time
	BillingAnchor      time.Time
	PeriodUnit         int
	PeriodType         BillingPeriod
	Timezone           string
}

// CalculatePeriodID determines the appropriate billing period start for an event timestamp
// and returns it as a uint64 epoch millisecond timestamp (for ClickHouse period_id column)
// It handles three cases:
// 1. Event timestamp falls within current billing period -> return current period start
// 2. Event timestamp is before current period start -> calculate periods from subscription start to find the appropriate period
// 3. Event timestamp is after current period end -> find appropriate future period
func CalculatePeriodID(p *CalculatePeriodIDParams) (uint64, error) {
	eventTimestamp := p.EventTimestamp
	subStart := p.SubStart
	currentPeriodStart := p.CurrentPeriodStart
	currentPeriodEnd := p.CurrentPeriodEnd
	billingAnchor := p.BillingAnchor
	periodUnit := p.PeriodUnit
	periodType := p.PeriodType
	timezone := p.Timezone

	// Validate that event timestamp is not before subscription start
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

	// Case 1: Event falls within current billing period
	if isBetween(eventTimestamp, currentPeriodStart, currentPeriodEnd) {
		// Return the current period start as milliseconds since epoch
		return calculatePeriodID(currentPeriodStart), nil
	}

	// Case 2: Event timestamp is before current period start
	// Calculate all periods from subscription start to find the appropriate period
	if eventTimestamp.Before(currentPeriodStart) {
		return findPeriodFromSubscriptionStart(
			eventTimestamp,
			subStart,
			currentPeriodStart,
			billingAnchor,
			periodUnit,
			periodType,
			timezone,
		)
	}

	// Case 3: Event timestamp is after current period end
	// Iterate forward from current period until we find the period containing the event
	periodStart := currentPeriodStart
	periodEnd := currentPeriodEnd

	// Iterate forward until we find the period containing the event
	for i := 0; i < 100; i++ { // Limit to 100 iterations to prevent infinite loops
		nextPeriodStart, err := NextBillingDate(&NextBillingDateParams{
			CurrentPeriodStart: periodStart,
			BillingAnchor:      billingAnchor,
			Unit:               periodUnit,
			Period:             periodType,
			Timezone:           timezone,
		})
		if err != nil {
			return 0, err
		}

		// Calculate the next period end
		nextPeriodEnd, err := NextBillingDate(&NextBillingDateParams{
			CurrentPeriodStart: nextPeriodStart,
			BillingAnchor:      billingAnchor,
			Unit:               periodUnit,
			Period:             periodType,
			Timezone:           timezone,
		})
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

// findPeriodFromSubscriptionStart calculates periods from subscription start date
// to find the appropriate period for a past event timestamp
func findPeriodFromSubscriptionStart(
	eventTimestamp time.Time,
	subStart time.Time,
	currentPeriodStart time.Time,
	billingAnchor time.Time,
	periodUnit int,
	periodType BillingPeriod,
	timezone string,
) (uint64, error) {
	// Start from subscription start date
	periodStart := subStart

	// Calculate the first period end
	periodEnd, err := NextBillingDate(&NextBillingDateParams{
		CurrentPeriodStart: periodStart,
		BillingAnchor:      billingAnchor,
		Unit:               periodUnit,
		Period:             periodType,
		Timezone:           timezone,
	})
	if err != nil {
		return 0, err
	}

	// Iterate through periods from subscription start until we find the period containing the event
	// or reach the current period (optimization to avoid infinite loops)
	for i := 0; i < 100; i++ { // Limit to 100 iterations to prevent infinite loops
		// Check if event falls within this period
		if isBetween(eventTimestamp, periodStart, periodEnd) {
			return calculatePeriodID(periodStart), nil
		}

		// If we've reached or passed the current period start, we can stop
		// This is an optimization - if we haven't found the period by now, something is wrong
		if !periodStart.Before(currentPeriodStart) {
			break
		}

		// Move to the next period
		nextPeriodStart := periodEnd
		nextPeriodEnd, err := NextBillingDate(&NextBillingDateParams{
			CurrentPeriodStart: nextPeriodStart,
			BillingAnchor:      billingAnchor,
			Unit:               periodUnit,
			Period:             periodType,
			Timezone:           timezone,
		})
		if err != nil {
			return 0, err
		}

		periodStart = nextPeriodStart
		periodEnd = nextPeriodEnd
	}

	return 0, ierr.NewError("failed to find appropriate period for past event timestamp").
		WithHint("Failed to find appropriate period for past event timestamp").
		WithReportableDetails(
			map[string]any{
				"event_timestamp":      eventTimestamp,
				"sub_start":            subStart,
				"current_period_start": currentPeriodStart,
				"billing_anchor":       billingAnchor,
				"period_unit":          periodUnit,
				"period_type":          periodType,
			},
		).
		Mark(ierr.ErrValidation)
}

func isBetween(eventTimestamp time.Time, periodStart time.Time, periodEnd time.Time) bool {
	return (eventTimestamp.Equal(periodStart) || eventTimestamp.After(periodStart)) &&
		eventTimestamp.Before(periodEnd)
}

func calculatePeriodID(periodStart time.Time) uint64 {
	unixSec := periodStart.Unix()
	// Billing periods are always post-epoch; a pre-epoch input has no
	// meaningful period ID, so treat it as epoch rather than underflowing.
	if unixSec < 0 {
		return 0
	}
	// Guard the *1000 multiply against overflowing int64 before the uint64 cast.
	if unixSec > math.MaxInt64/1000 {
		return math.MaxUint64
	}
	return uint64(unixSec * 1000) // #nosec G115 -- bounded above before cast
}

// GetNextUsageResetAtParams holds the inputs for GetNextUsageResetAt.
type GetNextUsageResetAtParams struct {
	CurrentTime                 time.Time
	SubscriptionStart           time.Time
	SubscriptionEnd             *time.Time
	BillingAnchor               time.Time
	EntitlementUsageResetPeriod EntitlementUsageResetPeriod
	Timezone                    string
}

// GetNextUsageResetAt calculates the next usage reset timestamp based on the entitlement usage reset period.
// The logic handles three main scenarios:
// 1. If entitlement usage reset period is NEVER, returns zero time
// 2. If entitlement usage reset period is DAILY, returns start of tomorrow (00:00:00)
// 3. If entitlement usage reset period is MONTHLY, calculates monthly periods based on subscription start and billing anchor
//
// For monthly reset, it finds the current monthly period containing currentTime and returns the end of that period at 00:00:00.
// All calculations respect the customer's timezone and handle subscription end cliffing.
func GetNextUsageResetAt(p *GetNextUsageResetAtParams) (time.Time, error) {
	currentTime := p.CurrentTime
	subscriptionStart := p.SubscriptionStart
	subscriptionEnd := p.SubscriptionEnd
	billingAnchor := p.BillingAnchor
	entitlementUsageResetPeriod := p.EntitlementUsageResetPeriod
	timezone := p.Timezone

	// Resolve the display location for reset-time boundaries. When a timezone is
	// provided, produce reset times at that local midnight; otherwise fall back to
	// the billingAnchor's embedded location (legacy/UTC behaviour).
	resetLoc := billingAnchor.Location()
	if timezone != "" && timezone != DefaultTimezone {
		resetLoc = loadTimezone(timezone)
	}

	switch entitlementUsageResetPeriod {
	case ENTITLEMENT_USAGE_RESET_PERIOD_NEVER:
		return time.Time{}, nil

	case ENTITLEMENT_USAGE_RESET_PERIOD_DAILY:
		// Calculate start of tomorrow in the customer's timezone
		currentInResetTZ := currentTime.In(resetLoc)
		nextDay := currentInResetTZ.AddDate(0, 0, 1)
		resetTime := time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), 0, 0, 0, 0, resetLoc)

		// Cliff to subscription end if provided
		if subscriptionEnd != nil && resetTime.After(*subscriptionEnd) {
			return *subscriptionEnd, nil
		}

		return resetTime, nil

	case ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY:
		// Calculate monthly periods starting from subscription start
		// Find the period containing currentTime and return its end at 00:00:00

		// Start from subscription start
		periodStart := subscriptionStart

		// Safeguard against infinite loops - allow up to 1000 periods (83+ years of monthly periods)
		for i := 0; i < 1000; i++ {
			// Calculate next monthly boundary using billing anchor
			periodEnd, err := NextBillingDate(&NextBillingDateParams{
				CurrentPeriodStart: periodStart,
				BillingAnchor:      billingAnchor,
				Unit:               1,
				Period:             BILLING_PERIOD_MONTHLY,
				Timezone:           timezone,
			})
			if err != nil {
				return time.Time{}, ierr.NewError("failed to calculate monthly period").
					WithHint("Failed to calculate monthly period for usage reset").
					WithReportableDetails(map[string]any{
						"period_start":   periodStart,
						"billing_anchor": billingAnchor,
						"current_time":   currentTime,
						"original_error": err.Error(),
					}).
					Mark(ierr.ErrValidation)
			}

			// Check if current time falls in this monthly period [periodStart, periodEnd)
			if isBetween(currentTime, periodStart, periodEnd) {
				// Return the period end date at 00:00:00 in the customer's local timezone.
				periodEndLocal := periodEnd.In(resetLoc)
				resetTime := time.Date(periodEndLocal.Year(), periodEndLocal.Month(), periodEndLocal.Day(), 0, 0, 0, 0, resetLoc)

				// Cliff to subscription end if provided
				if subscriptionEnd != nil && resetTime.After(*subscriptionEnd) {
					return *subscriptionEnd, nil
				}

				return resetTime, nil
			}

			// Move to next period
			periodStart = periodEnd

			// Safety check: if we've gone way beyond current time, something is wrong
			if periodStart.After(currentTime.AddDate(1, 0, 0)) {
				break
			}
		}

		return time.Time{}, ierr.NewError("failed to find monthly reset period").
			WithHint("Failed to find appropriate monthly period for usage reset").
			WithReportableDetails(map[string]any{
				"current_time":       currentTime,
				"subscription_start": subscriptionStart,
				"billing_anchor":     billingAnchor,
			}).
			Mark(ierr.ErrValidation)

	default:
		return time.Time{}, ierr.NewError("unsupported entitlement usage reset period").
			WithHint("Unsupported entitlement usage reset period. Only DAILY, MONTHLY, and NEVER are supported").
			WithReportableDetails(map[string]any{
				"reset_period": entitlementUsageResetPeriod,
			}).
			Mark(ierr.ErrValidation)
	}
}

// FindPeriodForDateParams holds the inputs for FindPeriodForDate.
type FindPeriodForDateParams struct {
	Target           time.Time
	KnownPeriodStart time.Time
	KnownPeriodEnd   time.Time
	Anchor           time.Time
	PeriodCount      int
	BillingPeriod    BillingPeriod
	Timezone         string
}

// FindPeriodForDate returns the end of the billing period that contains target,
// starting the search from knownPeriodStart/End and walking forward as needed.
//
// Unlike CalculateBillingPeriods, this function passes nil to NextBillingDate so
// period ends are never capped — giving natural boundaries regardless of target.
// Capped at 240 forward steps (~20 years of monthly billing).
func FindPeriodForDate(p *FindPeriodForDateParams) (Period, error) {
	target := p.Target
	anchor := p.Anchor
	periodCount := p.PeriodCount
	billingPeriod := p.BillingPeriod
	timezone := p.Timezone

	inPeriod := func(t, start, end time.Time) bool {
		return !t.Before(start) && t.Before(end)
	}

	periodStart := p.KnownPeriodStart
	periodEnd := p.KnownPeriodEnd

	// Fast path: target is already within the known period.
	if inPeriod(target, periodStart, periodEnd) {
		return Period{Start: periodStart, End: periodEnd}, nil
	}

	const maxIter = 240
	for i := 0; i < maxIter; i++ {
		nextEnd, err := NextBillingDate(&NextBillingDateParams{
			CurrentPeriodStart: periodEnd,
			BillingAnchor:      anchor,
			Unit:               periodCount,
			Period:             billingPeriod,
			Timezone:           timezone,
		})
		if err != nil {
			return Period{}, err
		}
		if nextEnd.Equal(periodEnd) {
			break
		}
		periodStart, periodEnd = periodEnd, nextEnd
		if inPeriod(target, periodStart, periodEnd) {
			return Period{Start: periodStart, End: periodEnd}, nil
		}
	}

	return Period{}, ierr.NewError("could not find billing period for date").
		WithHint("The target date may be too far in the future or past relative to the known billing period").
		WithReportableDetails(map[string]any{
			"target":             target,
			"known_period_start": p.KnownPeriodStart,
			"known_period_end":   p.KnownPeriodEnd,
		}).
		Mark(ierr.ErrValidation)
}
