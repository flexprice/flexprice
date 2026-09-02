package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	itypes "github.com/flexprice/flexprice/internal/types"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
)

func TestBucketedMeterProbe_HappyPath(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	reg.LoadSeeds(e2eprobe.Seeds{
		PersistentCustomerIDs: []string{"e2eprobe-cust-persistent-0"},
		BucketedFeatureIDs: map[string]string{
			"e2eprobe_max_15min_feature": "feat_15min",
			"e2eprobe_sum_hour_feature":  "feat_hour",
			"e2eprobe_max_day_feature":   "feat_day",
		},
	})

	// Fake analytics returns items with 3 points (buckets) — enough to satisfy the
	// count assertion. The bucket-value tightening is deferred to a follow-up
	// once the exact per-bucket point shape is confirmed against staging.
	pt := sdktypes.UsageAnalyticPoint{}
	fc.events.analyticsItems = []sdktypes.UsageAnalyticItem{
		{Points: []sdktypes.UsageAnalyticPoint{pt, pt, pt}},
	}

	p := NewBucketedMeterProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(fc.events.ingested) != 3 {
		t.Errorf("ingested = %d, want 3", len(fc.events.ingested))
	}
	// Each event has EventID + Timestamp populated.
	for i, ev := range fc.events.ingested {
		if ev.EventID == nil || *ev.EventID == "" {
			t.Errorf("event %d: EventID missing", i)
		}
		if ev.Timestamp == nil || *ev.Timestamp == "" {
			t.Errorf("event %d: Timestamp missing", i)
		}
	}
	// Timestamps parse as RFC3339Nano.
	for i, ev := range fc.events.ingested {
		if ev.Timestamp == nil {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, *ev.Timestamp); err != nil {
			t.Errorf("event %d: Timestamp %q is not RFC3339Nano: %v", i, *ev.Timestamp, err)
		}
	}
}

func TestBucketedMeterProbe_MissingSeedsSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	p := NewBucketedMeterProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("empty seeds must soft-skip; got %v", err)
	}
	if len(fc.events.ingested) != 0 {
		t.Errorf("no events should be ingested on empty seeds; got %d", len(fc.events.ingested))
	}
}

func TestBucketedMeterProbe_MissingBucketedFeatureSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	// PersistentCustomerIDs set but BucketedFeatureIDs missing the current cursor's spec.
	reg.LoadSeeds(e2eprobe.Seeds{
		PersistentCustomerIDs: []string{"e2eprobe-cust-persistent-0"},
		// Only supply the second spec so the first-run cursor (spec 0) misses.
		BucketedFeatureIDs: map[string]string{"e2eprobe_sum_hour_feature": "feat_hour"},
	})

	p := NewBucketedMeterProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("missing bucketed feature must soft-skip; got %v", err)
	}
	if len(fc.events.ingested) != 0 {
		t.Errorf("no events should be ingested when bucketed feature missing; got %d", len(fc.events.ingested))
	}
}
