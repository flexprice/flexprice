package types

import (
	"testing"
	"time"

	"github.com/samber/lo"
)

func TestCalculateCalendarBillingAnchor(t *testing.T) {
	tests := []struct {
		name          string
		startDate     time.Time
		billingPeriod BillingPeriod
		want          time.Time
	}{
		{
			name:          "Start of next day (DAILY)",
			startDate:     time.Date(2024, 3, 10, 15, 30, 0, 0, time.UTC),
			billingPeriod: BILLING_PERIOD_DAILY,
			want:          time.Date(2024, 3, 11, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "Start of next week (WEEKLY) from Wednesday",
			startDate:     time.Date(2024, 3, 6, 12, 0, 0, 0, time.UTC), // Wednesday
			billingPeriod: BILLING_PERIOD_WEEKLY,
			want:          time.Date(2024, 3, 11, 0, 0, 0, 0, time.UTC), // Next Monday
		},
		{
			name:          "Start of next week (WEEKLY) from Sunday",
			startDate:     time.Date(2024, 3, 10, 8, 0, 0, 0, time.UTC), // Sunday
			billingPeriod: BILLING_PERIOD_WEEKLY,
			want:          time.Date(2024, 3, 11, 0, 0, 0, 0, time.UTC), // Next Monday
		},
		{
			name:          "Start of next month (MONTHLY)",
			startDate:     time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			billingPeriod: BILLING_PERIOD_MONTHLY,
			want:          time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "Start of next month (MONTHLY) leap year Feb",
			startDate:     time.Date(2024, 2, 10, 0, 0, 0, 0, time.UTC),
			billingPeriod: BILLING_PERIOD_MONTHLY,
			want:          time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "Start of next month (MONTHLY) non-leap year Feb",
			startDate:     time.Date(2023, 2, 10, 0, 0, 0, 0, time.UTC),
			billingPeriod: BILLING_PERIOD_MONTHLY,
			want:          time.Date(2023, 3, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "Start of next quarter (QUARTER) Q1",
			startDate:     time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC),
			billingPeriod: BILLING_PERIOD_QUARTER,
			want:          time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "Start of next quarter (QUARTER) Q2",
			startDate:     time.Date(2024, 5, 10, 0, 0, 0, 0, time.UTC),
			billingPeriod: BILLING_PERIOD_QUARTER,
			want:          time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "Start of next half-year (HALF_YEAR) H1",
			startDate:     time.Date(2024, 3, 20, 0, 0, 0, 0, time.UTC),
			billingPeriod: BILLING_PERIOD_HALF_YEAR,
			want:          time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "Start of next half-year (HALF_YEAR) H2",
			startDate:     time.Date(2024, 10, 5, 0, 0, 0, 0, time.UTC),
			billingPeriod: BILLING_PERIOD_HALF_YEAR,
			want:          time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "Start of next year (ANNUAL)",
			startDate:     time.Date(2024, 5, 10, 0, 0, 0, 0, time.UTC),
			billingPeriod: BILLING_PERIOD_ANNUAL,
			want:          time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "1st may 2025",
			startDate:     time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC),
			billingPeriod: BILLING_PERIOD_MONTHLY,
			want:          time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "Default (unknown period)",
			startDate:     time.Date(2024, 5, 10, 0, 0, 0, 0, time.UTC),
			billingPeriod: "UNKNOWN",
			want:          time.Date(2024, 5, 10, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateCalendarBillingAnchor(tt.startDate, tt.billingPeriod)
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextBillingDateWithSubscriptionEndDate(t *testing.T) {
	tests := []struct {
		name                string
		currentPeriodStart  time.Time
		billingAnchor       time.Time
		unit                int
		billingPeriod       BillingPeriod
		subscriptionEndDate *time.Time
		want                time.Time
		description         string
	}{
		{
			name:                "monthly billing without end date",
			currentPeriodStart:  time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			billingAnchor:       time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			unit:                1,
			billingPeriod:       BILLING_PERIOD_MONTHLY,
			subscriptionEndDate: nil,
			want:                time.Date(2024, 2, 15, 12, 0, 0, 0, time.UTC),
			description:         "Should calculate next billing date normally when no end date",
		},
		{
			name:                "monthly billing with end date after next period",
			currentPeriodStart:  time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			billingAnchor:       time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			unit:                1,
			billingPeriod:       BILLING_PERIOD_MONTHLY,
			subscriptionEndDate: lo.ToPtr(time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)),
			want:                time.Date(2024, 2, 15, 12, 0, 0, 0, time.UTC),
			description:         "Should calculate next billing date normally when end date is after",
		},
		{
			name:                "monthly billing with end date before next period",
			currentPeriodStart:  time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			billingAnchor:       time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			unit:                1,
			billingPeriod:       BILLING_PERIOD_MONTHLY,
			subscriptionEndDate: lo.ToPtr(time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)),
			want:                time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			description:         "Should cliff to end date when next period would exceed it",
		},
		{
			name:                "annual billing with end date before next period",
			currentPeriodStart:  time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			billingAnchor:       time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			unit:                1,
			billingPeriod:       BILLING_PERIOD_ANNUAL,
			subscriptionEndDate: lo.ToPtr(time.Date(2024, 6, 30, 23, 59, 59, 0, time.UTC)),
			want:                time.Date(2024, 6, 30, 23, 59, 59, 0, time.UTC),
			description:         "Should cliff to end date for annual billing",
		},
		{
			name:                "weekly billing with end date",
			currentPeriodStart:  time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC), // Monday
			billingAnchor:       time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			unit:                1,
			billingPeriod:       BILLING_PERIOD_WEEKLY,
			subscriptionEndDate: lo.ToPtr(time.Date(2024, 1, 18, 10, 0, 0, 0, time.UTC)), // Thursday
			want:                time.Date(2024, 1, 18, 10, 0, 0, 0, time.UTC),
			description:         "Should cliff to end date for weekly billing",
		},
		{
			name:                "daily billing with end date",
			currentPeriodStart:  time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			billingAnchor:       time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			unit:                3,
			billingPeriod:       BILLING_PERIOD_DAILY,
			subscriptionEndDate: lo.ToPtr(time.Date(2024, 1, 17, 6, 0, 0, 0, time.UTC)),
			want:                time.Date(2024, 1, 17, 6, 0, 0, 0, time.UTC),
			description:         "Should cliff to end date for daily billing when end date is before next period",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(tt.currentPeriodStart, tt.billingAnchor, tt.unit, tt.billingPeriod, tt.subscriptionEndDate)
			if err != nil {
				t.Errorf("NextBillingDate() error = %v", err)
				return
			}
			if !got.Equal(tt.want) {
				t.Errorf("NextBillingDate() = %v, want %v\nDescription: %s", got, tt.want, tt.description)
			}
		})
	}
}

func TestNextBillingDate_Monthly_FirstAnchorStripeLike(t *testing.T) {
	tests := []struct {
		name                string
		currentPeriodStart  time.Time
		billingAnchor       time.Time
		unit                int
		subscriptionEndDate *time.Time
		want                time.Time
	}{
		{
			name:               "start before anchor day same month snaps to anchor",
			currentPeriodStart: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor:      time.Date(2024, 1, 14, 12, 0, 0, 0, time.UTC),
			unit:               1,
			want:               time.Date(2024, 4, 14, 12, 0, 0, 0, time.UTC),
		},
		{
			name:               "start on anchor day advances one month",
			currentPeriodStart: time.Date(2024, 4, 14, 0, 0, 0, 0, time.UTC),
			billingAnchor:      time.Date(2024, 1, 14, 12, 0, 0, 0, time.UTC),
			unit:               1,
			want:               time.Date(2024, 5, 14, 12, 0, 0, 0, time.UTC),
		},
		{
			name:               "start after anchor day in month advances to next month anchor",
			currentPeriodStart: time.Date(2024, 4, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor:      time.Date(2024, 1, 14, 12, 0, 0, 0, time.UTC),
			unit:               1,
			want:               time.Date(2024, 5, 14, 12, 0, 0, 0, time.UTC),
		},
		{
			name:               "anchor equals start day-of-month advances one month",
			currentPeriodStart: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			billingAnchor:      time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			unit:               1,
			want:               time.Date(2024, 2, 15, 12, 0, 0, 0, time.UTC),
		},
		{
			name:               "anchor day 31 in 30-day month clamps",
			currentPeriodStart: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor:      time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
			unit:               1,
			want:               time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC),
		},
		{
			name:               "unit 2 before anchor snaps then next call uses two-month step",
			currentPeriodStart: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor:      time.Date(2024, 1, 14, 0, 0, 0, 0, time.UTC),
			unit:               2,
			want:               time.Date(2024, 4, 14, 0, 0, 0, 0, time.UTC),
		},
		{
			name:                "subscription end before first anchor cliffs",
			currentPeriodStart:  time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC),
			billingAnchor:       time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			unit:                1,
			subscriptionEndDate: lo.ToPtr(time.Date(2024, 1, 12, 0, 0, 0, 0, time.UTC)),
			want:                time.Date(2024, 1, 12, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(tt.currentPeriodStart, tt.billingAnchor, tt.unit, BILLING_PERIOD_MONTHLY, tt.subscriptionEndDate)
			if err != nil {
				t.Fatalf("NextBillingDate() error = %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("NextBillingDate() = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("chain Apr1 anchor14 unit2 then next from Apr14", func(t *testing.T) {
		anchor := time.Date(2024, 1, 14, 0, 0, 0, 0, time.UTC)
		first, err := NextBillingDate(time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC), anchor, 2, BILLING_PERIOD_MONTHLY, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !first.Equal(time.Date(2024, 4, 14, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("first = %v", first)
		}
		second, err := NextBillingDate(first, anchor, 2, BILLING_PERIOD_MONTHLY, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := time.Date(2024, 6, 14, 0, 0, 0, 0, time.UTC)
		if !second.Equal(want) {
			t.Errorf("second = %v, want %v", second, want)
		}
	})
}

// TestNextBillingDate_Monthly_Cliffing_AdvancePath covers monthly billing when the period is
// already past the anchor day in the month (no first-month snap): the next boundary is
// computed via month advance and must still respect subscriptionEndDate.
func TestNextBillingDate_Monthly_Cliffing_AdvancePath(t *testing.T) {
	// After anchor day in April → next May 14; end before that → cliff.
	current := time.Date(2024, 4, 20, 0, 0, 0, 0, time.UTC)
	anchor := time.Date(2024, 1, 14, 12, 0, 0, 0, time.UTC)
	end := lo.ToPtr(time.Date(2024, 5, 10, 0, 0, 0, 0, time.UTC))

	got, err := NextBillingDate(current, anchor, 1, BILLING_PERIOD_MONTHLY, end)
	if err != nil {
		t.Fatalf("NextBillingDate() error = %v", err)
	}
	want := time.Date(2024, 5, 10, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestNextBillingDate_Monthly_Cliffing_FirstSnapSameCalendarDayAsEnd verifies that when the
// Stripe-style first anchor instant would be after the subscription end on the same calendar day,
// the result is cliffed to subscription end.
func TestNextBillingDate_Monthly_Cliffing_FirstSnapSameCalendarDayAsEnd(t *testing.T) {
	current := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	anchor := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)
	end := lo.ToPtr(time.Date(2024, 4, 15, 9, 0, 0, 0, time.UTC))

	got, err := NextBillingDate(current, anchor, 1, BILLING_PERIOD_MONTHLY, end)
	if err != nil {
		t.Fatalf("NextBillingDate() error = %v", err)
	}
	if !got.Equal(*end) {
		t.Errorf("got %v, want %v", got, end)
	}
}

// TestCalculateBillingPeriods_Monthly_FirstPeriodCliffedToSubscriptionEnd ensures period generation
// uses NextBillingDate with the subscription end so the first (possibly short) period does not extend past end.
func TestCalculateBillingPeriods_Monthly_FirstPeriodCliffedToSubscriptionEnd(t *testing.T) {
	start := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	anchor := time.Date(2024, 1, 14, 12, 0, 0, 0, time.UTC)
	subscriptionEnd := lo.ToPtr(time.Date(2024, 4, 12, 0, 0, 0, 0, time.UTC))

	periods, err := CalculateBillingPeriods(start, subscriptionEnd, anchor, 1, BILLING_PERIOD_MONTHLY)
	if err != nil {
		t.Fatalf("CalculateBillingPeriods: %v", err)
	}
	if len(periods) != 1 {
		t.Fatalf("len(periods) = %d, want 1", len(periods))
	}
	wantEnd := time.Date(2024, 4, 12, 0, 0, 0, 0, time.UTC)
	if !periods[0].Start.Equal(start) || !periods[0].End.Equal(wantEnd) {
		t.Errorf("first period = [%v, %v], want [%v, %v]", periods[0].Start, periods[0].End, start, wantEnd)
	}
}
