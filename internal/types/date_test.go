package types

import (
	"testing"
	"time"
)

var (
	ist, _ = time.LoadLocation("Asia/Kolkata")
	pst, _ = time.LoadLocation("America/Los_Angeles")
	jst, _ = time.LoadLocation("Asia/Tokyo")
)

// Anniversary billing - start date and billing anchor are the same
// or start date is after the billing anchor but the same day
func TestNextbillingDate_Monthly_Anniversary(t *testing.T) {
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
			name:          "start: 31 Jan 2024, anchor: 31 Jan 2024, unit: 1",
			currentPeriod: time.Date(2024, time.January, 31, 0, 0, 0, 0, ist),
			billingAnchor: time.Date(2024, time.January, 31, 0, 0, 0, 0, ist),
			unit:          1,
			want:          time.Date(2024, time.February, 29, 0, 0, 0, 0, ist),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "start: 15 jan 2024, anchor: 15 jan 2024, unit: 1",
			currentPeriod: time.Date(2024, time.January, 15, 0, 0, 0, 0, ist),
			billingAnchor: time.Date(2024, time.January, 15, 0, 0, 0, 0, ist),
			unit:          1,
			want:          time.Date(2024, time.February, 15, 0, 0, 0, 0, ist),
			wantErr:       false,
			errMsg:        "",
		},
		// leap month
		{
			name:          "start: 30 dec 2023, anchor: 30 dec 2023, unit: 2",
			currentPeriod: time.Date(2023, time.December, 31, 10, 37, 0, 0, ist),
			billingAnchor: time.Date(2023, time.December, 31, 0, 0, 0, 0, ist),
			unit:          2,
			want:          time.Date(2024, time.February, 29, 0, 0, 0, 0, ist),
			wantErr:       false,
			errMsg:        "",
		},
		// regular february
		{
			name:          "start: 30 dec 2023, anchor: 30 dec 2023, unit: 2",
			currentPeriod: time.Date(2024, time.December, 31, 10, 37, 0, 0, ist).UTC(),
			billingAnchor: time.Date(2024, time.December, 31, 10, 37, 0, 0, ist).UTC(),
			unit:          2,
			want:          time.Date(2025, time.February, 28, 10, 37, 0, 0, ist),
			wantErr:       false,
			errMsg:        "",
		},
		// leap feb 29 to march
		{
			name:          "start: 29 feb 2024, anchor: 29 feb 2024, unit: 1",
			currentPeriod: time.Date(2024, time.February, 29, 10, 37, 0, 0, ist),
			billingAnchor: time.Date(2024, time.February, 29, 10, 37, 0, 0, ist),
			unit:          1,
			want:          time.Date(2024, time.March, 29, 10, 37, 0, 0, ist),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "start: 28 feb 2025, anchor: 28 feb 2025, unit: 1",
			currentPeriod: time.Date(2025, time.February, 28, 10, 37, 0, 0, ist),
			billingAnchor: time.Date(2025, time.February, 28, 10, 37, 0, 0, ist),
			unit:          1,
			want:          time.Date(2025, time.March, 28, 10, 37, 0, 0, ist),
			wantErr:       false,
			errMsg:        "",
		},
		// billing anchor is same as start date but older
		{
			name:          "start: 28 feb 2025, anchor: 28 feb 2024, unit: 1",
			currentPeriod: time.Date(2024, time.February, 28, 10, 37, 0, 0, ist),
			billingAnchor: time.Date(2023, time.February, 28, 10, 37, 0, 0, ist),
			unit:          1,
			want:          time.Date(2024, time.March, 28, 10, 37, 0, 0, ist),
		},
		// billing anchor is leap year
		{
			name:          "start: 28 feb 2025, anchor: 28 feb 2024, unit: 1",
			currentPeriod: time.Date(2024, time.April, 29, 10, 37, 0, 0, ist),
			billingAnchor: time.Date(2024, time.February, 29, 10, 37, 0, 0, ist),
			unit:          2,
			want:          time.Date(2024, time.June, 29, 10, 37, 0, 0, ist),
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

func TestNextBillingDate_Monthly_Calendar(t *testing.T) {
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
			name:          "start: 15 jan 2024, anchor: 1 feb 2024, unit: 1",
			currentPeriod: time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_MONTHLY),
			unit:          1,
			want:          time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
			wantErr:       false,
			errMsg:        "",
		},
		// leap month
		{
			name:          "start: 15 jan 2023, anchor: 1 feb 2024, unit: 2",
			currentPeriod: time.Date(2023, time.January, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2023, time.January, 15, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_MONTHLY),
			unit:          2,
			want:          time.Date(2023, time.March, 1, 0, 0, 0, 0, time.UTC),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "start: 30 dec 2024, anchor: 1 jan 2025, unit: 1",
			currentPeriod: time.Date(2024, time.December, 30, 10, 37, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.December, 30, 10, 37, 0, 0, time.UTC), BILLING_PERIOD_MONTHLY),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "start: 30 dec 2024, anchor: 1 jan 2025, unit: 2",
			currentPeriod: time.Date(2024, time.December, 30, 10, 37, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.December, 30, 10, 37, 0, 0, time.UTC), BILLING_PERIOD_MONTHLY),
			unit:          2,
			want:          time.Date(2025, time.February, 1, 0, 0, 0, 0, time.UTC),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "start: 29 feb 2024, anchor: 1 mar 2024, unit: 1",
			currentPeriod: time.Date(2024, time.February, 29, 10, 37, 0, 0, ist),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.February, 29, 10, 37, 0, 0, ist), BILLING_PERIOD_MONTHLY),
			unit:          1,
			want:          time.Date(2024, time.March, 1, 0, 0, 0, 0, ist),
			wantErr:       false,
			errMsg:        "",
		},
		// timezone tests
		{
			name:          "timezone: PST leap year February with time",
			currentPeriod: time.Date(2024, time.January, 31, 15, 45, 30, 0, pst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.January, 31, 15, 45, 30, 0, pst), BILLING_PERIOD_MONTHLY),
			unit:          1,
			want:          time.Date(2024, time.February, 1, 0, 0, 0, 0, pst),
		},
		{
			name:          "timezone: JST month-end consistency",
			currentPeriod: time.Date(2024, time.January, 31, 23, 59, 59, 0, jst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.January, 31, 23, 59, 59, 0, jst), BILLING_PERIOD_MONTHLY),
			unit:          2,
			want:          time.Date(2024, time.March, 1, 0, 0, 0, 0, jst),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("currentPeriod: %v, billingAnchor: %v, unit: %d", tt.currentPeriod, tt.billingAnchor, tt.unit)
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

// TestNextBillingDate_Annual_Anniversary tests the NextBillingDate function for annual billing
// with anniversary billing cycle, focusing on leap year handling
func TestNextBillingDate_Annual_Anniversary(t *testing.T) {
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
			name:          "start: feb 20 2023, anchor: feb 20 2023, unit: 1",
			currentPeriod: time.Date(2023, time.February, 20, 12, 30, 0, 0, time.UTC),
			billingAnchor: time.Date(2023, time.February, 20, 12, 30, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2024, time.February, 20, 12, 30, 0, 0, time.UTC),
		},
		{
			name:          "start: feb 29 2024 (leap year), anchor: feb 29 2024, unit: 1 to 2025 (non-leap year)",
			currentPeriod: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2025, time.February, 28, 0, 0, 0, 0, time.UTC), // Should adjust to Feb 28 in non-leap year
		},
		{
			name:          "start: feb 28 2023 (non-leap year), anchor: feb 28 2023, unit: 1 to 2024 (leap year)",
			currentPeriod: time.Date(2023, time.February, 28, 10, 15, 30, 0, pst),
			billingAnchor: time.Date(2023, time.February, 28, 10, 15, 30, 0, pst),
			unit:          1,
			want:          time.Date(2024, time.February, 28, 10, 15, 30, 0, pst), // Should remain Feb 28 even in leap year
		},
		{
			name:          "from feb 29 2024 to feb 28 2028 (leap to leap), unit: 4",
			currentPeriod: time.Date(2024, time.February, 29, 15, 45, 30, 0, jst),
			billingAnchor: time.Date(2024, time.February, 29, 15, 45, 30, 0, jst),
			unit:          4,
			want:          time.Date(2028, time.February, 29, 15, 45, 30, 0, jst), // Should be Feb 29 in another leap year
		},
		{
			name:          "start: june 30 2023, anchor: june 30 2023, unit: 1",
			currentPeriod: time.Date(2023, time.June, 30, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2023, time.June, 30, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2024, time.June, 30, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "billing anchor is older date but same day",
			currentPeriod: time.Date(2024, time.April, 15, 12, 0, 0, 0, ist),
			billingAnchor: time.Date(2022, time.April, 15, 12, 0, 0, 0, ist),
			unit:          1,
			want:          time.Date(2025, time.April, 15, 12, 0, 0, 0, ist),
		},
		{
			name:          "leap to non leap crossing another non leap",
			currentPeriod: time.Date(2024, time.February, 29, 12, 0, 0, 0, ist),
			billingAnchor: time.Date(2024, time.February, 29, 12, 0, 0, 0, ist),
			unit:          3,
			want:          time.Date(2027, time.February, 28, 12, 0, 0, 0, ist),
		},
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
			name:          "billing anchor cutoff",
			currentPeriod: time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2025, time.February, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "preserve billing anchor across years",
			currentPeriod: time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
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

// TestNextBillingDate_Annual_Calendar tests the NextBillingDate function for annual billing
// with calendar billing cycle
func TestNextBillingDate_Annual_Calendar(t *testing.T) {
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
			name:          "start: mar 15 2023, anchor: jan 1 2024, unit: 1",
			currentPeriod: time.Date(2023, time.March, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2023, time.March, 15, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "start: dec 31 2023, anchor: jan 1 2024, unit: 1",
			currentPeriod: time.Date(2023, time.December, 31, 23, 59, 59, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2023, time.December, 31, 23, 59, 59, 0, time.UTC), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "start: jan 1 2024, anchor: jan 1 2025, unit: 1",
			currentPeriod: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "start: feb 29 2024 (leap year), anchor: jan 1 2025, unit: 1",
			currentPeriod: time.Date(2024, time.February, 29, 12, 30, 0, 0, pst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.February, 29, 12, 30, 0, 0, pst), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, pst),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "start: dec 31 2023, anchor: jan 1 2024, unit: 2 (skip a year)",
			currentPeriod: time.Date(2023, time.December, 31, 0, 0, 0, 0, jst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2023, time.December, 31, 0, 0, 0, 0, jst), BILLING_PERIOD_ANNUAL),
			unit:          2,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, jst),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "timezone: JST with time preservation",
			currentPeriod: time.Date(2024, time.March, 15, 23, 59, 59, 0, jst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.March, 15, 23, 59, 59, 0, jst), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, jst),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "annual: Feb 29, 2024, expect Jan 1, 2025 (IST)",
			currentPeriod: time.Date(2024, time.February, 29, 0, 0, 0, 0, ist),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.February, 29, 0, 0, 0, 0, ist), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, ist),
		},
		{
			name:          "annual: Feb 29, 2024, expect Jan 1, 2025 (PST)",
			currentPeriod: time.Date(2024, time.February, 29, 0, 0, 0, 0, pst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.February, 29, 0, 0, 0, 0, pst), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, pst),
		},
		{
			name:          "annual: Feb 29, 2024, expect Jan 1, 2025 (JST)",
			currentPeriod: time.Date(2024, time.February, 29, 0, 0, 0, 0, jst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.February, 29, 0, 0, 0, 0, jst), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, jst),
		},
		{
			name:          "annual: Feb 20, 2023, expect Jan 1, 2024",
			currentPeriod: time.Date(2023, time.February, 20, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2023, time.February, 20, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "annual: Mar 1, 2024, expect Jan 1, 2025",
			currentPeriod: time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "annual: 15 jan 2024, anchor: 1 jan 2025, unit: 1",
			currentPeriod: time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("currentPeriod: %v, billingAnchor: %v, unit: %d", tt.currentPeriod, tt.billingAnchor, tt.unit)
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

// TestNextBillingDate_Weekly_Anniversary tests weekly billing with anniversary cycle
func TestNextBillingDate_Weekly_Anniversary(t *testing.T) {
	tests := []struct {
		name               string
		currentPeriodStart time.Time
		billingAnchor      time.Time
		unit               int
		want               time.Time
		wantErr            bool
		errMsg             string
	}{
		{
			name:               "weekly: Same weekday (Wednesday), unit 1",
			currentPeriodStart: time.Date(2024, time.March, 6, 12, 30, 45, 0, time.UTC),   // Wednesday
			billingAnchor:      time.Date(2023, time.January, 4, 12, 30, 45, 0, time.UTC), // Wednesday, older date
			unit:               1,
			want:               time.Date(2024, time.March, 13, 12, 30, 45, 0, time.UTC), // Next Wednesday
		},
		{
			name:               "weekly: Same weekday (Friday), unit 2",
			currentPeriodStart: time.Date(2024, time.March, 8, 18, 0, 0, 0, time.UTC),   // Friday
			billingAnchor:      time.Date(2023, time.January, 6, 18, 0, 0, 0, time.UTC), // Friday, older date
			unit:               2,
			want:               time.Date(2024, time.March, 22, 18, 0, 0, 0, time.UTC), // Skip to 2nd Friday
		},
		{
			name:               "weekly: Different weekday (Wed → Mon), unit 1",
			currentPeriodStart: time.Date(2024, time.March, 6, 0, 0, 0, 0, time.UTC),     // Wednesday
			billingAnchor:      time.Date(2023, time.January, 2, 9, 15, 30, 0, time.UTC), // Monday, older date
			unit:               1,
			want:               time.Date(2024, time.March, 11, 9, 15, 30, 0, time.UTC), // Next Monday
		},
		{
			name:               "weekly: Crossing month boundary (Sat → Tue), unit 1",
			currentPeriodStart: time.Date(2024, time.March, 30, 0, 0, 0, 0, time.UTC),  // Saturday
			billingAnchor:      time.Date(2024, time.March, 26, 10, 0, 0, 0, time.UTC), // Tuesday
			unit:               1,
			want:               time.Date(2024, time.April, 2, 10, 0, 0, 0, time.UTC), // Next Tuesday
		},
		{
			name:               "weekly: Crossing year boundary, unit 3",
			currentPeriodStart: time.Date(2024, time.December, 29, 0, 0, 0, 0, time.UTC), // Sunday
			billingAnchor:      time.Date(2024, time.January, 4, 15, 30, 0, 0, time.UTC), // Thursday
			unit:               3,
			want:               time.Date(2025, time.January, 16, 15, 30, 0, 0, time.UTC), // 3rd Thursday after
		},
		{
			name:               "weekly: Different timezone, unit 1",
			currentPeriodStart: time.Date(2024, time.March, 6, 0, 0, 0, 0, ist),     // Wednesday in IST
			billingAnchor:      time.Date(2023, time.January, 4, 14, 30, 0, 0, ist), // Wednesday in IST
			unit:               1,
			want:               time.Date(2024, time.March, 13, 14, 30, 0, 0, ist), // Next Wednesday in IST
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(tt.currentPeriodStart, tt.billingAnchor, tt.unit, BILLING_PERIOD_WEEKLY)
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
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2024, time.March, 6, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_WEEKLY),
			unit:               1,
			want:               time.Date(2024, time.March, 11, 0, 0, 0, 0, time.UTC),
		},
		{
			name:               "weekly: Mar 10, 2024 (Sunday), anchor Mar 11, expect Mar 11 (next day is Monday)",
			currentPeriodStart: time.Date(2024, time.March, 10, 0, 0, 0, 0, time.UTC),
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2024, time.March, 10, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_WEEKLY),
			unit:               1,
			want:               time.Date(2024, time.March, 11, 0, 0, 0, 0, time.UTC),
		},
		{
			name:               "weekly: Monday → unit 1 → next Monday",
			currentPeriodStart: time.Date(2024, time.March, 11, 0, 0, 0, 0, time.UTC), // Monday
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2024, time.March, 11, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_WEEKLY),
			unit:               1,
			want:               time.Date(2024, time.March, 18, 0, 0, 0, 0, time.UTC), // Next Monday
		},
		{
			name:               "weekly: unit 2 → skip a week",
			currentPeriodStart: time.Date(2024, time.March, 6, 0, 0, 0, 0, time.UTC), // Wednesday
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2024, time.March, 6, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_WEEKLY),
			unit:               2,
			want:               time.Date(2024, time.March, 18, 0, 0, 0, 0, time.UTC), // Monday after next
		},
		{
			name:               "weekly: crossing month boundary",
			currentPeriodStart: time.Date(2024, time.March, 27, 0, 0, 0, 0, time.UTC), // Wednesday
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2024, time.March, 27, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_WEEKLY),
			unit:               1,
			want:               time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC), // Monday in April
		},
		{
			name:               "weekly: Dec 31, 2023 (Sunday), anchor Jan 1, expect Jan 1, 2024 (next Monday)",
			currentPeriodStart: time.Date(2023, time.December, 31, 0, 0, 0, 0, time.UTC),
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2023, time.December, 31, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_WEEKLY),
			unit:               1,
			want:               time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:               "weekly: timezone test (IST)",
			currentPeriodStart: time.Date(2024, time.March, 6, 12, 30, 45, 0, ist),
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2024, time.March, 6, 12, 30, 45, 0, ist), BILLING_PERIOD_WEEKLY),
			unit:               1,
			want:               time.Date(2024, time.March, 11, 0, 0, 0, 0, ist),
		},
		{
			name:               "weekly: timezone test (PST)",
			currentPeriodStart: time.Date(2024, time.March, 10, 23, 59, 59, 0, pst),
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2024, time.March, 10, 23, 59, 59, 0, pst), BILLING_PERIOD_WEEKLY),
			unit:               1,
			want:               time.Date(2024, time.March, 11, 0, 0, 0, 0, pst),
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

func TestNextBillingDate_Daily_Anniversary(t *testing.T) {
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

// Helper function to check if a string contains another string
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[0:len(substr)] == substr
}
