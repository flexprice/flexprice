package clickhouse

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func rollupParams(agg types.AggregationType, start, end time.Time) *events.MeterUsageQueryParams {
	return &events.MeterUsageQueryParams{
		TenantID:           "t1",
		EnvironmentID:      "e1",
		ExternalCustomerID: "c1",
		MeterID:            "m1",
		AggregationType:    agg,
		StartTime:          start,
		EndTime:            end,
	}
}

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestPlanRollup_HourBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		start, end  time.Time
		ok          bool
		bucketStart time.Time
		bucketEnd   time.Time
		edges       []events.TimeRange
	}{
		{
			name:  "aligned range has no edges",
			start: ts("2026-11-01T00:00:00Z"), end: ts("2026-11-02T00:00:00Z"),
			ok: true, bucketStart: ts("2026-11-01T00:00:00Z"), bucketEnd: ts("2026-11-02T00:00:00Z"),
		},
		{
			name:  "both edges partial",
			start: ts("2026-11-01T00:15:30Z"), end: ts("2026-11-01T05:42:00Z"),
			ok: true, bucketStart: ts("2026-11-01T01:00:00Z"), bucketEnd: ts("2026-11-01T05:00:00Z"),
			edges: []events.TimeRange{
				{Start: ts("2026-11-01T00:15:30Z"), End: ts("2026-11-01T01:00:00Z")},
				{Start: ts("2026-11-01T05:00:00Z"), End: ts("2026-11-01T05:42:00Z")},
			},
		},
		{
			name:  "only right edge partial",
			start: ts("2026-11-01T00:00:00Z"), end: ts("2026-11-15T08:10:00Z"),
			ok: true, bucketStart: ts("2026-11-01T00:00:00Z"), bucketEnd: ts("2026-11-15T08:00:00Z"),
			edges: []events.TimeRange{{Start: ts("2026-11-15T08:00:00Z"), End: ts("2026-11-15T08:10:00Z")}},
		},
		{
			name:  "no full hour falls back",
			start: ts("2026-11-01T00:10:00Z"), end: ts("2026-11-01T00:50:00Z"),
		},
		{
			name:  "straddling one boundary falls back",
			start: ts("2026-11-01T00:30:00Z"), end: ts("2026-11-01T01:30:00Z"),
		},
		{
			name:  "unbounded start",
			start: time.Time{}, end: ts("2026-11-01T03:20:00Z"),
			ok: true, bucketEnd: ts("2026-11-01T03:00:00Z"),
			edges: []events.TimeRange{{Start: ts("2026-11-01T03:00:00Z"), End: ts("2026-11-01T03:20:00Z")}},
		},
		{
			name:  "non-UTC input normalised",
			start: ts("2026-11-01T05:30:00+05:30"), end: ts("2026-11-01T10:00:00+05:30"),
			ok: true, bucketStart: ts("2026-11-01T00:00:00Z"), bucketEnd: ts("2026-11-01T04:00:00Z"),
			edges: []events.TimeRange{{Start: ts("2026-11-01T04:00:00Z"), End: ts("2026-11-01T04:30:00Z")}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, ok := planRollup(rollupParams(types.AggregationSum, tt.start, tt.end))
			require.Equal(t, tt.ok, ok)
			if !ok {
				return
			}
			require.True(t, tt.bucketStart.Equal(plan.bucketStart), "bucketStart %s", plan.bucketStart)
			require.True(t, tt.bucketEnd.Equal(plan.bucketEnd), "bucketEnd %s", plan.bucketEnd)
			require.Len(t, plan.edges, len(tt.edges))
			for i := range tt.edges {
				require.True(t, tt.edges[i].Start.Equal(plan.edges[i].Start))
				require.True(t, tt.edges[i].End.Equal(plan.edges[i].End))
			}
		})
	}
}

func TestBuildRollupScalarQuery_SumSplitsIntoRollupAndEdges(t *testing.T) {
	p := rollupParams(types.AggregationSum, ts("2026-11-01T00:15:00Z"), ts("2026-11-01T05:42:00Z"))
	q, args, ok := buildRollupScalarQuery(p, "meter_usage_hourly")
	require.True(t, ok)
	require.Contains(t, q, "FROM meter_usage_hourly WHERE")
	require.Contains(t, q, "sum(qty_sum)")
	require.Contains(t, q, "UNION ALL")
	require.Contains(t, q, "FROM meter_usage WHERE")
	require.Contains(t, q, "SUM(qty_total)")
	require.NotContains(t, q, "FINAL")
	require.Equal(t, strings.Count(q, "?"), len(args))
	require.Equal(t, []interface{}{
		"t1", "e1", "c1", "m1", ts("2026-11-01T01:00:00Z"), ts("2026-11-01T05:00:00Z"),
		"t1", "e1", "c1", "m1",
		ts("2026-11-01T00:15:00Z"), ts("2026-11-01T01:00:00Z"),
		ts("2026-11-01T05:00:00Z"), ts("2026-11-01T05:42:00Z"),
	}, args)
}

func TestBuildRollupScalarQuery_AlignedRangeSkipsRaw(t *testing.T) {
	p := rollupParams(types.AggregationCount, ts("2026-11-01T00:00:00Z"), ts("2026-11-15T00:00:00Z"))
	q, args, ok := buildRollupScalarQuery(p, "flexprice.meter_usage_rollup_lt")
	require.True(t, ok)
	require.Contains(t, q, "FROM flexprice.meter_usage_rollup_lt WHERE")
	require.Contains(t, q, "sum(event_count)")
	require.NotContains(t, q, "UNION ALL")
	require.NotContains(t, q, "FROM meter_usage WHERE")
	require.Equal(t, strings.Count(q, "?"), len(args))
}

func TestBuildRollupMultiMeterQuery_GroupsByMeter(t *testing.T) {
	p := &events.MeterUsageQueryParams{
		TenantID:            "t1",
		EnvironmentID:       "e1",
		ExternalCustomerIDs: []string{"c1", "c2"},
		MeterIDs:            []string{"m1", "m2", "m3"},
		AggregationType:     types.AggregationSum,
		StartTime:           ts("2026-11-01T00:00:00Z"),
		EndTime:             ts("2026-11-15T08:10:00Z"),
	}
	q, args, ok := buildRollupMultiMeterQuery(p, "meter_usage_hourly")
	require.True(t, ok)
	require.Equal(t, 3, strings.Count(q, "GROUP BY meter_id"))
	require.Contains(t, q, "external_customer_id IN (?, ?)")
	require.Contains(t, q, "meter_id IN (?, ?, ?)")
	require.Equal(t, strings.Count(q, "?"), len(args))
}

func TestBuildRollupQueries_FallBack(t *testing.T) {
	start, end := ts("2026-11-01T00:00:00Z"), ts("2026-11-15T08:10:00Z")
	cases := map[string]func(p *events.MeterUsageQueryParams){
		"final":           func(p *events.MeterUsageQueryParams) { p.UseFinal = true },
		"max":             func(p *events.MeterUsageQueryParams) { p.AggregationType = types.AggregationMax },
		"count unique":    func(p *events.MeterUsageQueryParams) { p.AggregationType = types.AggregationCountUnique },
		"latest":          func(p *events.MeterUsageQueryParams) { p.AggregationType = types.AggregationLatest },
		"avg":             func(p *events.MeterUsageQueryParams) { p.AggregationType = types.AggregationAvg },
		"windowed":        func(p *events.MeterUsageQueryParams) { p.WindowSize = types.WindowSizeDay },
		"group by":        func(p *events.MeterUsageQueryParams) { p.GroupBy = []string{"source"} },
		"property filter": func(p *events.MeterUsageQueryParams) { p.PropertyFilters = map[string][]string{"model": {"x"}} },
		"source filter":   func(p *events.MeterUsageQueryParams) { p.Sources = []string{"api"} },
		"id filter":       func(p *events.MeterUsageQueryParams) { p.IDs = []string{"ev1"} },
		"multi range": func(p *events.MeterUsageQueryParams) {
			p.TimeRanges = []events.TimeRange{{Start: start, End: end}, {Start: start, End: end}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := rollupParams(types.AggregationSum, start, end)
			p.MeterIDs = []string{"m1"}
			mutate(p)
			_, _, ok := buildRollupScalarQuery(p, "meter_usage_hourly")
			require.False(t, ok)
			_, _, ok = buildRollupMultiMeterQuery(p, "meter_usage_hourly")
			require.False(t, ok)
		})
	}

	t.Run("disabled", func(t *testing.T) {
		_, _, ok := buildRollupScalarQuery(rollupParams(types.AggregationSum, start, end), "")
		require.False(t, ok)
	})
}

func TestBuildRollupScalarQuery_SingleTimeRange(t *testing.T) {
	p := rollupParams(types.AggregationSum, time.Time{}, time.Time{})
	p.TimeRanges = []events.TimeRange{{Start: ts("2026-11-01T00:00:00Z"), End: ts("2026-11-02T00:30:00Z")}}
	q, args, ok := buildRollupScalarQuery(p, "meter_usage_hourly")
	require.True(t, ok)
	require.Contains(t, q, "UNION ALL")
	require.Equal(t, strings.Count(q, "?"), len(args))
}

func TestBuildRollupDistinctMetersQuery(t *testing.T) {
	p := &events.MeterUsageQueryParams{
		TenantID: "t1", EnvironmentID: "e1", ExternalCustomerIDs: []string{"c1"},
		StartTime: ts("2026-11-01T00:00:00Z"), EndTime: ts("2026-11-15T08:10:00Z"),
	}
	q, args, ok := buildRollupDistinctMetersQuery(p, "meter_usage_hourly")
	require.True(t, ok)
	require.Contains(t, q, "SELECT DISTINCT meter_id FROM meter_usage_hourly")
	require.Contains(t, q, "SELECT DISTINCT meter_id FROM meter_usage WHERE")
	require.Equal(t, strings.Count(q, "?"), len(args))

	p.UseFinal = true
	_, _, ok = buildRollupDistinctMetersQuery(p, "meter_usage_hourly")
	require.False(t, ok)
}

func TestIsValidRollupTable(t *testing.T) {
	require.True(t, IsValidRollupTable("meter_usage_hourly"))
	require.True(t, IsValidRollupTable("flexprice.meter_usage_rollup_lt"))
	require.False(t, IsValidRollupTable(""))
	require.False(t, IsValidRollupTable("x; DROP TABLE y"))
	require.False(t, IsValidRollupTable("a.b.c"))
}
