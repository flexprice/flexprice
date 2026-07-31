package types

import (
	"strconv"
	"testing"
	"time"
)

// TestNextBillingDate_FirstPeriodNeverExceedsOneInterval asserts the invariant that motivated the
// anchor bound: wherever the anchor sits, the first billing period ends after the period start and
// no later than one full interval from it.
//
// DAILY and WEEKLY are excluded because they re-align the result to the anchor's clock and weekday
// by design, which can push the first period past a full interval by less than one sub-interval
// (a start at 08:00 with a 14:30 anchor legitimately yields a 30.5-hour first day). The four period
// types below treat the anchor as a date-level boundary, so the invariant is exact for them.
func TestNextBillingDate_FirstPeriodNeverExceedsOneInterval(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 3, 2, 0, 0, 0, 0, loc)

	periods := []BillingPeriod{
		BILLING_PERIOD_MONTHLY,
		BILLING_PERIOD_QUARTER,
		BILLING_PERIOD_HALF_YEAR,
		BILLING_PERIOD_ANNUAL,
	}

	for _, period := range periods {
		for _, unit := range []int{1, 2} {
			// One full interval from the start, which is what a self-anchored subscription
			// produces and therefore the ceiling every other anchor must respect.
			oneInterval, err := NextBillingDate(&NextBillingDateParams{
				CurrentPeriodStart: start,
				BillingAnchor:      start,
				Unit:               unit,
				Period:             period,
				Timezone:           DefaultTimezone,
			})
			if err != nil {
				t.Fatalf("%s unit=%d: computing one interval: %v", period, unit, err)
			}

			midpoint := start.Add(oneInterval.Sub(start) / 2).Truncate(24 * time.Hour)

			anchors := map[string]time.Time{
				"anchor equals start":             start,
				"anchor midway to the boundary":   midpoint,
				"anchor exactly one interval out": oneInterval,
				"anchor in the past":              start.AddDate(-4, 0, 0),
			}

			for name, anchor := range anchors {
				t.Run(string(period)+"/unit="+strconv.Itoa(unit)+"/"+name, func(t *testing.T) {
					got, err := NextBillingDate(&NextBillingDateParams{
						CurrentPeriodStart: start,
						BillingAnchor:      anchor,
						Unit:               unit,
						Period:             period,
						Timezone:           DefaultTimezone,
					})
					if err != nil {
						t.Fatalf("NextBillingDate() error = %v", err)
					}
					if !got.After(start) {
						t.Errorf("first period must advance: got %v, start %v", got, start)
					}
					if got.After(oneInterval) {
						t.Errorf("first period exceeds one interval: got %v, ceiling %v", got, oneInterval)
					}
				})
			}
		}
	}
}

// TestNextBillingDate_AnchorEqualToStartVsAnchorAfterStart documents NextBillingDate when the
// billing anchor equals the current period start (“same”) versus when the anchor is strictly
// after the period start (“after”), for each billing period type.
//
// “Same” means currentPeriodStart and billingAnchor are identical instants (subscription start
// aligned with the chosen anchor).
//
// “After” means billingAnchor is strictly after currentPeriodStart (e.g. later calendar boundary
// or later day-of-month / clock in the monthly case).
func TestNextBillingDate_AnchorEqualToStartVsAnchorAfterStart(t *testing.T) {
	loc := time.UTC
	unit := 1

	tests := []struct {
		name    string
		period  BillingPeriod
		current time.Time
		anchor  time.Time
		want    time.Time
	}{
		// DAILY: next calendar day at anchor clock; “same” uses matching clock on start day.
		{
			name:    "daily_same_anchor_as_start_advances_one_day",
			period:  BILLING_PERIOD_DAILY,
			current: time.Date(2024, 3, 10, 10, 0, 0, 0, loc),
			anchor:  time.Date(2024, 3, 10, 10, 0, 0, 0, loc),
			want:    time.Date(2024, 3, 11, 10, 0, 0, 0, loc),
		},
		{
			name:    "daily_anchor_clock_after_start_same_calendar_day_uses_anchor_clock_next_day",
			period:  BILLING_PERIOD_DAILY,
			current: time.Date(2024, 3, 10, 8, 0, 0, 0, loc),
			anchor:  time.Date(2024, 1, 1, 14, 30, 0, 0, loc),
			want:    time.Date(2024, 3, 11, 14, 30, 0, 0, loc),
		},

		// WEEKLY: anchor weekday + clock; same weekday as start adds unit weeks.
		{
			name:    "weekly_same_anchor_as_start_advances_one_week",
			period:  BILLING_PERIOD_WEEKLY,
			current: time.Date(2024, 3, 6, 14, 30, 0, 0, loc), // Wed
			anchor:  time.Date(2024, 1, 3, 14, 30, 0, 0, loc), // Wed
			want:    time.Date(2024, 3, 13, 14, 30, 0, 0, loc),
		},
		{
			name:    "weekly_anchor_weekday_after_current_weekday_moves_to_first_occurrence",
			period:  BILLING_PERIOD_WEEKLY,
			current: time.Date(2024, 3, 4, 10, 0, 0, 0, loc), // Mon
			anchor:  time.Date(2024, 1, 3, 14, 0, 0, 0, loc), // Wed
			want:    time.Date(2024, 3, 6, 14, 0, 0, 0, loc),
		},

		// MONTHLY: same calendar day → advance by unit months; start before anchor day in month → snap.
		{
			name:    "monthly_same_anchor_day_as_start_advances_next_month",
			period:  BILLING_PERIOD_MONTHLY,
			current: time.Date(2024, 4, 15, 10, 0, 0, 0, loc),
			anchor:  time.Date(2024, 1, 15, 10, 0, 0, 0, loc),
			want:    time.Date(2024, 5, 15, 10, 0, 0, 0, loc),
		},
		{
			name:    "monthly_anchor_day_after_start_day_in_month_snaps_to_anchor_first_period",
			period:  BILLING_PERIOD_MONTHLY,
			current: time.Date(2024, 4, 1, 0, 0, 0, 0, loc),
			anchor:  time.Date(2024, 1, 15, 12, 0, 0, 0, loc),
			want:    time.Date(2024, 4, 15, 12, 0, 0, 0, loc),
		},

		// QUARTERLY: partial first period until calendar anchor; boundary-aligned start advances +3 months.
		{
			name:    "quarterly_same_anchor_start_as_boundary_advances_next_quarter",
			period:  BILLING_PERIOD_QUARTER,
			current: time.Date(2024, 4, 1, 0, 0, 0, 0, loc),
			anchor:  time.Date(2024, 4, 1, 0, 0, 0, 0, loc),
			want:    time.Date(2024, 7, 1, 0, 0, 0, 0, loc),
		},
		{
			name:    "quarterly_start_before_calendar_anchor_returns_anchor",
			period:  BILLING_PERIOD_QUARTER,
			current: time.Date(2024, 2, 15, 0, 0, 0, 0, loc),
			anchor:  time.Date(2024, 4, 1, 0, 0, 0, 0, loc),
			want:    time.Date(2024, 4, 1, 0, 0, 0, 0, loc),
		},

		// HALF_YEARLY: partial first period until calendar anchor; boundary-aligned start advances +6 months.
		{
			name:    "half_yearly_same_anchor_start_as_boundary_advances_next_half",
			period:  BILLING_PERIOD_HALF_YEAR,
			current: time.Date(2024, 7, 1, 0, 0, 0, 0, loc),
			anchor:  time.Date(2024, 7, 1, 0, 0, 0, 0, loc),
			want:    time.Date(2025, 1, 1, 0, 0, 0, 0, loc),
		},
		{
			name:    "half_yearly_start_before_calendar_anchor_returns_anchor",
			period:  BILLING_PERIOD_HALF_YEAR,
			current: time.Date(2024, 3, 15, 0, 0, 0, 0, loc),
			anchor:  time.Date(2024, 7, 1, 0, 0, 0, 0, loc),
			want:    time.Date(2024, 7, 1, 0, 0, 0, 0, loc),
		},

		// ANNUAL: partial first period until the anchor; anchor-aligned start advances +1 year.
		{
			name:    "annual_same_anchor_as_start_advances_one_year",
			period:  BILLING_PERIOD_ANNUAL,
			current: time.Date(2024, 5, 15, 10, 0, 0, 0, loc),
			anchor:  time.Date(2024, 5, 15, 10, 0, 0, 0, loc),
			want:    time.Date(2025, 5, 15, 10, 0, 0, 0, loc),
		},
		{
			// The anchor lands five months out, so the first period is a five-month stub
			// ending on the anchor. Previously this returned the anchor a year later, a
			// seventeen-month first period.
			name:    "annual_start_before_anchor_returns_anchor_as_short_first_period",
			period:  BILLING_PERIOD_ANNUAL,
			current: time.Date(2024, 1, 15, 0, 0, 0, 0, loc),
			anchor:  time.Date(2024, 6, 15, 12, 0, 0, 0, loc),
			want:    time.Date(2024, 6, 15, 12, 0, 0, 0, loc),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(&NextBillingDateParams{
				CurrentPeriodStart: tt.current,
				BillingAnchor:      tt.anchor,
				Unit:               unit,
				Period:             tt.period,
				Timezone:           DefaultTimezone,
			})
			if err != nil {
				t.Fatalf("NextBillingDate() error = %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("NextBillingDate() = %v, want %v", got, tt.want)
			}
		})
	}
}
