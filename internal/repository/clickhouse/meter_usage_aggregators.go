package clickhouse

// meter_usage_aggregators.go
//
// Query builder functions for the meter_usage ClickHouse table.
//
// Unlike the raw events aggregators (aggregators.go), these functions operate on
// pre-enriched rows that already contain:
//   - meter_id  — no event_name filter or JOIN required
//   - qty_total — the raw per-event value (no JSONExtract needed at read time)
//   - unique_hash — pre-extracted property value for COUNT_UNIQUE meters
//
// All queries use FINAL to account for ReplacingMergeTree deduplication.
// The timestamp column is DateTime (not DateTime64), so use toDateTime() for literals.
//
// Each function returns a plain SQL string following the same fmt.Sprintf interpolation
// pattern used in aggregators.go. These are intended for review before wiring into
// the billing path as a replacement for the raw events aggregators.

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// MeterUsageQueryParams carries the parameters needed to build a meter_usage query.
type MeterUsageQueryParams struct {
	TenantID           string
	EnvironmentID      string
	ExternalCustomerID string
	MeterID            string
	StartTime          time.Time
	EndTime            time.Time

	// WindowSize groups results by time bucket (DAY, HOUR, MONTH, etc.).
	// When empty, a single scalar total is returned.
	WindowSize types.WindowSize

	// BucketSize is used for windowed MAX/SUM (e.g. peak-per-day then sum of peaks).
	// Distinct from WindowSize: BucketSize controls the inner aggregation window.
	BucketSize types.WindowSize

	// BillingAnchor shifts MONTH windows to custom billing periods (e.g. 5th→5th).
	BillingAnchor *time.Time

	// PeriodStart / PeriodEnd are used by WEIGHTED_SUM to compute the weight denominator.
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// formatMeterUsageDateTime formats a time.Time for use in meter_usage queries.
// The timestamp column is DateTime (second precision), so we truncate to seconds.
func formatMeterUsageDateTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// buildMeterUsageWhere returns the common WHERE predicate shared by all query types.
func buildMeterUsageWhere(p *MeterUsageQueryParams) string {
	return fmt.Sprintf(
		`WHERE tenant_id = '%s'
  AND environment_id = '%s'
  AND external_customer_id = '%s'
  AND meter_id = '%s'
  AND timestamp >= toDateTime('%s')
  AND timestamp < toDateTime('%s')`,
		p.TenantID,
		p.EnvironmentID,
		p.ExternalCustomerID,
		p.MeterID,
		formatMeterUsageDateTime(p.StartTime),
		formatMeterUsageDateTime(p.EndTime),
	)
}

// buildMeterUsageGroupBy returns a GROUP BY clause when WindowSize is set, or empty string.
func buildMeterUsageGroupBy(p *MeterUsageQueryParams) string {
	ws := formatWindowSizeWithBillingAnchor(p.WindowSize, p.BillingAnchor)
	if ws == "" {
		return ""
	}
	return fmt.Sprintf("GROUP BY %s AS window_size", ws)
}

// buildMeterUsageSelectWindow returns the window column in the SELECT list, or empty string.
func buildMeterUsageSelectWindow(p *MeterUsageQueryParams) string {
	ws := formatWindowSizeWithBillingAnchor(p.WindowSize, p.BillingAnchor)
	if ws == "" {
		return ""
	}
	return fmt.Sprintf("%s AS window_size,", ws)
}

// ---------------------------------------------------------------------------
// COUNT
//
// qty_total is set to 1.0 for every COUNT event at write time.
// Billing query: sum(qty_total) == count of events.
// ---------------------------------------------------------------------------

func GetMeterUsageCountQuery(p *MeterUsageQueryParams) string {
	return fmt.Sprintf(`
SELECT
    %s sum(qty_total) AS total
FROM flexprice.meter_usage FINAL
%s
%s
SETTINGS do_not_merge_across_partitions_select_final = 1`,
		buildMeterUsageSelectWindow(p),
		buildMeterUsageWhere(p),
		buildMeterUsageGroupBy(p),
	)
}

// ---------------------------------------------------------------------------
// SUM
//
// qty_total holds the extracted property value at write time.
// Billing query: sum(qty_total).
// ---------------------------------------------------------------------------

func GetMeterUsageSumQuery(p *MeterUsageQueryParams) string {
	return fmt.Sprintf(`
SELECT
    %s sum(qty_total) AS total
FROM flexprice.meter_usage FINAL
%s
%s
SETTINGS do_not_merge_across_partitions_select_final = 1`,
		buildMeterUsageSelectWindow(p),
		buildMeterUsageWhere(p),
		buildMeterUsageGroupBy(p),
	)
}

// ---------------------------------------------------------------------------
// SUM_WITH_MULTIPLIER
//
// The multiplier is applied at write time (qty_total = property_value × multiplier).
// Billing query is identical to SUM — no additional multiplication needed here.
// ---------------------------------------------------------------------------

func GetMeterUsageSumWithMultiplierQuery(p *MeterUsageQueryParams) string {
	return GetMeterUsageSumQuery(p)
}

// ---------------------------------------------------------------------------
// AVG
//
// qty_total holds the raw per-event value. avg(qty_total) computes the mean.
// ---------------------------------------------------------------------------

func GetMeterUsageAvgQuery(p *MeterUsageQueryParams) string {
	return fmt.Sprintf(`
SELECT
    %s avg(qty_total) AS total
FROM flexprice.meter_usage FINAL
%s
%s
SETTINGS do_not_merge_across_partitions_select_final = 1`,
		buildMeterUsageSelectWindow(p),
		buildMeterUsageWhere(p),
		buildMeterUsageGroupBy(p),
	)
}

// ---------------------------------------------------------------------------
// MAX (non-windowed)
//
// Simple max across all rows in the time range.
// ---------------------------------------------------------------------------

func GetMeterUsageMaxQuery(p *MeterUsageQueryParams) string {
	return fmt.Sprintf(`
SELECT
    %s max(qty_total) AS total
FROM flexprice.meter_usage FINAL
%s
%s
SETTINGS do_not_merge_across_partitions_select_final = 1`,
		buildMeterUsageSelectWindow(p),
		buildMeterUsageWhere(p),
		buildMeterUsageGroupBy(p),
	)
}

// ---------------------------------------------------------------------------
// MAX (windowed / bucketed)
//
// Computes the max qty_total within each bucket (BucketSize), then sums those
// bucket maxes to produce the total. This matches the "peak per period" billing
// model (e.g. "charge for peak usage in each day, billed as monthly sum of peaks").
// ---------------------------------------------------------------------------

func GetMeterUsageMaxWindowedQuery(p *MeterUsageQueryParams) string {
	bucketExpr := formatWindowSizeWithBillingAnchor(p.BucketSize, p.BillingAnchor)

	return fmt.Sprintf(`
WITH bucket_maxes AS (
    SELECT
        %s AS bucket_start,
        max(qty_total) AS bucket_max
    FROM flexprice.meter_usage FINAL
    %s
    GROUP BY bucket_start
    ORDER BY bucket_start
)
SELECT
    (SELECT sum(bucket_max) FROM bucket_maxes) AS total,
    bucket_start AS timestamp,
    bucket_max AS value
FROM bucket_maxes
ORDER BY bucket_start
SETTINGS do_not_merge_across_partitions_select_final = 1`,
		bucketExpr,
		buildMeterUsageWhere(p),
	)
}

// ---------------------------------------------------------------------------
// COUNT_UNIQUE
//
// unique_hash is set to the property value at write time for COUNT_UNIQUE meters.
// uniqExact gives exact distinct count (HyperLogLog-free, safe for billing).
// The unique_hash != '' guard skips rows from non-COUNT_UNIQUE meters that share
// the same meter_id (defensive; shouldn't happen in practice).
// ---------------------------------------------------------------------------

func GetMeterUsageCountUniqueQuery(p *MeterUsageQueryParams) string {
	return fmt.Sprintf(`
SELECT
    %s uniqExact(unique_hash) AS total
FROM flexprice.meter_usage FINAL
%s
  AND unique_hash != ''
%s
SETTINGS do_not_merge_across_partitions_select_final = 1`,
		buildMeterUsageSelectWindow(p),
		buildMeterUsageWhere(p),
		buildMeterUsageGroupBy(p),
	)
}

// ---------------------------------------------------------------------------
// LATEST
//
// Returns the qty_total of the most recent event (by timestamp) in the range.
// argMax is used to pick the value associated with the maximum timestamp.
// ---------------------------------------------------------------------------

func GetMeterUsageLatestQuery(p *MeterUsageQueryParams) string {
	return fmt.Sprintf(`
SELECT
    %s argMax(qty_total, timestamp) AS total
FROM flexprice.meter_usage FINAL
%s
%s
SETTINGS do_not_merge_across_partitions_select_final = 1`,
		buildMeterUsageSelectWindow(p),
		buildMeterUsageWhere(p),
		buildMeterUsageGroupBy(p),
	)
}

// ---------------------------------------------------------------------------
// WEIGHTED_SUM
//
// qty_total holds the raw property value at write time.
// The weight is: proportion of the billing period remaining after this event.
// Formula per row: (qty_total / total_seconds) * dateDiff('second', timestamp, period_end)
// Sum across all rows gives the time-weighted total.
//
// PeriodStart and PeriodEnd must be set in MeterUsageQueryParams.
// ---------------------------------------------------------------------------

func GetMeterUsageWeightedSumQuery(p *MeterUsageQueryParams) string {
	return fmt.Sprintf(`
WITH
    toDateTime('%s') AS period_start,
    toDateTime('%s') AS period_end,
    dateDiff('second', period_start, period_end) AS total_seconds
SELECT
    %s sum(
        (qty_total / nullIf(toFloat64(total_seconds), 0)) *
        dateDiff('second', timestamp, period_end)
    ) AS total
FROM flexprice.meter_usage FINAL
%s
%s
SETTINGS do_not_merge_across_partitions_select_final = 1`,
		formatMeterUsageDateTime(p.PeriodStart),
		formatMeterUsageDateTime(p.PeriodEnd),
		buildMeterUsageSelectWindow(p),
		buildMeterUsageWhere(p),
		buildMeterUsageGroupBy(p),
	)
}
