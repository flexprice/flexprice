package testutil

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// featureUsageRow builds a realistic feature_usage fixture (Sign 1, scoped to
// the test tenant/environment) — mirrors what the production tracking service
// writes.
func featureUsageRow(id string, ts time.Time, qty string, mutate func(*events.FeatureUsage)) *events.FeatureUsage {
	row := &events.FeatureUsage{
		Event: events.Event{
			ID:                 id,
			TenantID:           types.DefaultTenantID,
			EnvironmentID:      TestEnvironmentID,
			EventName:          "gpu_usage",
			ExternalCustomerID: "ext_cust_1",
			Timestamp:          ts,
			Properties:         map[string]interface{}{},
		},
		SubscriptionID: "sub_1",
		SubLineItemID:  "li_1",
		PriceID:        "price_1",
		FeatureID:      "feat_1",
		MeterID:        "meter_1",
		QtyTotal:       decimal.RequireFromString(qty),
		Sign:           1,
	}
	if mutate != nil {
		mutate(row)
	}
	return row
}

func bucketedParams(agg types.AggregationType, ws types.WindowSize, start, end time.Time) *events.FeatureUsageParams {
	return &events.FeatureUsageParams{
		MeterID: "meter_1",
		PriceID: "price_1",
		UsageParams: &events.UsageParams{
			ExternalCustomerIDs: []string{"ext_cust_1"},
			AggregationType:     agg,
			WindowSize:          ws,
			StartTime:           start,
			EndTime:             end,
		},
	}
}

// TestInMemoryFeatureUsageStore_GetUsageForBucketedMeters pins the in-memory
// double against the real ClickHouse query semantics
// (internal/repository/clickhouse/feature_usage.go:2275-2501): per-bucket
// max(qty_total) (or sum for SUM meters), total = sum of bucket values,
// buckets ordered by window start.
func TestInMemoryFeatureUsageStore_GetUsageForBucketedMeters(t *testing.T) {
	ctx := SetupContext()
	day1 := time.Date(2025, 6, 3, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2025, 6, 4, 0, 0, 0, 0, time.UTC)
	periodStart := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)

	seed := func(t *testing.T) *InMemoryFeatureUsageStore {
		store := NewInMemoryFeatureUsageStore()
		// Day 1: qty 10 and 7 → max 10, sum 17. Day 2: qty 20.
		require.NoError(t, store.InsertProcessedEvent(ctx, featureUsageRow("fu1", day1.Add(8*time.Hour), "10", nil)))
		require.NoError(t, store.InsertProcessedEvent(ctx, featureUsageRow("fu2", day1.Add(9*time.Hour), "7", nil)))
		require.NoError(t, store.InsertProcessedEvent(ctx, featureUsageRow("fu3", day2.Add(8*time.Hour), "20", nil)))
		return store
	}

	testCases := []struct {
		name        string
		agg         types.AggregationType
		wantWindows []time.Time
		wantValues  []string
		wantTotal   string
	}{
		{
			// MAX is also the default when AggregationType is unset
			// (feature_usage.go:2386-2396).
			name:        "max_per_day_bucket_total_is_sum_of_maxes",
			agg:         types.AggregationMax,
			wantWindows: []time.Time{day1, day2},
			wantValues:  []string{"10", "20"},
			wantTotal:   "30",
		},
		{
			name:        "sum_per_day_bucket_total_is_sum_of_sums",
			agg:         types.AggregationSum,
			wantWindows: []time.Time{day1, day2},
			wantValues:  []string{"17", "20"},
			wantTotal:   "37",
		},
		{
			name:        "unset_aggregation_defaults_to_max",
			agg:         "",
			wantWindows: []time.Time{day1, day2},
			wantValues:  []string{"10", "20"},
			wantTotal:   "30",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := seed(t)
			result, err := store.GetUsageForBucketedMeters(ctx,
				bucketedParams(tc.agg, types.WindowSizeDay, periodStart, periodEnd))
			require.NoError(t, err)
			require.Equal(t, tc.agg, result.Type)
			require.Len(t, result.Results, len(tc.wantWindows))
			for i, want := range tc.wantWindows {
				require.True(t, result.Results[i].WindowSize.Equal(want),
					"window %d: want %s got %s", i, want, result.Results[i].WindowSize)
				wantVal := decimal.RequireFromString(tc.wantValues[i])
				require.True(t, result.Results[i].Value.Equal(wantVal),
					"window %d: want value %s got %s", i, wantVal, result.Results[i].Value)
			}
			require.True(t, result.Value.Equal(decimal.RequireFromString(tc.wantTotal)),
				"total: want %s got %s", tc.wantTotal, result.Value)
		})
	}
}

// TestInMemoryFeatureUsageStore_GetUsageForBucketedMeters_Filters pins the
// PREWHERE filters of the real query: tenant/environment scoping, sign != 0,
// entity ID filters, property filters, and the exclusive end bound
// (timestamp >= StartTime AND timestamp < EndTime, aggregators.go:243-259).
func TestInMemoryFeatureUsageStore_GetUsageForBucketedMeters_Filters(t *testing.T) {
	ctx := SetupContext()
	day1 := time.Date(2025, 6, 3, 0, 0, 0, 0, time.UTC)
	periodStart := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)

	testCases := []struct {
		name      string
		mutate    func(*events.FeatureUsage)
		params    func() *events.FeatureUsageParams
		wantTotal string
	}{
		{
			name:      "row_with_sign_zero_is_excluded",
			mutate:    func(r *events.FeatureUsage) { r.Sign = 0 },
			wantTotal: "10",
		},
		{
			name:      "row_from_other_tenant_is_excluded",
			mutate:    func(r *events.FeatureUsage) { r.TenantID = "other_tenant" },
			wantTotal: "10",
		},
		{
			name:      "row_from_other_environment_is_excluded",
			mutate:    func(r *events.FeatureUsage) { r.EnvironmentID = "env_other" },
			wantTotal: "10",
		},
		{
			name:      "row_for_other_meter_is_excluded",
			mutate:    func(r *events.FeatureUsage) { r.MeterID = "meter_other" },
			wantTotal: "10",
		},
		{
			name:      "row_for_other_customer_is_excluded",
			mutate:    func(r *events.FeatureUsage) { r.ExternalCustomerID = "ext_other" },
			wantTotal: "10",
		},
		{
			name:      "row_at_end_time_is_excluded_end_bound_is_exclusive",
			mutate:    func(r *events.FeatureUsage) { r.Timestamp = periodEnd },
			wantTotal: "10",
		},
		{
			name: "sub_line_item_filter_applies",
			mutate: func(r *events.FeatureUsage) {
				r.SubLineItemID = "li_other"
				r.Timestamp = day1.Add(10 * time.Hour)
			},
			params: func() *events.FeatureUsageParams {
				p := bucketedParams(types.AggregationMax, types.WindowSizeDay, periodStart, periodEnd)
				p.SubLineItemID = "li_1"
				return p
			},
			wantTotal: "10",
		},
		{
			name: "property_filter_excludes_non_matching_rows",
			mutate: func(r *events.FeatureUsage) {
				r.Properties = map[string]interface{}{"region": "eu"}
				r.Timestamp = day1.Add(10 * time.Hour)
			},
			params: func() *events.FeatureUsageParams {
				p := bucketedParams(types.AggregationMax, types.WindowSizeDay, periodStart, periodEnd)
				p.Filters = map[string][]string{"region": {"us"}}
				return p
			},
			wantTotal: "10",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemoryFeatureUsageStore()
			// Matching row (qty 10) plus one row mutated out of scope (qty 99).
			base := featureUsageRow("fu_in", day1.Add(8*time.Hour), "10", nil)
			base.Properties = map[string]interface{}{"region": "us"}
			require.NoError(t, store.InsertProcessedEvent(ctx, base))
			require.NoError(t, store.InsertProcessedEvent(ctx,
				featureUsageRow("fu_out", day1.Add(9*time.Hour), "99", tc.mutate)))

			params := bucketedParams(types.AggregationMax, types.WindowSizeDay, periodStart, periodEnd)
			if tc.params != nil {
				params = tc.params()
			}
			result, err := store.GetUsageForBucketedMeters(ctx, params)
			require.NoError(t, err)
			require.True(t, result.Value.Equal(decimal.RequireFromString(tc.wantTotal)),
				"total: want %s got %s", tc.wantTotal, result.Value)
		})
	}
}

// TestInMemoryFeatureUsageStore_GetUsageForBucketedMeters_GroupBy pins the
// 3-level group-by aggregation (feature_usage.go:2404-2453): max per
// (bucket, group), rows carry GroupKey ordered by bucket then group key, and
// total = sum of all group values.
func TestInMemoryFeatureUsageStore_GetUsageForBucketedMeters_GroupBy(t *testing.T) {
	ctx := SetupContext()
	day1 := time.Date(2025, 6, 3, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2025, 6, 4, 0, 0, 0, 0, time.UTC)

	store := NewInMemoryFeatureUsageStore()
	withKRN := func(krn, qty string, ts time.Time, id string) *events.FeatureUsage {
		return featureUsageRow(id, ts, qty, func(r *events.FeatureUsage) {
			r.Properties = map[string]interface{}{"krn": krn}
		})
	}
	// Day 1: krn_a max(4, 9) = 9, krn_b = 5. Day 2: krn_a = 3.
	require.NoError(t, store.InsertProcessedEvent(ctx, withKRN("krn_a", "4", day1.Add(1*time.Hour), "g1")))
	require.NoError(t, store.InsertProcessedEvent(ctx, withKRN("krn_a", "9", day1.Add(2*time.Hour), "g2")))
	require.NoError(t, store.InsertProcessedEvent(ctx, withKRN("krn_b", "5", day1.Add(3*time.Hour), "g3")))
	require.NoError(t, store.InsertProcessedEvent(ctx, withKRN("krn_a", "3", day2.Add(1*time.Hour), "g4")))

	params := bucketedParams(types.AggregationMax, types.WindowSizeDay,
		time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC))
	params.GroupBy = []string{"properties.krn"}

	result, err := store.GetUsageForBucketedMeters(ctx, params)
	require.NoError(t, err)
	require.Len(t, result.Results, 3)

	require.True(t, result.Results[0].WindowSize.Equal(day1))
	require.Equal(t, "krn_a", result.Results[0].GroupKey)
	require.True(t, result.Results[0].Value.Equal(decimal.NewFromInt(9)))

	require.True(t, result.Results[1].WindowSize.Equal(day1))
	require.Equal(t, "krn_b", result.Results[1].GroupKey)
	require.True(t, result.Results[1].Value.Equal(decimal.NewFromInt(5)))

	require.True(t, result.Results[2].WindowSize.Equal(day2))
	require.Equal(t, "krn_a", result.Results[2].GroupKey)
	require.True(t, result.Results[2].Value.Equal(decimal.NewFromInt(3)))

	// total = sum of all per-(bucket, group) values (feature_usage.go:2432).
	require.True(t, result.Value.Equal(decimal.NewFromInt(17)),
		"total: want 17 got %s", result.Value)
}
