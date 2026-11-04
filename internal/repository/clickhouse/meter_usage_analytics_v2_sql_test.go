package clickhouse

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Run `go test ./internal/repository/clickhouse -run TestAnalyticsV2SQLGolden -update-golden` after an intended
// change to the generated SQL, then review the .sql diffs.
var updateGolden = flag.Bool("update-golden", false, "rewrite the golden SQL files under testdata/analytics_v2")

// TestAnalyticsV2SQLGolden pins the SQL each analytics v2 query builds, so query changes show up as readable
// .sql diffs in review. Behaviour is covered by TestRankAnalyticsV2Rows_MatchesOracle_Integration.
func TestAnalyticsV2SQLGolden(t *testing.T) {
	aug := func(d, h int) time.Time { return time.Date(2026, 8, d, h, 0, 0, 0, time.UTC) }
	d := decimal.RequireFromString
	lines := []events.AnalyticsV2Line{
		{ID: "li_ai_old", MeterID: "ai", Start: aug(1, 0), End: aug(15, 12)},
		{ID: "li_ai_new", MeterID: "ai", Start: aug(15, 12), End: aug(31, 0)},
		{ID: "li_compute_a", MeterID: "compute", Start: aug(1, 0), End: aug(31, 0)},
		{ID: "li_compute_b", MeterID: "compute", Start: aug(1, 0), End: aug(31, 0)},
		{ID: "li_gpu", MeterID: "gpu", Start: aug(1, 0), End: aug(31, 0), BucketSize: types.WindowSizeDay, BucketGroup: "type", BillingAnchor: aug(1, 0)},
	}
	rank := events.AnalyticsV2RankParams{
		Scope: events.AnalyticsV2Scope{TenantID: "tenant", EnvironmentID: "env", ExternalCustomerIDs: []string{"cust"},
			DataThrough: aug(31, 0)},
		StartTime: aug(1, 0), EndTime: aug(31, 0),
		Lines: lines,
		Rates: []events.AnalyticsV2Amounts{
			{Usage: d("2"), Discount: d("0.2"), Free: d("0.25")}, {Usage: d("1.5")},
			{CommitmentUtilized: d("3"), TrueUp: d("0.5")}, {Usage: d("0.1"), Overage: d("0.05")}, {Usage: d("12")},
		},
		MeterFeatures: map[string]string{"ai": "feat_ai", "compute": "feat_compute", "gpu": "feat_gpu"},
		GroupBy:       []string{"properties.app"},
		Column:        "properties.type",
		Timezone:      "UTC",
		Filters: []events.AnalyticsV2Filter{
			{Key: "properties.app", Exclude: true, Values: []string{"payout"}},
			{Key: "source", Values: []string{"api"}},
		},
		Search: "pro",
		Sort:   events.AnalyticsV2Sort{By: events.AnalyticsV2SortByColumn, Column: "run", Desc: true},
		Limit:  10,
	}
	detail := &events.AnalyticsV2DetailParams{
		Rank: rank, Rows: [][]string{{"promo"}, {"launch"}}, WindowSize: types.WindowSizeDay,
		DistinctKeys: []string{"source", "properties.type"},
	}

	cases := map[string]*sqlBuilder{
		"line_usage": buildLineUsageQuery(&events.AnalyticsV2LineUsageParams{
			Scope: rank.Scope, Lines: lines, RangeStart: aug(10, 0), RangeEnd: aug(20, 0),
		}),
		"rank":     buildRankQuery(&rank),
		"points":   buildPointsQuery(detail),
		"distinct": buildDistinctQuery(detail),
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			got := renderSQL(q)
			path := filepath.Join("testdata", "analytics_v2", name+".sql")
			if *updateGolden {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err, "missing golden file; run with -update-golden")
			require.Equal(t, string(want), got, "generated SQL changed; review and run with -update-golden if intended")
		})
	}
}

// renderSQL prints a query and its server-side parameters, one per line, in placeholder order.
func renderSQL(q *sqlBuilder) string {
	names := make([]string, 0, len(q.params))
	for n := range q.params {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		var a, b int
		fmt.Sscanf(names[i], "p%d", &a)
		fmt.Sscanf(names[j], "p%d", &b)
		return a < b
	})
	var out strings.Builder
	out.WriteString(strings.TrimSpace(q.sql.String()))
	out.WriteString("\n\n-- parameters\n")
	for _, n := range names {
		out.WriteString("-- " + n + " = " + q.params[n] + "\n")
	}
	return out.String()
}
