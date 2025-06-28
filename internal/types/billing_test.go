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
