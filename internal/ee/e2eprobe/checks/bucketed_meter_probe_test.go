package checks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	itypes "github.com/flexprice/flexprice/internal/types"
	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
)

func TestBackdatedBuckets(t *testing.T) {
	hour := time.Hour
	day := 24 * time.Hour
	min15 := 15 * time.Minute

	parse := func(s string) time.Time {
		t.Helper()
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return ts
	}

	tests := []struct {
		name       string
		now        time.Time
		window     time.Duration
		activeFrom time.Time
		want       []string // RFC3339, empty → skip
	}{
		{
			name:       "hour earliest three overlapping complete buckets",
			now:        parse("2026-09-09T08:10:00Z"),
			window:     hour,
			activeFrom: parse("2026-09-09T05:46:01Z"),
			want:       []string{"2026-09-09T05:00:00Z", "2026-09-09T06:00:00Z", "2026-09-09T07:00:00Z"},
		},
		{
			name:       "hour sub younger than oldest bucket",
			now:        parse("2026-09-09T07:41:00Z"),
			window:     hour,
			activeFrom: parse("2026-09-09T05:46:01Z"),
			want:       nil,
		},
		{
			name:       "hour bucket start equal to activeFrom is allowed",
			now:        parse("2026-09-09T09:10:00Z"),
			window:     hour,
			activeFrom: parse("2026-09-09T06:00:00Z"),
			want:       []string{"2026-09-09T06:00:00Z", "2026-09-09T07:00:00Z", "2026-09-09T08:00:00Z"},
		},
		{
			name:       "15min already fits shortly after start",
			now:        parse("2026-09-09T07:41:00Z"),
			window:     min15,
			activeFrom: parse("2026-09-09T05:46:01Z"),
			want:       []string{"2026-09-09T06:45:00Z", "2026-09-09T07:00:00Z", "2026-09-09T07:15:00Z"},
		},
		{
			name:       "day needs three prior midnights after start",
			now:        parse("2026-09-09T12:00:00Z"),
			window:     day,
			activeFrom: parse("2026-09-09T05:46:01Z"),
			want:       nil,
		},
		{
			name:       "day first-day bucket overlaps even when start is after midnight",
			now:        parse("2026-09-12T12:00:00Z"),
			window:     day,
			activeFrom: parse("2026-09-09T05:46:01Z"),
			want:       []string{"2026-09-09T00:00:00Z", "2026-09-10T00:00:00Z", "2026-09-11T00:00:00Z"},
		},
		{
			name:       "zero activeFrom does not clamp",
			now:        parse("2026-09-09T07:41:00Z"),
			window:     hour,
			activeFrom: time.Time{},
			want:       []string{"2026-09-09T04:00:00Z", "2026-09-09T05:00:00Z", "2026-09-09T06:00:00Z"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := backdatedBuckets(tc.now, tc.window, tc.activeFrom)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("got %v, want skip", formatBuckets(got))
				}
				return
			}
			if got == nil {
				t.Fatal("got skip, want buckets")
			}
			if formatBuckets(got) != strings.Join(tc.want, ",") {
				t.Errorf("got %s, want %s", formatBuckets(got), strings.Join(tc.want, ","))
			}
		})
	}
}

func oldPersistentSub(id string) sdktypes.SubscriptionResponse {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return sdktypes.SubscriptionResponse{ID: &id, StartDate: &start}
}

func TestBucketedMeterProbe_HappyPath(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	reg.LoadSeeds(e2eprobe.Seeds{
		PersistentCustomerIDs: []string{"e2eprobe-cust-persistent-0"},
		PersistentSubIDs:      []string{"sub_old"},
		BucketedFeatureIDs: map[string]string{
			"e2eprobe_max_15min_feature": "feat_15min",
			"e2eprobe_sum_hour_feature":  "feat_hour",
			"e2eprobe_max_day_feature":   "feat_day",
		},
	})
	fc.subs.subs = map[string]sdktypes.SubscriptionResponse{"sub_old": oldPersistentSub("sub_old")}

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

func TestBucketedMeterProbe_YoungSubSoftSkip(t *testing.T) {
	fc := newFakeClient()
	reg := e2eprobe.NewRegistry()
	lg, _ := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: itypes.LogLevelInfo}})

	start := time.Now().UTC().Add(-10 * time.Minute)
	subID := "sub_young"
	reg.LoadSeeds(e2eprobe.Seeds{
		PersistentCustomerIDs: []string{"e2eprobe-cust-persistent-0"},
		PersistentSubIDs:      []string{subID},
		BucketedFeatureIDs: map[string]string{
			"e2eprobe_max_15min_feature": "feat_15min",
			"e2eprobe_sum_hour_feature":  "feat_hour",
			"e2eprobe_max_day_feature":   "feat_day",
		},
	})
	fc.subs.subs = map[string]sdktypes.SubscriptionResponse{
		subID: {ID: &subID, StartDate: &start},
	}
	fc.events.analyticsEcho = true

	p := NewBucketedMeterProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("young sub must soft-skip; got %v", err)
	}
	if len(fc.events.ingested) != 0 {
		t.Errorf("no events should be ingested until 3 complete buckets fit; got %d", len(fc.events.ingested))
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
		PersistentSubIDs:      []string{"sub_old"},
		BucketedFeatureIDs: map[string]string{
			"e2eprobe_max_15min_feature": "feat_15min",
			"e2eprobe_sum_hour_feature":  "feat_hour",
			"e2eprobe_max_day_feature":   "feat_day",
		},
	})
	fc.subs.subs = map[string]sdktypes.SubscriptionResponse{"sub_old": oldPersistentSub("sub_old")}

	// Leading item: no points, as a subscription younger than the seeded window
	// returns. It must not decide the outcome.
	fc.events.analyticsItems = []sdktypes.UsageAnalyticItem{{}}
	fc.events.analyticsEcho = true

	p := NewBucketedMeterProbe(fc, reg, "test-run", lg)
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
}

func TestEventTimeInBucket(t *testing.T) {
	bucket := time.Date(2026, 9, 9, 5, 0, 0, 0, time.UTC)
	active := time.Date(2026, 9, 9, 5, 46, 1, 0, time.UTC)
	if got := eventTimeInBucket(bucket, active); !got.Equal(active) {
		t.Errorf("clamped = %s, want %s", got.Format(time.RFC3339), active.Format(time.RFC3339))
	}
	if got := eventTimeInBucket(bucket, time.Time{}); !got.Equal(bucket) {
		t.Errorf("zero activeFrom = %s, want bucket start", got.Format(time.RFC3339))
	}
	later := time.Date(2026, 9, 9, 6, 0, 0, 0, time.UTC)
	if got := eventTimeInBucket(later, active); !got.Equal(later) {
		t.Errorf("already after start = %s, want bucket start", got.Format(time.RFC3339))
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
