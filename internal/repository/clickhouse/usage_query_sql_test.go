package clickhouse

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/flexprice/flexprice/internal/domain/usageanalytics"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update", false, "rewrite golden SQL files")

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

func goldenLine(id, meter string, start, end time.Time, price string) usageanalytics.LineWindow {
	return usageanalytics.LineWindow{
		LineItemID: id, MeterID: meter, FeatureID: "feat_" + meter,
		ExternalCustomerIDs: []string{"cust_a"},
		Start:               start, End: end, Currency: "usd",
		UnitPrice:     decimal.RequireFromString(price),
		ListUnitPrice: decimal.RequireFromString(price),
	}
}

func goldenQuery(mutate func(q *usageanalytics.Query)) *usageanalytics.Query {
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

func TestBuildBreakdownSQL_Golden(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)

	tests := []struct {
		name  string
		query *usageanalytics.Query
		lines []usageanalytics.LineWindow
	}{
		{
			name:  "one_line",
			query: goldenQuery(nil),
			lines: []usageanalytics.LineWindow{goldenLine("li_1", "m_tokens", oct1, nov1, "0.002")},
		},
		{
			// A price change ends one line and starts another; each event is priced by its own line.
			name:  "price_change",
			query: goldenQuery(nil),
			lines: []usageanalytics.LineWindow{
				goldenLine("li_old", "m_tokens", oct1, oct15, "0.002"),
				goldenLine("li_new", "m_tokens", oct15, nov1, "0.003"),
			},
		},
		{
			// Two lines on the same meter at the same time both price the event.
			name: "overlapping_lines_parent_child",
			query: goldenQuery(func(q *usageanalytics.Query) {
				q.GroupBy = []usageanalytics.Key{keyLine, keyUser}
			}),
			lines: []usageanalytics.LineWindow{
				func() usageanalytics.LineWindow {
					l := goldenLine("li_parent", "m_tokens", oct1, nov1, "0.002")
					l.ExternalCustomerIDs = []string{"cust_parent", "cust_child"}
					return l
				}(),
				goldenLine("li_addon", "m_tokens", oct1, nov1, "0.001"),
			},
		},
		{
			name: "filters_search_dedupe",
			query: goldenQuery(func(q *usageanalytics.Query) {
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
			lines: []usageanalytics.LineWindow{goldenLine("li_1", "m_tokens", oct1, nov1, "0.002")},
		},
		{
			name: "columns_metrics_column_sort",
			query: goldenQuery(func(q *usageanalytics.Query) {
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
				goldenLine("li_1", "m_tokens", oct1, nov1, "0.002"),
				goldenLine("li_2", "m_minutes", oct1, nov1, "0.5"),
			},
		},
		{
			name: "month_columns_timezone",
			query: goldenQuery(func(q *usageanalytics.Query) {
				q.Columns = &keyMonth
				q.Timezone = ist
				q.Sort = usageanalytics.Sort{Field: usageanalytics.SortUsage, Desc: true}
			}),
			lines: []usageanalytics.LineWindow{goldenLine("li_1", "m_tokens", oct1, nov1, "0.002")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, tt.query.Validate())
			built := buildBreakdownSQL(&usageanalytics.QueryParams{
				TenantID: "tenant_1", EnvironmentID: "env_1", Query: tt.query, Lines: tt.lines,
			})
			got := renderGolden(built)

			path := filepath.Join("testdata", "usage_query", tt.name+".sql")
			if *updateGolden {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err, "missing golden file; run with -update")
			require.Equal(t, string(want), got)
		})
	}
}

func TestLinesCTEPricesEachCustomerOnce(t *testing.T) {
	l := goldenLine("li_1", "m_tokens", oct1, nov1, "0.002")
	l.ExternalCustomerIDs = []string{"cust_a", "cust_a"}
	built := buildBreakdownSQL(&usageanalytics.QueryParams{TenantID: "t", EnvironmentID: "e", Query: goldenQuery(nil), Lines: []usageanalytics.LineWindow{l}})
	for _, a := range built.args {
		if nv := a.(driver.NamedValue); nv.Name == "line_customer_ids" {
			require.Equal(t, []string{"cust_a"}, nv.Value)
			return
		}
	}
	t.Fatal("line_customer_ids not bound")
}

func TestBuildPointsSQL_Golden(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	q := goldenQuery(func(q *usageanalytics.Query) {
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
		Lines: []usageanalytics.LineWindow{goldenLine("li_1", "m_tokens", oct1, nov1, "0.002")},
	}, [][]string{{"alice", "api"}, {"", "sdk"}})
	got := renderGolden(built)

	path := filepath.Join("testdata", "usage_query", "points_weekly_timezone.sql")
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file; run with -update")
	require.Equal(t, string(want), got)
}

// renderGolden prints the query followed by its named arguments.
func renderGolden(built *breakdownSQL) string {
	var sb strings.Builder
	sb.WriteString(built.query)
	sb.WriteString("\n\n-- arguments\n")
	for _, a := range built.args {
		nv := a.(driver.NamedValue)
		v := nv.Value
		if ts, ok := v.(time.Time); ok {
			v = ts.Format(time.RFC3339)
		}
		sb.WriteString(fmt.Sprintf("-- @%s = %v\n", nv.Name, v))
	}
	return sb.String()
}

func TestSQLStringLiteral(t *testing.T) {
	require.Equal(t, `'it\'s'`, sqlStringLiteral("it's"))
	require.Equal(t, `'a\\b'`, sqlStringLiteral(`a\b`))
}
