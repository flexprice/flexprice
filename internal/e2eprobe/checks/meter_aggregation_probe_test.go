package checks

import (
	"context"
	"strings"
	"testing"

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
	fc.events.analyticsItems = []types.UsageAnalyticItem{
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
