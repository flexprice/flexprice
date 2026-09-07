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

	// Analytics echoes the seeded event timestamps back as bucket points, which
	// is what the assertion matches on.
	fc.events.analyticsEcho = true

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

// A persistent customer holds more than one subscription on the probe plan, so
// the analytics response leads with an item for whichever subscription the
// server lists first — often a young or cancelled one carrying no points. The
// seeded buckets still have to be found.
func TestBucketedMeterProbe_EmptyItemFromOtherSubscription(t *testing.T) {
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

	// Leading item: no points, as a subscription younger than the seeded window
	// returns. It must not decide the outcome.
	fc.events.analyticsItems = []sdktypes.UsageAnalyticItem{{}}
	fc.events.analyticsEcho = true

	p := NewBucketedMeterProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
}

func TestSeenBuckets(t *testing.T) {
	ts := func(s string) *string { return &s }
	featID := "feat_day"
	other := "feat_other"
	items := []sdktypes.UsageAnalyticItem{
		// Other feature: ignored even though it carries a seeded boundary.
		{FeatureID: &other, Points: []sdktypes.UsageAnalyticPoint{{Timestamp: ts("2026-08-30T00:00:00Z")}}},
		// Matching feature, split across two subscriptions.
		{FeatureID: &featID, Points: []sdktypes.UsageAnalyticPoint{{Timestamp: ts("2026-08-31T00:00:00Z")}}},
		{FeatureID: &featID, Points: []sdktypes.UsageAnalyticPoint{
			{Timestamp: ts("2026-09-01T00:00:00Z")},
			{Timestamp: nil},
			{Timestamp: ts("not-a-timestamp")},
		}},
		// No feature_id: counted, the query is already feature-scoped.
		{Points: []sdktypes.UsageAnalyticPoint{{Timestamp: ts("2026-09-02T00:00:00Z")}}},
	}

	seen := seenBuckets(items, featID)
	day := func(s string) time.Time {
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return parsed
	}
	want := []time.Time{day("2026-08-31T00:00:00Z"), day("2026-09-01T00:00:00Z"), day("2026-09-02T00:00:00Z")}
	if missing := missingBuckets(want, seen); len(missing) != 0 {
		t.Errorf("missingBuckets = %v, want none", formatBuckets(missing))
	}
	if !seen[day("2026-08-30T00:00:00Z").Unix()] {
		// The only source for this boundary was the other feature's item.
		t.Log("boundary from another feature correctly excluded")
	} else {
		t.Error("seenBuckets counted a point from a different feature")
	}

	missing := missingBuckets([]time.Time{day("2026-08-30T00:00:00Z"), day("2026-08-31T00:00:00Z")}, seen)
	if got := formatBuckets(missing); got != "2026-08-30T00:00:00Z" {
		t.Errorf("formatBuckets(missing) = %q, want %q", got, "2026-08-30T00:00:00Z")
	}
}
