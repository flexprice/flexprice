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

// BuildDetailedWhereClause constructs WHERE conditions for detailed analytics queries.
// Extends the base where clause with source filtering and property filters.
func (qb *MeterUsageQueryBuilder) BuildDetailedWhereClause(params *events.MeterUsageDetailedAnalyticsParams) (string, []interface{}) {
	conditions := make([]string, 0, 10)
	args := make([]interface{}, 0, 10)

	// Tenant scope
	conditions = append(conditions, "tenant_id = ?")
	args = append(args, params.TenantID)

	conditions = append(conditions, "environment_id = ?")
	args = append(args, params.EnvironmentID)

	// Customer filter
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

	// Meter filter
	if len(params.MeterIDs) > 0 {
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

	// Source filter
	if len(params.Sources) > 0 {
		placeholders := make([]string, len(params.Sources))
		for i, src := range params.Sources {
			placeholders[i] = "?"
			args = append(args, src)
		}
		conditions = append(conditions, fmt.Sprintf("source IN (%s)", strings.Join(placeholders, ", ")))
	}

	// Property filters — guarded with properties != '' for tenants with empty properties
	if len(params.PropertyFilters) > 0 {
		conditions = append(conditions, "properties != ''")
		for property, values := range params.PropertyFilters {
			if len(values) == 1 {
				conditions = append(conditions, "JSONExtractString(properties, ?) = ?")
				args = append(args, property, values[0])
			} else if len(values) > 1 {
				placeholders := make([]string, len(values))
				for i := range values {
					placeholders[i] = "?"
				}
				conditions = append(conditions, fmt.Sprintf("JSONExtractString(properties, ?) IN (%s)", strings.Join(placeholders, ",")))
				args = append(args, property)
				for _, v := range values {
					args = append(args, v)
				}
			}
		}
	}

	return strings.Join(conditions, " AND "), args
}

// DetailedGroupByResult holds the parsed group-by configuration for detailed analytics queries.
type DetailedGroupByResult struct {
	// Columns are the raw SQL expressions used in the GROUP BY clause
	Columns []string
	// Aliases are the SELECT expressions (with AS aliases for properties)
	Aliases []string
	// FieldMapping maps the original group-by field name to its column alias
	FieldMapping map[string]string
}

// BuildDetailedGroupByColumns parses and validates group-by fields, returning SQL columns and aliases.
func (qb *MeterUsageQueryBuilder) BuildDetailedGroupByColumns(params *events.MeterUsageDetailedAnalyticsParams) (*DetailedGroupByResult, error) {
	result := &DetailedGroupByResult{
		Columns:      make([]string, 0, len(params.GroupBy)),
		Aliases:      make([]string, 0, len(params.GroupBy)),
		FieldMapping: make(map[string]string),
	}

	for _, groupBy := range params.GroupBy {
		switch {
		case groupBy == "meter_id":
			result.Columns = append(result.Columns, "meter_id")
			result.Aliases = append(result.Aliases, "meter_id")
			result.FieldMapping["meter_id"] = "meter_id"
		case groupBy == "source":
			result.Columns = append(result.Columns, "source")
			result.Aliases = append(result.Aliases, "source")
			result.FieldMapping["source"] = "source"
		case strings.HasPrefix(groupBy, "properties."):
			propertyName := strings.TrimPrefix(groupBy, "properties.")
			if propertyName == "" || !validMeterUsageGroupByPattern.MatchString(propertyName) {
				return nil, fmt.Errorf("invalid property name in group_by: %s", groupBy)
			}
			alias := "prop_" + strings.ReplaceAll(propertyName, ".", "_")
			jsonExpr := fmt.Sprintf("JSONExtractString(properties, '%s')", propertyName)
			result.Columns = append(result.Columns, jsonExpr)
			result.Aliases = append(result.Aliases, fmt.Sprintf("%s AS %s", jsonExpr, alias))
			result.FieldMapping[groupBy] = alias
		default:
			return nil, fmt.Errorf("invalid group_by value: %s (allowed: meter_id, source, properties.<field>)", groupBy)
		}
	}

	return result, nil
}

// BuildDetailedPointsQuery constructs a time-series sub-query for a single group's analytics points.
func (qb *MeterUsageQueryBuilder) BuildDetailedPointsQuery(
	params *events.MeterUsageDetailedAnalyticsParams,
	result *events.MeterUsageDetailedResult,
	groupByResult *DetailedGroupByResult,
) (string, []interface{}) {
	windowExpr := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
	if windowExpr == "" {
		return "", nil
	}

	aggColumns := buildConditionalAggregationColumns(params.AggregationTypes)
	finalClause, settings := qb.BuildFinalClause(params.UseFinal)

	// Start with the base WHERE from the detailed params
	where, args := qb.BuildDetailedWhereClause(params)

	// Narrow to this specific group
	if result.MeterID != "" {
		where += " AND meter_id = ?"
		args = append(args, result.MeterID)
	}
	if result.Source != "" {
		where += " AND source = ?"
		args = append(args, result.Source)
	}
	for propName, propValue := range result.Properties {
		if propValue != "" {
			where += " AND JSONExtractString(properties, ?) = ?"
			args = append(args, propName, propValue)
		}
	}

	selectCols := append([]string{fmt.Sprintf("%s AS window_start", windowExpr)}, aggColumns...)

	query := fmt.Sprintf(`
		SELECT
			%s
		FROM meter_usage %s
		WHERE %s
		GROUP BY window_start
		ORDER BY window_start ASC
		%s
	`, strings.Join(selectCols, ",\n\t\t\t"), finalClause, where, settings)

	return query, args
}
