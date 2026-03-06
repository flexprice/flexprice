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
