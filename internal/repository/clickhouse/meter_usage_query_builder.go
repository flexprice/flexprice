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

// BucketedGroupByDim describes one group_by dimension supported by the bucketed
// query: "source" and "properties.X" only. Public so the repo's scan code can
// read the alias / property name without re-parsing the input.
type BucketedGroupByDim struct {
	// kind: "source" or "property"
	kind string
	// PropertyName: the JSON property name when kind=="property", "" when kind=="source"
	PropertyName string
	// sql is the column expression used inside the inner CTE
	sql string
	// alias is the column alias used in the SELECT and the outer GROUP BY/ORDER BY
	alias string
}

// IsSource reports whether this dim is the "source" axis.
func (d BucketedGroupByDim) IsSource() bool { return d.kind == "source" }

// IsProperty reports whether this dim is a property axis.
func (d BucketedGroupByDim) IsProperty() bool { return d.kind == "property" }

// Alias is the output column alias for the dim (used by the scanner).
func (d BucketedGroupByDim) Alias() string { return d.alias }

// bucketedGroupByDims parses the GroupBy list into the dimensions
// BuildBucketedQuery / BuildBucketedAggregateQuery know how to emit.
// feature_id / meter_id are silently dropped (implicit at the meter level);
// unknown entries and invalid property names are also dropped. Duplicate names
// are deduped. Empty input → nil → caller takes the non-grouped path.
func bucketedGroupByDims(groupBy []string) []BucketedGroupByDim {
	if len(groupBy) == 0 {
		return nil
	}
	out := make([]BucketedGroupByDim, 0, len(groupBy))
	seen := make(map[string]struct{}, len(groupBy))
	for _, g := range groupBy {
		switch {
		case g == "source":
			if _, ok := seen["source"]; ok {
				continue
			}
			seen["source"] = struct{}{}
			out = append(out, BucketedGroupByDim{
				kind:  "source",
				sql:   "source",
				alias: "grp_source",
			})
		case strings.HasPrefix(g, "properties."):
			propName := strings.TrimPrefix(g, "properties.")
			if propName == "" || !validMeterUsageGroupByPattern.MatchString(propName) {
				continue
			}
			key := "p:" + propName
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			alias := "prop_" + strings.ReplaceAll(propName, ".", "_")
			out = append(out, BucketedGroupByDim{
				kind:         "property",
				PropertyName: propName,
				sql:          fmt.Sprintf("JSONExtractString(properties, '%s')", propName),
				alias:        alias,
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

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
	windowExpr := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
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

	// Source filter
	if clause, addArgs := buildSourceFilterClause(params.Sources); clause != "" {
		conditions = append(conditions, clause)
		args = append(args, addArgs...)
	}

	// Property filters
	if clauses, addArgs := buildPropertyFilterClauses(params.PropertyFilters); len(clauses) > 0 {
		conditions = append(conditions, clauses...)
		args = append(args, addArgs...)
	}

	return strings.Join(conditions, " AND "), args
}

// buildSourceFilterClause builds the "source IN (?, ?, …)" fragment.
// Returns ("", nil) when sources is empty.
func buildSourceFilterClause(sources []string) (string, []interface{}) {
	if len(sources) == 0 {
		return "", nil
	}
	placeholders := make([]string, len(sources))
	args := make([]interface{}, 0, len(sources))
	for i, src := range sources {
		placeholders[i] = "?"
		args = append(args, src)
	}
	return fmt.Sprintf("source IN (%s)", strings.Join(placeholders, ", ")), args
}

// buildPropertyFilterClauses builds JSONExtractString filter fragments. First clause
// is "properties != ”" so the JSON extracts work; remaining clauses are per-property
// equality (one value) or IN (multi-value). Returns (nil, nil) for empty filters.
func buildPropertyFilterClauses(filters map[string][]string) ([]string, []interface{}) {
	if len(filters) == 0 {
		return nil, nil
	}
	clauses := make([]string, 0, len(filters))
	args := make([]interface{}, 0, len(filters))
	for property, values := range filters {
		if len(values) == 1 {
			clauses = append(clauses, "JSONExtractString(properties, ?) = ?")
			args = append(args, property, values[0])
		} else if len(values) > 1 {
			placeholders := make([]string, len(values))
			for i := range values {
				placeholders[i] = "?"
			}
			clauses = append(clauses, fmt.Sprintf("JSONExtractString(properties, ?) IN (%s)", strings.Join(placeholders, ",")))
			args = append(args, property)
			for _, v := range values {
				args = append(args, v)
			}
		}
	}
	return clauses, args
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
	bucketWindow := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
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

	// With GroupBy (billing per-KRN, source, properties.*): per-(bucket, dim
	// combo) rows. Same shape downstream consumers used to get from the legacy
	// group_key path — multiple Values per WindowSize, which
	// CalculateCostFromUsageResults prices independently and
	// aggregateUsageResultsByWindow rolls up. Analytics callers go through
	// BuildBucketedAggregateQuery instead for per-combo totals + separate
	// Points; this method is for billing and the legacy path.
	if dims := bucketedGroupByDims(params.GroupBy); len(dims) > 0 {
		selectExprs := make([]string, 0, len(dims))
		aliases := make([]string, 0, len(dims))
		for _, d := range dims {
			selectExprs = append(selectExprs, fmt.Sprintf("%s as %s", d.sql, d.alias))
			aliases = append(aliases, d.alias)
		}
		dimSelects := strings.Join(selectExprs, ",\n\t\t\t\t\t")
		dimGroupBy := strings.Join(aliases, ", ")

		query := fmt.Sprintf(`
			WITH per_group AS (
				SELECT
					%s as bucket_start,
					%s,
					%s(qty_total) as group_value,
					COUNT(DISTINCT id) as group_event_count
				FROM %s
				WHERE %s
				GROUP BY bucket_start, %s
			)
			SELECT
				(SELECT sum(group_value) FROM per_group) as total,
				(SELECT sum(group_event_count) FROM per_group) as total_event_count,
				bucket_start as timestamp,
				group_value as value,
				group_event_count as event_count
			FROM per_group
			ORDER BY bucket_start, %s
			%s
		`, bucketWindow, dimSelects, aggFunc, tableRef, where, dimGroupBy, dimGroupBy, settings)

		return query, args
	}

	// Without GroupBy: 2-level aggregation
	query := fmt.Sprintf(`
		WITH %s AS (
			SELECT
				%s as bucket_start,
				%s(qty_total) as %s,
				COUNT(DISTINCT id) as bucket_event_count
			FROM %s
			WHERE %s
			GROUP BY bucket_start
			ORDER BY bucket_start
		)
		SELECT
			(SELECT sum(%s) FROM %s) as total,
			(SELECT sum(bucket_event_count) FROM %s) as total_event_count,
			bucket_start as timestamp,
			%s as value,
			bucket_event_count as event_count
		FROM %s
		ORDER BY bucket_start
		%s
	`,
		bucketTableName,
		bucketWindow, aggFunc, bucketColumnName,
		tableRef, where,
		bucketColumnName, bucketTableName,
		bucketTableName,
		bucketColumnName,
		bucketTableName,
		settings)

	return query, args
}

// BuildBucketedAggregateQuery returns a per-combo aggregate query for the
// bucketed-meter analytics path with GroupBy (subset of {"source",
// "properties.X"}). Two-level CTE — inner per-(bucket, combo), outer per-combo —
// so ClickHouse does the bucket→total roll-up and we ship one row per combo
// instead of bucket × combo rows. Mirrors feature_usage's getMaxBucketTotals.
//
// Output columns (in order):
//
//	<dim aliases (one per GroupBy entry)>, group_total, group_event_count
//
// Returns ("", nil) when params.GroupBy yields no valid dims — caller should
// fall back to BuildBucketedQuery in that case.
func (qb *MeterUsageQueryBuilder) BuildBucketedAggregateQuery(params *events.MeterUsageQueryParams) (string, []interface{}, []BucketedGroupByDim) {
	dims := bucketedGroupByDims(params.GroupBy)
	if len(dims) == 0 {
		return "", nil, nil
	}

	bucketWindow := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
	where, args := qb.BuildWhereClause(params)
	finalClause, settings := qb.BuildFinalClause(params.UseFinal)

	aggFunc := "MAX"
	if params.AggregationType == types.AggregationSum {
		aggFunc = "SUM"
	}
	tableRef := "meter_usage"
	if finalClause != "" {
		tableRef = "meter_usage " + finalClause
	}

	selectExprs := make([]string, 0, len(dims))
	aliases := make([]string, 0, len(dims))
	for _, d := range dims {
		selectExprs = append(selectExprs, fmt.Sprintf("%s as %s", d.sql, d.alias))
		aliases = append(aliases, d.alias)
	}
	dimSelects := strings.Join(selectExprs, ",\n\t\t\t\t\t")
	dimAliases := strings.Join(aliases, ", ")

	query := fmt.Sprintf(`
		WITH per_group_bucket AS (
			SELECT
				%s as bucket_start,
				%s,
				%s(qty_total) as bucket_value,
				COUNT(DISTINCT id) as bucket_event_count
			FROM %s
			WHERE %s
			GROUP BY bucket_start, %s
		)
		SELECT
			%s,
			sum(bucket_value) as group_total,
			sum(bucket_event_count) as group_event_count
		FROM per_group_bucket
		GROUP BY %s
		ORDER BY %s
		%s
	`,
		bucketWindow, dimSelects, aggFunc, tableRef, where, dimAliases,
		dimAliases, dimAliases, dimAliases, settings)

	return query, args, dims
}

// BuildBucketedPointsQuery returns a per-bucket time-series query narrowed to
// a single (source, properties) combo. Mirrors feature_usage's
// getAnalyticsPoints — invoked once per row returned by
// BuildBucketedAggregateQuery to populate that result's Points slice.
//
// comboSource is "" when "source" is not in GroupBy; comboProps maps each
// property dim's name → its concrete value for this combo (empty map allowed).
//
// Output columns: bucket_start, bucket_value, bucket_event_count.
func (qb *MeterUsageQueryBuilder) BuildBucketedPointsQuery(
	params *events.MeterUsageQueryParams,
	dims []BucketedGroupByDim,
	comboSource string,
	comboProps map[string]string,
) (string, []interface{}) {
	bucketWindow := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
	where, args := qb.BuildWhereClause(params)
	finalClause, settings := qb.BuildFinalClause(params.UseFinal)

	aggFunc := "MAX"
	if params.AggregationType == types.AggregationSum {
		aggFunc = "SUM"
	}
	tableRef := "meter_usage"
	if finalClause != "" {
		tableRef = "meter_usage " + finalClause
	}

	// Append combo equality predicates so this query returns only the rows
	// that contributed to one specific aggregate row.
	extraWhere := where
	for _, d := range dims {
		if d.IsSource() {
			extraWhere += " AND source = ?"
			args = append(args, comboSource)
		} else if d.IsProperty() {
			extraWhere += " AND JSONExtractString(properties, ?) = ?"
			args = append(args, d.PropertyName, comboProps[d.PropertyName])
		}
	}

	query := fmt.Sprintf(`
		SELECT
			%s as bucket_start,
			%s(qty_total) as bucket_value,
			COUNT(DISTINCT id) as bucket_event_count
		FROM %s
		WHERE %s
		GROUP BY bucket_start
		ORDER BY bucket_start
		%s
	`, bucketWindow, aggFunc, tableRef, extraWhere, settings)

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
	if clause, addArgs := buildSourceFilterClause(params.Sources); clause != "" {
		conditions = append(conditions, clause)
		args = append(args, addArgs...)
	}

	// Property filters
	if clauses, addArgs := buildPropertyFilterClauses(params.PropertyFilters); len(clauses) > 0 {
		conditions = append(conditions, clauses...)
		args = append(args, addArgs...)
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
	windowExpr := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, "")
	if windowExpr == "" {
		return "", nil
	}

	aggColumns := buildMeterUsageAggregationColumns(params.AggregationTypes)
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
