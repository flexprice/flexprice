package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart: tt.currentPeriod,
				BillingAnchor:      tt.billingAnchor,
				Unit:               tt.unit,
				Period:             BILLING_PERIOD_MONTHLY,
				Timezone:           tt.currentPeriod.Location().String(),
			})
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
			want:          time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			wantErr:       false,
			errMsg:        "",
		},
		// timezone tests
		{
			name:          "timezone: PST leap year February with time",
			currentPeriod: time.Date(2024, time.January, 31, 15, 45, 30, 0, pst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.January, 31, 15, 45, 30, 0, pst), BILLING_PERIOD_MONTHLY),
			unit:          1,
			want:          time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "timezone: JST month-end consistency",
			currentPeriod: time.Date(2024, time.January, 31, 23, 59, 59, 0, jst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.January, 31, 23, 59, 59, 0, jst), BILLING_PERIOD_MONTHLY),
			unit:          2,
			want:          time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("currentPeriod: %v, billingAnchor: %v, unit: %d", tt.currentPeriod, tt.billingAnchor, tt.unit)
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart: tt.currentPeriod,
				BillingAnchor:      tt.billingAnchor,
				Unit:               tt.unit,
				Period:             BILLING_PERIOD_MONTHLY,
			})
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
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart: tt.currentPeriod,
				BillingAnchor:      tt.billingAnchor,
				Unit:               tt.unit,
				Period:             BILLING_PERIOD_ANNUAL,
				Timezone:           tt.currentPeriod.Location().String(),
			})
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
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "start: dec 31 2023, anchor: jan 1 2024, unit: 2 (skip a year)",
			currentPeriod: time.Date(2023, time.December, 31, 0, 0, 0, 0, jst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2023, time.December, 31, 0, 0, 0, 0, jst), BILLING_PERIOD_ANNUAL),
			unit:          2,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "timezone: JST with time preservation",
			currentPeriod: time.Date(2024, time.March, 15, 23, 59, 59, 0, jst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.March, 15, 23, 59, 59, 0, jst), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			wantErr:       false,
			errMsg:        "",
		},
		{
			name:          "annual: Feb 29, 2024, expect Jan 1, 2025 (IST)",
			currentPeriod: time.Date(2024, time.February, 29, 0, 0, 0, 0, ist),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.February, 29, 0, 0, 0, 0, ist), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "annual: Feb 29, 2024, expect Jan 1, 2025 (PST)",
			currentPeriod: time.Date(2024, time.February, 29, 0, 0, 0, 0, pst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.February, 29, 0, 0, 0, 0, pst), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "annual: Feb 29, 2024, expect Jan 1, 2025 (JST)",
			currentPeriod: time.Date(2024, time.February, 29, 0, 0, 0, 0, jst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.February, 29, 0, 0, 0, 0, jst), BILLING_PERIOD_ANNUAL),
			unit:          1,
			want:          time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
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
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart: tt.currentPeriod,
				BillingAnchor:      tt.billingAnchor,
				Unit:               tt.unit,
				Period:             BILLING_PERIOD_ANNUAL,
			})
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
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart: tt.currentPeriodStart,
				BillingAnchor:      tt.billingAnchor,
				Unit:               tt.unit,
				Period:             BILLING_PERIOD_WEEKLY,
				Timezone:           tt.currentPeriodStart.Location().String(),
			})
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
			want:               time.Date(2024, time.March, 11, 0, 0, 0, 0, time.UTC),
		},
		{
			name:               "weekly: timezone test (PST)",
			currentPeriodStart: time.Date(2024, time.March, 10, 23, 59, 59, 0, pst),
			billingAnchor:      CalculateCalendarBillingAnchor(time.Date(2024, time.March, 10, 23, 59, 59, 0, pst), BILLING_PERIOD_WEEKLY),
			unit:               1,
			want:               time.Date(2024, time.March, 18, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart: tt.currentPeriodStart,
				BillingAnchor:      tt.billingAnchor,
				Unit:               tt.unit,
				Period:             BILLING_PERIOD_WEEKLY,
			})
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
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart: tt.currentPeriod,
				BillingAnchor:      tt.billingAnchor,
				Unit:               tt.unit,
				Period:             BILLING_PERIOD_DAILY,
				Timezone:           tt.currentPeriod.Location().String(),
			})
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
			want:          time.Date(2024, time.February, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "daily: Feb 28, 2024, anchor Feb 29, expect Feb 29, 2024 (PST)",
			currentPeriod: time.Date(2024, time.February, 28, 0, 0, 0, 0, pst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.February, 28, 0, 0, 0, 0, pst), BILLING_PERIOD_DAILY),
			unit:          1,
			want:          time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "daily: Feb 28, 2024, anchor Feb 29, expect Feb 29, 2024 (JST)",
			currentPeriod: time.Date(2024, time.February, 28, 0, 0, 0, 0, jst),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2024, time.February, 28, 0, 0, 0, 0, jst), BILLING_PERIOD_DAILY),
			unit:          1,
			want:          time.Date(2024, time.February, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "daily: mid-day start with calendar billing should align to midnight",
			currentPeriod: time.Date(2026, time.March, 2, 7, 58, 45, 337000000, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2026, time.March, 2, 7, 58, 45, 337000000, time.UTC), BILLING_PERIOD_DAILY),
			unit:          1,
			want:          time.Date(2026, time.March, 3, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart: tt.currentPeriod,
				BillingAnchor:      tt.billingAnchor,
				Unit:               tt.unit,
				Period:             BILLING_PERIOD_DAILY,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNextBillingDate_Quarterly_Calendar tests quarterly calendar billing period calculations.
// Calendar billing aligns periods with calendar quarter boundaries (Q1: Jan-Mar, Q2: Apr-Jun, etc.)
func TestNextBillingDate_Quarterly_Calendar(t *testing.T) {
	tests := []struct {
		name          string
		currentPeriod time.Time
		billingAnchor time.Time
		unit          int
		want          time.Time
	}{
		{
			// Sub starts March 22. Calendar anchor = April 1 (start of Q2).
			// First period: March 22 → April 1 (partial Q1 tail).
			name:          "partial first period: mid-Q1 start (Mar 22) → Apr 1",
			currentPeriod: time.Date(2026, time.March, 22, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2026, time.March, 22, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_QUARTER),
			unit:          1,
			want:          time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// Second full period: April 1 → July 1.
			name:          "full Q2 period: Apr 1 → Jul 1",
			currentPeriod: time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2026, time.March, 22, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_QUARTER),
			unit:          1,
			want:          time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// Third full period: July 1 → October 1.
			name:          "full Q3 period: Jul 1 → Oct 1",
			currentPeriod: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2026, time.March, 22, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_QUARTER),
			unit:          1,
			want:          time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// Q4 period: October 1 → January 1.
			name:          "full Q4 period: Oct 1 → Jan 1",
			currentPeriod: time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2026, time.March, 22, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_QUARTER),
			unit:          1,
			want:          time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// Sub starts exactly on a quarter boundary (April 1). Anchor = July 1.
			// First (full) period: April 1 → July 1.
			name:          "start exactly on quarter boundary: Apr 1 → Jul 1",
			currentPeriod: time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_QUARTER),
			unit:          1,
			want:          time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// Anniversary quarterly: anchor = start date. No calendar alignment.
			name:          "anniversary quarterly: Mar 22, anchor Mar 22 → Jun 22",
			currentPeriod: time.Date(2026, time.March, 22, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2026, time.March, 22, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2026, time.June, 22, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("currentPeriod: %v, billingAnchor: %v, unit: %d", tt.currentPeriod, tt.billingAnchor, tt.unit)
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart: tt.currentPeriod,
				BillingAnchor:      tt.billingAnchor,
				Unit:               tt.unit,
				Period:             BILLING_PERIOD_QUARTER,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNextBillingDate_Quarterly_Calendar_BackwardCompat verifies that existing subscriptions
// whose first period was calculated with the OLD behavior (e.g. March 22 → June 1 instead of
// March 22 → April 1) continue to advance consistently and are NOT realigned.
// We must not break existing subscriptions — only new ones get the corrected calendar alignment.
func TestNextBillingDate_Quarterly_Calendar_BackwardCompat(t *testing.T) {
	// Simulate an existing subscription:
	//   subscriptionStart = March 22 (created before the fix)
	//   billingAnchor     = April 1  (correctly stored on creation)
	//   OLD first period  : March 22 → June 1  (was the wrong end date)
	//
	// After June 1 the cron advances currentPeriodStart to June 1 and calls
	// NextBillingDate(June 1, April 1, ...). Since June 1 > April 1 the new
	// guard does NOT fire; we simply add 3 months → September 1, keeping the
	// existing cadence intact.
	tests := []struct {
		name          string
		currentPeriod time.Time
		billingAnchor time.Time
		unit          int
		want          time.Time
	}{
		{
			name:          "2nd period of old sub: Jun 1 (old wrong end) → Sep 1",
			currentPeriod: time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "3rd period of old sub: Sep 1 → Dec 1",
			currentPeriod: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "4th period of old sub: Dec 1 → Mar 1 2027",
			currentPeriod: time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart: tt.currentPeriod,
				BillingAnchor:      tt.billingAnchor,
				Unit:               tt.unit,
				Period:             BILLING_PERIOD_QUARTER,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNextBillingDate_HalfYearly_Calendar_BackwardCompat mirrors the quarterly backward-compat
// test for half-yearly subscriptions created before the fix.
func TestNextBillingDate_HalfYearly_Calendar_BackwardCompat(t *testing.T) {
	// Old sub: started March 15, anchor = July 1 (day=1).
	// OLD first period: March 15 → September 1
	//   (targetD = billingAnchor.Day() = 1, targetM = March+6 = September)
	// OLD second period: September 1 → March 1 2027
	// Since all these currentPeriodStarts are > billingAnchor, the new guard
	// does NOT fire and the cadence is preserved as-is.
	tests := []struct {
		name          string
		currentPeriod time.Time
		billingAnchor time.Time
		unit          int
		want          time.Time
	}{
		{
			name:          "2nd period of old half-yearly sub: Sep 1 → Mar 1 2027",
			currentPeriod: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:          "3rd period of old half-yearly sub: Mar 1 2027 → Sep 1 2027",
			currentPeriod: time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2027, time.September, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart: tt.currentPeriod,
				BillingAnchor:      tt.billingAnchor,
				Unit:               tt.unit,
				Period:             BILLING_PERIOD_HALF_YEAR,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNextBillingDate_HalfYearly_Calendar tests half-yearly calendar billing period calculations.
// Calendar billing aligns periods with half-year boundaries (H1: Jan-Jun, H2: Jul-Dec).
func TestNextBillingDate_HalfYearly_Calendar(t *testing.T) {
	tests := []struct {
		name          string
		currentPeriod time.Time
		billingAnchor time.Time
		unit          int
		want          time.Time
	}{
		{
			// Sub starts March 15. Anchor = July 1 (start of H2).
			// First period: March 15 → July 1 (partial H1 tail).
			name:          "partial first period: mid-H1 start (Mar 15) → Jul 1",
			currentPeriod: time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_HALF_YEAR),
			unit:          1,
			want:          time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// Second full period: July 1 → January 1.
			name:          "full H2 period: Jul 1 → Jan 1",
			currentPeriod: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_HALF_YEAR),
			unit:          1,
			want:          time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// Third full period: January 1 → July 1.
			name:          "full H1 period: Jan 1 2027 → Jul 1 2027",
			currentPeriod: time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_HALF_YEAR),
			unit:          1,
			want:          time.Date(2027, time.July, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// Sub starts October 5 (H2). Anchor = January 1.
			// First period: Oct 5 → Jan 1 (partial H2 tail).
			name:          "partial first period: mid-H2 start (Oct 5) → Jan 1",
			currentPeriod: time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC),
			billingAnchor: CalculateCalendarBillingAnchor(time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC), BILLING_PERIOD_HALF_YEAR),
			unit:          1,
			want:          time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// Anniversary half-yearly: anchor = start date.
			name:          "anniversary half-yearly: Mar 15, anchor Mar 15 → Sep 15",
			currentPeriod: time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor: time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
			unit:          1,
			want:          time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("currentPeriod: %v, billingAnchor: %v, unit: %d", tt.currentPeriod, tt.billingAnchor, tt.unit)
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart: tt.currentPeriod,
				BillingAnchor:      tt.billingAnchor,
				Unit:               tt.unit,
				Period:             BILLING_PERIOD_HALF_YEAR,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNextBillingDate_Quarterly_Calendar_EndDateCliff exercises the subscriptionEndDate
// clamp branch inside the new early-return guard for QUARTERLY calendar billing.
func TestNextBillingDate_Quarterly_Calendar_EndDateCliff(t *testing.T) {
	tests := []struct {
		name    string
		current time.Time
		anchor  time.Time
		endDate time.Time
		want    time.Time
	}{
		{
			name:    "end date before anchor cliffs to end date",
			current: time.Date(2026, time.March, 22, 0, 0, 0, 0, time.UTC),
			anchor:  time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
			endDate: time.Date(2026, time.March, 28, 0, 0, 0, 0, time.UTC),
			want:    time.Date(2026, time.March, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "end date after anchor returns anchor",
			current: time.Date(2026, time.March, 22, 0, 0, 0, 0, time.UTC),
			anchor:  time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
			endDate: time.Date(2026, time.June, 30, 0, 0, 0, 0, time.UTC),
			want:    time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart:  tt.current,
				BillingAnchor:       tt.anchor,
				Unit:                1,
				Period:              BILLING_PERIOD_QUARTER,
				SubscriptionEndDate: &tt.endDate,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNextBillingDate_HalfYearly_Calendar_EndDateCliff exercises the subscriptionEndDate
// clamp branch inside the new early-return guard for HALF_YEAR calendar billing.
func TestNextBillingDate_HalfYearly_Calendar_EndDateCliff(t *testing.T) {
	tests := []struct {
		name    string
		current time.Time
		anchor  time.Time
		endDate time.Time
		want    time.Time
	}{
		{
			name:    "end date before anchor cliffs to end date",
			current: time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
			anchor:  time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			endDate: time.Date(2026, time.May, 31, 0, 0, 0, 0, time.UTC),
			want:    time.Date(2026, time.May, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:    "end date after anchor returns anchor",
			current: time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
			anchor:  time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			endDate: time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC),
			want:    time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart:  tt.current,
				BillingAnchor:       tt.anchor,
				Unit:                1,
				Period:              BILLING_PERIOD_HALF_YEAR,
				SubscriptionEndDate: &tt.endDate,
			})
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

func TestCalculatePeriodID(t *testing.T) {
	// Define common test values
	billingAnchor := time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)
	subscriptionStart := time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)
	currentPeriodStart := time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)
	currentPeriodEnd := time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC)
	periodUnit := 1
	periodType := BILLING_PERIOD_MONTHLY

	// Helper function to convert time to period ID
	expectedPeriodID := func(t time.Time) uint64 {
		return uint64(t.Unix() * 1000)
	}

	tests := []struct {
		name           string
		eventTimestamp time.Time
		subStart       time.Time
		periodStart    time.Time
		periodEnd      time.Time
		anchor         time.Time
		unit           int
		period         BillingPeriod
		want           uint64
		wantErr        bool
		errContains    string
	}{
		{
			name:           "Event in current period",
			eventTimestamp: time.Date(2024, time.January, 20, 0, 0, 0, 0, time.UTC),
			subStart:       subscriptionStart,
			periodStart:    currentPeriodStart,
			periodEnd:      currentPeriodEnd,
			anchor:         billingAnchor,
			unit:           periodUnit,
			period:         periodType,
			want:           expectedPeriodID(currentPeriodStart),
			wantErr:        false,
		},
		{
			name:           "Event at period start",
			eventTimestamp: currentPeriodStart,
			subStart:       subscriptionStart,
			periodStart:    currentPeriodStart,
			periodEnd:      currentPeriodEnd,
			anchor:         billingAnchor,
			unit:           periodUnit,
			period:         periodType,
			want:           expectedPeriodID(currentPeriodStart),
			wantErr:        false,
		},
		{
			name:           "Event right before period end",
			eventTimestamp: currentPeriodEnd.Add(-time.Second),
			subStart:       subscriptionStart,
			periodStart:    currentPeriodStart,
			periodEnd:      currentPeriodEnd,
			anchor:         billingAnchor,
			unit:           periodUnit,
			period:         periodType,
			want:           expectedPeriodID(currentPeriodStart),
			wantErr:        false,
		},
		{
			name:           "Event before subscription start",
			eventTimestamp: subscriptionStart.Add(-24 * time.Hour),
			subStart:       subscriptionStart,
			periodStart:    currentPeriodStart,
			periodEnd:      currentPeriodEnd,
			anchor:         billingAnchor,
			unit:           periodUnit,
			period:         periodType,
			want:           0,
			wantErr:        true,
			errContains:    "event timestamp is before subscription start date",
		},
		{
			name:           "Event before current period start",
			eventTimestamp: currentPeriodStart.Add(-time.Hour),
			subStart:       subscriptionStart.Add(-48 * time.Hour), // Sub started earlier
			periodStart:    currentPeriodStart,
			periodEnd:      currentPeriodEnd,
			anchor:         billingAnchor,
			unit:           periodUnit,
			period:         periodType,
			// The event is 1 hour before current period start, but since sub started 48h earlier,
			// the first period runs from sub start (Jan 13) to the first billing date (Feb 15)
			// So the event at Jan 14 23:00 falls in the first period starting Jan 13
			want:    expectedPeriodID(subscriptionStart.Add(-48 * time.Hour)),
			wantErr: false,
		},
		{
			name:           "Event in next period",
			eventTimestamp: currentPeriodEnd.Add(time.Hour), // Just after current period
			subStart:       subscriptionStart,
			periodStart:    currentPeriodStart,
			periodEnd:      currentPeriodEnd,
			anchor:         billingAnchor,
			unit:           periodUnit,
			period:         periodType,
			want:           expectedPeriodID(currentPeriodEnd), // Next period starts at current period end
			wantErr:        false,
		},
		{
			name:           "Event in future period (2 periods ahead)",
			eventTimestamp: time.Date(2024, time.March, 20, 0, 0, 0, 0, time.UTC),
			subStart:       subscriptionStart,
			periodStart:    currentPeriodStart,
			periodEnd:      currentPeriodEnd,
			anchor:         billingAnchor,
			unit:           periodUnit,
			period:         periodType,
			want:           expectedPeriodID(time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Weekly billing period",
			eventTimestamp: time.Date(2024, time.January, 25, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.January, 22, 0, 0, 0, 0, time.UTC),
			periodEnd:      time.Date(2024, time.January, 29, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_WEEKLY,
			want:           expectedPeriodID(time.Date(2024, time.January, 22, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Annual billing period",
			eventTimestamp: time.Date(2024, time.June, 15, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodEnd:      time.Date(2025, time.January, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_ANNUAL,
			want:           expectedPeriodID(time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Different timezone (IST)",
			eventTimestamp: time.Date(2024, time.January, 20, 5, 30, 0, 0, ist),
			subStart:       time.Date(2024, time.January, 15, 5, 30, 0, 0, ist),
			periodStart:    time.Date(2024, time.January, 15, 5, 30, 0, 0, ist),
			periodEnd:      time.Date(2024, time.February, 15, 5, 30, 0, 0, ist),
			anchor:         time.Date(2024, time.January, 15, 5, 30, 0, 0, ist),
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			want:           expectedPeriodID(time.Date(2024, time.January, 15, 5, 30, 0, 0, ist)),
			wantErr:        false,
		},
		{
			name:           "Past event processing - 3 months back from current period",
			eventTimestamp: time.Date(2023, time.November, 20, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2023, time.October, 15, 0, 0, 0, 0, time.UTC), // Sub started even earlier
			periodStart:    time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2023, time.October, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			// Event should fall in Nov 15 - Dec 15 period
			want:    expectedPeriodID(time.Date(2023, time.November, 15, 0, 0, 0, 0, time.UTC)),
			wantErr: false,
		},
		{
			name:           "Past event processing - weekly billing",
			eventTimestamp: time.Date(2024, time.January, 10, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2024, time.January, 22, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_WEEKLY,
			// Event should fall in Jan 8 - Jan 15 period
			want:    expectedPeriodID(time.Date(2024, time.January, 8, 0, 0, 0, 0, time.UTC)),
			wantErr: false,
		},
		// ===== MONTHLY BILLING - COMPREHENSIVE PAST EVENT TESTS =====
		{
			name:           "Monthly - Event 1 month back",
			eventTimestamp: time.Date(2024, time.February, 20, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2024, time.April, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			want:           expectedPeriodID(time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Monthly - Event 2 months back",
			eventTimestamp: time.Date(2024, time.January, 20, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2024, time.April, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			want:           expectedPeriodID(time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Monthly - Event at exact period boundary (start)",
			eventTimestamp: time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2024, time.April, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			want:           expectedPeriodID(time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Monthly - Event just before period boundary",
			eventTimestamp: time.Date(2024, time.March, 14, 23, 59, 59, 0, time.UTC),
			subStart:       time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2024, time.April, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			want:           expectedPeriodID(time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Monthly - Month-end billing anchor with February",
			eventTimestamp: time.Date(2024, time.February, 20, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2024, time.April, 30, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			// Feb only has 29 days in 2024 (leap year), so period runs Jan 31 - Feb 29
			want:    expectedPeriodID(time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC)),
			wantErr: false,
		},
		{
			name:           "Monthly - Cross year boundary past event",
			eventTimestamp: time.Date(2023, time.December, 20, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2023, time.November, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2023, time.November, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			want:           expectedPeriodID(time.Date(2023, time.December, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Monthly - Bi-monthly billing (2 month unit)",
			eventTimestamp: time.Date(2024, time.February, 20, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.May, 15, 0, 0, 0, 0, time.UTC), // Current period (every 2 months)
			periodEnd:      time.Date(2024, time.July, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           2,
			period:         BILLING_PERIOD_MONTHLY,
			// Periods: Jan 15 - Mar 15, Mar 15 - May 15, May 15 - July 15
			want:    expectedPeriodID(time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)),
			wantErr: false,
		},
		{
			name:           "Monthly - Different timezone (PST)",
			eventTimestamp: time.Date(2024, time.February, 20, 5, 30, 0, 0, pst),
			subStart:       time.Date(2024, time.January, 15, 10, 0, 0, 0, pst),
			periodStart:    time.Date(2024, time.March, 15, 10, 0, 0, 0, pst), // Current period
			periodEnd:      time.Date(2024, time.April, 15, 10, 0, 0, 0, pst),
			anchor:         time.Date(2024, time.January, 15, 10, 0, 0, 0, pst),
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			want:           expectedPeriodID(time.Date(2024, time.February, 15, 10, 0, 0, 0, pst)),
			wantErr:        false,
		},
		// ===== ANNUAL BILLING - COMPREHENSIVE PAST EVENT TESTS =====
		{
			name:           "Annual - Event 1 year back",
			eventTimestamp: time.Date(2023, time.June, 15, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2023, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2025, time.January, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2023, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_ANNUAL,
			want:           expectedPeriodID(time.Date(2023, time.January, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Annual - Event 2 years back",
			eventTimestamp: time.Date(2022, time.June, 15, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2022, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2025, time.January, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2022, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_ANNUAL,
			want:           expectedPeriodID(time.Date(2022, time.January, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Annual - Leap year anchor, non-leap year event",
			eventTimestamp: time.Date(2023, time.February, 28, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2020, time.February, 29, 0, 0, 0, 0, time.UTC), // Leap year start
			periodStart:    time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC), // Current period (leap year)
			periodEnd:      time.Date(2025, time.February, 28, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2020, time.February, 29, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_ANNUAL,
			// 2023 is non-leap year, so period runs from 2023-02-28 to 2024-02-29
			want:    expectedPeriodID(time.Date(2023, time.February, 28, 0, 0, 0, 0, time.UTC)),
			wantErr: false,
		},
		{
			name:           "Annual - Bi-annual billing (2 year unit)",
			eventTimestamp: time.Date(2023, time.June, 15, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2022, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC), // Current period (every 2 years)
			periodEnd:      time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2022, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           2,
			period:         BILLING_PERIOD_ANNUAL,
			// Periods: 2022-01-15 to 2024-01-15, 2024-01-15 to 2026-01-15
			want:    expectedPeriodID(time.Date(2022, time.January, 15, 0, 0, 0, 0, time.UTC)),
			wantErr: false,
		},
		{
			name:           "Annual - Different timezone (JST)",
			eventTimestamp: time.Date(2023, time.June, 15, 12, 30, 0, 0, jst),
			subStart:       time.Date(2023, time.January, 15, 9, 0, 0, 0, jst),
			periodStart:    time.Date(2024, time.January, 15, 9, 0, 0, 0, jst), // Current period
			periodEnd:      time.Date(2025, time.January, 15, 9, 0, 0, 0, jst),
			anchor:         time.Date(2023, time.January, 15, 9, 0, 0, 0, jst),
			unit:           1,
			period:         BILLING_PERIOD_ANNUAL,
			want:           expectedPeriodID(time.Date(2023, time.January, 15, 9, 0, 0, 0, jst)),
			wantErr:        false,
		},
		// ===== QUARTERLY BILLING PAST EVENT TESTS =====
		{
			name:           "Quarterly - Event 1 quarter back",
			eventTimestamp: time.Date(2024, time.February, 20, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.April, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2024, time.July, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_QUARTER,
			want:           expectedPeriodID(time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Quarterly - Event 2 quarters back",
			eventTimestamp: time.Date(2023, time.November, 20, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2023, time.October, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.April, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2024, time.July, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2023, time.October, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_QUARTER,
			// Periods: Oct 15 - Jan 15, Jan 15 - Apr 15, Apr 15 - Jul 15
			want:    expectedPeriodID(time.Date(2023, time.October, 15, 0, 0, 0, 0, time.UTC)),
			wantErr: false,
		},
		// ===== EDGE CASES =====
		{
			name:           "Edge case - Event at subscription start",
			eventTimestamp: time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			want:           expectedPeriodID(time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Edge case - Event 1 second after subscription start",
			eventTimestamp: time.Date(2024, time.January, 15, 0, 0, 1, 0, time.UTC),
			subStart:       time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			want:           expectedPeriodID(time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Edge case - Event microseconds before period end",
			eventTimestamp: time.Date(2024, time.February, 14, 23, 59, 59, 999999000, time.UTC),
			subStart:       time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC), // Current period
			periodEnd:      time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			want:           expectedPeriodID(time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)),
			wantErr:        false,
		},
		{
			name:           "Edge case - Multiple period units with past event",
			eventTimestamp: time.Date(2024, time.March, 20, 0, 0, 0, 0, time.UTC),
			subStart:       time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			periodStart:    time.Date(2024, time.July, 15, 0, 0, 0, 0, time.UTC), // Current period (every 3 months)
			periodEnd:      time.Date(2024, time.October, 15, 0, 0, 0, 0, time.UTC),
			anchor:         time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			unit:           3,
			period:         BILLING_PERIOD_MONTHLY,
			// Periods: Jan 15 - Apr 15, Apr 15 - Jul 15, Jul 15 - Oct 15
			want:    expectedPeriodID(time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculatePeriodID(tt.eventTimestamp, tt.subStart, tt.periodStart, tt.periodEnd, tt.anchor, tt.unit, tt.period, "")
			if tt.wantErr {
				if err == nil {
					t.Errorf("CalculatePeriodID() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if !contains(err.Error(), tt.errContains) {
					t.Errorf("CalculatePeriodID() error = %v, want to contain %v", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("CalculatePeriodID() unexpected error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("CalculatePeriodID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBetween(t *testing.T) {
	tests := []struct {
		name      string
		timestamp time.Time
		start     time.Time
		end       time.Time
		want      bool
	}{
		{
			name:      "Timestamp equal to start",
			timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			start:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:       time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			want:      true,
		},
		{
			name:      "Timestamp between start and end",
			timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			start:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:       time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			want:      true,
		},
		{
			name:      "Timestamp right before end",
			timestamp: time.Date(2024, 1, 1, 23, 59, 59, 999999999, time.UTC),
			start:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:       time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			want:      true,
		},
		{
			name:      "Timestamp equal to end",
			timestamp: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			start:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:       time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			want:      false, // End is exclusive
		},
		{
			name:      "Timestamp before start",
			timestamp: time.Date(2023, 12, 31, 23, 59, 59, 0, time.UTC),
			start:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:       time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			want:      false,
		},
		{
			name:      "Timestamp after end",
			timestamp: time.Date(2024, 1, 2, 0, 0, 1, 0, time.UTC),
			start:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:       time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBetween(tt.timestamp, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("isBetween() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculatePeriodID_Simple(t *testing.T) {
	tests := []struct {
		name  string
		input time.Time
		want  uint64
	}{
		{
			name:  "Unix epoch",
			input: time.Unix(0, 0).UTC(),
			want:  0,
		},
		{
			name:  "1 second after epoch",
			input: time.Unix(1, 0).UTC(),
			want:  1000,
		},
		{
			name:  "January 1, 2024",
			input: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			want:  1704067200000,
		},
		{
			name:  "With time component",
			input: time.Date(2024, 3, 15, 12, 30, 45, 0, time.UTC),
			want:  1710505845000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculatePeriodID(tt.input)
			if got != tt.want {
				t.Errorf("calculatePeriodID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetNextUsageResetAt_Never(t *testing.T) {
	currentTime := time.Date(2024, time.March, 15, 12, 30, 0, 0, time.UTC)
	subscriptionStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	billingAnchor := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)

	resetTime, err := GetNextUsageResetAt(
		currentTime,
		subscriptionStart,
		nil,
		billingAnchor,
		ENTITLEMENT_USAGE_RESET_PERIOD_NEVER,
		"",
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resetTime.IsZero() {
		t.Errorf("expected zero time for NEVER reset period, got %v", resetTime)
	}
}

func TestGetNextUsageResetAt_Daily(t *testing.T) {
	tests := []struct {
		name              string
		currentTime       time.Time
		subscriptionStart time.Time
		billingAnchor     time.Time
		subscriptionEnd   *time.Time
		want              time.Time
		wantErr           bool
	}{
		{
			name:              "simple daily reset - UTC",
			currentTime:       time.Date(2024, time.March, 15, 12, 30, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.March, 16, 0, 0, 0, 0, time.UTC),
			wantErr:           false,
		},
		{
			name:              "daily reset - crossing month boundary",
			currentTime:       time.Date(2024, time.February, 29, 23, 59, 59, 0, time.UTC), // leap year
			subscriptionStart: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			wantErr:           false,
		},
		{
			name:              "daily reset - crossing year boundary",
			currentTime:       time.Date(2024, time.December, 31, 12, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			wantErr:           false,
		},
		{
			name:              "daily reset - with subscription end cliffing",
			currentTime:       time.Date(2024, time.March, 15, 12, 30, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   timePtr(time.Date(2024, time.March, 15, 18, 0, 0, 0, time.UTC)),
			want:              time.Date(2024, time.March, 15, 18, 0, 0, 0, time.UTC), // cliffed
			wantErr:           false,
		},
		{
			name:              "daily reset - IST timezone",
			currentTime:       time.Date(2024, time.March, 15, 12, 30, 0, 0, ist),
			subscriptionStart: time.Date(2024, time.January, 1, 0, 0, 0, 0, ist),
			billingAnchor:     time.Date(2024, time.January, 1, 5, 30, 0, 0, ist),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.March, 16, 0, 0, 0, 0, ist),
			wantErr:           false,
		},
		{
			name:              "daily reset - PST timezone with DST considerations",
			currentTime:       time.Date(2024, time.March, 15, 12, 30, 0, 0, pst),
			subscriptionStart: time.Date(2024, time.January, 1, 0, 0, 0, 0, pst),
			billingAnchor:     time.Date(2024, time.January, 1, 8, 0, 0, 0, pst),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.March, 16, 0, 0, 0, 0, pst),
			wantErr:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetNextUsageResetAt(
				tt.currentTime,
				tt.subscriptionStart,
				tt.subscriptionEnd,
				tt.billingAnchor,
				ENTITLEMENT_USAGE_RESET_PERIOD_DAILY,
				"",
			)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
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

func TestGetNextUsageResetAt_Monthly(t *testing.T) {
	tests := []struct {
		name              string
		currentTime       time.Time
		subscriptionStart time.Time
		billingAnchor     time.Time
		subscriptionEnd   *time.Time
		timezone          string
		want              time.Time
		wantErr           bool
	}{
		{
			name:              "monthly reset - subscription starts 5th Jan, current 10th Jan",
			currentTime:       time.Date(2024, time.January, 10, 12, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.February, 5, 0, 0, 0, 0, time.UTC),
			wantErr:           false,
		},
		{
			name:              "monthly reset - subscription starts 5th Jan, current 15th Feb",
			currentTime:       time.Date(2024, time.February, 15, 14, 30, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.March, 5, 0, 0, 0, 0, time.UTC),
			wantErr:           false,
		},
		{
			name:              "monthly reset - subscription starts 5th Jan, current 19th Oct",
			currentTime:       time.Date(2024, time.October, 19, 9, 15, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.November, 5, 0, 0, 0, 0, time.UTC),
			wantErr:           false,
		},
		{
			name:              "monthly reset - subscription starts 5th Jan, billing anchor 1st Feb",
			currentTime:       time.Date(2024, time.January, 10, 12, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
			wantErr:           false,
		},
		{
			name:              "monthly reset - subscription starts 5th Jan, billing anchor 1st Feb, current 15th Feb",
			currentTime:       time.Date(2024, time.February, 15, 14, 30, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			wantErr:           false,
		},
		{
			name:              "monthly reset - month-end billing anchor with February leap year",
			currentTime:       time.Date(2024, time.February, 15, 12, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC), // Feb 29 in leap year
			wantErr:           false,
		},
		{
			name:              "monthly reset - month-end billing anchor with February non-leap year",
			currentTime:       time.Date(2025, time.February, 15, 12, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2025, time.January, 31, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2025, time.January, 31, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2025, time.February, 28, 0, 0, 0, 0, time.UTC), // Feb 28 in non-leap year
			wantErr:           false,
		},
		{
			name:              "monthly reset - with subscription end cliffing",
			currentTime:       time.Date(2024, time.March, 15, 12, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   timePtr(time.Date(2024, time.March, 20, 0, 0, 0, 0, time.UTC)),
			want:              time.Date(2024, time.March, 20, 0, 0, 0, 0, time.UTC), // cliffed to subscription end
			wantErr:           false,
		},
		{
			name:              "monthly reset - current time at period boundary (start)",
			currentTime:       time.Date(2024, time.February, 5, 0, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.March, 5, 0, 0, 0, 0, time.UTC),
			wantErr:           false,
		},
		{
			name:              "monthly reset - current time just before period boundary",
			currentTime:       time.Date(2024, time.March, 4, 23, 59, 59, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.March, 5, 0, 0, 0, 0, time.UTC),
			wantErr:           false,
		},
		{
			name:              "monthly reset - cross year boundary",
			currentTime:       time.Date(2024, time.December, 15, 12, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.November, 5, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.November, 5, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2025, time.January, 5, 0, 0, 0, 0, time.UTC),
			wantErr:           false,
		},
		{
			name:              "monthly reset - IST timezone",
			currentTime:       time.Date(2024, time.March, 15, 14, 30, 0, 0, ist),
			subscriptionStart: time.Date(2024, time.January, 5, 5, 30, 0, 0, ist),
			billingAnchor:     time.Date(2024, time.January, 5, 5, 30, 0, 0, ist),
			subscriptionEnd:   nil,
			timezone:          "Asia/Kolkata",
			want:              time.Date(2024, time.April, 5, 0, 0, 0, 0, ist), // Reset time always at 00:00:00
			wantErr:           false,
		},
		{
			name:              "monthly reset - PST timezone",
			currentTime:       time.Date(2024, time.March, 15, 14, 30, 0, 0, pst),
			subscriptionStart: time.Date(2024, time.January, 5, 8, 0, 0, 0, pst),
			billingAnchor:     time.Date(2024, time.January, 5, 8, 0, 0, 0, pst),
			subscriptionEnd:   nil,
			timezone:          "America/Los_Angeles",
			want:              time.Date(2024, time.April, 5, 0, 0, 0, 0, pst),
			wantErr:           false,
		},
		{
			name:              "monthly reset - subscription start before current time by several months",
			currentTime:       time.Date(2024, time.June, 10, 12, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2023, time.December, 15, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2023, time.December, 15, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.June, 15, 0, 0, 0, 0, time.UTC),
			wantErr:           false,
		},
		{
			name:              "monthly reset - current time exactly at subscription start",
			currentTime:       time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			want:              time.Date(2024, time.February, 5, 0, 0, 0, 0, time.UTC),
			wantErr:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetNextUsageResetAt(
				tt.currentTime,
				tt.subscriptionStart,
				tt.subscriptionEnd,
				tt.billingAnchor,
				ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
				tt.timezone,
			)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
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

func TestGetNextUsageResetAt_UnsupportedPeriod(t *testing.T) {
	currentTime := time.Date(2024, time.March, 15, 12, 30, 0, 0, time.UTC)
	subscriptionStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	billingAnchor := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)

	// Test with an unsupported period (WEEKLY is available but not implemented in our simplified version)
	_, err := GetNextUsageResetAt(
		currentTime,
		subscriptionStart,
		nil,
		billingAnchor,
		ENTITLEMENT_USAGE_RESET_PERIOD_WEEKLY, // This should trigger the default case
		"",
	)

	if err == nil {
		t.Errorf("expected error for unsupported period, got nil")
	}

	if !contains(err.Error(), "unsupported entitlement usage reset period") {
		t.Errorf("expected error message to contain 'unsupported entitlement usage reset period', got %v", err)
	}
}

func TestGetNextUsageResetAt_EdgeCases(t *testing.T) {
	tests := []struct {
		name              string
		currentTime       time.Time
		subscriptionStart time.Time
		billingAnchor     time.Time
		subscriptionEnd   *time.Time
		resetPeriod       EntitlementUsageResetPeriod
		want              time.Time
		wantErr           bool
		errContains       string
	}{
		{
			name:              "monthly reset - current time way before subscription start should error",
			currentTime:       time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			resetPeriod:       ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
			wantErr:           true,
			errContains:       "failed to find monthly reset period",
		},
		{
			name:              "daily reset - current time equals subscription end",
			currentTime:       time.Date(2024, time.March, 15, 12, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   timePtr(time.Date(2024, time.March, 15, 12, 0, 0, 0, time.UTC)),
			resetPeriod:       ENTITLEMENT_USAGE_RESET_PERIOD_DAILY,
			want:              time.Date(2024, time.March, 15, 12, 0, 0, 0, time.UTC), // cliffed to subscription end
			wantErr:           false,
		},
		{
			name:              "monthly reset - current time equals subscription end",
			currentTime:       time.Date(2024, time.March, 15, 12, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   timePtr(time.Date(2024, time.March, 15, 12, 0, 0, 0, time.UTC)),
			resetPeriod:       ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
			want:              time.Date(2024, time.March, 15, 12, 0, 0, 0, time.UTC), // cliffed to subscription end
			wantErr:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetNextUsageResetAt(
				tt.currentTime,
				tt.subscriptionStart,
				tt.subscriptionEnd,
				tt.billingAnchor,
				tt.resetPeriod,
				"",
			)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("expected error to contain %q, got %v", tt.errContains, err)
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

// Helper function for creating time pointers in tests
func timePtr(t time.Time) *time.Time {
	return &t
}

// TestCalculateBillingPeriods_Timezone verifies that CalculateBillingPeriods generates
// correct period boundaries for IST and EST (with DST) customers in both anniversary
// and calendar billing modes.
func TestCalculateBillingPeriods_Timezone(t *testing.T) {
	// Pre-compute the IST calendar anchor for the "IST calendar, monthly" case.
	// subStart = 2024-02-15T18:30:00Z  (IST midnight Feb 16, 2024)
	// CalculateCalendarBillingAnchor works in UTC and returns start of next month:
	// next month of February = March → 2024-03-01T00:00:00Z
	istCalSubStart := time.Date(2024, time.February, 15, 18, 30, 0, 0, time.UTC)
	istCalAnchor := CalculateCalendarBillingAnchor(istCalSubStart, BILLING_PERIOD_MONTHLY)
	// Expected: 2024-03-01T00:00:00Z
	_ = istCalAnchor

	tests := []struct {
		name                string
		initialPeriodStart  time.Time
		endDate             *time.Time
		anchor              time.Time
		periodCount         int
		billingPeriod       BillingPeriod
		timezone            string
		wantPeriodCount     int
		wantFirstPeriodEnd  time.Time
		wantSecondPeriodEnd time.Time
	}{
		{
			// UTC anniversary, monthly: 2 full periods March–May 2024.
			name:                "UTC anniversary monthly",
			initialPeriodStart:  time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			endDate:             timePtr(time.Date(2024, time.May, 1, 0, 0, 0, 0, time.UTC)),
			anchor:              time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			periodCount:         1,
			billingPeriod:       BILLING_PERIOD_MONTHLY,
			timezone:            "",
			wantPeriodCount:     2,
			wantFirstPeriodEnd:  time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC),
			wantSecondPeriodEnd: time.Date(2024, time.May, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// IST anniversary, monthly.
			// subStart = IST midnight March 1, 2024 = 2024-02-29T18:30:00Z
			// Period 1: [Feb29 18:30Z, Mar31 18:30Z)  (IST March 1 → April 1)
			// Period 2: [Mar31 18:30Z, Apr30 18:30Z)  (IST April 1 → May 1)
			// endDate = IST midnight May 1 = 2024-04-30T18:30:00Z
			name:                "IST anniversary monthly",
			initialPeriodStart:  time.Date(2024, time.February, 29, 18, 30, 0, 0, time.UTC),
			endDate:             timePtr(time.Date(2024, time.April, 30, 18, 30, 0, 0, time.UTC)),
			anchor:              time.Date(2024, time.February, 29, 18, 30, 0, 0, time.UTC),
			periodCount:         1,
			billingPeriod:       BILLING_PERIOD_MONTHLY,
			timezone:            "Asia/Kolkata",
			wantPeriodCount:     2,
			wantFirstPeriodEnd:  time.Date(2024, time.March, 31, 18, 30, 0, 0, time.UTC),
			wantSecondPeriodEnd: time.Date(2024, time.April, 30, 18, 30, 0, 0, time.UTC),
		},
		{
			// IST calendar, monthly.
			// subStart = IST midnight Feb 16 = 2024-02-15T18:30:00Z
			// calendar anchor = 2024-03-01T00:00:00Z (CalculateCalendarBillingAnchor returns UTC start-of-month)
			// In IST the anchor is March 1 05:30 IST (day=1). subStart in IST is Feb 16.
			// NextBillingDate(Feb15 18:30Z, anchor=Mar01 00:00Z, tz=IST):
			//   localStart = Feb 16 00:00 IST, localAnchor = Mar 1 05:30 IST → anchor.Day() = 1
			//   d=16, clampedAnchorD=1; 16 < 1? No → advance by 1 month from Feb:
			//   time.Date(2024, March, 1, 5, 30, 0, 0, IST) = 2024-03-01T00:00:00Z (UTC)
			// So first period [Feb15 18:30Z, Mar01 00:00Z) = partial period in UTC terms
			// Second period starts Mar01 00:00Z, nextEnd = Apr01 05:30 IST = 2024-04-01T00:00:00Z
			// endDate = IST midnight May 1 = 2024-04-30T18:30:00Z → 3 periods total
			name:                "IST calendar monthly",
			initialPeriodStart:  istCalSubStart, // 2024-02-15T18:30:00Z
			endDate:             timePtr(time.Date(2024, time.April, 30, 18, 30, 0, 0, time.UTC)),
			anchor:              istCalAnchor, // 2024-03-01T00:00:00Z
			periodCount:         1,
			billingPeriod:       BILLING_PERIOD_MONTHLY,
			timezone:            "Asia/Kolkata",
			wantPeriodCount:     3,
			wantFirstPeriodEnd:  time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),  // anchor boundary in UTC
			wantSecondPeriodEnd: time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC),  // one month later in UTC
		},
		{
			// EST anniversary, monthly — spans the DST transition (March 10, 2024).
			// subStart = EST midnight Feb 1 = 2024-02-01T05:00:00Z
			// Period 1: [Feb01 05:00Z, Mar01 05:00Z)  (EST: Feb 1 → March 1; still winter)
			// Period 2: [Mar01 05:00Z, Apr01 04:00Z)  (DST springs forward March 10; April 1 is EDT = UTC-4)
			// endDate = EDT midnight April 1 = 2024-04-01T04:00:00Z
			name:                "EST anniversary monthly DST",
			initialPeriodStart:  time.Date(2024, time.February, 1, 5, 0, 0, 0, time.UTC),
			endDate:             timePtr(time.Date(2024, time.April, 1, 4, 0, 0, 0, time.UTC)),
			anchor:              time.Date(2024, time.February, 1, 5, 0, 0, 0, time.UTC),
			periodCount:         1,
			billingPeriod:       BILLING_PERIOD_MONTHLY,
			timezone:            "America/New_York",
			wantPeriodCount:     2,
			wantFirstPeriodEnd:  time.Date(2024, time.March, 1, 5, 0, 0, 0, time.UTC),   // EST midnight March 1
			wantSecondPeriodEnd: time.Date(2024, time.April, 1, 4, 0, 0, 0, time.UTC),   // EDT midnight April 1
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			periods, err := CalculateBillingPeriods(
				tc.initialPeriodStart,
				tc.endDate,
				tc.anchor,
				tc.periodCount,
				tc.billingPeriod,
				tc.timezone,
			)
			require.NoError(t, err)
			require.Lenf(t, periods, tc.wantPeriodCount,
				"got %d periods: %v", len(periods), periods)
			require.Truef(t, periods[0].End.Equal(tc.wantFirstPeriodEnd),
				"periods[0].End: got %v, want %v", periods[0].End, tc.wantFirstPeriodEnd)
			require.Truef(t, periods[1].End.Equal(tc.wantSecondPeriodEnd),
				"periods[1].End: got %v, want %v", periods[1].End, tc.wantSecondPeriodEnd)
		})
	}
}

// TestFindPeriodForDate_Timezone verifies that FindPeriodForDate locates the correct
// billing period for IST and EST customers, including the fast-path case and DST crossing.
func TestFindPeriodForDate_Timezone(t *testing.T) {
	tests := []struct {
		name          string
		target        time.Time
		knownStart    time.Time
		knownEnd      time.Time
		anchor        time.Time
		periodCount   int
		billingPeriod BillingPeriod
		timezone      string
		wantStart     time.Time
		wantEnd       time.Time
	}{
		{
			// UTC — target falls inside the known period; fast path returns it unchanged.
			name:          "UTC target in known period (fast path)",
			target:        time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC),
			knownStart:    time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			knownEnd:      time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC),
			anchor:        time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			periodCount:   1,
			billingPeriod: BILLING_PERIOD_MONTHLY,
			timezone:      "",
			wantStart:     time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			wantEnd:       time.Date(2024, time.April, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// IST anniversary — target is 1 month ahead of the known period.
			// knownStart = IST midnight March 1 = 2024-02-29T18:30:00Z
			// knownEnd   = IST midnight April 1 = 2024-03-31T18:30:00Z
			// target     = April 15 UTC — falls in [Mar31 18:30Z, Apr30 18:30Z)
			// Expected: IST April 1 → May 1
			name:          "IST anniversary target 1 month ahead",
			target:        time.Date(2024, time.April, 15, 0, 0, 0, 0, time.UTC),
			knownStart:    time.Date(2024, time.February, 29, 18, 30, 0, 0, time.UTC),
			knownEnd:      time.Date(2024, time.March, 31, 18, 30, 0, 0, time.UTC),
			anchor:        time.Date(2024, time.February, 29, 18, 30, 0, 0, time.UTC),
			periodCount:   1,
			billingPeriod: BILLING_PERIOD_MONTHLY,
			timezone:      "Asia/Kolkata",
			wantStart:     time.Date(2024, time.March, 31, 18, 30, 0, 0, time.UTC),
			wantEnd:       time.Date(2024, time.April, 30, 18, 30, 0, 0, time.UTC),
		},
		{
			// IST calendar — target in the partial first period (fast path).
			// knownStart = IST midnight Feb 16 = 2024-02-15T18:30:00Z
			// knownEnd   = IST midnight March 1 = 2024-02-29T18:30:00Z
			// target     = Feb 20 UTC → inside the partial period
			name:          "IST calendar target in first partial period (fast path)",
			target:        time.Date(2024, time.February, 20, 0, 0, 0, 0, time.UTC),
			knownStart:    time.Date(2024, time.February, 15, 18, 30, 0, 0, time.UTC),
			knownEnd:      time.Date(2024, time.February, 29, 18, 30, 0, 0, time.UTC),
			anchor:        time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC), // calendar anchor
			periodCount:   1,
			billingPeriod: BILLING_PERIOD_MONTHLY,
			timezone:      "Asia/Kolkata",
			wantStart:     time.Date(2024, time.February, 15, 18, 30, 0, 0, time.UTC),
			wantEnd:       time.Date(2024, time.February, 29, 18, 30, 0, 0, time.UTC),
		},
		{
			// EST anniversary — target crosses into the DST period.
			// knownStart = EST midnight Feb 1 = 2024-02-01T05:00:00Z
			// knownEnd   = EST midnight March 1 = 2024-03-01T05:00:00Z
			// target     = March 15 UTC → falls in [Mar01 05:00Z, Apr01 04:00Z)
			// DST starts March 10, so April 1 midnight EDT = 2024-04-01T04:00:00Z
			name:          "EST anniversary target crosses DST boundary",
			target:        time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC),
			knownStart:    time.Date(2024, time.February, 1, 5, 0, 0, 0, time.UTC),
			knownEnd:      time.Date(2024, time.March, 1, 5, 0, 0, 0, time.UTC),
			anchor:        time.Date(2024, time.February, 1, 5, 0, 0, 0, time.UTC),
			periodCount:   1,
			billingPeriod: BILLING_PERIOD_MONTHLY,
			timezone:      "America/New_York",
			wantStart:     time.Date(2024, time.March, 1, 5, 0, 0, 0, time.UTC),
			wantEnd:       time.Date(2024, time.April, 1, 4, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			period, err := FindPeriodForDate(
				tc.target,
				tc.knownStart,
				tc.knownEnd,
				tc.anchor,
				tc.periodCount,
				tc.billingPeriod,
				tc.timezone,
			)
			require.NoError(t, err)
			require.Truef(t, period.Start.Equal(tc.wantStart),
				"period.Start: got %v, want %v", period.Start, tc.wantStart)
			require.Truef(t, period.End.Equal(tc.wantEnd),
				"period.End: got %v, want %v", period.End, tc.wantEnd)
		})
	}
}

// TestCalculatePeriodID_Timezone verifies that CalculatePeriodID maps UTC event
// timestamps to the correct period ID for IST and EST (DST) customers.
func TestCalculatePeriodID_Timezone(t *testing.T) {
	expectedPeriodID := func(t time.Time) uint64 {
		return uint64(t.Unix() * 1000)
	}

	// IST anniversary sub start = IST midnight March 1 = 2024-02-29T18:30:00Z
	istSubStart := time.Date(2024, time.February, 29, 18, 30, 0, 0, time.UTC)
	// IST period [March 1 IST, April 1 IST) = [Feb29 18:30Z, Mar31 18:30Z)
	istPeriodStart := istSubStart
	istPeriodEnd := time.Date(2024, time.March, 31, 18, 30, 0, 0, time.UTC)

	// EST anniversary sub start = EST midnight Feb 1 = 2024-02-01T05:00:00Z
	estSubStart := time.Date(2024, time.February, 1, 5, 0, 0, 0, time.UTC)
	// EST period [Feb 1 EST, March 1 EST) = [Feb01 05:00Z, Mar01 05:00Z)
	estPeriodStart := estSubStart
	estPeriodEnd := time.Date(2024, time.March, 1, 5, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		eventTimestamp time.Time
		subStart       time.Time
		periodStart    time.Time
		periodEnd      time.Time
		anchor         time.Time
		unit           int
		period         BillingPeriod
		timezone       string
		wantPeriodID   uint64
		wantErr        bool
	}{
		{
			// IST anniversary — event clearly inside the period.
			name:           "IST anniversary event in current period",
			eventTimestamp: time.Date(2024, time.March, 15, 12, 0, 0, 0, time.UTC),
			subStart:       istSubStart,
			periodStart:    istPeriodStart,
			periodEnd:      istPeriodEnd,
			anchor:         istSubStart,
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			timezone:       "Asia/Kolkata",
			wantPeriodID:   expectedPeriodID(istPeriodStart),
		},
		{
			// IST anniversary — event at 12:00 UTC on March 31 = 17:30 IST March 31,
			// which is before IST midnight April 1 (= 18:30 UTC March 31).
			// So this event is still within the current period [Feb29 18:30Z, Mar31 18:30Z).
			name:           "IST anniversary event just before period end in IST",
			eventTimestamp: time.Date(2024, time.March, 31, 12, 0, 0, 0, time.UTC),
			subStart:       istSubStart,
			periodStart:    istPeriodStart,
			periodEnd:      istPeriodEnd,
			anchor:         istSubStart,
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			timezone:       "Asia/Kolkata",
			wantPeriodID:   expectedPeriodID(istPeriodStart),
		},
		{
			// EST anniversary — event inside the Feb 1–March 1 period.
			name:           "EST anniversary event in current period",
			eventTimestamp: time.Date(2024, time.February, 15, 10, 0, 0, 0, time.UTC),
			subStart:       estSubStart,
			periodStart:    estPeriodStart,
			periodEnd:      estPeriodEnd,
			anchor:         estSubStart,
			unit:           1,
			period:         BILLING_PERIOD_MONTHLY,
			timezone:       "America/New_York",
			wantPeriodID:   expectedPeriodID(estPeriodStart),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CalculatePeriodID(
				tc.eventTimestamp,
				tc.subStart,
				tc.periodStart,
				tc.periodEnd,
				tc.anchor,
				tc.unit,
				tc.period,
				tc.timezone,
			)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equalf(t, tc.wantPeriodID, got,
				"got period ID %d (ts=%v), want %d (ts=%v)",
				got, time.UnixMilli(int64(got)),
				tc.wantPeriodID, time.UnixMilli(int64(tc.wantPeriodID)))
		})
	}
}

// TestGetNextUsageResetAt_Monthly_Timezone adds IST anniversary, IST calendar, and
// EST-with-DST cases to exercise the timezone-aware monthly reset path.
func TestGetNextUsageResetAt_Monthly_Timezone(t *testing.T) {
	// Pre-compute IST location for expected values.
	istLoc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		// Fall back to fixed offset if tzdata unavailable; tests will still exercise the logic.
		istLoc = time.FixedZone("IST", 5*3600+30*60)
	}

	tests := []struct {
		name              string
		currentTime       time.Time
		subscriptionStart time.Time
		billingAnchor     time.Time
		subscriptionEnd   *time.Time
		timezone          string
		want              time.Time
		wantErr           bool
	}{
		{
			// IST anniversary: sub starts IST midnight March 1 (= Feb29 18:30Z).
			// Current time = March 15 14:00 UTC (still in IST March period).
			// Period end = IST midnight April 1 = Mar31 18:30Z.
			// resetTime = April 1 00:00 IST = Mar31 18:30Z.
			name:              "IST anniversary monthly reset",
			currentTime:       time.Date(2024, time.March, 15, 14, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.February, 29, 18, 30, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.February, 29, 18, 30, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			timezone:          "Asia/Kolkata",
			want:              time.Date(2024, time.April, 1, 0, 0, 0, 0, istLoc),
		},
		{
			// IST calendar: sub starts IST midnight Feb 16 (= Feb15 18:30Z).
			// Calendar anchor = 2024-03-01T00:00:00Z.
			// Partial first period [Feb15 18:30Z, Feb29 18:30Z).
			// currentTime = Mar15 14:00Z → in second period [Feb29 18:30Z, Mar31 18:30Z).
			// Period end = Mar31 18:30Z → in IST = April 1 00:00 IST.
			// resetTime = April 1 00:00 IST = Mar31 18:30Z.
			name:              "IST calendar monthly reset",
			currentTime:       time.Date(2024, time.March, 15, 14, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.February, 15, 18, 30, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			timezone:          "Asia/Kolkata",
			want:              time.Date(2024, time.April, 1, 0, 0, 0, 0, istLoc),
		},
		{
			// EST anniversary crossing DST: sub starts EST midnight Feb 1 (= Feb01 05:00Z).
			// Current time = Feb15 12:00 UTC → in period [Feb01 05:00Z, Mar01 05:00Z).
			// Period end = Mar01 05:00Z → in EST = March 1 00:00 EST.
			// resetTime = March 1 00:00 EST = Mar01 05:00Z.
			name:              "EST anniversary crossing DST monthly reset",
			currentTime:       time.Date(2024, time.February, 15, 12, 0, 0, 0, time.UTC),
			subscriptionStart: time.Date(2024, time.February, 1, 5, 0, 0, 0, time.UTC),
			billingAnchor:     time.Date(2024, time.February, 1, 5, 0, 0, 0, time.UTC),
			subscriptionEnd:   nil,
			timezone:          "America/New_York",
			want:              time.Date(2024, time.March, 1, 5, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GetNextUsageResetAt(
				tc.currentTime,
				tc.subscriptionStart,
				tc.subscriptionEnd,
				tc.billingAnchor,
				ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
				tc.timezone,
			)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Truef(t, got.Equal(tc.want),
				"got %v (UTC: %v), want %v (UTC: %v)",
				got, got.UTC(), tc.want, tc.want.UTC())
		})
	}
}

// TestNextBillingDate_IST tests the timezone-aware billing date calculation
// with IST (Asia/Kolkata, UTC+5:30) and regression cases.
func TestNextBillingDate_IST(t *testing.T) {
	// IST = UTC+5:30 (5*3600 + 30*60 = 19800 seconds)
	istOffset := 5*3600 + 30*60
	ist := time.FixedZone("IST", istOffset)

	// IST midnight Jan 1, 2024 = 2023-12-31T18:30:00Z
	istMidnightJan1UTC := time.Date(2023, 12, 31, 18, 30, 0, 0, time.UTC)
	// IST midnight Feb 1, 2024 = 2024-01-31T18:30:00Z
	istMidnightFeb1UTC := time.Date(2024, 1, 31, 18, 30, 0, 0, time.UTC)

	// IST midnight Jan 15, 2024 = 2024-01-14T18:30:00Z
	istMidnightJan15UTC := time.Date(2024, 1, 14, 18, 30, 0, 0, time.UTC)
	// IST midnight Feb 15, 2024 = 2024-02-14T18:30:00Z
	istMidnightFeb15UTC := time.Date(2024, 2, 14, 18, 30, 0, 0, time.UTC)

	// IST midnight Jan 15, 2024 (for daily)
	istMidnightJan16UTC := time.Date(2024, 1, 15, 18, 30, 0, 0, time.UTC)

	_ = ist // used via FixedZone above; reference to avoid unused var warning

	cases := []struct {
		name               string
		currentPeriodStart time.Time
		billingAnchor      time.Time
		unit               int
		period             BillingPeriod
		timezone           string
		expectedUTC        time.Time
	}{
		{
			// Case 1: Monthly, midnight IST Jan 1. IST midnight = UTC 18:30 previous day.
			// Next period: Feb 1 midnight IST = 2024-01-31T18:30:00Z
			name:               "monthly midnight IST Jan1 to Feb1",
			currentPeriodStart: istMidnightJan1UTC,
			billingAnchor:      istMidnightJan1UTC,
			unit:               1,
			period:             BILLING_PERIOD_MONTHLY,
			timezone:           "Asia/Kolkata",
			expectedUTC:        istMidnightFeb1UTC,
		},
		{
			// Case 2: Monthly, mid-month anchor Jan 15 midnight IST. Next: Feb 15 midnight IST.
			name:               "monthly mid-month IST Jan15 to Feb15",
			currentPeriodStart: istMidnightJan15UTC,
			billingAnchor:      istMidnightJan15UTC,
			unit:               1,
			period:             BILLING_PERIOD_MONTHLY,
			timezone:           "Asia/Kolkata",
			expectedUTC:        istMidnightFeb15UTC,
		},
		{
			// Case 3: Daily reset — IST midnight Jan 15 + 1 day = Jan 16 midnight IST.
			name:               "daily IST Jan15 midnight to Jan16 midnight",
			currentPeriodStart: istMidnightJan15UTC,
			billingAnchor:      istMidnightJan15UTC,
			unit:               1,
			period:             BILLING_PERIOD_DAILY,
			timezone:           "Asia/Kolkata",
			expectedUTC:        istMidnightJan16UTC,
		},
		{
			// Case 4: UTC customer (no regression). Same dates as case 1 but Timezone = "UTC".
			// With UTC, Jan 1 00:00 UTC + 1 month = Feb 1 00:00 UTC.
			name:               "monthly UTC no regression",
			currentPeriodStart: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			unit:               1,
			period:             BILLING_PERIOD_MONTHLY,
			timezone:           "UTC",
			expectedUTC:        time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// Case 5: Invalid timezone falls back to UTC — result should match UTC computation.
			name:               "invalid timezone falls back to UTC",
			currentPeriodStart: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			billingAnchor:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			unit:               1,
			period:             BILLING_PERIOD_MONTHLY,
			timezone:           "Invalid/Timezone",
			expectedUTC:        time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NextBillingDate(NextBillingDateParams{
				CurrentPeriodStart: c.currentPeriodStart,
				BillingAnchor:      c.billingAnchor,
				Unit:               c.unit,
				Period:             c.period,
				Timezone:           c.timezone,
			})
			require.NoError(t, err)
			require.Equal(t, c.expectedUTC.UTC(), got.UTC())
		})
	}
}
