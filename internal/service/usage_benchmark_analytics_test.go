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

// TestUsageBenchmark_JoinAnalyticsResults verifies outer-join semantics: items
// only in feature side → feature_only; only in meter → meter_only; in both → matched.
func TestUsageBenchmark_JoinAnalyticsResults(t *testing.T) {
	feature := []dto.UsageAnalyticItem{
		{
			FeatureID:  "feat_1",
			Source:     "api",
			TotalUsage: decimal.NewFromInt(100),
			TotalCost:  decimal.NewFromInt(10),
			EventCount: 5,
		},
		{
			FeatureID:  "feat_2",
			Source:     "stripe",
			TotalUsage: decimal.NewFromInt(50),
			TotalCost:  decimal.NewFromInt(5),
			EventCount: 2,
		},
	}
	meter := []dto.UsageAnalyticItem{
		{
			FeatureID:  "feat_1",
			Source:     "api",
			TotalUsage: decimal.NewFromInt(99),
			TotalCost:  decimal.NewFromInt(10),
			EventCount: 5,
		},
		{
			FeatureID:  "feat_3",
			Source:     "import",
			TotalUsage: decimal.NewFromInt(7),
			TotalCost:  decimal.NewFromInt(1),
			EventCount: 1,
		},
	}

	records := joinAnalyticsResults(feature, meter)
	require.Len(t, records, 3)

	byKey := make(map[string]*events.AnalyticsBenchmarkRecord, len(records))
	for _, r := range records {
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

	featureOnly := byKey["feat_2|source=stripe"]
	require.NotNil(t, featureOnly)
	require.Equal(t, events.AnalyticsBenchmarkMatchFeatureOnly, featureOnly.MatchStatus)
	require.True(t, featureOnly.FeatureTotalUsage.Equal(decimal.NewFromInt(50)))
	require.True(t, featureOnly.MeterTotalUsage.IsZero())
	require.True(t, featureOnly.UsageDiff.Equal(decimal.NewFromInt(50)))
	require.Equal(t, uint64(2), featureOnly.FeatureEventCount)
	require.Equal(t, uint64(0), featureOnly.MeterEventCount)

	meterOnly := byKey["feat_3|source=import"]
	require.NotNil(t, meterOnly)
	require.Equal(t, events.AnalyticsBenchmarkMatchMeterOnly, meterOnly.MatchStatus)
	require.True(t, meterOnly.FeatureTotalUsage.IsZero())
	require.True(t, meterOnly.MeterTotalUsage.Equal(decimal.NewFromInt(7)))
	require.True(t, meterOnly.UsageDiff.Equal(decimal.NewFromInt(-7)))
	require.Equal(t, uint64(0), meterOnly.FeatureEventCount)
	require.Equal(t, uint64(1), meterOnly.MeterEventCount)
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

		require.NoError(t, svc.ProcessMessageForTest(context.Background(), msg))
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

		require.NoError(t, svc.ProcessMessageForTest(context.Background(), msg))
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

		require.NoError(t, svc.ProcessMessageForTest(context.Background(), msg))
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
