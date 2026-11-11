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
	"github.com/flexprice/flexprice/internal/domain/costanalytics"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update", false, "rewrite golden SQL files")

var (
	oct1  = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	oct15 = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	nov1  = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	keyUser    = costanalytics.Key{Kind: costanalytics.KeyProperty, Property: "user"}
	keyApp     = costanalytics.Key{Kind: costanalytics.KeyProperty, Property: "app"}
	keyOwner   = costanalytics.Key{Kind: costanalytics.KeyProperty, Property: "owner"}
	keySource  = costanalytics.Key{Kind: costanalytics.KeySource}
	keyLine    = costanalytics.Key{Kind: costanalytics.KeySubLineItemID}
	keyFeature = costanalytics.Key{Kind: costanalytics.KeyFeatureID}
	keyMonth   = costanalytics.Key{Kind: costanalytics.KeyMonth}
)

func goldenLine(id, meter string, start, end time.Time, price string) costanalytics.LineWindow {
	return costanalytics.LineWindow{
		LineItemID: id, MeterID: meter, FeatureID: "feat_" + meter,
		ExternalCustomerIDs: []string{"cust_a"},
		Start:               start, End: end, Currency: "usd",
		UnitPrice: decimal.RequireFromString(price),
	}
}

func goldenQuery(mutate func(q *costanalytics.Query)) *costanalytics.Query {
	q := &costanalytics.Query{
		Start: oct1, End: nov1, Timezone: time.UTC,
		GroupBy: []costanalytics.Key{keyUser},
		Sort:    costanalytics.Sort{Field: costanalytics.SortTotal, Desc: true},
		Limit:   25,
	}
	if mutate != nil {
		mutate(q)
	}
	return q
}

func TestBuildCostBreakdownSQL_Golden(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)

	tests := []struct {
		name  string
		query *costanalytics.Query
		lines []costanalytics.LineWindow
	}{
		{
			name:  "one_line",
			query: goldenQuery(nil),
			lines: []costanalytics.LineWindow{goldenLine("li_1", "m_tokens", oct1, nov1, "0.002")},
		},
		{
			// A price change ends one line and starts another; each event is priced by its own line.
			name:  "price_change",
			query: goldenQuery(nil),
			lines: []costanalytics.LineWindow{
				goldenLine("li_old", "m_tokens", oct1, oct15, "0.002"),
				goldenLine("li_new", "m_tokens", oct15, nov1, "0.003"),
			},
		},
		{
			// Two lines on the same meter at the same time both price the event.
			name: "overlapping_lines_parent_child",
			query: goldenQuery(func(q *costanalytics.Query) {
				q.GroupBy = []costanalytics.Key{keyLine, keyUser}
			}),
			lines: []costanalytics.LineWindow{
				func() costanalytics.LineWindow {
					l := goldenLine("li_parent", "m_tokens", oct1, nov1, "0.002")
					l.ExternalCustomerIDs = []string{"cust_parent", "cust_child"}
					return l
				}(),
				goldenLine("li_addon", "m_tokens", oct1, nov1, "0.001"),
			},
		},
		{
			name: "filters_search_dedupe",
			query: goldenQuery(func(q *costanalytics.Query) {
				q.GroupBy = []costanalytics.Key{keyUser, keySource}
				q.Filters = []costanalytics.Filter{
					{Key: keyApp, Op: costanalytics.FilterIn, Values: []string{"build", "run"}},
					{Key: keySource, Op: costanalytics.FilterNotIn, Values: []string{"test"}},
				}
				q.Search = "ali"
				q.Deduplicate = true
				q.Sort = costanalytics.Sort{Field: costanalytics.SortName}
				q.Offset = 50
			}),
			lines: []costanalytics.LineWindow{goldenLine("li_1", "m_tokens", oct1, nov1, "0.002")},
		},
		{
			name: "columns_metrics_column_sort",
			query: goldenQuery(func(q *costanalytics.Query) {
				q.GroupBy = []costanalytics.Key{keyFeature}
				q.Columns = &keyApp
				q.Metrics = []costanalytics.Metric{
					{Type: costanalytics.MetricDistinct, Key: keyOwner},
					{Type: costanalytics.MetricDistinct, Key: keyUser},
				}
				q.Sort = costanalytics.Sort{Field: costanalytics.SortColumn, Column: "run", Desc: true}
			}),
			lines: []costanalytics.LineWindow{
				goldenLine("li_1", "m_tokens", oct1, nov1, "0.002"),
				goldenLine("li_2", "m_minutes", oct1, nov1, "0.5"),
			},
		},
		{
			name: "month_columns_timezone",
			query: goldenQuery(func(q *costanalytics.Query) {
				q.Columns = &keyMonth
				q.Timezone = ist
				q.Sort = costanalytics.Sort{Field: costanalytics.SortUsage, Desc: true}
			}),
			lines: []costanalytics.LineWindow{goldenLine("li_1", "m_tokens", oct1, nov1, "0.002")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, tt.query.Validate())
			built := buildCostBreakdownSQL(&costanalytics.QueryParams{
				TenantID: "tenant_1", EnvironmentID: "env_1", Query: tt.query, Lines: tt.lines,
			})
			got := renderGolden(built)

			path := filepath.Join("testdata", "cost_analytics", tt.name+".sql")
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
	built := buildCostBreakdownSQL(&costanalytics.QueryParams{TenantID: "t", EnvironmentID: "e", Query: goldenQuery(nil), Lines: []costanalytics.LineWindow{l}})
	for _, a := range built.args {
		if nv := a.(driver.NamedValue); nv.Name == "line_customer_ids" {
			require.Equal(t, []string{"cust_a"}, nv.Value)
			return
		}
	}
	t.Fatal("line_customer_ids not bound")
}

// renderGolden prints the query followed by its named arguments.
func renderGolden(built *costSQL) string {
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
