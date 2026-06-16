package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestUsageBenchmark_CanonicalGroupKey verifies the group key is stable across
// map iteration orders for the same Source+Properties combination.
func TestUsageBenchmark_CanonicalGroupKey(t *testing.T) {
	cases := []struct {
		name string
		item dto.UsageAnalyticItem
		want string
	}{
		{
			name: "empty item",
			item: dto.UsageAnalyticItem{},
			want: "",
		},
		{
			name: "source only",
			item: dto.UsageAnalyticItem{Source: "stripe"},
			want: "source=stripe",
		},
		{
			name: "sources slice sorted",
			item: dto.UsageAnalyticItem{Sources: []string{"stripe", "api", "import"}},
			want: "sources=api,import,stripe",
		},
		{
			name: "properties sorted",
			item: dto.UsageAnalyticItem{
				Source: "api",
				Properties: map[string]string{
					"region": "us-east-1",
					"model":  "gpt-4",
				},
			},
			want: "source=api|properties.model=gpt-4|properties.region=us-east-1",
		},
		{
			name: "properties only no source",
			item: dto.UsageAnalyticItem{
				Properties: map[string]string{"x": "1"},
			},
			want: "properties.x=1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := canonicalGroupKey(&tc.item)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("stable across iterations", func(t *testing.T) {
		item := dto.UsageAnalyticItem{
			Source: "api",
			Properties: map[string]string{
				"a": "1", "b": "2", "c": "3", "d": "4", "e": "5",
			},
		}
		first := canonicalGroupKey(&item)
		for i := 0; i < 100; i++ {
			require.Equal(t, first, canonicalGroupKey(&item))
		}
	})
}

// TestUsageBenchmark_JoinAnalyticsResults verifies the three-tier emission:
//   - one summary row per event with response.TotalCost from each pipeline,
//   - one feature row per (feature_id, group_key) on each side,
//   - one line_item row per (feature_id, sub_line_item_id, group_key) on each side.
//
// Outer-join semantics still hold at the feature/line_item tier (feature-side
// only → feature_only; meter-side only → meter_only; both → matched).
func TestUsageBenchmark_JoinAnalyticsResults(t *testing.T) {
	featureResp := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.NewFromInt(15), // 10 + 5
		Items: []dto.UsageAnalyticItem{
			{FeatureID: "feat_1", Source: "api", TotalUsage: decimal.NewFromInt(100), TotalCost: decimal.NewFromInt(10), EventCount: 5},
			{FeatureID: "feat_2", Source: "stripe", TotalUsage: decimal.NewFromInt(50), TotalCost: decimal.NewFromInt(5), EventCount: 2},
		},
	}
	meterResp := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.NewFromInt(11), // 10 + 1
		Items: []dto.UsageAnalyticItem{
			{FeatureID: "feat_1", Source: "api", TotalUsage: decimal.NewFromInt(99), TotalCost: decimal.NewFromInt(10), EventCount: 5},
			{FeatureID: "feat_3", Source: "import", TotalUsage: decimal.NewFromInt(7), TotalCost: decimal.NewFromInt(1), EventCount: 1},
		},
	}

	records := joinAnalyticsResults(featureResp, meterResp)

	// Bucket records by row_type for tier-specific assertions.
	byType := make(map[events.AnalyticsBenchmarkRowType][]*events.AnalyticsBenchmarkRecord)
	for _, r := range records {
		byType[r.RowType] = append(byType[r.RowType], r)
	}

	// --- summary tier: 1 row carrying response.TotalCost from each side ---
	summary := byType[events.AnalyticsBenchmarkRowSummary]
	require.Len(t, summary, 1)
	require.True(t, summary[0].FeatureTotalCost.Equal(decimal.NewFromInt(15)))
	require.True(t, summary[0].MeterTotalCost.Equal(decimal.NewFromInt(11)))
	require.True(t, summary[0].CostDiff.Equal(decimal.NewFromInt(4)))
	require.Equal(t, uint64(2), summary[0].FeatureItemCount)
	require.Equal(t, uint64(2), summary[0].MeterItemCount)
	require.Equal(t, events.AnalyticsBenchmarkMatchMatched, summary[0].MatchStatus)
	require.Equal(t, events.AnalyticsBenchmarkDiffMaterial, summary[0].DiffReason)

	// --- feature tier: outer-joined by (feature_id, group_key) ---
	featureRows := byType[events.AnalyticsBenchmarkRowFeature]
	require.Len(t, featureRows, 3)

	byKey := make(map[string]*events.AnalyticsBenchmarkRecord, len(featureRows))
	for _, r := range featureRows {
		byKey[r.FeatureID+"|"+r.GroupKey] = r
	}

	matched := byKey["feat_1|source=api"]
	require.NotNil(t, matched)
	require.Equal(t, events.AnalyticsBenchmarkMatchMatched, matched.MatchStatus)
	require.True(t, matched.FeatureTotalUsage.Equal(decimal.NewFromInt(100)))
	require.True(t, matched.MeterTotalUsage.Equal(decimal.NewFromInt(99)))
	require.True(t, matched.UsageDiff.Equal(decimal.NewFromInt(1)))
	require.Equal(t, uint64(5), matched.FeatureEventCount)
	require.Equal(t, uint64(5), matched.MeterEventCount)
	// cost matches exactly → diff_reason = none
	require.Equal(t, events.AnalyticsBenchmarkDiffNone, matched.DiffReason)

	featureOnly := byKey["feat_2|source=stripe"]
	require.NotNil(t, featureOnly)
	require.Equal(t, events.AnalyticsBenchmarkMatchFeatureOnly, featureOnly.MatchStatus)
	require.True(t, featureOnly.FeatureTotalUsage.Equal(decimal.NewFromInt(50)))
	require.True(t, featureOnly.MeterTotalUsage.IsZero())
	require.Equal(t, events.AnalyticsBenchmarkDiffUnmatched, featureOnly.DiffReason)

	meterOnly := byKey["feat_3|source=import"]
	require.NotNil(t, meterOnly)
	require.Equal(t, events.AnalyticsBenchmarkMatchMeterOnly, meterOnly.MatchStatus)
	require.True(t, meterOnly.MeterTotalUsage.Equal(decimal.NewFromInt(7)))
	require.Equal(t, events.AnalyticsBenchmarkDiffUnmatched, meterOnly.DiffReason)

	// --- line_item tier: same shape as feature tier here since no sub_line_item_id ---
	lineItems := byType[events.AnalyticsBenchmarkRowLineItem]
	require.Len(t, lineItems, 3)
}

// TestUsageBenchmark_FeatureRowAggregatesLineItemSplits is the "h100" case: one
// feature billed across two sub_line_item_ids. Both sides emit two items each
// (different prices). The feature row must SUM them; the summary row's
// response.TotalCost must match the sum.
func TestUsageBenchmark_FeatureRowAggregatesLineItemSplits(t *testing.T) {
	featureResp := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.NewFromInt(1118), // 921 + 197
		Items: []dto.UsageAnalyticItem{
			{FeatureID: "feat_x", SubLineItemID: "li_1", PriceID: "p1", MeterID: "m_x", TotalUsage: decimal.NewFromInt(27659), TotalCost: decimal.NewFromInt(921), EventCount: 27659},
			{FeatureID: "feat_x", SubLineItemID: "li_2", PriceID: "p2", MeterID: "m_x", TotalUsage: decimal.NewFromInt(2954), TotalCost: decimal.NewFromInt(197), EventCount: 2954},
		},
	}
	meterResp := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.NewFromInt(1500), // pretend meter pipeline disagrees
		Items: []dto.UsageAnalyticItem{
			{FeatureID: "feat_x", SubLineItemID: "li_1", PriceID: "p1", MeterID: "m_x", TotalUsage: decimal.NewFromInt(30613), TotalCost: decimal.NewFromInt(1000), EventCount: 30613},
			{FeatureID: "feat_x", SubLineItemID: "li_2", PriceID: "p2", MeterID: "m_x", TotalUsage: decimal.NewFromInt(30613), TotalCost: decimal.NewFromInt(500), EventCount: 30613},
		},
	}

	records := joinAnalyticsResults(featureResp, meterResp)

	var feature *events.AnalyticsBenchmarkRecord
	for _, r := range records {
		if r.RowType == events.AnalyticsBenchmarkRowFeature && r.FeatureID == "feat_x" {
			feature = r
			break
		}
	}
	require.NotNil(t, feature, "expected one aggregated feature row for feat_x")

	// Feature row aggregates both line-item splits on each side.
	require.True(t, feature.FeatureTotalCost.Equal(decimal.NewFromInt(1118)),
		"feature aggregated cost should be 921+197=1118, got %s", feature.FeatureTotalCost)
	require.True(t, feature.MeterTotalCost.Equal(decimal.NewFromInt(1500)),
		"meter aggregated cost should be 1000+500=1500, got %s", feature.MeterTotalCost)
	require.True(t, feature.CostDiff.Equal(decimal.NewFromInt(-382)))
	require.Equal(t, uint64(2), feature.FeatureItemCount)
	require.Equal(t, uint64(2), feature.MeterItemCount)
	// >1 item on either side → multi_item flag (look at line_item rows for detail)
	require.Equal(t, events.AnalyticsBenchmarkDiffMultiItem, feature.DiffReason)
}

// TestUsageBenchmark_MultiFeatureMeterTagged covers the multi-feature-meter
// attribution case (the 144-meters-with-2-features finding): one meter, two
// features. Feature side has both; meter side has only the one its 1:1
// MeterToFeature map picked. The diff_reason on the resulting rows must surface
// as multi_feature_meter so we can filter out this known false positive.
func TestUsageBenchmark_MultiFeatureMeterTagged(t *testing.T) {
	featureResp := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.NewFromInt(15),
		Items: []dto.UsageAnalyticItem{
			{FeatureID: "feat_a", MeterID: "m_shared", TotalUsage: decimal.NewFromInt(100), TotalCost: decimal.NewFromInt(10), EventCount: 5},
			{FeatureID: "feat_b", MeterID: "m_shared", TotalUsage: decimal.NewFromInt(50), TotalCost: decimal.NewFromInt(5), EventCount: 3},
		},
	}
	meterResp := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.NewFromInt(12),
		Items: []dto.UsageAnalyticItem{
			{FeatureID: "feat_a", MeterID: "m_shared", TotalUsage: decimal.NewFromInt(150), TotalCost: decimal.NewFromInt(12), EventCount: 8},
		},
	}

	records := joinAnalyticsResults(featureResp, meterResp)

	var aRow, bRow *events.AnalyticsBenchmarkRecord
	for _, r := range records {
		if r.RowType != events.AnalyticsBenchmarkRowFeature {
			continue
		}
		switch r.FeatureID {
		case "feat_a":
			aRow = r
		case "feat_b":
			bRow = r
		}
	}
	require.NotNil(t, aRow)
	require.NotNil(t, bRow)
	// feat_a: matched but diff is non-zero AND m_shared has multiple features →
	// multi_feature_meter wins over material.
	require.Equal(t, events.AnalyticsBenchmarkDiffMultiFeatureMeter, aRow.DiffReason)
	// feat_b: meter side missing entirely → unmatched (takes priority over
	// multi_feature_meter).
	require.Equal(t, events.AnalyticsBenchmarkDiffUnmatched, bRow.DiffReason)
}

// TestUsageBenchmark_ExtractRequestFields verifies request fields promote correctly.
func TestUsageBenchmark_ExtractRequestFields(t *testing.T) {
	req := &dto.GetUsageAnalyticsRequest{
		ExternalCustomerID:  "cust_1",
		ExternalCustomerIDs: []string{"cust_2", "cust_3"},
		FeatureIDs:          []string{"feat_a"},
		Sources:             []string{"stripe"},
		GroupBy:             []string{"source", "feature_id"},
		WindowSize:          "HOUR",
		Expand:              []string{"price", "meter"},
		PropertyFilters:     map[string][]string{"model": {"gpt-4"}},
		IncludeChildren:     true,
	}
	raw, err := json.Marshal(req)
	require.NoError(t, err)

	got := extractRequestFields(req, raw)
	require.Equal(t, "cust_1", got.ExternalCustomerID)
	require.Equal(t, []string{"cust_2", "cust_3"}, got.ExternalCustomerIDs)
	require.Equal(t, []string{"feat_a"}, got.FeatureIDs)
	require.Equal(t, []string{"stripe"}, got.Sources)
	require.Equal(t, []string{"source", "feature_id"}, got.GroupBy)
	require.Equal(t, "HOUR", got.WindowSize)
	require.Equal(t, []string{"price", "meter"}, got.Expand)
	require.Equal(t, uint8(1), got.IncludeChildren)
	require.Equal(t, uint8(1), got.HasPropertyFilters)
	require.NotEmpty(t, got.RequestJSON)
}

// TestUsageBenchmark_DispatchByKind verifies the ProcessMessageForTest dispatcher
// routes subscription-kind and empty-kind events to the subscription path, and
// analytics-kind events to the analytics path. We use a stub repo to detect which
// codepath was hit without invoking real pipelines.
func TestUsageBenchmark_DispatchByKind(t *testing.T) {
	t.Run("empty kind treated as subscription", func(t *testing.T) {
		stub := &stubSubscriptionBenchRepo{}
		svc := &usageBenchmarkService{benchRepo: stub}

		evt := events.UsageBenchmarkEvent{
			SubscriptionID: "sub_1",
			StartTime:      time.Now().Add(-time.Hour),
			EndTime:        time.Now(),
			TenantID:       "tenant_1",
			EnvironmentID:  "env_1",
		}
		payload, err := json.Marshal(evt)
		require.NoError(t, err)
		msg := message.NewMessage("test-1", payload)
		msg.Metadata.Set("tenant_id", evt.TenantID)
		msg.Metadata.Set("environment_id", evt.EnvironmentID)

		require.NoError(t, svc.ProcessMessageForTest(msg))
		require.Equal(t, 1, stub.calls, "subscription benchRepo.Insert should be called for empty Kind")
	})

	t.Run("explicit subscription kind", func(t *testing.T) {
		stub := &stubSubscriptionBenchRepo{}
		svc := &usageBenchmarkService{benchRepo: stub}

		evt := events.UsageBenchmarkEvent{
			Kind:           events.UsageBenchmarkKindSubscription,
			SubscriptionID: "sub_2",
			StartTime:      time.Now().Add(-time.Hour),
			EndTime:        time.Now(),
			TenantID:       "tenant_1",
			EnvironmentID:  "env_1",
		}
		payload, err := json.Marshal(evt)
		require.NoError(t, err)
		msg := message.NewMessage("test-2", payload)

		require.NoError(t, svc.ProcessMessageForTest(msg))
		require.Equal(t, 1, stub.calls)
	})

	t.Run("analytics kind routes to analytics path", func(t *testing.T) {
		stubAnalytics := &stubAnalyticsBenchRepo{}
		svc := &usageBenchmarkService{
			analyticsBenchRepo: stubAnalytics,
			// no featureUsageTrackingService / meterUsageService → both sides return empty
		}

		req := dto.GetUsageAnalyticsRequest{
			ExternalCustomerID: "cust_1",
			StartTime:          time.Now().Add(-time.Hour),
			EndTime:            time.Now(),
		}
		raw, err := json.Marshal(&req)
		require.NoError(t, err)

		evt := events.UsageBenchmarkEvent{
			Kind:             events.UsageBenchmarkKindAnalytics,
			TenantID:         "tenant_1",
			EnvironmentID:    "env_1",
			StartTime:        req.StartTime,
			EndTime:          req.EndTime,
			AnalyticsRequest: raw,
		}
		payload, err := json.Marshal(evt)
		require.NoError(t, err)
		msg := message.NewMessage("test-3", payload)

		require.NoError(t, svc.ProcessMessageForTest(msg))
		// Both pipelines return nil → no records → BulkInsert NOT called.
		require.Equal(t, 0, stubAnalytics.calls)
	})
}

// --- test stubs ---

type stubSubscriptionBenchRepo struct {
	calls int
}

func (s *stubSubscriptionBenchRepo) Insert(_ context.Context, _ *events.UsageBenchmarkRecord) error {
	s.calls++
	return nil
}

type stubAnalyticsBenchRepo struct {
	calls int
	rows  int
}

func (s *stubAnalyticsBenchRepo) BulkInsert(_ context.Context, records []*events.AnalyticsBenchmarkRecord) error {
	s.calls++
	s.rows += len(records)
	return nil
}
