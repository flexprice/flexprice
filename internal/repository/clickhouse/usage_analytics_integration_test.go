package clickhouse

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/usageanalytics"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// usageFixture is one tenant's events, written to both ClickHouse and the in-memory reference.
type usageFixture struct {
	tenantID, envID string
	lines           []usageanalytics.LineWindow
}

func seedUsageFixture(t *testing.T, rng *rand.Rand, repo *MeterUsageRepository, ref *testutil.InMemoryMeterUsageStore, withDuplicates bool) usageFixture {
	t.Helper()
	f := usageFixture{tenantID: "usage_tenant_" + uuid.NewString(), envID: "usage_env"}

	price := func(s string) decimal.Decimal { return decimal.RequireFromString(s) }
	f.lines = []usageanalytics.LineWindow{
		// Price change on m_tokens mid-month.
		{LineItemID: "li_tok_old", MeterID: "m_tokens", FeatureID: "f_tokens", ExternalCustomerIDs: []string{"parent", "child"},
			Start: oct1, End: oct15, UnitPrice: price("0.002"), ListUnitPrice: price("0.002")},
		// A 30% discount on the new price.
		{LineItemID: "li_tok_new", MeterID: "m_tokens", FeatureID: "f_tokens", ExternalCustomerIDs: []string{"parent", "child"},
			Start: oct15, End: nov1, UnitPrice: price("0.0035"), ListUnitPrice: price("0.005")},
		// An overlapping add-on line on the same meter, parent only.
		{LineItemID: "li_tok_addon", MeterID: "m_tokens", FeatureID: "f_tokens", ExternalCustomerIDs: []string{"parent"},
			Start: oct1.AddDate(0, 0, 10), End: oct1.AddDate(0, 0, 20), UnitPrice: price("0.001"), ListUnitPrice: price("0.001")},
		{LineItemID: "li_min", MeterID: "m_minutes", FeatureID: "f_minutes", ExternalCustomerIDs: []string{"parent", "child"},
			Start: oct1, End: nov1, UnitPrice: price("1.25"), ListUnitPrice: price("1.25")},
	}

	users := []any{"alice", "bob", "Carol", 123, 45.5, nil, "missing"}
	apps := []string{"build", "run", "deploy"}
	owners := []string{"o1", "o2", "o3", "o4", "o5", "o6", "o7"}
	sources := []string{"api", "sdk", "test"}
	customers := []string{"parent", "child", "stranger"}
	meters := []string{"m_tokens", "m_minutes", "m_unpriced"}

	var records []*events.MeterUsage
	for i := 0; i < 600; i++ {
		props := map[string]interface{}{
			"app":   apps[rng.Intn(len(apps))],
			"owner": owners[rng.Intn(len(owners))],
		}
		if u := users[rng.Intn(len(users))]; u != "missing" {
			props["user"] = u
		}
		// Events from Sep 25 to Nov 5 so some fall outside every line window.
		ts := oct1.AddDate(0, 0, -6).Add(time.Duration(rng.Int63n(int64(41 * 24 * time.Hour)))).Truncate(time.Second)
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID: fmt.Sprintf("ev_%d", i), TenantID: f.tenantID, EnvironmentID: f.envID, EventName: "usage",
				ExternalCustomerID: customers[rng.Intn(len(customers))],
				Source:             sources[rng.Intn(len(sources))],
				Timestamp:          ts, Properties: props,
			},
			MeterID:  meters[rng.Intn(len(meters))],
			QtyTotal: decimal.NewFromInt(rng.Int63n(5000) + 1).Div(decimal.NewFromInt(100)),
		})
	}
	// Exact ties across groups for stable-order checks.
	for i, u := range []string{"tie_a", "tie_b", "tie_c"} {
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID: fmt.Sprintf("tie_%d", i), TenantID: f.tenantID, EnvironmentID: f.envID, EventName: "usage",
				ExternalCustomerID: "child", Source: "api", Timestamp: oct1.Add(time.Hour),
				Properties: map[string]interface{}{"user": u, "app": "run", "owner": "o1"},
			},
			MeterID: "m_minutes", QtyTotal: decimal.NewFromInt(4),
		})
	}
	// Events exactly on line edges: a window includes its start and excludes its end.
	for i, ts := range []time.Time{oct1, oct15, oct15.Add(-time.Second), nov1, oct1.AddDate(0, 0, 20)} {
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID: fmt.Sprintf("edge_%d", i), TenantID: f.tenantID, EnvironmentID: f.envID, EventName: "usage",
				ExternalCustomerID: "parent", Source: "api", Timestamp: ts,
				Properties: map[string]interface{}{"user": "edge", "app": "build", "owner": "o2"},
			},
			MeterID: "m_tokens", QtyTotal: decimal.NewFromInt(1000),
		})
	}
	require.NoError(t, repo.BulkInsertMeterUsage(context.Background(), records))
	require.NoError(t, ref.BulkInsertMeterUsage(context.Background(), records))

	if withDuplicates {
		// Re-ingest some events with a new quantity; deduplication keeps the latest.
		var dups []*events.MeterUsage
		for _, r := range records[:100] {
			d := *r
			d.QtyTotal = r.QtyTotal.Add(decimal.NewFromInt(7))
			d.IngestedAt = time.Now().UTC().Add(time.Minute)
			dups = append(dups, &d)
		}
		require.NoError(t, insertWithIngestedAt(repo, dups))
		require.NoError(t, ref.BulkInsertMeterUsage(context.Background(), dups))
	}
	return f
}

// insertWithIngestedAt writes rows with an explicit ingested_at, which BulkInsertMeterUsage leaves to the server.
func insertWithIngestedAt(repo *MeterUsageRepository, records []*events.MeterUsage) error {
	ctx := context.Background()
	batch, err := repo.store.GetConn().PrepareBatch(ctx, `INSERT INTO meter_usage (
		id, tenant_id, environment_id, external_customer_id, meter_id, event_name,
		timestamp, qty_total, unique_hash, source, properties, ingested_at)`)
	if err != nil {
		return err
	}
	for _, r := range records {
		if err := batch.Append(r.ID, r.TenantID, r.EnvironmentID, r.ExternalCustomerID, r.MeterID, r.EventName,
			r.Timestamp, r.QtyTotal, r.UniqueHash, r.Source, repo.marshalProperties(r), r.IngestedAt); err != nil {
			return err
		}
	}
	return batch.Send()
}

func randomUsageAnalyticsQuery(rng *rand.Rand, dedupe bool) *usageanalytics.Query {
	allKeys := []usageanalytics.Key{keyUser, keyApp, keySource, keyLine, keyFeature,
		{Kind: usageanalytics.KeyMeterID}}
	pick := func(n int) []usageanalytics.Key {
		perm := rng.Perm(len(allKeys))
		return lo.Map(perm[:n], func(i int, _ int) usageanalytics.Key { return allKeys[i] })
	}

	q := &usageanalytics.Query{
		Start: oct1, End: nov1, Timezone: time.UTC,
		GroupBy:         pick(1 + rng.Intn(3)),
		Deduplicate:     dedupe,
		WithLineItemIDs: rng.Intn(2) == 0,
		Limit:           1 + rng.Intn(8),
		Offset:          rng.Intn(10),
		Sort: usageanalytics.Sort{
			Field: []usageanalytics.SortField{usageanalytics.SortAmount, usageanalytics.SortName, usageanalytics.SortUsage, usageanalytics.SortEventCount}[rng.Intn(4)],
			Desc:  rng.Intn(2) == 0,
		},
	}
	if rng.Intn(2) == 0 {
		col := []usageanalytics.Key{keyApp, keyMonth, keySource}[rng.Intn(3)]
		q.Columns = &col
		if rng.Intn(2) == 0 {
			q.Sort = usageanalytics.Sort{Field: usageanalytics.SortColumn, Column: "run", Desc: true}
		}
		if col == keyMonth {
			ist, _ := time.LoadLocation("Asia/Kolkata")
			q.Timezone = ist
		}
	}
	if rng.Intn(2) == 0 {
		q.Metrics = []usageanalytics.Metric{{Type: usageanalytics.MetricDistinct, Key: keyOwner}}
		if rng.Intn(2) == 0 {
			q.Metrics = append(q.Metrics, usageanalytics.Metric{Type: usageanalytics.MetricDistinct, Key: keyUser})
		}
	}
	switch rng.Intn(6) {
	case 0:
		q.Filters = []usageanalytics.Filter{{Key: keyApp, Op: usageanalytics.FilterIn, Values: []string{"build", "run"}}}
	case 1:
		q.Filters = []usageanalytics.Filter{{Key: keyUser, Op: usageanalytics.FilterNotIn, Values: []string{"", "123"}}}
	case 2:
		// li_tok_old overlaps li_tok_addon; dropping it moves those events' usage to the add-on.
		q.Filters = []usageanalytics.Filter{{Key: keyLine, Op: usageanalytics.FilterNotIn, Values: []string{"li_tok_old"}}}
	case 3:
		q.Filters = []usageanalytics.Filter{{Key: keyLine, Op: usageanalytics.FilterIn, Values: []string{"li_tok_addon", "li_min"}}}
	}
	if rng.Intn(4) == 0 {
		q.Search = []string{"AL", "o", "12", "zzz"}[rng.Intn(4)]
	}
	if rng.Intn(2) == 0 {
		q.WindowSize = []types.WindowSize{types.WindowSizeDay, types.WindowSizeWeek, types.WindowSizeMonth}[rng.Intn(3)]
		// Los Angeles leaves daylight saving on Nov 1, inside the range.
		tz, _ := time.LoadLocation([]string{"UTC", "Asia/Kolkata", "America/Los_Angeles"}[rng.Intn(3)])
		q.Timezone = tz
	}
	return q
}

func TestUsageBreakdown_MatchesReference_Integration(t *testing.T) {
	store := newRealClickHouseTestStore(t)
	log, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
	require.NoError(t, err)

	usageRepo := &MeterUsageRepository{store: store, logger: log, qb: NewMeterUsageQueryBuilder()}
	queryRepo := NewUsageAnalyticsRepository(store, log)
	refUsage := testutil.NewInMemoryMeterUsageStore()
	ref := testutil.NewInMemoryUsageAnalyticsStore(refUsage)

	rng := rand.New(rand.NewSource(1804))
	clean := seedUsageFixture(t, rng, usageRepo, refUsage, false)
	dupes := seedUsageFixture(t, rng, usageRepo, refUsage, true)

	run := func(t *testing.T, f usageFixture, q *usageanalytics.Query) {
		params := &usageanalytics.QueryParams{TenantID: f.tenantID, EnvironmentID: f.envID, Query: q, Lines: f.lines}
		got, err := queryRepo.GetBreakdown(context.Background(), params)
		require.NoError(t, err)
		want, err := ref.GetBreakdown(context.Background(), params)
		require.NoError(t, err)
		requireSameUsageResult(t, want, got)
	}

	for i := 0; i < 150; i++ {
		// The clean tenant has no duplicates, so both dedupe modes must agree with the reference;
		// the duplicate tenant is only deterministic with deduplication on.
		q := randomUsageAnalyticsQuery(rng, i%2 == 0)
		t.Run(fmt.Sprintf("clean_%d", i), func(t *testing.T) { run(t, clean, q) })
		dq := randomUsageAnalyticsQuery(rng, true)
		t.Run(fmt.Sprintf("dedupe_%d", i), func(t *testing.T) { run(t, dupes, dq) })
	}

	t.Run("page past the end keeps totals", func(t *testing.T) {
		q := randomUsageAnalyticsQuery(rng, false)
		q.Offset = 10_000
		run(t, clean, q)
	})

	t.Run("no lines returns empty without querying", func(t *testing.T) {
		got, err := queryRepo.GetBreakdown(context.Background(), &usageanalytics.QueryParams{
			TenantID: clean.tenantID, EnvironmentID: clean.envID, Query: randomUsageAnalyticsQuery(rng, false),
		})
		require.NoError(t, err)
		require.Empty(t, got.Items)
		require.Zero(t, got.TotalCount)
	})

	t.Run("weekly points start on Sunday and add up to the totals", func(t *testing.T) {
		la, err := time.LoadLocation("America/Los_Angeles")
		require.NoError(t, err)
		q := &usageanalytics.Query{
			Start: oct1, End: nov1, Timezone: la, WindowSize: types.WindowSizeWeek,
			GroupBy: []usageanalytics.Key{keyApp}, Sort: usageanalytics.Sort{Field: usageanalytics.SortAmount, Desc: true}, Limit: 10,
		}
		got, err := queryRepo.GetBreakdown(context.Background(), &usageanalytics.QueryParams{
			TenantID: clean.tenantID, EnvironmentID: clean.envID, Query: q, Lines: clean.lines,
		})
		require.NoError(t, err)
		require.NotEmpty(t, got.Items)
		for _, row := range got.Items {
			require.NotEmpty(t, row.Points)
			sum := decimal.Zero
			for _, p := range row.Points {
				require.Equal(t, time.Sunday, p.WindowStart.Weekday(), "window %s", p.WindowStart)
				require.Zero(t, p.WindowStart.Hour(), "window %s must be local midnight", p.WindowStart)
				sum = sum.Add(p.Amount)
			}
			require.True(t, sum.Equal(row.Amount), "points must add up to the row: %s vs %s", sum, row.Amount)
		}
	})

	t.Run("worked example: price change and numeric property", func(t *testing.T) {
		q := &usageanalytics.Query{
			Start: oct1, End: nov1, Timezone: time.UTC,
			GroupBy: []usageanalytics.Key{keyLine}, Filters: []usageanalytics.Filter{
				{Key: keyUser, Op: usageanalytics.FilterIn, Values: []string{"123"}},
			},
			Sort: usageanalytics.Sort{Field: usageanalytics.SortName}, Limit: 10,
		}
		got, err := queryRepo.GetBreakdown(context.Background(), &usageanalytics.QueryParams{
			TenantID: clean.tenantID, EnvironmentID: clean.envID, Query: q, Lines: clean.lines,
		})
		require.NoError(t, err)
		require.NotEmpty(t, got.Items, "numeric property values must be groupable, not (none)")
		for _, row := range got.Items {
			line, ok := lo.Find(clean.lines, func(l usageanalytics.LineWindow) bool { return l.LineItemID == row.Group[0] })
			require.True(t, ok)
			require.True(t, row.Quantity.Mul(line.UnitPrice).Equal(row.Amount), "line %s: amount must be quantity × its own price", line.LineItemID)
			require.True(t, row.Quantity.Mul(line.ListUnitPrice).Equal(row.Subtotal), "line %s: subtotal must be quantity × its list price", line.LineItemID)
		}
	})
}

func requireSameUsageResult(t *testing.T, want, got *usageanalytics.Result) {
	t.Helper()
	require.Equal(t, want.TotalCount, got.TotalCount, "total_count")
	requireSameUsageRow(t, "totals", want.Totals, got.Totals)
	require.Len(t, got.Items, len(want.Items))
	for i := range want.Items {
		requireSameUsageRow(t, fmt.Sprintf("item %d", i), want.Items[i], got.Items[i])
	}
}

func requireSameUsageRow(t *testing.T, label string, want, got usageanalytics.Row) {
	t.Helper()
	require.Equal(t, want.Group, got.Group, "%s group", label)
	require.True(t, want.Amount.Equal(got.Amount), "%s amount: want %s got %s", label, want.Amount, got.Amount)
	require.True(t, want.Subtotal.Equal(got.Subtotal), "%s subtotal: want %s got %s", label, want.Subtotal, got.Subtotal)
	require.True(t, want.Quantity.Equal(got.Quantity), "%s quantity: want %s got %s", label, want.Quantity, got.Quantity)
	require.Equal(t, want.EventCount, got.EventCount, "%s event_count", label)
	require.Equal(t, want.LineItemIDs, got.LineItemIDs, "%s line item ids", label)
	require.Len(t, got.Columns, len(want.Columns), "%s columns: want %v got %v", label, want.Columns, got.Columns)
	for k, v := range want.Columns {
		require.True(t, v.Equal(got.Columns[k]), "%s column %q: want %s got %s", label, k, v, got.Columns[k])
	}
	require.Equal(t, len(want.Metrics), len(got.Metrics), "%s metrics", label)
	for name, w := range want.Metrics {
		g := got.Metrics[name]
		require.Equal(t, w.Count, g.Count, "%s metric %s count", label, name)
		require.ElementsMatch(t, w.Values, g.Values, "%s metric %s values", label, name)
	}
	require.Len(t, got.Points, len(want.Points), "%s points: want %v got %v", label, want.Points, got.Points)
	for i, w := range want.Points {
		g := got.Points[i]
		require.True(t, w.WindowStart.Equal(g.WindowStart), "%s point %d window: want %s got %s", label, i, w.WindowStart, g.WindowStart)
		require.True(t, w.Amount.Equal(g.Amount), "%s point %d amount: want %s got %s", label, i, w.Amount, g.Amount)
		require.True(t, w.Subtotal.Equal(g.Subtotal), "%s point %d subtotal: want %s got %s", label, i, w.Subtotal, g.Subtotal)
		require.True(t, w.Quantity.Equal(g.Quantity), "%s point %d quantity: want %s got %s", label, i, w.Quantity, g.Quantity)
	}
}
