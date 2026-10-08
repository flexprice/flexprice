package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/shopspring/decimal"
)

func TestGetEarliestUsageTimestamp_ScopedByTenantAndEnvironment(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryMeterUsageStore()

	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	seed := func(id, tenant, env string, at time.Time) *events.MeterUsage {
		return &events.MeterUsage{
			Event: events.Event{
				ID:                 id,
				TenantID:           tenant,
				EnvironmentID:      env,
				ExternalCustomerID: "cust_ext",
				EventName:          "api_call",
				Timestamp:          at,
			},
			MeterID:  "meter_1",
			QtyTotal: decimal.NewFromInt(1),
		}
	}
	if err := store.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		seed("ev_other_tenant", "tenant_b", "env_1", base),               // earlier, wrong tenant
		seed("ev_other_env", "tenant_a", "env_2", base.Add(1*time.Hour)), // earlier, wrong env
		seed("ev_in_scope", "tenant_a", "env_1", base.Add(2*time.Hour)),  // the one that must win
		seed("ev_in_scope_later", "tenant_a", "env_1", base.Add(3*time.Hour)),
	}); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	got, err := store.GetEarliestUsageTimestamp(ctx, &events.MeterUsageQueryParams{
		TenantID:            "tenant_a",
		EnvironmentID:       "env_1",
		ExternalCustomerIDs: []string{"cust_ext"},
		MeterID:             "meter_1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || !got.Equal(base.Add(2*time.Hour)) {
		t.Fatalf("earliest must come from the requested tenant+env only; got %v", got)
	}

	// Missing scope is malformed input, never a match-everything wildcard.
	for name, params := range map[string]*events.MeterUsageQueryParams{
		"missing tenant": {EnvironmentID: "env_1", MeterID: "meter_1"},
		"missing env":    {TenantID: "tenant_a", MeterID: "meter_1"},
		"nil params":     nil,
	} {
		if _, err := store.GetEarliestUsageTimestamp(ctx, params); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

// TestGetCumulativeDailyUsage_HonorsTimezone proves day bucketing follows
// params.Timezone (mirroring the real query's normalizeCHTimezone-driven
// toStartOfDay), not a hardcoded UTC boundary. Two events straddle UTC
// midnight but fall on the same calendar day in Asia/Kolkata (UTC+5:30):
// with that timezone they must roll into a single day; with the UTC default
// they must split into two.
func TestGetCumulativeDailyUsage_HonorsTimezone(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryMeterUsageStore()

	seed := func(id string, at time.Time, qty int64) *events.MeterUsage {
		return &events.MeterUsage{
			Event: events.Event{
				ID:            id,
				TenantID:      "tenant_a",
				EnvironmentID: "env_1",
				Timestamp:     at,
			},
			MeterID:  "meter_1",
			QtyTotal: decimal.NewFromInt(qty),
		}
	}

	// 23:00 UTC on the 1st and 01:00 UTC on the 2nd are different UTC days,
	// but both land on 2026-07-02 in Asia/Kolkata (UTC+05:30).
	before := time.Date(2026, 7, 1, 23, 0, 0, 0, time.UTC)
	after := time.Date(2026, 7, 2, 1, 0, 0, 0, time.UTC)

	if err := store.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		seed("ev_1", before, 10),
		seed("ev_2", after, 5),
	}); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	baseParams := events.DailyUsageParams{
		TenantID:      "tenant_a",
		EnvironmentID: "env_1",
		MeterIDs:      []string{"meter_1"},
		StartTime:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndTime:       time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
	}

	t.Run("non-UTC timezone merges both events into one local day", func(t *testing.T) {
		params := baseParams
		params.Timezone = "Asia/Kolkata"

		byMeter, err := store.GetDailyUsageByMeter(ctx, &params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		points := byMeter["meter_1"]
		if len(points) != 1 {
			t.Fatalf("expected 1 day bucket in Asia/Kolkata, got %d: %+v", len(points), points)
		}

		loc, err := time.LoadLocation("Asia/Kolkata")
		if err != nil {
			t.Fatalf("failed to load Asia/Kolkata: %v", err)
		}
		wantDay := time.Date(2026, 7, 2, 0, 0, 0, 0, loc)
		if !points[0].Day.Equal(wantDay) {
			t.Fatalf("expected day %v, got %v", wantDay, points[0].Day)
		}
		if !points[0].Qty.Equal(decimal.NewFromInt(15)) {
			t.Fatalf("expected merged day qty 15, got %v", points[0].Qty)
		}
	})

	t.Run("empty timezone defaults to UTC and splits across the boundary", func(t *testing.T) {
		params := baseParams
		params.Timezone = ""

		byMeter, err := store.GetDailyUsageByMeter(ctx, &params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		points := byMeter["meter_1"]
		if len(points) != 2 {
			t.Fatalf("expected 2 day buckets in UTC, got %d: %+v", len(points), points)
		}
		// Per-day, not cumulative: the caller accumulates from its own window
		// start, which is what lets one read serve every line item.
		if !points[0].Qty.Equal(decimal.NewFromInt(10)) {
			t.Fatalf("expected first-day qty 10, got %v", points[0].Qty)
		}
		if !points[1].Qty.Equal(decimal.NewFromInt(5)) {
			t.Fatalf("expected second-day qty 5 (not a running total), got %v", points[1].Qty)
		}
	})

	t.Run("invalid timezone falls back to UTC", func(t *testing.T) {
		params := baseParams
		params.Timezone = "Not/A_Real_Zone"

		byMeter, err := store.GetDailyUsageByMeter(ctx, &params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		points := byMeter["meter_1"]
		if len(points) != 2 {
			t.Fatalf("expected UTC fallback to split into 2 day buckets, got %d: %+v", len(points), points)
		}
	})
}
