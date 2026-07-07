package testutil

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// seedUsageEvent inserts one event with a "qty" property (and optional extras).
func seedUsageEvent(t *testing.T, store *InMemoryEventStore, id string, ts time.Time, qty float64, extra map[string]interface{}) {
	t.Helper()
	props := map[string]interface{}{"qty": qty}
	for k, v := range extra {
		props[k] = v
	}
	require.NoError(t, store.InsertEvent(SetupContext(), &events.Event{
		ID:                 id,
		TenantID:           types.DefaultTenantID,
		EnvironmentID:      TestEnvironmentID,
		EventName:          "api_call",
		ExternalCustomerID: "ext_cust_1",
		Timestamp:          ts,
		Properties:         props,
	}))
}

// TestInMemoryEventStore_GetUsage_Windowed pins the windowed (WindowSize)
// aggregation branch against the real ClickHouse repository semantics
// (internal/repository/clickhouse/event.go:286-371 + aggregators.go): one
// result per window with events, ordered by window start, value computed by
// the aggregation type, and — like the real repo — result.Value left at zero
// for pure WindowSize queries.
func TestInMemoryEventStore_GetUsage_Windowed(t *testing.T) {
	day1 := time.Date(2025, 6, 3, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2025, 6, 4, 0, 0, 0, 0, time.UTC)
	month1 := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	month2 := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)

	seed := func(t *testing.T, store *InMemoryEventStore) {
		// Day 1: values 10, 7 (different hours); Day 2 (next month case reuses): 20.
		seedUsageEvent(t, store, "ev1", day1.Add(8*time.Hour), 10, map[string]interface{}{"region": "us"})
		seedUsageEvent(t, store, "ev2", day1.Add(9*time.Hour), 7, map[string]interface{}{"region": "eu"})
		seedUsageEvent(t, store, "ev3", day2.Add(8*time.Hour), 20, map[string]interface{}{"region": "us"})
		// July event for the month-window cases.
		seedUsageEvent(t, store, "ev4", month2.Add(24*time.Hour), 5, map[string]interface{}{"region": "us"})
	}

	baseParams := func(agg types.AggregationType, ws types.WindowSize) *events.UsageParams {
		return &events.UsageParams{
			EventName:       "api_call",
			PropertyName:    "qty",
			AggregationType: agg,
			WindowSize:      ws,
			StartTime:       time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			EndTime:         time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC),
		}
	}

	testCases := []struct {
		name        string
		agg         types.AggregationType
		windowSize  types.WindowSize
		wantWindows []time.Time
		wantValues  []string
	}{
		{
			name:        "day_window_max_takes_max_per_day",
			agg:         types.AggregationMax,
			windowSize:  types.WindowSizeDay,
			wantWindows: []time.Time{day1, day2, month2.Add(24 * time.Hour)},
			wantValues:  []string{"10", "20", "5"},
		},
		{
			name:        "month_window_max_takes_max_per_month",
			agg:         types.AggregationMax,
			windowSize:  types.WindowSizeMonth,
			wantWindows: []time.Time{month1, month2},
			wantValues:  []string{"20", "5"},
		},
		{
			name:        "day_window_sum_sums_per_day",
			agg:         types.AggregationSum,
			windowSize:  types.WindowSizeDay,
			wantWindows: []time.Time{day1, day2, month2.Add(24 * time.Hour)},
			wantValues:  []string{"17", "20", "5"},
		},
		{
			name:        "day_window_count_counts_per_day",
			agg:         types.AggregationCount,
			windowSize:  types.WindowSizeDay,
			wantWindows: []time.Time{day1, day2, month2.Add(24 * time.Hour)},
			wantValues:  []string{"2", "1", "1"},
		},
		{
			name:        "day_window_avg_averages_per_day",
			agg:         types.AggregationAvg,
			windowSize:  types.WindowSizeDay,
			wantWindows: []time.Time{day1, day2, month2.Add(24 * time.Hour)},
			wantValues:  []string{"8.5", "20", "5"},
		},
		{
			name:        "day_window_latest_takes_value_of_latest_event",
			agg:         types.AggregationLatest,
			windowSize:  types.WindowSizeDay,
			wantWindows: []time.Time{day1, day2, month2.Add(24 * time.Hour)},
			wantValues:  []string{"7", "20", "5"},
		},
		{
			name:        "month_window_count_unique_counts_distinct_property_values",
			agg:         types.AggregationCountUnique,
			windowSize:  types.WindowSizeMonth,
			wantWindows: []time.Time{month1, month2},
			// June has qty values {10, 7, 20} → 3 distinct; July has {5} → 1.
			wantValues: []string{"3", "1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemoryEventStore()
			seed(t, store)

			result, err := store.GetUsage(SetupContext(), baseParams(tc.agg, tc.windowSize))
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
			// The real repo never assigns result.Value for pure WindowSize
			// queries (event.go:286-371 only appends Results; Value is set only
			// in the BucketSize path at event.go:317).
			require.True(t, result.Value.IsZero(),
				"windowed queries must not populate Value (got %s)", result.Value)
		})
	}
}

// TestInMemoryEventStore_GetUsage_WindowedCountUniqueMissingProperty pins the
// JSONExtractString semantics: events without the property still count as one
// distinct "" value per window.
func TestInMemoryEventStore_GetUsage_WindowedCountUniqueMissingProperty(t *testing.T) {
	store := NewInMemoryEventStore()
	day := time.Date(2025, 6, 3, 0, 0, 0, 0, time.UTC)
	require.NoError(t, store.InsertEvent(SetupContext(), &events.Event{
		ID: "ev_no_prop", EventName: "api_call", ExternalCustomerID: "ext_cust_1",
		Timestamp: day.Add(time.Hour), Properties: map[string]interface{}{},
	}))
	seedUsageEvent(t, store, "ev_with_prop", day.Add(2*time.Hour), 4, nil)

	result, err := store.GetUsage(SetupContext(), &events.UsageParams{
		EventName:       "api_call",
		PropertyName:    "qty",
		AggregationType: types.AggregationCountUnique,
		WindowSize:      types.WindowSizeDay,
		StartTime:       day,
		EndTime:         day.AddDate(0, 0, 1),
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	// distinct values: {"", "4"} → 2, matching count(DISTINCT JSONExtractString(...)).
	require.True(t, result.Results[0].Value.Equal(decimal.NewFromInt(2)))
}

// TestInMemoryEventStore_GetUsage_MonthWindowWithBillingAnchor pins the custom
// monthly period bucketing (formatWindowSizeWithBillingAnchor,
// aggregators.go:187-201): with an anchor on the 5th, events on Jun 4 and
// Jun 6 fall into different windows (May 5 and Jun 5 period starts).
func TestInMemoryEventStore_GetUsage_MonthWindowWithBillingAnchor(t *testing.T) {
	store := NewInMemoryEventStore()
	seedUsageEvent(t, store, "ev_a", time.Date(2025, 6, 4, 12, 0, 0, 0, time.UTC), 10, nil)
	seedUsageEvent(t, store, "ev_b", time.Date(2025, 6, 6, 12, 0, 0, 0, time.UTC), 20, nil)

	anchor := time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC)
	result, err := store.GetUsage(SetupContext(), &events.UsageParams{
		EventName:       "api_call",
		PropertyName:    "qty",
		AggregationType: types.AggregationMax,
		WindowSize:      types.WindowSizeMonth,
		BillingAnchor:   &anchor,
		StartTime:       time.Date(2025, 5, 5, 0, 0, 0, 0, time.UTC),
		EndTime:         time.Date(2025, 7, 5, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 2)
	require.True(t, result.Results[0].WindowSize.Equal(time.Date(2025, 5, 5, 0, 0, 0, 0, time.UTC)))
	require.True(t, result.Results[0].Value.Equal(decimal.NewFromInt(10)))
	require.True(t, result.Results[1].WindowSize.Equal(time.Date(2025, 6, 5, 0, 0, 0, 0, time.UTC)))
	require.True(t, result.Results[1].Value.Equal(decimal.NewFromInt(20)))
}

// TestInMemoryEventStore_GetUsage_Bucketed pins the BucketSize branch
// (MaxAggregator/SumAggregator getWindowedQuery, aggregators.go:325-366 and
// 714-799): per-bucket reduce, total = sum of bucket values, and BucketSize
// winning over WindowSize (aggregator dispatch, aggregators.go:264-271,
// 653-660).
func TestInMemoryEventStore_GetUsage_Bucketed(t *testing.T) {
	day1 := time.Date(2025, 6, 3, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2025, 6, 4, 0, 0, 0, 0, time.UTC)

	newStore := func(t *testing.T) *InMemoryEventStore {
		store := NewInMemoryEventStore()
		seedUsageEvent(t, store, "ev1", day1.Add(8*time.Hour), 10, map[string]interface{}{"region": "us"})
		seedUsageEvent(t, store, "ev2", day1.Add(9*time.Hour), 7, map[string]interface{}{"region": "eu"})
		seedUsageEvent(t, store, "ev3", day2.Add(8*time.Hour), 20, map[string]interface{}{"region": "us"})
		return store
	}

	params := func(agg types.AggregationType) *events.UsageParams {
		return &events.UsageParams{
			EventName:       "api_call",
			PropertyName:    "qty",
			AggregationType: agg,
			BucketSize:      types.WindowSizeDay,
			StartTime:       day1,
			EndTime:         day2.AddDate(0, 0, 1),
		}
	}

	t.Run("max_bucketed_by_day_sums_bucket_maxes", func(t *testing.T) {
		result, err := newStore(t).GetUsage(SetupContext(), params(types.AggregationMax))
		require.NoError(t, err)
		require.Len(t, result.Results, 2)
		require.True(t, result.Results[0].Value.Equal(decimal.NewFromInt(10)))
		require.True(t, result.Results[1].Value.Equal(decimal.NewFromInt(20)))
		// total = sum of per-bucket maxes (aggregators.go:784).
		require.True(t, result.Value.Equal(decimal.NewFromInt(30)),
			"want total 30, got %s", result.Value)
	})

	t.Run("sum_bucketed_by_day_sums_bucket_sums", func(t *testing.T) {
		result, err := newStore(t).GetUsage(SetupContext(), params(types.AggregationSum))
		require.NoError(t, err)
		require.Len(t, result.Results, 2)
		require.True(t, result.Results[0].Value.Equal(decimal.NewFromInt(17)))
		require.True(t, result.Results[1].Value.Equal(decimal.NewFromInt(20)))
		require.True(t, result.Value.Equal(decimal.NewFromInt(37)))
	})

	t.Run("bucket_size_wins_over_window_size", func(t *testing.T) {
		p := params(types.AggregationMax)
		p.WindowSize = types.WindowSizeMonth // must be ignored when BucketSize is set
		result, err := newStore(t).GetUsage(SetupContext(), p)
		require.NoError(t, err)
		require.Len(t, result.Results, 2, "buckets must follow BucketSize (day), not WindowSize (month)")
		require.True(t, result.Value.Equal(decimal.NewFromInt(30)))
	})

	t.Run("max_with_group_by_returns_per_group_rows", func(t *testing.T) {
		p := params(types.AggregationMax)
		p.GroupBy = []string{"properties.region"}
		result, err := newStore(t).GetUsage(SetupContext(), p)
		require.NoError(t, err)
		// day1: us→10, eu→7; day2: us→20. Ordered by bucket then group key.
		require.Len(t, result.Results, 3)
		require.Equal(t, "eu", result.Results[0].GroupKey)
		require.True(t, result.Results[0].Value.Equal(decimal.NewFromInt(7)))
		require.Equal(t, "us", result.Results[1].GroupKey)
		require.True(t, result.Results[1].Value.Equal(decimal.NewFromInt(10)))
		require.Equal(t, "us", result.Results[2].GroupKey)
		require.True(t, result.Results[2].Value.Equal(decimal.NewFromInt(20)))
		// total = sum of all group values (aggregators.go:747).
		require.True(t, result.Value.Equal(decimal.NewFromInt(37)))
	})
}

// TestInMemoryEventStore_GetUsage_NonWindowed pins the non-windowed scalar
// branch (event.go:372-414) for the aggregation types shared with the windowed
// branch — parity across branches.
func TestInMemoryEventStore_GetUsage_NonWindowed(t *testing.T) {
	day := time.Date(2025, 6, 3, 0, 0, 0, 0, time.UTC)

	testCases := []struct {
		name string
		agg  types.AggregationType
		want string
	}{
		{name: "count", agg: types.AggregationCount, want: "3"},
		{name: "sum", agg: types.AggregationSum, want: "37"},
		{name: "max", agg: types.AggregationMax, want: "20"},
		{name: "avg", agg: types.AggregationAvg, want: "12.3333333333333333"},
		{name: "latest", agg: types.AggregationLatest, want: "20"},
		{name: "count_unique", agg: types.AggregationCountUnique, want: "3"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemoryEventStore()
			seedUsageEvent(t, store, "ev1", day.Add(8*time.Hour), 10, nil)
			seedUsageEvent(t, store, "ev2", day.Add(9*time.Hour), 7, nil)
			seedUsageEvent(t, store, "ev3", day.Add(10*time.Hour), 20, nil)

			result, err := store.GetUsage(SetupContext(), &events.UsageParams{
				EventName:       "api_call",
				PropertyName:    "qty",
				AggregationType: tc.agg,
				StartTime:       day,
				EndTime:         day.AddDate(0, 0, 1),
			})
			require.NoError(t, err)
			want := decimal.RequireFromString(tc.want)
			if tc.agg == types.AggregationAvg {
				// 37/3 is non-terminating; compare with tolerance.
				require.True(t, result.Value.Sub(want).Abs().LessThan(decimal.RequireFromString("0.0000001")),
					"want ~%s got %s", want, result.Value)
				return
			}
			require.True(t, result.Value.Equal(want), "want %s got %s", want, result.Value)
		})
	}
}
