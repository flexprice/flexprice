package types

import (
	"testing"
	"time"
)

var (
	ist = time.FixedZone("IST", 5*60*60)
	pst = time.FixedZone("PST", -8*60*60)
	jst = time.FixedZone("JST", 9*60*60)
)

func TestNextBillingDate_Daily(t *testing.T) {
	tests := []struct {
		name          string
		currentPeriod time.Time
		billingAnchor time.Time
		unit          int
		want          time.Time
		wantErr       bool
		errMsg        string
	}{
		{
			name:          "simple 10 days",
			currentPeriod: time.Date(2024, time.March, 10, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.March, 10, 0, 0, 0, 0, time.UTC),
			unit:          10,
			want:          time.Date(2024, time.March, 20, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "cross month boundary",
			currentPeriod: time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC),
			unit:          5,
			want:          time.Date(2024, time.April, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "cross year boundary",
			currentPeriod: time.Date(2024, time.December, 29, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.December, 29, 0, 0, 0, 0, time.UTC),
			unit:          5,
			want:          time.Date(2025, time.January, 3, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "leap year February",
			currentPeriod: time.Date(2024, time.February, 27, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.February, 27, 0, 0, 0, 0, time.UTC),
			unit:          3,
			want:          time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "non leap year February",
			currentPeriod: time.Date(2023, time.February, 27, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2023, time.February, 27, 0, 0, 0, 0, time.UTC),
			unit:          3,
			want:          time.Date(2023, time.March, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "invalid unit",
			currentPeriod: time.Date(2024, time.March, 10, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.March, 10, 0, 0, 0, 0, time.UTC),
			unit:          0,
			wantErr:       true,
			errMsg:        "billing period unit must be a positive integer",
		},
		{
			name:          "timezone: IST crossing day boundary",
			currentPeriod: time.Date(2024, time.January, 31, 23, 30, 0, 0, ist),
			billingAnchor: time.Date(2024, time.January, 31, 23, 30, 0, 0, ist),
			unit:          1,
			want:          time.Date(2024, time.February, 1, 23, 30, 0, 0, ist),
		},
		{
			name:          "timezone: PST to next day in UTC",
			currentPeriod: time.Date(2024, time.March, 1, 20, 0, 0, 0, pst),
			billingAnchor: time.Date(2024, time.March, 1, 20, 0, 0, 0, pst),
			unit:          1,
			want:          time.Date(2024, time.March, 2, 20, 0, 0, 0, pst),
		},
		{
			name:          "timezone: JST crossing month boundary",
			currentPeriod: time.Date(2024, time.January, 31, 23, 59, 59, 0, jst),
			billingAnchor: time.Date(2024, time.January, 31, 23, 59, 59, 0, jst),
			unit:          1,
			want:          time.Date(2024, time.February, 1, 23, 59, 59, 0, jst),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(tt.currentPeriod, tt.billingAnchor, tt.unit, BILLING_PERIOD_DAILY)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextBillingDate_Monthly(t *testing.T) {
	tests := []struct {
		name          string
		currentPeriod time.Time
		billingAnchor time.Time
		unit          int
		want          time.Time
		wantErr       bool
		errMsg        string
	}{
		{
			name:          "simple month",
			currentPeriod: time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "31st to shorter month",
			currentPeriod: time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC), // leap year
		},
		{
			name:          "preserve billing anchor day",
			currentPeriod: time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC), // leap year
		},
		{
			name:          "leap year February to March",
			currentPeriod: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2024, time.March, 29, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "cross year with month end",
			currentPeriod: time.Date(2024, time.December, 31, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.December, 31, 0, 0, 0, 0, time.UTC),
			unit:          2,
			want:          time.Date(2025, time.February, 28, 0, 0, 0, 0, time.UTC), // 2025 not leap year
		},
		{
			name:          "timezone: IST crossing month with DST",
			currentPeriod: time.Date(2024, time.March, 31, 22, 30, 0, 0, ist),
			billingAnchor: time.Date(2024, time.March, 31, 22, 30, 0, 0, ist),
			unit:          1,
			want:          time.Date(2024, time.April, 30, 22, 30, 0, 0, ist),
		},
		{
			name:          "timezone: PST leap year February with time",
			currentPeriod: time.Date(2024, time.January, 31, 15, 45, 30, 0, pst),
			billingAnchor: time.Date(2024, time.January, 31, 15, 45, 30, 0, pst),
			unit:          1,
			want:          time.Date(2024, time.February, 29, 15, 45, 30, 0, pst),
		},
		{
			name:          "timezone: JST month-end consistency",
			currentPeriod: time.Date(2024, time.January, 31, 23, 59, 59, 0, jst),
			billingAnchor: time.Date(2024, time.January, 31, 23, 59, 59, 0, jst),
			unit:          2,
			want:          time.Date(2024, time.March, 31, 23, 59, 59, 0, jst),
		},
		{
			name:          "calendar billing: current Jan 15, anchor Feb 1, next billing Feb 1",
			currentPeriod: time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "calendar billing: current May 1 2025, anchor May 1 2025",
			currentPeriod: time.Date(2025, time.May, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2025, time.May, 1, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "calendar billing: current June 1 2025, anchor May 1 2025",
			currentPeriod: time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2025, time.May, 1, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2025, time.July, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "calendar billing: current Feb 1, anchor Mar 1, next billing Mar 1",
			currentPeriod: time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "calendar billing: current Mar 1, anchor Apr 1, next billing Apr 1",
			currentPeriod: time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "calendar billing: current Dec 15, anchor Jan 1 next year, next billing Jan 1 next year",
			currentPeriod: time.Date(2024, time.December, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "leap year: Jan 31, 2024, anchor Feb 29, expect Feb 29, 2024 (IST)",
			currentPeriod: time.Date(2024, time.January, 31, 0, 0, 0, 0, ist),
			billingAnchor: time.Date(2024, time.February, 29, 0, 0, 0, 0, ist),
			unit:          1,
			want:          time.Date(2024, time.February, 29, 0, 0, 0, 0, ist),
		},
		{
			name:          "leap year: Jan 31, 2024, anchor Feb 29, expect Feb 29, 2024 (PST)",
			currentPeriod: time.Date(2024, time.January, 31, 0, 0, 0, 0, pst),
			billingAnchor: time.Date(2024, time.February, 29, 0, 0, 0, 0, pst),
			unit:          1,
			want:          time.Date(2024, time.February, 29, 0, 0, 0, 0, pst),
		},
		{
			name:          "leap year: Jan 31, 2024, anchor Feb 29, expect Feb 29, 2024 (JST)",
			currentPeriod: time.Date(2024, time.January, 31, 0, 0, 0, 0, jst),
			billingAnchor: time.Date(2024, time.February, 29, 0, 0, 0, 0, jst),
			unit:          1,
			want:          time.Date(2024, time.February, 29, 0, 0, 0, 0, jst),
		},
		{
			name:          "non-leap year: Jan 31, 2023, anchor Feb 28, expect Feb 28, 2023",
			currentPeriod: time.Date(2023, time.January, 31, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2023, time.February, 28, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2023, time.February, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "leap year: Feb 29, 2024, anchor Mar 31, expect Mar 31, 2024",
			currentPeriod: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "non-leap year: Feb 28, 2023, anchor Mar 31, expect Mar 31, 2023",
			currentPeriod: time.Date(2023, time.February, 28, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2023, time.March, 31, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2023, time.March, 31, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(tt.currentPeriod, tt.billingAnchor, tt.unit, BILLING_PERIOD_MONTHLY)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextBillingDate_Annual(t *testing.T) {
	tests := []struct {
		name          string
		currentPeriod time.Time
		billingAnchor time.Time
		unit          int
		want          time.Time
		wantErr       bool
		errMsg        string
	}{
		{
			name:          "simple year",
			currentPeriod: time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2025, time.March, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "leap year to non-leap year Feb 29",
			currentPeriod: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2025, time.February, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "preserve billing anchor across years",
			currentPeriod: time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2025, time.February, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "multiple years crossing leap years",
			currentPeriod: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
			unit:          4,
			want:          time.Date(2028, time.February, 29, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "timezone: IST leap year to non-leap year",
			currentPeriod: time.Date(2024, time.February, 29, 13, 30, 0, 0, ist),
			billingAnchor: time.Date(2024, time.February, 29, 13, 30, 0, 0, ist),
			unit:          1,
			want:          time.Date(2025, time.February, 28, 13, 30, 0, 0, ist),
		},
		{
			name:          "timezone: PST crossing year boundary",
			currentPeriod: time.Date(2024, time.December, 31, 23, 59, 59, 0, pst),
			billingAnchor: time.Date(2024, time.December, 31, 23, 59, 59, 0, pst),
			unit:          1,
			want:          time.Date(2025, time.December, 31, 23, 59, 59, 0, pst),
		},
		{
			name:          "timezone: JST preserving time across years",
			currentPeriod: time.Date(2024, time.March, 15, 19, 30, 45, 0, jst),
			billingAnchor: time.Date(2024, time.March, 15, 19, 30, 45, 0, jst),
			unit:          2,
			want:          time.Date(2026, time.March, 15, 19, 30, 45, 0, jst),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(tt.currentPeriod, tt.billingAnchor, tt.unit, BILLING_PERIOD_ANNUAL)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper function to check if a string contains another string
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[0:len(substr)] == substr
}

func TestNextBillingDate_Daily_CalendarBilling(t *testing.T) {
	tests := []struct {
		name          string
		currentPeriod time.Time
		billingAnchor time.Time
		unit          int
		want          time.Time
	}{
		{
			name:          "daily: Dec 31, 2024, anchor Jan 1, expect Jan 1, 2025",
			currentPeriod: time.Date(2024, time.December, 31, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.December, 31, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_DAILY),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "daily: Feb 28, 2023, anchor Mar 1, expect Mar 1, 2023",
			currentPeriod: time.Date(2023, time.February, 28, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2023, time.February, 28, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_DAILY),
			unit:          1,
			want:          time.Date(2023, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "daily: Feb 28, 2024, anchor Feb 29, expect Feb 29, 2024 (IST)",
			currentPeriod: time.Date(2024, time.February, 28, 0, 0, 0, 0, ist),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.February, 28, 0, 0, 0, 0, ist), BILLING_PERIOD_DAILY),
			unit:          1,
			want:          time.Date(2024, time.February, 29, 0, 0, 0, 0, ist),
		},
		{
			name:          "daily: Feb 28, 2024, anchor Feb 29, expect Feb 29, 2024 (PST)",
			currentPeriod: time.Date(2024, time.February, 28, 0, 0, 0, 0, pst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.February, 28, 0, 0, 0, 0, pst), BILLING_PERIOD_DAILY),
			unit:          1,
			want:          time.Date(2024, time.February, 29, 0, 0, 0, 0, pst),
		},
		{
			name:          "daily: Feb 28, 2024, anchor Feb 29, expect Feb 29, 2024 (JST)",
			currentPeriod: time.Date(2024, time.February, 28, 0, 0, 0, 0, jst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.February, 28, 0, 0, 0, 0, jst), BILLING_PERIOD_DAILY),
			unit:          1,
			want:          time.Date(2024, time.February, 29, 0, 0, 0, 0, jst),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(tt.currentPeriod, tt.billingAnchor, tt.unit, BILLING_PERIOD_DAILY)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextBillingDate_Annual_CalendarBilling(t *testing.T) {
	tests := []struct {
		name               string
		currentPeriodStart time.Time
		billingAnchor      time.Time
		unit               int
		want               time.Time
	}{
		{
			name:               "annual: Feb 29, 2024, expect Jan 1, 2025 (IST)",
			currentPeriodStart: time.Date(2024, time.February, 29, 0, 0, 0, 0, ist),
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2024, time.February, 29, 0, 0, 0, 0, ist), BILLING_PERIOD_ANNUAL),
			unit:               1,
			want:               time.Date(2025, time.January, 1, 0, 0, 0, 0, ist),
		},
		{
			name:               "annual: Feb 29, 2024, expect Jan 1, 2025 (PST)",
			currentPeriodStart: time.Date(2024, time.February, 29, 0, 0, 0, 0, pst),
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2024, time.February, 29, 0, 0, 0, 0, pst), BILLING_PERIOD_ANNUAL),
			unit:               1,
			want:               time.Date(2025, time.January, 1, 0, 0, 0, 0, pst),
		},
		{
			name:               "annual: Feb 29, 2024, expect Jan 1, 2025 (JST)",
			currentPeriodStart: time.Date(2024, time.February, 29, 0, 0, 0, 0, jst),
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2024, time.February, 29, 0, 0, 0, 0, jst), BILLING_PERIOD_ANNUAL),
			unit:               1,
			want:               time.Date(2025, time.January, 1, 0, 0, 0, 0, jst),
		},
		{
			name:               "annual: Feb 20, 2023, expect Jan 1, 2024",
			currentPeriodStart: time.Date(2023, time.February, 20, 0, 0, 0, 0, time.UTC),
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2023, time.February, 20, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_ANNUAL),
			unit:               1,
			want:               time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:               "annual: Mar 1, 2024, expect Jan 1, 2025",
			currentPeriodStart: time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_ANNUAL),
			unit:               1,
			want:               time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("start: %v billing anchor: %v", tt.currentPeriodStart, tt.billingAnchor)
			got, err := NextBillingDate(tt.currentPeriodStart, tt.billingAnchor, tt.unit, BILLING_PERIOD_ANNUAL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNextBillingDate_Weekly_CalendarBilling(t *testing.T) {
	// for calendar-aligned weekly billing, the next billing date should
	// always be the start of the next calendar week (typically Monday at 00:00:00)
	// regardless of the current period's weekday
	tests := []struct {
		name               string
		currentPeriodStart time.Time
		billingAnchor      time.Time
		unit               int
		want               time.Time
	}{
		{
			name:               "weekly: Mar 6, 2024 (Wednesday), anchor Mar 11, expect Mar 11, 2024 (next Monday)",
			currentPeriodStart: time.Date(2024, time.March, 6, 0, 0, 0, 0, time.UTC),
			billingAnchor:      time.Date(2024, time.March, 11, 0, 0, 0, 0, time.UTC),
			unit:               1,
			want:               time.Date(2024, time.March, 11, 0, 0, 0, 0, time.UTC),
		},
		{
			name:               "weekly: Mar 10, 2024 (Sunday), anchor Mar 11, expect Mar 17, 2024 (next Sunday + 7)",
			currentPeriodStart: time.Date(2024, time.March, 10, 0, 0, 0, 0, time.UTC),
			billingAnchor:      time.Date(2024, time.March, 11, 0, 0, 0, 0, time.UTC),
			unit:               1,
			want:               time.Date(2024, time.March, 11, 0, 0, 0, 0, time.UTC),
		},
		{
			name:               "weekly: Dec 31, 2023 (Sunday), anchor Jan 1, expect Jan 7, 2024 (next Sunday + 7)",
			currentPeriodStart: time.Date(2023, time.December, 31, 0, 0, 0, 0, time.UTC),
			billingAnchor:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			unit:               1,
			want:               time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(tt.currentPeriodStart, tt.billingAnchor, tt.unit, BILLING_PERIOD_WEEKLY)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
