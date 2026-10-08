package clickhouse

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// day parses a "2006-01-02" date string into a UTC time.Time for test fixtures.
func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// TestBuildDailyUsageQuery_GroupsByDayWithRLS proves the builder groups by
// meter and day, scopes by tenant/environment/meter,
// and honors UseFinal — mirroring BuildDetailedWhereClause / BuildFinalClause
// used elsewhere in this file, without touching any existing method.
func TestBuildDailyUsageQuery_GroupsByDayWithRLS(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	q, args := qb.BuildDailyUsageQuery(&events.DailyUsageParams{
		TenantID:      "t1",
		EnvironmentID: "e1",
		MeterIDs:      []string{"m1"},
		StartTime:     day("2026-09-01"),
		EndTime:       day("2026-10-01"),
		UseFinal:      true,
	})

	assert.Contains(t, q, "toStartOfDay(timestamp")
	assert.Contains(t, q, "tenant_id = ?")
	assert.Contains(t, q, "environment_id = ?")
	assert.Contains(t, q, "GROUP BY meter_id, day")
	assert.Contains(t, q, "ORDER BY meter_id ASC, day ASC")
	assert.Contains(t, q, "FINAL")

	assert.Contains(t, args, "t1")
	assert.Contains(t, args, "e1")
	assert.Contains(t, args, "m1")
}

// TestBuildDailyUsageQuery_NoFinalWhenNotRequested proves UseFinal=false
// omits the FINAL clause, matching BuildFinalClause's existing behavior.
func TestBuildDailyUsageQuery_NoFinalWhenNotRequested(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	q, args := qb.BuildDailyUsageQuery(&events.DailyUsageParams{
		TenantID:      "t1",
		EnvironmentID: "e1",
		MeterIDs:      []string{"m1"},
		StartTime:     day("2026-09-01"),
		EndTime:       day("2026-10-01"),
		UseFinal:      false,
	})

	assert.NotContains(t, q, "FINAL")
	require.NotEmpty(t, args)
}

// TestBuildDailyUsageQuery_SumsQtyTotal proves the aggregation
// expression is SUM(qty_total) — the billable pre-materialized quantity —
// not a JSONExtract over properties.
func TestBuildDailyUsageQuery_SumsQtyTotal(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	q, _ := qb.BuildDailyUsageQuery(&events.DailyUsageParams{
		TenantID:      "t1",
		EnvironmentID: "e1",
		MeterIDs:      []string{"m1"},
		StartTime:     day("2026-09-01"),
		EndTime:       day("2026-10-01"),
	})

	assert.Contains(t, q, "SUM(qty_total)")
}

// TestBuildDailyUsageQuery_BoundsMaxMemoryUsage proves the query
// carries the same inline 90GB max_memory_usage bound as its sibling
// meter_usage.go queries (AGENTS.md: every ClickHouse query bounded by 90GB).
func TestBuildDailyUsageQuery_BoundsMaxMemoryUsage(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	q, _ := qb.BuildDailyUsageQuery(&events.DailyUsageParams{
		TenantID:      "t1",
		EnvironmentID: "e1",
		MeterIDs:      []string{"m1"},
		StartTime:     day("2026-09-01"),
		EndTime:       day("2026-10-01"),
		UseFinal:      true,
	})

	assert.Contains(t, q, "SETTINGS")
	assert.Contains(t, q, "max_memory_usage = 96636764160")
	assert.Contains(t, q, "do_not_merge_across_partitions_select_final = 1")
}

// TestBuildDailyUsageQuery_BatchesMeters proves several meters go out in one
// query, keyed by meter in the projection. One query per line item is what made
// a full rollup pass take hours.
func TestBuildDailyUsageQuery_BatchesMeters(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	q, args := qb.BuildDailyUsageQuery(&events.DailyUsageParams{
		TenantID:      "t1",
		EnvironmentID: "e1",
		MeterIDs:      []string{"m1", "m2", "m3"},
		StartTime:     day("2026-09-01"),
		EndTime:       day("2026-10-01"),
	})

	assert.Contains(t, q, "meter_id,")
	assert.Contains(t, q, "GROUP BY meter_id, day")
	for _, m := range []string{"m1", "m2", "m3"} {
		assert.Contains(t, args, m, "every meter must reach the query")
	}
}

// meterUsageColumns is the meter_usage column list, parsed from the migration
// itself rather than transcribed — a transcribed list drifts silently.
func meterUsageColumns(t *testing.T) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..",
		"migrations", "clickhouse", "000007_create_meter_usage.sql"))
	require.NoError(t, err)

	cols := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		m := regexp.MustCompile(`^\s{4}([a-z_]+)\s+\S`).FindStringSubmatch(line)
		if m != nil {
			cols[m[1]] = true
		}
	}
	require.NotEmpty(t, cols, "failed to parse columns from the migration")
	return cols
}

// TestBuildUsageActivityQuery_SelectsRealColumns runs the actual generated SQL
// against the migration's columns. MeterUsage embeds an Event struct carrying
// fields with no column behind them — CustomerID among them — so a query naming
// one compiles, passes against an in-memory store, and fails only in production
// with "Unknown expression identifier". That is exactly what shipped once.
func TestBuildUsageActivityQuery_SelectsRealColumns(t *testing.T) {
	cols := meterUsageColumns(t)
	require.False(t, cols["customer_id"],
		"guard: meter_usage must not have an internal customer_id column")

	qb := NewMeterUsageQueryBuilder()
	q, args := qb.BuildUsageActivityQuery(&events.UsageActivityParams{
		TenantID: "t1", EnvironmentID: "e1",
		IngestedAfter:  day("2026-09-23"),
		TimestampAfter: day("2026-06-25"),
	})

	// Every bare identifier the query names must be a real column.
	for _, ident := range regexp.MustCompile(`\b[a-z_]{3,}\b`).FindAllString(q, -1) {
		if cols[ident] {
			continue
		}
		switch ident {
		case "select", "distinct", "from", "where", "and", "settings",
			"meter_usage", "final", "max_memory_usage":
			continue
		}
		t.Fatalf("query names %q, which is not a meter_usage column", ident)
	}

	assert.Contains(t, q, "external_customer_id")
	assert.NotContains(t, q, "DISTINCT customer_id")
	// timestamp is bounded too, or the probe scans every partition ever written.
	assert.Contains(t, q, "timestamp >= ?")
	assert.Len(t, args, 4)
}
