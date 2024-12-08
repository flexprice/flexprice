package types

import (
	"testing"
	"time"
)

func TestNextBillingDate_Daily(t *testing.T) {
	start := time.Date(2024, time.March, 10, 0, 0, 0, 0, time.UTC)
	unit := 10
	got, err := NextBillingDate(start, unit, BILLING_PERIOD_DAILY)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2024, time.March, 20, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextBillingDate(Daily): got %v, want %v", got, want)
	}
}

func TestNextBillingDate_Weekly(t *testing.T) {
	start := time.Date(2024, time.March, 10, 0, 0, 0, 0, time.UTC)
	unit := 2 // 2 weeks = 14 days
	got, err := NextBillingDate(start, unit, BILLING_PERIOD_WEEKLY)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2024, time.March, 24, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextBillingDate(Weekly): got %v, want %v", got, want)
	}
}

func TestNextBillingDate_Monthly_Simple(t *testing.T) {
	start := time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)
	unit := 2 // Every 2 months
	got, err := NextBillingDate(start, unit, BILLING_PERIOD_MONTHLY)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// January 15 + 2 months = March 15 (no clamping needed)
	want := time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextBillingDate(Monthly Simple): got %v, want %v", got, want)
	}
}

func TestNextBillingDate_Monthly_Clamping(t *testing.T) {
	start := time.Date(2024, time.October, 31, 0, 0, 0, 0, time.UTC)
	unit := 1
	got, err := NextBillingDate(start, unit, BILLING_PERIOD_MONTHLY)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// October 31 + 1 month -> Ideally "November 31" doesn't exist, clamp to November 30.
	want := time.Date(2024, time.November, 30, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextBillingDate(Monthly Clamping): got %v, want %v", got, want)
	}
}

func TestNextBillingDate_Annual(t *testing.T) {
	start := time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC) // 2024 is a leap year
	unit := 1
	got, err := NextBillingDate(start, unit, BILLING_PERIOD_ANNUAL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Feb 29, 2024 + 1 year = Feb 29 doesn't exist in 2025 (not a leap year), clamp to Feb 28, 2025.
	want := time.Date(2025, time.February, 28, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextBillingDate(Annual): got %v, want %v", got, want)
	}
}

func TestNextBillingDate_InvalidPeriod(t *testing.T) {
	start := time.Now()
	_, err := NextBillingDate(start, 1, "INVALID")
	if err == nil {
		t.Error("expected error for invalid billing period, got nil")
	}
}

func TestNextBillingDate_ZeroUnit(t *testing.T) {
	start := time.Now()
	_, err := NextBillingDate(start, 0, BILLING_PERIOD_DAILY)
	if err == nil {
		t.Error("expected error for zero billing period unit, got nil")
	}
}

func TestAddClampedDate_DaysOverMonthEnd(t *testing.T) {
	start := time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC)
	// Adding one month from Jan 31 normally would be "Feb 31" which doesn't exist.
	// The function should clamp to Feb 29, 2024 (leap year).
	got := AddClampedDate(start, 0, 1, 0)
	want := time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("AddClampedDate Monthly Clamp: got %v, want %v", got, want)
	}
}

func TestAddClampedDate_MultipleMonths(t *testing.T) {
	start := time.Date(2024, time.December, 31, 0, 0, 0, 0, time.UTC)
	// Add 2 months to December 31, 2024:
	// December 31 + 2 months = February 31, 2025 (does not exist)
	// Clamp to Feb 29, 2025 is not valid either since 2025 is not a leap year
	// The last valid day of Feb 2025 is Feb 28.
	got := AddClampedDate(start, 0, 2, 0)
	want := time.Date(2025, time.February, 28, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("AddClampedDate Multi-Month Clamp: got %v, want %v", got, want)
	}
}

func TestAddClampedDate_DaysIncrementWithinMonth(t *testing.T) {
	start := time.Date(2024, time.March, 10, 0, 0, 0, 0, time.UTC)
	got := AddClampedDate(start, 0, 0, 5)
	// March 10 + 5 days = March 15 (no clamp needed)
	want := time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("AddClampedDate Days Within Month: got %v, want %v", got, want)
	}
}

func TestAddClampedDate_YearRollOver(t *testing.T) {
	start := time.Date(2024, time.November, 30, 0, 0, 0, 0, time.UTC)
	// Add 2 months: November + 2 months = January (of next year)
	got := AddClampedDate(start, 0, 2, 0)
	want := time.Date(2025, time.January, 30, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("AddClampedDate Year Roll-Over: got %v, want %v", got, want)
	}
}

func TestAddClampedDate_MultipleYears(t *testing.T) {
	start := time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC)
	// Add 1 year to leap day 2024
	got := AddClampedDate(start, 1, 0, 0)
	// Feb 29, 2025 doesn't exist, clamp to Feb 28, 2025
	want := time.Date(2025, time.February, 28, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("AddClampedDate Leap Year: got %v, want %v", got, want)
	}
}
