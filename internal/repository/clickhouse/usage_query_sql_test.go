package clickhouse

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/flexprice/flexprice/internal/domain/usageanalytics"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

var (
	oct1  = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	oct15 = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	nov1  = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	keyUser    = usageanalytics.Key{Kind: usageanalytics.KeyProperty, Property: "user"}
	keyApp     = usageanalytics.Key{Kind: usageanalytics.KeyProperty, Property: "app"}
	keyOwner   = usageanalytics.Key{Kind: usageanalytics.KeyProperty, Property: "owner"}
	keySource  = usageanalytics.Key{Kind: usageanalytics.KeySource}
	keyLine    = usageanalytics.Key{Kind: usageanalytics.KeySubLineItemID}
	keyFeature = usageanalytics.Key{Kind: usageanalytics.KeyFeatureID}
	keyMonth   = usageanalytics.Key{Kind: usageanalytics.KeyMonth}
)

func testLine(id, meter string, start, end time.Time, price string) usageanalytics.LineWindow {
	return usageanalytics.LineWindow{
		LineItemID: id, MeterID: meter, FeatureID: "feat_" + meter,
		ExternalCustomerIDs: []string{"cust_a"},
		Start:               start, End: end, Currency: "usd",
		UnitPrice:     decimal.RequireFromString(price),
		ListUnitPrice: decimal.RequireFromString(price),
	}
}

func testQuery(mutate func(q *usageanalytics.Query)) *usageanalytics.Query {
	q := &usageanalytics.Query{
		Start: oct1, End: nov1, Timezone: time.UTC,
		GroupBy: []usageanalytics.Key{keyUser},
		Sort:    usageanalytics.Sort{Field: usageanalytics.SortAmount, Desc: true},
		Limit:   25,
	}
	if mutate != nil {
		mutate(q)
	}
	return q
}

func TestBuildBreakdownSQL_Invariants(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)

	tests := []struct {
		name  string
		query *usageanalytics.Query
		lines []usageanalytics.LineWindow
	}{
		{
			name:  "one_line",
			query: testQuery(nil),
			lines: []usageanalytics.LineWindow{testLine("li_1", "m_tokens", oct1, nov1, "0.002")},
		},
		{
			// A price change ends one line and starts another; each event is priced by its own line.
			name:  "price_change",
			query: testQuery(nil),
			lines: []usageanalytics.LineWindow{
				testLine("li_old", "m_tokens", oct1, oct15, "0.002"),
				testLine("li_new", "m_tokens", oct15, nov1, "0.003"),
			},
		},
		{
			// Two lines on the same meter at the same time both price the event.
			name: "overlapping_lines_parent_child",
			query: testQuery(func(q *usageanalytics.Query) {
				q.GroupBy = []usageanalytics.Key{keyLine, keyUser}
			}),
			lines: []usageanalytics.LineWindow{
				func() usageanalytics.LineWindow {
					l := testLine("li_parent", "m_tokens", oct1, nov1, "0.002")
					l.ExternalCustomerIDs = []string{"cust_parent", "cust_child"}
					return l
				}(),
				testLine("li_addon", "m_tokens", oct1, nov1, "0.001"),
			},
		},
		{
			name: "filters_search_dedupe",
			query: testQuery(func(q *usageanalytics.Query) {
				q.GroupBy = []usageanalytics.Key{keyUser, keySource}
				q.Filters = []usageanalytics.Filter{
					{Key: keyApp, Op: usageanalytics.FilterIn, Values: []string{"build", "run"}},
					{Key: keySource, Op: usageanalytics.FilterNotIn, Values: []string{"test"}},
				}
				q.Search = "ali"
				q.Deduplicate = true
				q.Sort = usageanalytics.Sort{Field: usageanalytics.SortName}
				q.Offset = 50
			}),
			lines: []usageanalytics.LineWindow{testLine("li_1", "m_tokens", oct1, nov1, "0.002")},
		},
		{
			name: "columns_metrics_column_sort",
			query: testQuery(func(q *usageanalytics.Query) {
				q.GroupBy = []usageanalytics.Key{keyFeature}
				q.Columns = &keyApp
				q.Metrics = []usageanalytics.Metric{
					{Type: usageanalytics.MetricDistinct, Key: keyOwner},
					{Type: usageanalytics.MetricDistinct, Key: keyUser},
				}
				q.Sort = usageanalytics.Sort{Field: usageanalytics.SortColumn, Column: "run", Desc: true}
				q.WithLineItemIDs = true
			}),
			lines: []usageanalytics.LineWindow{
				testLine("li_1", "m_tokens", oct1, nov1, "0.002"),
				testLine("li_2", "m_minutes", oct1, nov1, "0.5"),
			},
		},
		{
			name: "month_columns_timezone",
			query: testQuery(func(q *usageanalytics.Query) {
				q.Columns = &keyMonth
				q.Timezone = ist
				q.Sort = usageanalytics.Sort{Field: usageanalytics.SortUsage, Desc: true}
			}),
			lines: []usageanalytics.LineWindow{testLine("li_1", "m_tokens", oct1, nov1, "0.002")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, tt.query.Validate())
			built := buildBreakdownSQL(&usageanalytics.QueryParams{
				TenantID: "tenant_1", EnvironmentID: "env_1", Query: tt.query, Lines: tt.lines,
			})
			requireSQLInvariants(t, built, tt.query.Deduplicate)
			require.Contains(t, built.query, "LIMIT @limit OFFSET @offset")
			require.Equal(t, tt.query.Limit, boundArg(t, built, "limit"))
			require.Equal(t, tt.query.Offset, boundArg(t, built, "offset"))
		})
	}
}

func TestLinesCTEPricesEachCustomerOnce(t *testing.T) {
	l := testLine("li_1", "m_tokens", oct1, nov1, "0.002")
	l.ExternalCustomerIDs = []string{"cust_a", "cust_a"}
	built := buildBreakdownSQL(&usageanalytics.QueryParams{TenantID: "t", EnvironmentID: "e", Query: testQuery(nil), Lines: []usageanalytics.LineWindow{l}})
	for _, a := range built.args {
		if nv := a.(driver.NamedValue); nv.Name == "line_customer_ids" {
			require.Equal(t, []string{"cust_a"}, nv.Value)
			return
		}
	}
	t.Fatal("line_customer_ids not bound")
}

func TestBuildPointsSQL_Invariants(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	q := testQuery(func(q *usageanalytics.Query) {
		q.GroupBy = []usageanalytics.Key{keyUser, keySource}
		q.Filters = []usageanalytics.Filter{{Key: keyApp, Op: usageanalytics.FilterIn, Values: []string{"build"}}}
		q.Search = "ali"
		q.Deduplicate = true
		q.Timezone = ist
		q.WindowSize = types.WindowSizeWeek
	})
	require.NoError(t, q.Validate())
	built := buildPointsSQL(&usageanalytics.QueryParams{
		TenantID: "tenant_1", EnvironmentID: "env_1", Query: q,
		Lines: []usageanalytics.LineWindow{testLine("li_1", "m_tokens", oct1, nov1, "0.002")},
	}, [][]string{{"alice", "api"}, {"", "sdk"}})
	requireSQLInvariants(t, built, true)
	require.Equal(t, [][]string{{"alice", "api"}, {"", "sdk"}}, boundArg(t, built, "page_groups"))
}

// requireSQLInvariants checks what every usage query must do: scope to the tenant and environment,
// cap memory, bind every placeholder, and read FINAL only when deduplicating.
func requireSQLInvariants(t *testing.T, built *breakdownSQL, dedupe bool) {
	t.Helper()
	require.Contains(t, built.query, "mu.tenant_id = @tenant_id")
	require.Contains(t, built.query, "mu.environment_id = @environment_id")
	require.Equal(t, "tenant_1", boundArg(t, built, "tenant_id"))
	require.Equal(t, "env_1", boundArg(t, built, "environment_id"))
	require.Contains(t, built.query, maxMemoryUsageSetting)
	require.Equal(t, dedupe, strings.Contains(built.query, "FROM meter_usage AS mu FINAL"))
	for _, m := range regexp.MustCompile(`@(\w+)`).FindAllStringSubmatch(built.query, -1) {
		boundArg(t, built, m[1])
	}
}

func boundArg(t *testing.T, built *breakdownSQL, name string) any {
	t.Helper()
	for _, a := range built.args {
		if nv := a.(driver.NamedValue); nv.Name == name {
			return nv.Value
		}
	}
	t.Fatalf("@%s is not bound", name)
	return nil
}

func TestSQLStringLiteral(t *testing.T) {
	require.Equal(t, `'it\'s'`, sqlStringLiteral("it's"))
	require.Equal(t, `'a\\b'`, sqlStringLiteral(`a\b`))
}

func TestLineSlices_OnePrimaryLinePerInstant(t *testing.T) {
	monthly := testLine("li_a", "m_calls", oct1, nov1, "0.01")
	quarterly := testLine("li_b", "m_calls", oct15, nov1.AddDate(0, 2, 0), "0.02")
	other := testLine("li_c", "m_tokens", oct1, nov1, "0.002")
	type slice struct {
		line       string
		start, end time.Time
		primary    bool
	}
	got := func(q *usageanalytics.Query) []slice {
		var out []slice
		for _, sl := range lineSlices(q, []usageanalytics.LineWindow{quarterly, monthly, other}) {
			out = append(out, slice{sl.line.LineItemID, time.Unix(sl.start, 0).UTC(), time.Unix(sl.end, 0).UTC(), sl.primary})
		}
		return out
	}
	byMeter := testQuery(nil)
	byLine := testQuery(func(q *usageanalytics.Query) { q.GroupBy = []usageanalytics.Key{keyLine} })
	withoutA := testQuery(func(q *usageanalytics.Query) {
		q.Filters = []usageanalytics.Filter{{Key: keyLine, Op: usageanalytics.FilterNotIn, Values: []string{"li_a"}}}
	})

	require.Equal(t, []slice{
		{"li_b", oct15, nov1, false}, // li_a is the lower ID where both run
		{"li_b", nov1, nov1.AddDate(0, 2, 0), true},
		{"li_a", oct1, oct15, true},
		{"li_a", oct15, nov1, true},
		{"li_c", oct1, nov1, true}, // another meter never overlaps
	}, got(byMeter))
	require.True(t, lo.EveryBy(got(byLine), func(s slice) bool { return s.primary }), "split by line, every line counts its events")
	require.Equal(t, []slice{
		{"li_b", oct15, nov1, true}, // li_a is filtered out, so li_b counts
		{"li_b", nov1, nov1.AddDate(0, 2, 0), true},
		{"li_a", oct1, oct15, false},
		{"li_a", oct15, nov1, false},
		{"li_c", oct1, nov1, true},
	}, got(withoutA))
}
