package types

import (
	"testing"
	"time"

	"github.com/samber/lo"
)

// Backward-compatibility invariant for the timezone feature:
//
// Before the feature, all billing-date math ran in UTC. The feature must not
// change behaviour for customers without a timezone. Concretely, for every date
// helper, passing Timezone == "" (what legacy callers effectively used) and
// Timezone == "UTC" must produce IDENTICAL results, and those results must match
// the known pre-feature UTC boundaries.

// bcCase is one (start, anchor, unit, period) scenario exercised across timezones.
type bcCase struct {
	name   string
	start  time.Time
	anchor time.Time
	unit   int
	period BillingPeriod
}

func bcCases() []bcCase {
	start := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	anchor := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	return []bcCase{
		{"daily", start, anchor, 1, BILLING_PERIOD_DAILY},
		{"weekly", start, anchor, 1, BILLING_PERIOD_WEEKLY},
		{"monthly", start, anchor, 1, BILLING_PERIOD_MONTHLY},
		{"quarter", start, anchor, 1, BILLING_PERIOD_QUARTER},
		{"half_year", start, anchor, 1, BILLING_PERIOD_HALF_YEAR},
		{"annual", start, anchor, 1, BILLING_PERIOD_ANNUAL},
		{"monthly unit 3", start, anchor, 3, BILLING_PERIOD_MONTHLY},
	}
}

// TestBackwardCompat_NextBillingDate_EmptyEqualsUTC proves the empty-timezone path
// (legacy) and the explicit "UTC" path yield the same instant for every period.
func TestBackwardCompat_NextBillingDate_EmptyEqualsUTC(t *testing.T) {
	for _, c := range bcCases() {
		t.Run(c.name, func(t *testing.T) {
			empty, errE := NextBillingDate(&NextBillingDateParams{
				CurrentPeriodStart: c.start, BillingAnchor: c.anchor, Unit: c.unit, Period: c.period, Timezone: "",
			})
			utc, errU := NextBillingDate(&NextBillingDateParams{
				CurrentPeriodStart: c.start, BillingAnchor: c.anchor, Unit: c.unit, Period: c.period, Timezone: "UTC",
			})
			if errE != nil || errU != nil {
				t.Fatalf("unexpected error: empty=%v utc=%v", errE, errU)
			}
			if !empty.Equal(utc) {
				t.Errorf("empty (%s) != UTC (%s)", empty, utc)
			}
			if empty.Location() != time.UTC {
				t.Errorf("result not in UTC location: %s", empty.Location())
			}
		})
	}
}

// TestBackwardCompat_NextBillingDate_KnownUTCBoundaries pins absolute legacy values
// so a regression in the UTC path is caught even if both tz paths drift together.
func TestBackwardCompat_NextBillingDate_KnownUTCBoundaries(t *testing.T) {
	start := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	anchor := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		period BillingPeriod
		want   time.Time
	}{
		{"daily +1", BILLING_PERIOD_DAILY, time.Date(2024, 1, 16, 0, 0, 0, 0, time.UTC)},
		{"monthly +1", BILLING_PERIOD_MONTHLY, time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC)},
		{"annual +1", BILLING_PERIOD_ANNUAL, time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(&NextBillingDateParams{
				CurrentPeriodStart: start, BillingAnchor: anchor, Unit: 1, Period: tt.period,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

// TestBackwardCompat_CalendarAnchor_EmptyEqualsUTC proves the calendar-anchor helper
// is unchanged for UTC/empty customers across all periods.
func TestBackwardCompat_CalendarAnchor_EmptyEqualsUTC(t *testing.T) {
	start := time.Date(2024, 2, 15, 9, 30, 0, 0, time.UTC)
	for _, p := range []BillingPeriod{
		BILLING_PERIOD_DAILY, BILLING_PERIOD_WEEKLY, BILLING_PERIOD_MONTHLY,
		BILLING_PERIOD_QUARTER, BILLING_PERIOD_HALF_YEAR, BILLING_PERIOD_ANNUAL,
	} {
		t.Run(string(p), func(t *testing.T) {
			empty := CalculateCalendarBillingAnchor(start, p, "")
			utc := CalculateCalendarBillingAnchor(start, p, "UTC")
			if !empty.Equal(utc) {
				t.Errorf("calendar anchor empty (%s) != UTC (%s)", empty, utc)
			}
		})
	}
}

// TestBackwardCompat_CalculatePeriodID_EmptyEqualsUTC proves event period bucketing
// is unchanged for UTC/empty customers.
func TestBackwardCompat_CalculatePeriodID_EmptyEqualsUTC(t *testing.T) {
	subStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	periodStart := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	event := time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC)

	empty, errE := CalculatePeriodID(&CalculatePeriodIDParams{
		EventTimestamp: event, SubStart: subStart, CurrentPeriodStart: periodStart,
		CurrentPeriodEnd: periodEnd, BillingAnchor: subStart, PeriodUnit: 1, PeriodType: BILLING_PERIOD_MONTHLY, Timezone: "",
	})
	utc, errU := CalculatePeriodID(&CalculatePeriodIDParams{
		EventTimestamp: event, SubStart: subStart, CurrentPeriodStart: periodStart,
		CurrentPeriodEnd: periodEnd, BillingAnchor: subStart, PeriodUnit: 1, PeriodType: BILLING_PERIOD_MONTHLY, Timezone: "UTC",
	})
	if errE != nil || errU != nil {
		t.Fatalf("unexpected error: empty=%v utc=%v", errE, errU)
	}
	if empty != utc {
		t.Errorf("period_id empty (%d) != UTC (%d)", empty, utc)
	}
}

// TestBackwardCompat_CalculateBillingPeriods_EmptyEqualsUTC proves period generation
// is unchanged for UTC/empty customers.
func TestBackwardCompat_CalculateBillingPeriods_EmptyEqualsUTC(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := lo.ToPtr(time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))

	empty, errE := CalculateBillingPeriods(&CalculateBillingPeriodsParams{
		InitialPeriodStart: start, EndDate: end, Anchor: start, PeriodCount: 1, BillingPeriod: BILLING_PERIOD_MONTHLY, Timezone: "",
	})
	utc, errU := CalculateBillingPeriods(&CalculateBillingPeriodsParams{
		InitialPeriodStart: start, EndDate: end, Anchor: start, PeriodCount: 1, BillingPeriod: BILLING_PERIOD_MONTHLY, Timezone: "UTC",
	})
	if errE != nil || errU != nil {
		t.Fatalf("unexpected error: empty=%v utc=%v", errE, errU)
	}
	if len(empty) != len(utc) {
		t.Fatalf("period count differs: empty=%d utc=%d", len(empty), len(utc))
	}
	for i := range empty {
		if !empty[i].Start.Equal(utc[i].Start) || !empty[i].End.Equal(utc[i].End) {
			t.Errorf("period %d differs: empty=%v utc=%v", i, empty[i], utc[i])
		}
	}
}
