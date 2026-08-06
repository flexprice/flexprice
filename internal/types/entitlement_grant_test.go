package types

import (
	"testing"
	"time"
)

func TestEntitlementAggregationMode_Validate(t *testing.T) {
	cases := []struct {
		input   EntitlementAggregationMode
		wantErr bool
	}{
		{"", false},
		{EntitlementAggregationModeAdditive, false},
		{EntitlementAggregationModeParallel, false},
		{"combined", true},
	}
	for _, tc := range cases {
		if got := tc.input.Validate(); (got != nil) != tc.wantErr {
			t.Fatalf("Validate(%q) wantErr=%v got=%v", tc.input, tc.wantErr, got)
		}
	}
}

func TestEntitlementGrantMeasure_Validate(t *testing.T) {
	cases := []struct {
		input   EntitlementGrantMeasure
		wantErr bool
	}{
		{"", false},
		{EntitlementGrantMeasureQuantity, false},
		{EntitlementGrantMeasureAmount, false},
		{"volume", true},
	}
	for _, tc := range cases {
		if got := tc.input.Validate(); (got != nil) != tc.wantErr {
			t.Fatalf("Validate(%q) wantErr=%v got=%v", tc.input, tc.wantErr, got)
		}
	}
}

func TestEntitlementGrantScopeEntityType_Validate(t *testing.T) {
	cases := []struct {
		input   EntitlementGrantScopeEntityType
		wantErr bool
	}{
		{"", false},
		{EntitlementGrantScopeFeature, false},
		{EntitlementGrantScopeSubscription, false},
		{EntitlementGrantScopeGroup, false},
		{"tenant", true},
	}
	for _, tc := range cases {
		if got := tc.input.Validate(); (got != nil) != tc.wantErr {
			t.Fatalf("Validate(%q) wantErr=%v got=%v", tc.input, tc.wantErr, got)
		}
	}
}

func TestEntitlementGrantDurationOf(t *testing.T) {
	cases := []struct {
		name    string
		value   int
		unit    EntitlementGrantDurationUnit
		want    time.Duration
		wantErr bool
	}{
		{"5 hours", 5, EntitlementGrantDurationUnitHour, 5 * time.Hour, false},
		{"1 day", 1, EntitlementGrantDurationUnitDay, 24 * time.Hour, false},
		{"2 weeks", 2, EntitlementGrantDurationUnitWeek, 14 * 24 * time.Hour, false},
		{"zero value rejected", 0, EntitlementGrantDurationUnitHour, 0, true},
		{"negative value rejected", -1, EntitlementGrantDurationUnitHour, 0, true},
		{"unknown unit rejected", 3, "month", 0, true},
		// int64-nanosecond overflow boundaries: max hour 2562047, day 106751, week 15250.
		{"max days accepted", 106751, EntitlementGrantDurationUnitDay, 106751 * 24 * time.Hour, false},
		{"overflowing days rejected", 106752, EntitlementGrantDurationUnitDay, 0, true},
		{"max weeks accepted", 15250, EntitlementGrantDurationUnitWeek, 15250 * 7 * 24 * time.Hour, false},
		{"overflowing weeks rejected", 15251, EntitlementGrantDurationUnitWeek, 0, true},
		{"overflowing hours rejected", 2562048, EntitlementGrantDurationUnitHour, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EntitlementGrantDurationOf(tc.value, tc.unit)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestEntitlementGrantStatus_Validate(t *testing.T) {
	for _, s := range []EntitlementGrantStatus{EntitlementGrantStatusActive, EntitlementGrantStatusExhausted, ""} {
		if err := s.Validate(); err != nil {
			t.Fatalf("%q should validate, got %v", s, err)
		}
	}
	// Expiry is derived from valid_to, never stored — 'expired' is not a status.
	for _, s := range []EntitlementGrantStatus{"expired", "superseded", "bogus"} {
		if err := s.Validate(); err == nil {
			t.Fatalf("%q should be rejected", s)
		}
	}
}

func TestEntitlementGrantFilter_WithLiveOnly(t *testing.T) {
	at := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	f := NewNoLimitEntitlementGrantFilter().WithLiveOnly(at)

	// Liveness is purely time-based: window contains `at`, no status predicate.
	if len(f.Statuses) != 0 {
		t.Fatalf("WithLiveOnly should not filter on status, got %v", f.Statuses)
	}
	if f.ValidAtOrAfter == nil || !f.ValidAtOrAfter.Equal(at) {
		t.Fatalf("ValidAtOrAfter mismatch: want %v got %v", at, f.ValidAtOrAfter)
	}
}

func TestEntitlementGrantFilter_WithCycleOverlap(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	f := NewNoLimitEntitlementGrantFilter().WithCycleOverlap(start, end)

	// Billing path is status-free: a grant that lived + died inside a cycle
	// still contributes overage.
	if len(f.Statuses) != 0 {
		t.Fatalf("WithCycleOverlap should not filter on status, got %v", f.Statuses)
	}
	if f.ValidFromBefore == nil || !f.ValidFromBefore.Equal(end) {
		t.Fatalf("ValidFromBefore mismatch")
	}
	if f.ValidToAfter == nil || !f.ValidToAfter.Equal(start) {
		t.Fatalf("ValidToAfter mismatch")
	}
}

func TestEntitlementGrantFilter_WithFeatureIDs_IsSugarForScope(t *testing.T) {
	f := NewNoLimitEntitlementGrantFilter().WithFeatureIDs("feat_a", "feat_b")
	if f.ScopeEntityType == nil || *f.ScopeEntityType != EntitlementGrantScopeFeature {
		t.Fatalf("WithFeatureIDs should pin scope_entity_type=feature; got %v", f.ScopeEntityType)
	}
	if len(f.ScopeEntityIDs) != 2 || f.ScopeEntityIDs[0] != "feat_a" || f.ScopeEntityIDs[1] != "feat_b" {
		t.Fatalf("expected scope entity ids [feat_a feat_b], got %v", f.ScopeEntityIDs)
	}
}
