package types

import (
	"testing"
	"time"
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
