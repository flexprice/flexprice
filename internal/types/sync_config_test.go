package types

import (
	"testing"
	"time"
)

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func TestMonthsBetween(t *testing.T) {
	tests := []struct {
		name  string
		start time.Time
		end   time.Time
		want  int
	}{
		// ── basic cases ──
		{
			name:  "same date",
			start: date(2025, time.January, 15),
			end:   date(2025, time.January, 15),
			want:  0,
		},
		{
			name:  "exact one month same day",
			start: date(2025, time.January, 15),
			end:   date(2025, time.February, 15),
			want:  1,
		},
		{
			name:  "exact three months same day",
			start: date(2025, time.January, 1),
			end:   date(2025, time.April, 1),
			want:  3,
		},
		{
			name:  "exact twelve months",
			start: date(2025, time.January, 1),
			end:   date(2026, time.January, 1),
			want:  12,
		},

		// ── end-of-month boundary cases (the bug) ──
		{
			name:  "Jan 31 → Feb 28 (non-leap) should be 1 month",
			start: date(2025, time.January, 31),
			end:   date(2025, time.February, 28),
			want:  1,
		},
		{
			name:  "Jan 31 → Feb 29 (leap year) should be 1 month",
			start: date(2024, time.January, 31),
			end:   date(2024, time.February, 29),
			want:  1,
		},
		{
			name:  "Mar 31 → Apr 30 should be 1 month",
			start: date(2025, time.March, 31),
			end:   date(2025, time.April, 30),
			want:  1,
		},
		{
			name:  "Mar 31 → Jun 30 (quarterly) should be 3 months",
			start: date(2025, time.March, 31),
			end:   date(2025, time.June, 30),
			want:  3,
		},
		{
			name:  "Jan 31 → Jul 31 (half-year) should be 6 months",
			start: date(2025, time.January, 31),
			end:   date(2025, time.July, 31),
			want:  6,
		},
		{
			name:  "Jan 30 → Feb 28 (non-leap) should be 1 month",
			start: date(2025, time.January, 30),
			end:   date(2025, time.February, 28),
			want:  1,
		},
		{
			name:  "Aug 31 → Sep 30 should be 1 month",
			start: date(2025, time.August, 31),
			end:   date(2025, time.September, 30),
			want:  1,
		},
		{
			name:  "May 31 → Nov 30 (half-year) should be 6 months",
			start: date(2025, time.May, 31),
			end:   date(2025, time.November, 30),
			want:  6,
		},

		// ── cases that should NOT be affected ──
		{
			name:  "partial month mid-month",
			start: date(2025, time.January, 20),
			end:   date(2025, time.February, 15),
			want:  0,
		},
		{
			name:  "Jan 15 → Feb 28 is still 1 (end is last day but start is mid-month)",
			start: date(2025, time.January, 15),
			end:   date(2025, time.February, 28),
			want:  1,
		},
		{
			name:  "end before start returns 0",
			start: date(2025, time.March, 1),
			end:   date(2025, time.January, 1),
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := monthsBetween(tt.start, tt.end)
			if got != tt.want {
				t.Errorf("monthsBetween(%s, %s) = %d, want %d",
					tt.start.Format("2006-01-02"), tt.end.Format("2006-01-02"), got, tt.want)
			}
		})
	}
}

func TestPeriodQuantity(t *testing.T) {
	tests := []struct {
		name   string
		start  time.Time
		end    time.Time
		target BillingPeriod
		want   int
	}{
		{
			name:   "quarterly: Jan 31 → Apr 30 should be 1 quarter",
			start:  date(2025, time.January, 31),
			end:    date(2025, time.April, 30),
			target: BILLING_PERIOD_QUARTER,
			want:   1,
		},
		{
			name:   "quarterly: Mar 31 → Jun 30 should be 1 quarter",
			start:  date(2025, time.March, 31),
			end:    date(2025, time.June, 30),
			target: BILLING_PERIOD_QUARTER,
			want:   1,
		},
		{
			name:   "monthly: Jan 31 → Feb 28 should be 1",
			start:  date(2025, time.January, 31),
			end:    date(2025, time.February, 28),
			target: BILLING_PERIOD_MONTHLY,
			want:   1,
		},
		{
			name:   "half-year: Jan 31 → Jul 31 should be 1",
			start:  date(2025, time.January, 31),
			end:    date(2025, time.July, 31),
			target: BILLING_PERIOD_HALF_YEAR,
			want:   1,
		},
		{
			name:   "annual: Jan 31 2025 → Jan 31 2026 should be 1",
			start:  date(2025, time.January, 31),
			end:    date(2026, time.January, 31),
			target: BILLING_PERIOD_ANNUAL,
			want:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := periodQuantity(tt.start, tt.end, tt.target)
			if got != tt.want {
				t.Errorf("periodQuantity(%s, %s, %s) = %d, want %d",
					tt.start.Format("2006-01-02"), tt.end.Format("2006-01-02"), tt.target, got, tt.want)
			}
		})
	}
}

func TestNormalizedFixedQuantity(t *testing.T) {
	tests := []struct {
		name     string
		settings *InvoiceSyncSettings
		start    *time.Time
		end      *time.Time
		want     int
	}{
		{
			name:     "nil settings returns 0",
			settings: nil,
			start:    timePtr(date(2025, time.January, 1)),
			end:      timePtr(date(2025, time.February, 1)),
			want:     0,
		},
		{
			name:     "empty NormalizeFixedTo returns 0",
			settings: &InvoiceSyncSettings{NormalizeFixedTo: ""},
			start:    timePtr(date(2025, time.January, 1)),
			end:      timePtr(date(2025, time.February, 1)),
			want:     0,
		},
		{
			name:     "nil start returns 0",
			settings: &InvoiceSyncSettings{NormalizeFixedTo: BILLING_PERIOD_MONTHLY},
			start:    nil,
			end:      timePtr(date(2025, time.February, 1)),
			want:     0,
		},
		{
			name:     "quarterly charge normalized to monthly: Jan 31 → Apr 30 = 3",
			settings: &InvoiceSyncSettings{NormalizeFixedTo: BILLING_PERIOD_MONTHLY},
			start:    timePtr(date(2025, time.January, 31)),
			end:      timePtr(date(2025, time.April, 30)),
			want:     3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.settings.NormalizedFixedQuantity(tt.start, tt.end)
			if got != tt.want {
				t.Errorf("NormalizedFixedQuantity() = %d, want %d", got, tt.want)
			}
		})
	}
}

