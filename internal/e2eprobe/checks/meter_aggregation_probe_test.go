package checks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// TestMeterAggregationProbe_NoSeedsIsNoOp verifies that the probe is a no-op
// when the registry has not yet been populated by seed-ensure.
func TestMeterAggregationProbe_NoSeedsIsNoOp(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	// Empty registry — no seeds loaded.
	p := NewMeterAggregationProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() with empty registry: %v", err)
	}
	if fc.events.analytics != 0 {
		t.Errorf("expected 0 analytics calls with empty registry, got %d", fc.events.analytics)
	}
}

// TestMeterAggregationProbe_SuccessWhenSumPositive verifies that Run() returns
// nil when the analytics response contains a positive TotalUsage for the
// chosen event name.
func TestMeterAggregationProbe_SuccessWhenSumPositive(t *testing.T) {
	fc := newFakeClient()

	eventName := "e2eprobe_count"
	usage := "42.0000"
	fc.events.analyticsItems = []types.DtoUsageAnalyticItem{
		{EventName: &eventName, TotalUsage: &usage},
	}

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{
		PersistentCustomerIDs: []string{"c0"},
		MeterIDs:              map[string]string{eventName: "meter_001"},
	})

	p := NewMeterAggregationProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() with positive usage: %v", err)
	}
	if fc.events.analytics != 1 {
		t.Errorf("expected 1 analytics call, got %d", fc.events.analytics)
	}
}

// TestMeterAggregationProbe_RetriesTransientAPIErrors verifies the probe
// absorbs transient upstream errors (the empty `{}` body pattern we've seen
// from the analytics endpoint on rare occasions) and only alerts after all
// attempts fail. A single-tick `{}` response was firing false-positive
// alerts; one retry survives those without losing real-outage detection.
func TestMeterAggregationProbe_RetriesTransientAPIErrors(t *testing.T) {
	// Shorten retry delay so this test runs fast. Restore at end.
	origDelay := meterAggRetryDelay
	meterAggRetryDelay = 5 * time.Millisecond
	defer func() { meterAggRetryDelay = origDelay }()

	fc := newFakeClient()
	eventName := "e2eprobe_count"
	usage := "42.0000"
	fc.events.analyticsItems = []types.DtoUsageAnalyticItem{
		{EventName: &eventName, TotalUsage: &usage},
	}
	// Fail twice, then succeed on the third attempt.
	fc.events.anaErr = errors.New("{}")
	fc.events.anaErrTimes = 2
	fc.events.anaErrTransient = true

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{
		PersistentCustomerIDs: []string{"c0"},
		MeterIDs:              map[string]string{eventName: "meter_001"},
	})

	p := NewMeterAggregationProbe(fc, reg, "run-1")
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() should succeed after retries absorb transient errors; got: %v", err)
	}
	if fc.events.analytics != 3 {
		t.Errorf("expected 3 analytics calls (2 transient + 1 success), got %d", fc.events.analytics)
	}
}

// TestMeterAggregationProbe_PersistentAPIErrorAlerts verifies that when all
// retry attempts fail the probe still raises an alert — retry must not
// silence genuine outages, just absorb single-tick blips.
func TestMeterAggregationProbe_PersistentAPIErrorAlerts(t *testing.T) {
	origDelay := meterAggRetryDelay
	meterAggRetryDelay = 5 * time.Millisecond
	defer func() { meterAggRetryDelay = origDelay }()

	fc := newFakeClient()
	eventName := "e2eprobe_count"
	// All attempts return the same error.
	fc.events.anaErr = errors.New("{}")

	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{
		PersistentCustomerIDs: []string{"c0"},
		MeterIDs:              map[string]string{eventName: "meter_001"},
	})

	p := NewMeterAggregationProbe(fc, reg, "run-1")
	err := p.Run(context.Background())
	if err == nil {
		t.Fatal("expected error when all attempts fail")
	}
	if fc.events.analytics != meterAggMaxAttempts {
		t.Errorf("expected %d analytics calls, got %d", meterAggMaxAttempts, fc.events.analytics)
	}
	attrs := e2eprobe.AttributesFrom(err)
	if attrs == nil {
		t.Fatal("expected CheckError attributes, got nil")
	}
	if attrs["attempts"] == "" {
		t.Errorf("expected attempts attribute, got %v", attrs)
	}
}

// TestMeterAggregationProbe_FailsWhenSumZero verifies that Run() returns an
// error (with event_name and observed_sum attributes) when the analytics
// response contains no matching items for the chosen event name.
func TestMeterAggregationProbe_FailsWhenSumZero(t *testing.T) {
	fc := newFakeClient()
	// analyticsItems is empty → extractAnalyticsSum returns 0.

	eventName := "e2eprobe_count"
	reg := e2eprobe.NewRegistry()
	reg.LoadSeeds(e2eprobe.Seeds{
		PersistentCustomerIDs: []string{"c0"},
		MeterIDs:              map[string]string{eventName: "meter_001"},
	})

	p := NewMeterAggregationProbe(fc, reg, "run-1")
	err := p.Run(context.Background())
	if err == nil {
		t.Fatal("expected error when sum is zero, got nil")
	}

	attrs := e2eprobe.AttributesFrom(err)
	if attrs == nil {
		t.Fatal("expected CheckError attributes, got nil")
	}
	if attrs["event_name"] == "" {
		t.Errorf("expected event_name attribute in error, got %v", attrs)
	}
	if attrs["observed_sum"] == "" {
		t.Errorf("expected observed_sum attribute in error, got %v", attrs)
	}

	// Error message should name the meter.
	if !strings.Contains(err.Error(), eventName) {
		t.Errorf("error %q should contain event name %q", err.Error(), eventName)
	}
}
