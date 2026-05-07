package clickhouse

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
)

// validMeterUsageGroupByPattern matches safe property names (alphanumeric, underscores, dots).
var validMeterUsageGroupByPattern = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)

// MeterUsageQueryBuilder constructs SQL queries for the meter_usage table.
// It encapsulates WHERE clause construction, FINAL handling, and window grouping
// so that aggregators only need to specify their aggregation expression.
type MeterUsageQueryBuilder struct{}

// NewMeterUsageQueryBuilder creates a new query builder
func NewMeterUsageQueryBuilder() *MeterUsageQueryBuilder {
	return &MeterUsageQueryBuilder{}
}

// BuildQuery constructs a complete single-meter query with optional windowing.
// The aggregator provides aggExpr (e.g. "SUM(qty_total)") and countExpr (e.g. "COUNT(DISTINCT id)").
func (qb *MeterUsageQueryBuilder) BuildQuery(aggExpr, countExpr string, params *events.MeterUsageQueryParams) string {
	windowExpr := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
	where, _ := qb.BuildWhereClause(params)
	finalClause, settings := qb.BuildFinalClause(params.UseFinal)

	if windowExpr != "" {
		return fmt.Sprintf(`
			SELECT
				%s AS window_start,
				%s AS value,
				%s AS event_count
			FROM meter_usage %s
			WHERE %s
			GROUP BY window_start
			ORDER BY window_start ASC
			%s
		`, windowExpr, aggExpr, countExpr, finalClause, where, settings)
	}

	return fmt.Sprintf(`
		SELECT
			%s AS value,
			%s AS event_count
		FROM meter_usage %s
		WHERE %s
		%s
	`, aggExpr, countExpr, finalClause, where, settings)
}

// BuildWhereClause constructs the WHERE conditions and parameterized args
func (qb *MeterUsageQueryBuilder) BuildWhereClause(params *events.MeterUsageQueryParams) (string, []interface{}) {
	conditions := make([]string, 0, 8)
	args := make([]interface{}, 0, 8)

	// Tenant scope (always required)
	conditions = append(conditions, "tenant_id = ?")
	args = append(args, params.TenantID)

	conditions = append(conditions, "environment_id = ?")
	args = append(args, params.EnvironmentID)

	// Customer filter (single or multi)
	if params.ExternalCustomerID != "" {
		conditions = append(conditions, "external_customer_id = ?")
		args = append(args, params.ExternalCustomerID)
	} else if len(params.ExternalCustomerIDs) > 0 {
		placeholders := make([]string, len(params.ExternalCustomerIDs))
		for i, id := range params.ExternalCustomerIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		conditions = append(conditions, fmt.Sprintf("external_customer_id IN (%s)", strings.Join(placeholders, ", ")))
	}

	// Meter filter (single or multi)
	if params.MeterID != "" {
		conditions = append(conditions, "meter_id = ?")
		args = append(args, params.MeterID)
	} else if len(params.MeterIDs) > 0 {
		placeholders := make([]string, len(params.MeterIDs))
		for i, id := range params.MeterIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		conditions = append(conditions, fmt.Sprintf("meter_id IN (%s)", strings.Join(placeholders, ", ")))
	}

	// Time range
	if !params.StartTime.IsZero() {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, params.StartTime.UTC())
	}
	if !params.EndTime.IsZero() {
		conditions = append(conditions, "timestamp < ?")
		args = append(args, params.EndTime.UTC())
	}

	// COUNT_UNIQUE requires non-empty unique_hash
	if params.AggregationType == types.AggregationCountUnique {
		conditions = append(conditions, "unique_hash != ''")
	}

	return strings.Join(conditions, " AND "), args
}

// BuildFinalClause returns FINAL keyword and SETTINGS for ReplacingMergeTree dedup
func (qb *MeterUsageQueryBuilder) BuildFinalClause(useFinal bool) (finalClause string, settings string) {
	if useFinal {
		return "FINAL", "SETTINGS do_not_merge_across_partitions_select_final = 1"
	}
	return "", ""
}

// BuildBucketedQuery constructs a windowed aggregation query for bucketed meters (MAX/SUM with bucket_size).
// Mirrors the feature_usage getWindowedQuery logic but operates on the meter_usage table.
//
// With GroupBy (MAX meters with group_by pricing): 3-level CTE
//  1. per_group: aggregate per group per bucket (e.g. MAX per krn per hour)
//  2. Outer: return (total, bucket_start, value, group_key)
//
// Without GroupBy (SUM meters): 2-level CTE
//  1. bucket_aggs: aggregate per bucket
//  2. Outer: return (total, bucket_start, value)
func (qb *MeterUsageQueryBuilder) BuildBucketedQuery(params *events.MeterUsageQueryParams) (string, []interface{}) {
	bucketWindow := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
	where, args := qb.BuildWhereClause(params)
	finalClause, settings := qb.BuildFinalClause(params.UseFinal)

	// Determine aggregation function based on type (default MAX for backward compat)
	aggFunc := "MAX"
	bucketTableName := "bucket_maxes"
	bucketColumnName := "bucket_max"
	if params.AggregationType == types.AggregationSum {
		aggFunc = "SUM"
		bucketTableName = "bucket_sums"
		bucketColumnName = "bucket_sum"
	}

	tableRef := "meter_usage"
	if finalClause != "" {
		tableRef = "meter_usage " + finalClause
	}

	// With GroupBy: 3-level aggregation
	if params.GroupByProperty != "" && validMeterUsageGroupByPattern.MatchString(params.GroupByProperty) {
		groupByExpr := fmt.Sprintf("JSONExtractString(properties, '%s')", params.GroupByProperty)

		query := fmt.Sprintf(`
			WITH per_group AS (
				SELECT
					%s as bucket_start,
					%s as group_key,
					%s(qty_total) as group_value
				FROM %s
				WHERE %s
				GROUP BY bucket_start, group_key
			)
			SELECT
				(SELECT sum(group_value) FROM per_group) as total,
				bucket_start as timestamp,
				group_value as value,
				group_key
			FROM per_group
			ORDER BY bucket_start, group_key
			%s
		`, bucketWindow, groupByExpr, aggFunc, tableRef, where, settings)

		return query, args
	}

	// Without GroupBy: 2-level aggregation
	query := fmt.Sprintf(`
		WITH %s AS (
			SELECT
				%s as bucket_start,
				%s(qty_total) as %s
			FROM %s
			WHERE %s
			GROUP BY bucket_start
			ORDER BY bucket_start
		)
		SELECT
			(SELECT sum(%s) FROM %s) as total,
			bucket_start as timestamp,
			%s as value
		FROM %s
		ORDER BY bucket_start
		%s
	`,
		bucketTableName,
		bucketWindow, aggFunc, bucketColumnName,
		tableRef, where,
		bucketColumnName, bucketTableName,
		bucketColumnName,
		bucketTableName,
		settings)

	return query, args
}
