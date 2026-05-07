package types

import (
	"testing"
	"time"
)

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

		// ANNUAL: adds unit years with anchor month/day; same month/day → next year.
		{
			name:    "annual_same_anchor_as_start_advances_one_year",
			period:  BILLING_PERIOD_ANNUAL,
			current: time.Date(2024, 5, 15, 10, 0, 0, 0, loc),
			anchor:  time.Date(2024, 5, 15, 10, 0, 0, 0, loc),
			want:    time.Date(2025, 5, 15, 10, 0, 0, 0, loc),
		},
		{
			name:    "annual_anchor_month_day_after_start_in_year_moves_to_anchor_month_next_year",
			period:  BILLING_PERIOD_ANNUAL,
			current: time.Date(2024, 1, 15, 0, 0, 0, 0, loc),
			anchor:  time.Date(2024, 6, 15, 12, 0, 0, 0, loc),
			want:    time.Date(2025, 6, 15, 12, 0, 0, 0, loc),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(tt.current, tt.anchor, unit, tt.period, nil)
			if err != nil {
				t.Fatalf("NextBillingDate() error = %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("NextBillingDate() = %v, want %v", got, tt.want)
			}
		})
	}
}
