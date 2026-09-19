package clickhouse

import (
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

// TestBuildCumulativeDailyUsageQuery_GroupsByDayWithRLS proves the new,
// additive builder method groups by day, scopes by tenant/environment/meter,
// and honors UseFinal — mirroring BuildDetailedWhereClause / BuildFinalClause
// used elsewhere in this file, without touching any existing method.
func TestBuildCumulativeDailyUsageQuery_GroupsByDayWithRLS(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	q, args := qb.BuildCumulativeDailyUsageQuery(&events.CumulativeDailyUsageParams{
		TenantID:      "t1",
		EnvironmentID: "e1",
		MeterID:       "m1",
		StartTime:     day("2026-09-01"),
		EndTime:       day("2026-10-01"),
		UseFinal:      true,
	})

	assert.Contains(t, q, "toStartOfDay(timestamp")
	assert.Contains(t, q, "tenant_id = ?")
	assert.Contains(t, q, "environment_id = ?")
	assert.Contains(t, q, "GROUP BY day")
	assert.Contains(t, q, "ORDER BY day ASC")
	assert.Contains(t, q, "FINAL")

	assert.Contains(t, args, "t1")
	assert.Contains(t, args, "e1")
	assert.Contains(t, args, "m1")
}

// TestBuildCumulativeDailyUsageQuery_NoFinalWhenNotRequested proves UseFinal=false
// omits the FINAL clause, matching BuildFinalClause's existing behavior.
func TestBuildCumulativeDailyUsageQuery_NoFinalWhenNotRequested(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	q, args := qb.BuildCumulativeDailyUsageQuery(&events.CumulativeDailyUsageParams{
		TenantID:      "t1",
		EnvironmentID: "e1",
		MeterID:       "m1",
		StartTime:     day("2026-09-01"),
		EndTime:       day("2026-10-01"),
		UseFinal:      false,
	})

	assert.NotContains(t, q, "FINAL")
	require.NotEmpty(t, args)
}

// TestBuildCumulativeDailyUsageQuery_SumsQtyTotal proves the aggregation
// expression is SUM(qty_total) — the billable pre-materialized quantity —
// not a JSONExtract over properties.
func TestBuildCumulativeDailyUsageQuery_SumsQtyTotal(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	q, _ := qb.BuildCumulativeDailyUsageQuery(&events.CumulativeDailyUsageParams{
		TenantID:      "t1",
		EnvironmentID: "e1",
		MeterID:       "m1",
		StartTime:     day("2026-09-01"),
		EndTime:       day("2026-10-01"),
	})

	assert.Contains(t, q, "SUM(qty_total)")
}

// TestBuildCumulativeDailyUsageQuery_BoundsMaxMemoryUsage proves the query
// carries the same inline 90GB max_memory_usage bound as its sibling
// meter_usage.go queries (AGENTS.md: every ClickHouse query bounded by 90GB).
func TestBuildCumulativeDailyUsageQuery_BoundsMaxMemoryUsage(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	q, _ := qb.BuildCumulativeDailyUsageQuery(&events.CumulativeDailyUsageParams{
		TenantID:      "t1",
		EnvironmentID: "e1",
		MeterID:       "m1",
		StartTime:     day("2026-09-01"),
		EndTime:       day("2026-10-01"),
		UseFinal:      true,
	})

	assert.Contains(t, q, "SETTINGS")
	assert.Contains(t, q, "max_memory_usage = 96636764160")
	assert.Contains(t, q, "do_not_merge_across_partitions_select_final = 1")
}
