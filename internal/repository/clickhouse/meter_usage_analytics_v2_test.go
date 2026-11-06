package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestLineLayers(t *testing.T) {
	at := func(d int) time.Time { return time.Date(2026, 8, d, 0, 0, 0, 0, time.UTC) }
	lines := []events.AnalyticsV2Line{
		{ID: "a1", MeterID: "a", Start: at(1), End: at(15)},
		{ID: "a2", MeterID: "a", Start: at(15), End: at(31)}, // adjacent: same layer
		{ID: "a3", MeterID: "a", Start: at(1), End: at(31)},  // overlaps both: second layer
		{ID: "b1", MeterID: "b", Start: at(1), End: at(31)},  // other meter: first layer
	}
	layers := lineLayers(lines)
	require.Len(t, layers, 2)
	require.ElementsMatch(t, []int{0, 1, 3}, layers[0])
	require.Equal(t, []int{2}, layers[1])
}

// TestRankAnalyticsV2Rows_MatchesOracle_Integration checks the ranking query against a Go calculation
// of the same rules on real ClickHouse. Skips when no ClickHouse is reachable.
func TestRankAnalyticsV2Rows_MatchesOracle_Integration(t *testing.T) {
	store := newRealClickHouseTestStore(t)
	log, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
	require.NoError(t, err)
	repo := &MeterUsageRepository{store: store, qb: NewMeterUsageQueryBuilder(), logger: log}
	ctx := context.Background()

	tenant := "test_tenant_" + uuid.NewString()
	aug := func(d, h int) time.Time { return time.Date(2026, 8, d, h, 0, 0, 0, time.UTC) }
	ref := testutil.NewInMemoryMeterUsageStore()
	seedAnalyticsEvents(t, ctx, repo, ref, tenant, aug)
	dataThrough := aug(31, 0)

	scope := events.AnalyticsV2Scope{TenantID: tenant, EnvironmentID: "env", ExternalCustomerIDs: []string{"cust"}, DataThrough: dataThrough}
	d := decimal.RequireFromString
	// ai changes price at noon on the 15th; compute sits on two subscriptions (overlapping lines); gpu is
	// bucketed MAX: each day's peak per type goes to the row of the event that set it.
	lines := []events.AnalyticsV2Line{
		{ID: "li_ai_old", MeterID: "ai", Start: aug(1, 0), End: aug(15, 12)},
		{ID: "li_ai_new", MeterID: "ai", Start: aug(15, 12), End: aug(31, 0)},
		{ID: "li_compute_a", MeterID: "compute", Start: aug(1, 0), End: aug(31, 0)},
		{ID: "li_compute_b", MeterID: "compute", Start: aug(1, 0), End: aug(31, 0)},
		{ID: "li_gpu", MeterID: "gpu", Start: aug(1, 0), End: aug(31, 0), BucketSize: types.WindowSizeDay, BucketGroup: "type", BillingAnchor: aug(1, 0)},
	}
	rates := []events.AnalyticsV2Amounts{
		{Usage: d("2"), Discount: d("0.2"), Free: d("0.25")},
		{Usage: d("1.5")},
		{CommitmentUtilized: d("3"), TrueUp: d("0.5")},
		{Usage: d("0.1"), Overage: d("0.05")},
		{Usage: d("12"), Overage: d("1.5")},
	}

	base := func() *events.AnalyticsV2RankParams {
		return &events.AnalyticsV2RankParams{
			Scope: scope, StartTime: aug(1, 0), EndTime: aug(31, 0),
			Lines: lines, Rates: rates,
			MeterFeatures: map[string]string{"ai": "feat_ai", "compute": "feat_compute", "gpu": "feat_gpu"},
			GroupBy:       []string{"properties.app"},
			Timezone:      "Asia/Kolkata",
			Sort:          events.AnalyticsV2Sort{By: events.AnalyticsV2SortByTotal, Desc: true},
			Limit:         3,
		}
	}

	cases := []struct {
		name   string
		mutate func(p *events.AnalyticsV2RankParams)
	}{
		{"by app, top 3", func(p *events.AnalyticsV2RankParams) {}},
		{"by app, page 2", func(p *events.AnalyticsV2RankParams) { p.Offset = 3 }},
		{"build/run columns sorted by run", func(p *events.AnalyticsV2RankParams) {
			p.Column = "properties.type"
			p.Sort = events.AnalyticsV2Sort{By: events.AnalyticsV2SortByColumn, Column: "run", Desc: true}
			p.Limit = 10
		}},
		{"month columns in Kolkata time", func(p *events.AnalyticsV2RankParams) {
			p.Column = events.AnalyticsV2KeyMonth
			p.StartTime, p.EndTime = time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC), aug(31, 0)
			p.Limit = 10
		}},
		{"meter and app rows by name", func(p *events.AnalyticsV2RankParams) {
			p.GroupBy = []string{"meter_id", "properties.app"}
			p.Sort = events.AnalyticsV2Sort{By: events.AnalyticsV2SortByName}
			p.Limit = 20
		}},
		{"line and feature rows by usage", func(p *events.AnalyticsV2RankParams) {
			p.GroupBy = []string{"sub_line_item_id", "feature_id"}
			p.Sort = events.AnalyticsV2Sort{By: events.AnalyticsV2SortByUsage, Desc: true}
			p.Limit = 20
		}},
		{"exclude an app, include a source, search", func(p *events.AnalyticsV2RankParams) {
			p.Filters = []events.AnalyticsV2Filter{
				{Key: "properties.app", Exclude: true, Values: []string{"payout"}},
				{Key: "source", Values: []string{"api"}},
			}
			p.Search = "PRO" // case-insensitive: matches promo
			p.Limit = 10
		}},
		{"filter and search values with quotes and backslashes", func(p *events.AnalyticsV2RankParams) {
			p.Filters = []events.AnalyticsV2Filter{{Key: "properties.app", Exclude: true, Values: []string{"payout", "it's", `a\b`, "x') OR 1=1 --"}}}
			p.Search = "o"
			p.Limit = 10
		}},
		{"filter by line", func(p *events.AnalyticsV2RankParams) {
			p.Filters = []events.AnalyticsV2Filter{{Key: "sub_line_item_id", Values: []string{"li_ai_new", "li_gpu"}}}
			p.Sort = events.AnalyticsV2Sort{By: events.AnalyticsV2SortByEventCount, Desc: true}
			p.Limit = 10
		}},
		{"bucketed line without a group, by app and type", func(p *events.AnalyticsV2RankParams) {
			p.Lines = append([]events.AnalyticsV2Line{}, p.Lines...)
			p.Lines[4].BucketGroup = ""
			p.GroupBy = []string{"properties.app"}
			p.Column = "properties.type"
			p.Limit = 10
		}},
		{"bucketed peaks are chosen before filters", func(p *events.AnalyticsV2RankParams) {
			p.Filters = []events.AnalyticsV2Filter{{Key: "properties.app", Exclude: true, Values: []string{"payout", "support"}}}
			p.Limit = 10
		}},
		{"partial range", func(p *events.AnalyticsV2RankParams) {
			p.StartTime, p.EndTime = aug(10, 0), aug(20, 0)
			p.Limit = 10
		}},
		{"deduplicated", func(p *events.AnalyticsV2RankParams) {
			p.Scope.UseFinal = true
			p.Limit = 10
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := base()
			tc.mutate(params)

			got, err := repo.RankAnalyticsV2Rows(ctx, params)
			require.NoError(t, err)
			want, err := ref.RankAnalyticsV2Rows(ctx, params)
			require.NoError(t, err)

			require.Equal(t, want.TotalCount, got.TotalCount, "total count")
			require.NotEmpty(t, want.Rows)
			require.True(t, lo.SomeBy(got.Rows, func(r events.AnalyticsV2Row) bool { return lo.Contains(r.LineIndexes, 4) }) ||
				lo.SomeBy(params.Filters, func(f events.AnalyticsV2Filter) bool { return f.Key == "sub_line_item_id" || f.Key == "source" }),
				"the bucketed gpu line reaches the rows")
			require.Equal(t, renderRows(want.Rows), renderRows(got.Rows))
			requireMeasureEqual(t, "totals", want.Totals, got.Totals)
			if params.Column != "" {
				wantCols, gotCols := lo.Keys(want.ColumnTotals), lo.Keys(got.ColumnTotals)
				sort.Strings(wantCols)
				sort.Strings(gotCols)
				require.Equal(t, wantCols, gotCols)
				for k := range want.ColumnTotals {
					requireDecimalEqual(t, "column total "+k, want.ColumnTotals[k].Amounts.Usage, got.ColumnTotals[k].Amounts.Usage)
				}
			}
		})
	}

	detailCases := []struct {
		name   string
		mutate func(p *events.AnalyticsV2RankParams)
	}{
		{"daily points by app", func(p *events.AnalyticsV2RankParams) {}},
		{"daily points with build/run columns, filter and search", func(p *events.AnalyticsV2RankParams) {
			p.Column = "properties.type"
			p.Filters = []events.AnalyticsV2Filter{{Key: "properties.app", Exclude: true, Values: []string{"launch"}}}
			p.Search = "p"
		}},
		{"points by meter and app", func(p *events.AnalyticsV2RankParams) { p.GroupBy = []string{"meter_id", "properties.app"} }},
	}
	for _, tc := range detailCases {
		t.Run(tc.name, func(t *testing.T) {
			rank := base()
			rank.Timezone = "UTC"
			rank.Limit = 4
			tc.mutate(rank)
			page, err := ref.RankAnalyticsV2Rows(ctx, rank)
			require.NoError(t, err)
			require.NotEmpty(t, page.Rows)

			dp := &events.AnalyticsV2DetailParams{
				Rank:         *rank,
				Rows:         lo.Map(page.Rows, func(r events.AnalyticsV2Row, _ int) []string { return r.Group }),
				WindowSize:   types.WindowSizeDay,
				DistinctKeys: []string{"source", "properties.type"},
			}
			got, err := repo.GetAnalyticsV2Detail(ctx, dp)
			require.NoError(t, err)
			want, err := ref.GetAnalyticsV2Detail(ctx, dp)
			require.NoError(t, err)

			require.True(t, lo.SomeBy(want.Points, func(c events.AnalyticsV2PointCell) bool { return c.Row == -1 }), "has all-rows cells")
			require.True(t, lo.SomeBy(want.Points, func(c events.AnalyticsV2PointCell) bool { return c.Row == 0 }), "has page-row cells")
			require.NotEmpty(t, want.Distinct[0])
			require.Equal(t, renderPoints(want.Points), renderPoints(got.Points))
			require.Equal(t, want.Distinct, got.Distinct)
		})
	}

	t.Run("empty page still returns all-rows points", func(t *testing.T) {
		rank := base()
		rank.Timezone = "UTC"
		dp := &events.AnalyticsV2DetailParams{Rank: *rank, WindowSize: types.WindowSizeDay, DistinctKeys: []string{"source"}}
		got, err := repo.GetAnalyticsV2Detail(ctx, dp)
		require.NoError(t, err)
		want, err := ref.GetAnalyticsV2Detail(ctx, dp)
		require.NoError(t, err)
		require.NotEmpty(t, want.Points)
		require.True(t, lo.EveryBy(want.Points, func(c events.AnalyticsV2PointCell) bool { return c.Row == -1 }))
		require.Equal(t, renderPoints(want.Points), renderPoints(got.Points))
	})

	t.Run("fractional range bounds", func(t *testing.T) {
		// Events are whole seconds; a bound half a second after an event's second must treat it like the
		// in-memory reference does (excluded from a start, included before an end).
		rank := base()
		rank.StartTime = rank.StartTime.Add(500 * time.Millisecond)
		rank.EndTime = rank.EndTime.Add(-500 * time.Millisecond)
		got, err := repo.RankAnalyticsV2Rows(ctx, rank)
		require.NoError(t, err)
		want, err := ref.RankAnalyticsV2Rows(ctx, rank)
		require.NoError(t, err)
		require.Equal(t, renderRows(want.Rows), renderRows(got.Rows))
	})

	t.Run("line usage over full windows", func(t *testing.T) {
		for _, final := range []bool{false, true} {
			sc := scope
			sc.UseFinal = final
			lp := &events.AnalyticsV2LineUsageParams{Scope: sc, Lines: lines, RangeStart: aug(10, 0), RangeEnd: aug(20, 6)}
			got, err := repo.GetAnalyticsV2LineUsage(ctx, lp)
			require.NoError(t, err)
			want, err := ref.GetAnalyticsV2LineUsage(ctx, lp)
			require.NoError(t, err)
			require.Len(t, got, len(lines))
			for i := range lines {
				require.Truef(t, want[i].Quantity.Equal(got[i].Quantity), "final=%v line %s: want %s got %s", final, lines[i].ID, want[i].Quantity, got[i].Quantity)
				require.Equal(t, want[i].EventCount, got[i].EventCount)
				require.Truef(t, want[i].RangeQuantity.Equal(got[i].RangeQuantity), "final=%v line %s range: want %s got %s", final, lines[i].ID, want[i].RangeQuantity, got[i].RangeQuantity)
				require.True(t, got[i].RangeQuantity.IsPositive() && got[i].RangeQuantity.LessThan(got[i].Quantity), "range is a strict part of the window")
			}
		}
		// The bucketed gpu line counts each day's peak per type, so far less than the sum of its events.
		plain := append([]events.AnalyticsV2Line{}, lines...)
		plain[4].BucketSize = ""
		summed, err := ref.GetAnalyticsV2LineUsage(ctx, &events.AnalyticsV2LineUsageParams{Scope: scope, Lines: plain, RangeStart: aug(10, 0), RangeEnd: aug(20, 6)})
		require.NoError(t, err)
		got, err := repo.GetAnalyticsV2LineUsage(ctx, &events.AnalyticsV2LineUsageParams{Scope: scope, Lines: lines, RangeStart: aug(10, 0), RangeEnd: aug(20, 6)})
		require.NoError(t, err)
		require.True(t, got[4].Quantity.IsPositive() && got[4].Quantity.LessThan(summed[4].Quantity),
			"bucket peaks %s < event sum %s", got[4].Quantity, summed[4].Quantity)
	})
}

func seedAnalyticsEvents(t *testing.T, ctx context.Context, repo *MeterUsageRepository, ref *testutil.InMemoryMeterUsageStore, tenant string, aug func(d, h int) time.Time) {
	t.Helper()
	record := func(id, customer, meter, source string, props map[string]interface{}, ts, ingested time.Time, qty decimal.Decimal) *events.MeterUsage {
		return &events.MeterUsage{
			Event: events.Event{ID: id, TenantID: tenant, EnvironmentID: "env", ExternalCustomerID: customer, EventName: "evt",
				Source: source, Properties: props, Timestamp: ts, IngestedAt: ingested},
			MeterID: meter, QtyTotal: qty,
		}
	}

	apps := []string{"payout", "support", "promo", "launch", "hiring", ""}
	var records, duplicates []*events.MeterUsage
	for i := 0; i < 600; i++ {
		props := map[string]interface{}{"type": []string{"build", "run"}[(i/3)%2]}
		app := apps[(i*7)%len(apps)]
		if i%3 == 2 {
			app = apps[(i/3)%len(apps)] // spread gpu across apps so bucket peaks are contested
		}
		if app != "" {
			props["app"] = app
		}
		ts := aug(1+(i*13)%30, (i*5)%24)
		qty := decimal.NewFromInt(int64(1 + (i*17)%9)).Add(decimal.RequireFromString("0.25"))
		if i%3 == 2 {
			// gpu is bucketed MAX: unique values keep each bucket's peak owner unambiguous.
			qty = qty.Add(decimal.New(int64(i), -3))
		}
		rec := record(fmt.Sprintf("e%d", i), "cust", []string{"ai", "compute", "gpu"}[i%3], []string{"api", "batch"}[i%2], props, ts, ts.Add(time.Minute), qty)
		records = append(records, rec)
		if i%25 == 0 { // same event delivered twice: counted twice unless deduplicated
			dup := *rec
			dup.IngestedAt = ts.Add(2 * time.Minute)
			duplicates = append(duplicates, &dup)
		}
	}
	// Received after data_through: never counted, though its event time is in range.
	records = append(records, record("late", "cust", "ai", "api", map[string]interface{}{"app": "payout", "type": "run"}, aug(10, 0), aug(31, 1), decimal.NewFromInt(1000)))
	// Another customer's event: never counted.
	records = append(records, record("other", "cust2", "ai", "api", map[string]interface{}{"app": "payout"}, aug(5, 0), aug(5, 0), decimal.NewFromInt(999)))

	// Unmerged duplicates exist only until a background merge; pause merges so the test sees them.
	// Without the privilege, seed no duplicates rather than race the merge.
	conn := repo.store.GetConn()
	// Cleanups run last-in first-out, so merges resume before the rows are deleted.
	t.Cleanup(func() { _ = conn.Exec(context.Background(), "DELETE FROM meter_usage WHERE tenant_id = ?", tenant) })
	if err := conn.Exec(ctx, "SYSTEM STOP MERGES meter_usage"); err != nil {
		t.Logf("cannot pause merges (%v); seeding without duplicates", err)
		duplicates = nil
	} else {
		t.Cleanup(func() { _ = conn.Exec(context.Background(), "SYSTEM START MERGES meter_usage") })
	}

	// Duplicates go in a second insert, as redeliveries do: rows with the same key in one insert collapse on write.
	for _, part := range [][]*events.MeterUsage{records, duplicates} {
		if len(part) == 0 {
			continue
		}
		batch, err := conn.PrepareBatch(ctx, "INSERT INTO meter_usage (id, tenant_id, environment_id, external_customer_id, meter_id, event_name, timestamp, ingested_at, qty_total, source, properties)")
		require.NoError(t, err)
		for _, r := range part {
			props, _ := json.Marshal(r.Properties)
			require.NoError(t, batch.Append(r.ID, r.TenantID, r.EnvironmentID, r.ExternalCustomerID, r.MeterID, r.EventName, r.Timestamp, r.IngestedAt, r.QtyTotal, r.Source, string(props)))
		}
		require.NoError(t, batch.Send())
	}
	records = append(records, duplicates...)

	var stored uint64
	require.NoError(t, conn.QueryRow(ctx, "SELECT count() FROM meter_usage WHERE tenant_id = ?", tenant).Scan(&stored))
	require.Equal(t, uint64(len(records)), stored, "a background merge already collapsed the duplicates; rerun")
	require.NoError(t, ref.BulkInsertMeterUsage(ctx, records))
}

// renderPoints prints point cells sorted, at 9 decimals.
func renderPoints(cells []events.AnalyticsV2PointCell) []string {
	out := lo.Map(cells, func(c events.AnalyticsV2PointCell, _ int) string {
		a := c.Amounts
		return fmt.Sprintf("row=%d w=%s col=%s usage=%s util=%s over=%s trueup=%s disc=%s q=%s n=%d",
			c.Row, c.WindowStart.UTC().Format(time.RFC3339), c.Column, a.Usage.Round(9), a.CommitmentUtilized.Round(9),
			a.Overage.Round(9), a.TrueUp.Round(9), a.Discount.Round(9), c.Quantity.Round(9), c.EventCount)
	})
	sort.Strings(out)
	return out
}

// renderRows prints rows at 9 decimals so the two implementations' rounding cannot differ.
func renderRows(rows []events.AnalyticsV2Row) []string {
	f := func(d decimal.Decimal) string { return d.Round(9).String() }
	var out []string
	for _, r := range rows {
		a := r.Measure.Amounts
		line := fmt.Sprintf("%v usage=%s util=%s over=%s trueup=%s disc=%s free=%s qty=%s n=%d lines=%v",
			r.Group, f(a.Usage), f(a.CommitmentUtilized), f(a.Overage), f(a.TrueUp), f(a.Discount), f(a.Free),
			f(r.Measure.Quantity), r.Measure.EventCount, r.LineIndexes)
		cols := lo.Keys(r.Columns)
		sort.Strings(cols)
		for _, c := range cols {
			m := r.Columns[c]
			line += fmt.Sprintf(" [%s cost=%s qty=%s n=%d]", c, f(m.Amounts.Usage), f(m.Quantity), m.EventCount)
		}
		out = append(out, line)
	}
	return out
}

func requireDecimalEqual(t *testing.T, what string, want, got decimal.Decimal) {
	t.Helper()
	require.Truef(t, want.Round(9).Equal(got.Round(9)), "%s: want %s got %s", what, want, got)
}

func requireMeasureEqual(t *testing.T, what string, want, got events.AnalyticsV2Measure) {
	t.Helper()
	require.Equal(t, renderRows([]events.AnalyticsV2Row{{Measure: want}}), renderRows([]events.AnalyticsV2Row{{Measure: got}}), what)
}
