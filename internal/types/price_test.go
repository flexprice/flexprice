package types

import (
	"testing"
)

func TestBillingPeriodOrder(t *testing.T) {
	tests := []struct {
		name  string
		b     BillingPeriod
		want  int
		valid bool
	}{
		{"DAILY", BILLING_PERIOD_DAILY, 1, true},
		{"WEEKLY", BILLING_PERIOD_WEEKLY, 2, true},
		{"MONTHLY", BILLING_PERIOD_MONTHLY, 3, true},
		{"QUARTERLY", BILLING_PERIOD_QUARTER, 4, true},
		{"HALF_YEARLY", BILLING_PERIOD_HALF_YEAR, 5, true},
		{"ANNUAL", BILLING_PERIOD_ANNUAL, 6, true},
		{"empty", BillingPeriod(""), 0, true},
		{"unknown", BillingPeriod("UNKNOWN"), 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BillingPeriodOrder(tt.b)
			if got != tt.want {
				t.Errorf("BillingPeriodOrder(%q) = %d, want %d", tt.b, got, tt.want)
			}
		})
	}
	// Ordering: each should be less than the next
	periods := []BillingPeriod{
		BILLING_PERIOD_DAILY,
		BILLING_PERIOD_WEEKLY,
		BILLING_PERIOD_MONTHLY,
		BILLING_PERIOD_QUARTER,
		BILLING_PERIOD_HALF_YEAR,
		BILLING_PERIOD_ANNUAL,
	}
	for i := 0; i < len(periods)-1; i++ {
		a, b := periods[i], periods[i+1]
		if BillingPeriodOrder(a) >= BillingPeriodOrder(b) {
			t.Errorf("expected Order(%s)=%d < Order(%s)=%d", a, BillingPeriodOrder(a), b, BillingPeriodOrder(b))
		}
	}
}

func TestBillingPeriodGreaterThan(t *testing.T) {
	tests := []struct {
		a    BillingPeriod
		b    BillingPeriod
		want bool
	}{
		{BILLING_PERIOD_QUARTER, BILLING_PERIOD_MONTHLY, true},
		{BILLING_PERIOD_MONTHLY, BILLING_PERIOD_QUARTER, false},
		{BILLING_PERIOD_MONTHLY, BILLING_PERIOD_MONTHLY, false},
		{BILLING_PERIOD_ANNUAL, BILLING_PERIOD_DAILY, true},
		{BILLING_PERIOD_DAILY, BILLING_PERIOD_ANNUAL, false},
		{BILLING_PERIOD_HALF_YEAR, BILLING_PERIOD_QUARTER, true},
	}
	for _, tt := range tests {
		t.Run(tt.a.String()+"_vs_"+tt.b.String(), func(t *testing.T) {
			got := BillingPeriodGreaterThan(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("BillingPeriodGreaterThan(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestBillingPeriodToMonths(t *testing.T) {
	tests := []struct {
		name string
		b    BillingPeriod
		want int
	}{
		{"DAILY", BILLING_PERIOD_DAILY, 0},
		{"WEEKLY", BILLING_PERIOD_WEEKLY, 0},
		{"MONTHLY", BILLING_PERIOD_MONTHLY, 1},
		{"QUARTERLY", BILLING_PERIOD_QUARTER, 3},
		{"HALF_YEARLY", BILLING_PERIOD_HALF_YEAR, 6},
		{"ANNUAL", BILLING_PERIOD_ANNUAL, 12},
		{"empty", BillingPeriod(""), 0},
		{"unknown", BillingPeriod("BIWEEKLY"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BillingPeriodToMonths(tt.b)
			if got != tt.want {
				t.Errorf("BillingPeriodToMonths(%q) = %d, want %d", tt.b, got, tt.want)
			}
		})
	}
}

func TestIsBillingPeriodMultiple(t *testing.T) {
	tests := []struct {
		name    string
		longer  BillingPeriod
		shorter BillingPeriod
		want    bool
	}{
		{"same_monthly", BILLING_PERIOD_MONTHLY, BILLING_PERIOD_MONTHLY, true},
		{"quarterly_of_monthly", BILLING_PERIOD_QUARTER, BILLING_PERIOD_MONTHLY, true},
		{"half_yearly_of_monthly", BILLING_PERIOD_HALF_YEAR, BILLING_PERIOD_MONTHLY, true},
		{"annual_of_monthly", BILLING_PERIOD_ANNUAL, BILLING_PERIOD_MONTHLY, true},
		{"half_yearly_of_quarterly", BILLING_PERIOD_HALF_YEAR, BILLING_PERIOD_QUARTER, true},
		{"annual_of_quarterly", BILLING_PERIOD_ANNUAL, BILLING_PERIOD_QUARTER, true},
		{"annual_of_half_yearly", BILLING_PERIOD_ANNUAL, BILLING_PERIOD_HALF_YEAR, true},
		{"monthly_of_quarterly_false", BILLING_PERIOD_MONTHLY, BILLING_PERIOD_QUARTER, false},
		{"quarterly_of_half_yearly_false", BILLING_PERIOD_QUARTER, BILLING_PERIOD_HALF_YEAR, false},
		{"weekly_of_monthly_false", BILLING_PERIOD_WEEKLY, BILLING_PERIOD_MONTHLY, false},
		{"daily_of_monthly_false", BILLING_PERIOD_DAILY, BILLING_PERIOD_MONTHLY, false},
		{"daily_of_weekly_false", BILLING_PERIOD_DAILY, BILLING_PERIOD_WEEKLY, false},
		{"same_daily", BILLING_PERIOD_DAILY, BILLING_PERIOD_DAILY, true},
		{"same_weekly", BILLING_PERIOD_WEEKLY, BILLING_PERIOD_WEEKLY, true},
		{"same_annual", BILLING_PERIOD_ANNUAL, BILLING_PERIOD_ANNUAL, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBillingPeriodMultiple(tt.longer, tt.shorter)
			if got != tt.want {
				t.Errorf("IsBillingPeriodMultiple(%q, %q) = %v, want %v", tt.longer, tt.shorter, got, tt.want)
			}
		})
	}
}

func TestMinBillingPeriod(t *testing.T) {
	tests := []struct {
		name    string
		periods []BillingPeriod
		want    BillingPeriod
	}{
		{"M_Q_H", []BillingPeriod{BILLING_PERIOD_MONTHLY, BILLING_PERIOD_QUARTER, BILLING_PERIOD_HALF_YEAR}, BILLING_PERIOD_MONTHLY},
		{"Q_H_A", []BillingPeriod{BILLING_PERIOD_QUARTER, BILLING_PERIOD_HALF_YEAR, BILLING_PERIOD_ANNUAL}, BILLING_PERIOD_QUARTER},
		{"H_A", []BillingPeriod{BILLING_PERIOD_HALF_YEAR, BILLING_PERIOD_ANNUAL}, BILLING_PERIOD_HALF_YEAR},
		{"single_M", []BillingPeriod{BILLING_PERIOD_MONTHLY}, BILLING_PERIOD_MONTHLY},
		{"reverse_order", []BillingPeriod{BILLING_PERIOD_ANNUAL, BILLING_PERIOD_MONTHLY, BILLING_PERIOD_QUARTER}, BILLING_PERIOD_MONTHLY},
		{"empty", []BillingPeriod{}, ""},
		{"D_W_M", []BillingPeriod{BILLING_PERIOD_DAILY, BILLING_PERIOD_WEEKLY, BILLING_PERIOD_MONTHLY}, BILLING_PERIOD_DAILY},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MinBillingPeriod(tt.periods)
			if got != tt.want {
				t.Errorf("MinBillingPeriod(%v) = %q, want %q", tt.periods, got, tt.want)
			}
		})
	}
}

func TestValidateBillingPeriodAlignment(t *testing.T) {
	tests := []struct {
		name    string
		periods []BillingPeriod
		wantErr bool
	}{
		{"single", []BillingPeriod{BILLING_PERIOD_MONTHLY}, false},
		{"same_same", []BillingPeriod{BILLING_PERIOD_MONTHLY, BILLING_PERIOD_MONTHLY}, false},
		{"M_Q", []BillingPeriod{BILLING_PERIOD_MONTHLY, BILLING_PERIOD_QUARTER}, false},
		{"M_Q_H", []BillingPeriod{BILLING_PERIOD_MONTHLY, BILLING_PERIOD_QUARTER, BILLING_PERIOD_HALF_YEAR}, false},
		{"M_Q_H_A", []BillingPeriod{BILLING_PERIOD_MONTHLY, BILLING_PERIOD_QUARTER, BILLING_PERIOD_HALF_YEAR, BILLING_PERIOD_ANNUAL}, false},
		{"Q_A", []BillingPeriod{BILLING_PERIOD_QUARTER, BILLING_PERIOD_ANNUAL}, false},
		{"Q_H", []BillingPeriod{BILLING_PERIOD_QUARTER, BILLING_PERIOD_HALF_YEAR}, false},
		{"Q_H_A", []BillingPeriod{BILLING_PERIOD_QUARTER, BILLING_PERIOD_HALF_YEAR, BILLING_PERIOD_ANNUAL}, false},
		{"M_H_A", []BillingPeriod{BILLING_PERIOD_MONTHLY, BILLING_PERIOD_HALF_YEAR, BILLING_PERIOD_ANNUAL}, false},
		{"weekly_and_monthly_error", []BillingPeriod{BILLING_PERIOD_WEEKLY, BILLING_PERIOD_MONTHLY}, true},
		{"daily_and_monthly_error", []BillingPeriod{BILLING_PERIOD_DAILY, BILLING_PERIOD_MONTHLY}, true},
		{"daily_and_weekly_error", []BillingPeriod{BILLING_PERIOD_DAILY, BILLING_PERIOD_WEEKLY}, true},
		{"empty", []BillingPeriod{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBillingPeriodAlignment(tt.periods)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBillingPeriodAlignment(%v) error = %v, wantErr %v", tt.periods, err, tt.wantErr)
			}
		})
	}
}
