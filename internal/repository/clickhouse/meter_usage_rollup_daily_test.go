package clickhouse

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

var tieredTables = rollupTables{hourly: "meter_usage_hourly", daily: "meter_usage_daily"}

func TestSplitDays(t *testing.T) {
	tests := []struct {
		name       string
		start, end time.Time
		ok         bool
		dayStart   time.Time
		dayEnd     time.Time
		hours      []events.TimeRange
	}{
		{
			name:  "sample range",
			start: ts("2026-11-01T00:00:00Z"), end: ts("2026-11-15T10:37:00Z"),
			ok: true, dayStart: ts("2026-11-01T00:00:00Z"), dayEnd: ts("2026-11-15T00:00:00Z"),
			hours: []events.TimeRange{{Start: ts("2026-11-15T00:00:00Z"), End: ts("2026-11-15T10:00:00Z")}},
		},
		{
			name:  "day aligned has no hours",
			start: ts("2026-11-01T00:00:00Z"), end: ts("2026-11-15T00:00:00Z"),
			ok: true, dayStart: ts("2026-11-01T00:00:00Z"), dayEnd: ts("2026-11-15T00:00:00Z"),
		},
		{
			name:  "exactly one day",
			start: ts("2026-11-03T00:00:00Z"), end: ts("2026-11-04T00:00:00Z"),
			ok: true, dayStart: ts("2026-11-03T00:00:00Z"), dayEnd: ts("2026-11-04T00:00:00Z"),
		},
		{
			name:  "hours on both ends",
			start: ts("2026-11-01T05:20:00Z"), end: ts("2026-11-04T03:10:00Z"),
			ok: true, dayStart: ts("2026-11-02T00:00:00Z"), dayEnd: ts("2026-11-04T00:00:00Z"),
			hours: []events.TimeRange{
				{Start: ts("2026-11-01T06:00:00Z"), End: ts("2026-11-02T00:00:00Z")},
				{Start: ts("2026-11-04T00:00:00Z"), End: ts("2026-11-04T03:00:00Z")},
			},
		},
		{
			name:  "under a day falls back to hourly",
			start: ts("2026-11-01T00:00:00Z"), end: ts("2026-11-01T23:00:00Z"),
		},
		{
			name:  "spans midnight without a full day",
			start: ts("2026-11-01T10:00:00Z"), end: ts("2026-11-02T20:00:00Z"),
		},
		{
			name:  "unbounded start",
			start: time.Time{}, end: ts("2026-11-15T10:37:00Z"),
			ok: true, dayEnd: ts("2026-11-15T00:00:00Z"),
			hours: []events.TimeRange{{Start: ts("2026-11-15T00:00:00Z"), End: ts("2026-11-15T10:00:00Z")}},
		},
		{
			name:  "unbounded end",
			start: ts("2026-11-01T05:00:00Z"), end: time.Time{},
			ok: true, dayStart: ts("2026-11-02T00:00:00Z"),
			hours: []events.TimeRange{{Start: ts("2026-11-01T05:00:00Z"), End: ts("2026-11-02T00:00:00Z")}},
		},
		{
			name:  "non-UTC input uses UTC days",
			start: ts("2026-11-01T05:30:00+05:30"), end: ts("2026-11-03T05:30:00+05:30"),
			ok: true, dayStart: ts("2026-11-01T00:00:00Z"), dayEnd: ts("2026-11-03T00:00:00Z"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, ok := planRollup(rollupParams(types.AggregationSum, tt.start, tt.end))
			require.True(t, ok)
			split, ok := splitDays(plan)
			require.Equal(t, tt.ok, ok)
			if !ok {
				return
			}
			require.True(t, tt.dayStart.Equal(split.dayStart), "dayStart %s", split.dayStart)
			require.True(t, tt.dayEnd.Equal(split.dayEnd), "dayEnd %s", split.dayEnd)
			require.Len(t, split.hours, len(tt.hours))
			for i := range tt.hours {
				require.True(t, tt.hours[i].Start.Equal(split.hours[i].Start), "hour start %s", split.hours[i].Start)
				require.True(t, tt.hours[i].End.Equal(split.hours[i].End), "hour end %s", split.hours[i].End)
			}
		})
	}
}

func sampleMultiMeterParams() *events.MeterUsageQueryParams {
	return &events.MeterUsageQueryParams{
		TenantID:           "t1",
		EnvironmentID:      "e1",
		ExternalCustomerID: "c1",
		MeterIDs:           []string{"m1", "m2"},
		AggregationType:    types.AggregationSum,
		StartTime:          ts("2026-11-01T00:00:00Z"),
		EndTime:            ts("2026-11-15T10:37:00Z"),
	}
}

func TestBuildTieredMultiMeterQuery_SampleSQL(t *testing.T) {
	q, args, ok := buildTieredMultiMeterQuery(sampleMultiMeterParams(), tieredTables)
	require.True(t, ok)
	scope := "tenant_id = ? AND environment_id = ? AND external_customer_id = ? AND meter_id IN (?, ?)"
	want := `
		SELECT meter_id, sum(v) AS value, toUInt64(sum(n)) AS event_count
		FROM (` +
		"SELECT meter_id, toDecimal128(sum(qty_sum), 15) AS v, toUInt64(sum(event_count)) AS n FROM meter_usage_daily WHERE " + scope + " AND bucket >= ? AND bucket < ? GROUP BY meter_id" +
		" UNION ALL SELECT meter_id, toDecimal128(sum(qty_sum), 15) AS v, toUInt64(sum(event_count)) AS n FROM meter_usage_hourly WHERE " + scope + " AND ((bucket >= ? AND bucket < ?)) GROUP BY meter_id" +
		" UNION ALL SELECT meter_id, toDecimal128(SUM(qty_total), 15) AS v, toUInt64(COUNT(DISTINCT id)) AS n FROM meter_usage WHERE " + scope + " AND ((timestamp >= ? AND timestamp < ?)) GROUP BY meter_id" +
		`)
		GROUP BY meter_id
		SETTINGS ` + maxMemoryUsageSetting + `
	`
	require.Equal(t, want, q)
	require.Equal(t, []interface{}{
		"t1", "e1", "c1", "m1", "m2", ts("2026-11-01T00:00:00Z"), ts("2026-11-15T00:00:00Z"),
		"t1", "e1", "c1", "m1", "m2", ts("2026-11-15T00:00:00Z"), ts("2026-11-15T10:00:00Z"),
		"t1", "e1", "c1", "m1", "m2", ts("2026-11-15T10:00:00Z"), ts("2026-11-15T10:37:00Z"),
	}, args)
}

func TestBuildTieredQueries_DisabledDailyMatchesHourly(t *testing.T) {
	ranges := [][2]time.Time{
		{ts("2026-11-01T00:00:00Z"), ts("2026-11-15T10:37:00Z")},
		{ts("2026-11-01T00:15:00Z"), ts("2026-11-01T05:42:00Z")},
		{time.Time{}, ts("2026-11-15T10:37:00Z")},
	}
	for _, r := range ranges {
		p := sampleMultiMeterParams()
		p.StartTime, p.EndTime = r[0], r[1]
		for _, tables := range []rollupTables{{hourly: "meter_usage_hourly"}, {hourly: "meter_usage_hourly", daily: ""}} {
			q1, a1, ok1 := buildRollupMultiMeterQuery(p, "meter_usage_hourly")
			q2, a2, ok2 := buildTieredMultiMeterQuery(p, tables)
			require.Equal(t, ok1, ok2)
			require.Equal(t, q1, q2)
			require.Equal(t, a1, a2)

			q1, a1, ok1 = buildRollupScalarQuery(p, "meter_usage_hourly")
			q2, a2, ok2 = buildTieredScalarQuery(p, tables)
			require.Equal(t, ok1, ok2)
			require.Equal(t, q1, q2)
			require.Equal(t, a1, a2)

			q1, a1, ok1 = buildRollupDistinctMetersQuery(p, "meter_usage_hourly")
			q2, a2, ok2 = buildTieredDistinctMetersQuery(p, tables)
			require.Equal(t, ok1, ok2)
			require.Equal(t, q1, q2)
			require.Equal(t, a1, a2)
		}
	}
}

func TestBuildTieredQueries_ShortRangeSkipsDaily(t *testing.T) {
	p := sampleMultiMeterParams()
	p.StartTime, p.EndTime = ts("2026-11-01T10:20:00Z"), ts("2026-11-02T08:30:00Z")
	q, args, ok := buildTieredMultiMeterQuery(p, tieredTables)
	require.True(t, ok)
	hq, hargs, _ := buildRollupMultiMeterQuery(p, "meter_usage_hourly")
	require.Equal(t, hq, q)
	require.Equal(t, hargs, args)
	require.NotContains(t, q, "meter_usage_daily")
}

func TestBuildTieredQueries_Shapes(t *testing.T) {
	tests := []struct {
		name       string
		start, end time.Time
		hourly     bool
		raw        bool
	}{
		{"day aligned uses daily only", ts("2026-11-01T00:00:00Z"), ts("2026-11-15T00:00:00Z"), false, false},
		{"hour aligned adds hourly", ts("2026-11-01T00:00:00Z"), ts("2026-11-15T10:00:00Z"), true, false},
		{"partial hours add raw", ts("2026-11-01T05:20:00Z"), ts("2026-11-04T03:10:00Z"), true, true},
		{"day aligned with partial edge", ts("2026-10-31T23:59:00Z"), ts("2026-11-15T00:00:00Z"), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := sampleMultiMeterParams()
			p.StartTime, p.EndTime = tt.start, tt.end
			builders := map[string]func(*events.MeterUsageQueryParams, rollupTables) (string, []interface{}, bool){
				"scalar":   buildTieredScalarQuery,
				"multi":    buildTieredMultiMeterQuery,
				"distinct": buildTieredDistinctMetersQuery,
			}
			for name, build := range builders {
				q, args, ok := build(p, tieredTables)
				require.True(t, ok, name)
				require.Contains(t, q, "FROM meter_usage_daily WHERE", name)
				require.Equal(t, tt.hourly, strings.Contains(q, "FROM meter_usage_hourly WHERE"), name)
				require.Equal(t, tt.raw, strings.Contains(q, "FROM meter_usage WHERE"), name)
				require.Equal(t, strings.Count(q, "?"), len(args), name)
			}
		})
	}
}

func TestWithDailyRollupTable(t *testing.T) {
	r := &MeterUsageRepository{}
	WithRollupTable("meter_usage_hourly")(r)
	WithDailyRollupTable("x; DROP TABLE y")(r)
	require.Equal(t, rollupTables{hourly: "meter_usage_hourly"}, r.rollup)
	WithDailyRollupTable("flexprice.meter_usage_daily")(r)
	require.Equal(t, rollupTables{hourly: "meter_usage_hourly", daily: "flexprice.meter_usage_daily"}, r.rollup)

	daily := &MeterUsageRepository{}
	WithDailyRollupTable("meter_usage_daily")(daily)
	_, _, ok := buildTieredScalarQuery(rollupParams(types.AggregationSum, ts("2026-11-01T00:00:00Z"), ts("2026-11-15T00:00:00Z")), daily.rollup)
	require.False(t, ok)
}

const tieredFixture = `WITH meter_usage AS (
		SELECT toString(number) AS id, 't1' AS tenant_id, 'e1' AS environment_id,
			if(number % 2 = 0, 'c1', 'c2') AS external_customer_id,
			concat('m', toString(number % 3)) AS meter_id,
			toDateTime('2026-10-29 00:00:00', 'UTC') + toIntervalSecond(number * 1373) AS timestamp,
			toDecimal128(number % 7, 15) + toDecimal128('0.125', 15) AS qty_total
		FROM numbers(1500)
	),
	meter_usage_hourly AS (
		SELECT tenant_id, environment_id, external_customer_id, meter_id, toStartOfHour(timestamp, 'UTC') AS bucket,
			sum(toDecimal128(qty_total, 15)) AS qty_sum, count() AS event_count
		FROM meter_usage GROUP BY tenant_id, environment_id, external_customer_id, meter_id, bucket
	),
	meter_usage_daily AS (
		SELECT tenant_id, environment_id, external_customer_id, meter_id, toStartOfDay(timestamp, 'UTC') AS bucket,
			sum(toDecimal128(qty_total, 15)) AS qty_sum, count() AS event_count
		FROM meter_usage GROUP BY tenant_id, environment_id, external_customer_id, meter_id, bucket
	) `

func rawBaseline(p *events.MeterUsageQueryParams) (string, []interface{}) {
	conds, args := rollupScope(p)
	if !p.StartTime.IsZero() {
		conds = append(conds, "timestamp >= ?")
		args = append(args, p.StartTime)
	}
	if !p.EndTime.IsZero() {
		conds = append(conds, "timestamp < ?")
		args = append(args, p.EndTime)
	}
	_, rawExpr, _ := rollupValueExprs(p.AggregationType)
	return "SELECT meter_id, " + rawExpr + " AS value, toUInt64(COUNT(DISTINCT id)) AS event_count FROM meter_usage WHERE " +
		strings.Join(conds, " AND ") + " GROUP BY meter_id ORDER BY meter_id", args
}

func TestTieredRollupEquivalence_Integration(t *testing.T) {
	store := newRealClickHouseTestStore(t)
	repo := &MeterUsageRepository{store: store, qb: NewMeterUsageQueryBuilder()}
	ctx := context.Background()

	ranges := [][2]time.Time{
		{ts("2026-11-01T00:00:00Z"), ts("2026-11-15T10:37:00Z")},
		{ts("2026-11-01T00:00:00Z"), ts("2026-11-15T00:00:00Z")},
		{ts("2026-11-01T05:20:00Z"), ts("2026-11-04T03:10:00Z")},
		{ts("2026-11-03T00:00:00Z"), ts("2026-11-04T00:00:00Z")},
		{ts("2026-11-01T10:20:00Z"), ts("2026-11-02T08:30:00Z")},
		{ts("2026-11-02T03:00:00Z"), ts("2026-11-02T09:45:00Z")},
		{time.Time{}, ts("2026-11-10T07:30:00Z")},
		{ts("2026-11-05T13:13:00Z"), time.Time{}},
	}
	for _, r := range ranges {
		for _, agg := range []types.AggregationType{types.AggregationSum, types.AggregationCount} {
			p := &events.MeterUsageQueryParams{
				TenantID: "t1", EnvironmentID: "e1", ExternalCustomerID: "c1",
				MeterIDs: []string{"m0", "m1", "m2"}, AggregationType: agg,
				StartTime: r[0], EndTime: r[1],
			}
			name := r[0].Format(time.RFC3339) + ".." + r[1].Format(time.RFC3339) + "/" + string(agg)
			t.Run(name, func(t *testing.T) {
				bq, bargs := rawBaseline(p)
				base, err := repo.executeMultiMeterScalarQuery(ctx, tieredFixture+bq, bargs, p)
				require.NoError(t, err)
				require.NotEmpty(t, base)
				want := map[string]*events.MeterUsageAggregationResult{}
				for _, b := range base {
					want[b.MeterID] = b
				}

				q, args, ok := buildTieredMultiMeterQuery(p, tieredTables)
				require.True(t, ok)
				got, err := repo.executeMultiMeterScalarQuery(ctx, tieredFixture+q, args, p)
				require.NoError(t, err)
				require.Len(t, got, len(want))
				for _, g := range got {
					w, found := want[g.MeterID]
					require.True(t, found, g.MeterID)
					require.True(t, w.TotalValue.Equal(g.TotalValue), "%s: %s vs %s", g.MeterID, g.TotalValue, w.TotalValue)
					require.Equal(t, w.EventCount, g.EventCount, g.MeterID)
				}

				single := *p
				single.MeterID, single.MeterIDs = "m1", nil
				sq, sargs, ok := buildTieredScalarQuery(&single, tieredTables)
				require.True(t, ok)
				scalar, err := repo.executeScalarQuery(ctx, tieredFixture+sq, sargs, &single)
				require.NoError(t, err)
				for _, b := range base {
					if b.MeterID == "m1" {
						require.True(t, b.TotalValue.Equal(scalar.TotalValue), "scalar %s vs %s", scalar.TotalValue, b.TotalValue)
						require.Equal(t, b.EventCount, scalar.EventCount)
					}
				}

				distinct := *p
				distinct.MeterIDs = nil
				dq, dargs, ok := buildTieredDistinctMetersQuery(&distinct, tieredTables)
				require.True(t, ok)
				rows, err := store.GetConn().Query(ctx, tieredFixture+dq, dargs...)
				require.NoError(t, err)
				var meters []string
				for rows.Next() {
					var m string
					require.NoError(t, rows.Scan(&m))
					meters = append(meters, m)
				}
				rows.Close()
				wantMeters := make([]string, 0, len(base))
				for _, b := range base {
					wantMeters = append(wantMeters, b.MeterID)
				}
				sort.Strings(wantMeters)
				require.Equal(t, wantMeters, meters)
			})
		}
	}
}
