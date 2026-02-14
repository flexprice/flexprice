package builder

import (
	"context"
	"fmt"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
)

type QueryBuilder struct {
	baseQuery    string
	filterQuery  string
	matchedQuery string
	finalQuery   string
	args         []interface{}
	filterGroups []events.FilterGroup
	params       *events.UsageParams
}

func NewQueryBuilder() *QueryBuilder {
	return &QueryBuilder{}
}

// getDeduplicationKey returns the columns used for deduplication
func (qb *QueryBuilder) getDeduplicationKey() string {
	return "tenant_id, environment_id, timestamp, id"
}

func (qb *QueryBuilder) WithBaseFilters(ctx context.Context, params *events.UsageParams) *QueryBuilder {
	conditions := []string{
		"event_name = ?",
	}
	qb.args = append(qb.args, params.EventName)

	tenantID := types.GetTenantID(ctx)
	if tenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		qb.args = append(qb.args, tenantID)
	}

	// Add environment_id filter if present in context
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		conditions = append(conditions, "environment_id = ?")
		qb.args = append(qb.args, environmentID)
	}

	timeConditions, timeArgs := parseTimeConditions(params)
	conditions = append(conditions, timeConditions...)
	qb.args = append(qb.args, timeArgs...)

	if params.ExternalCustomerID != "" {
		conditions = append(conditions, "external_customer_id = ?")
		qb.args = append(qb.args, params.ExternalCustomerID)
	}
	if params.CustomerID != "" {
		conditions = append(conditions, "customer_id = ?")
		qb.args = append(qb.args, params.CustomerID)
	}

	if params.Filters != nil {
		for property, values := range params.Filters {
			if len(values) > 0 {
				var condition string
				if len(values) == 1 {
					condition = "JSONExtractString(properties, ?) = ?"
					qb.args = append(qb.args, property, values[0])
				} else {
					quotedValues := make([]string, len(values))
					for i := range values {
						quotedValues[i] = "?"
					}
					condition = fmt.Sprintf(
						"JSONExtractString(properties, ?) IN (%s)",
						strings.Join(quotedValues, ","),
					)
					qb.args = append(qb.args, property)
					for _, v := range values {
						qb.args = append(qb.args, v)
					}
				}
				conditions = append(conditions, condition)
			}
		}
	}

	qb.params = params
	qb.baseQuery = fmt.Sprintf(`base_events AS (
			SELECT * FROM (
				SELECT DISTINCT ON (%s) * FROM events WHERE %s ORDER BY %s DESC
			)
		)`,
		qb.getDeduplicationKey(),
		strings.Join(conditions, " AND "),
		qb.getDeduplicationKey(),
	)

	return qb
}

func (qb *QueryBuilder) WithFilterGroups(ctx context.Context, groups []events.FilterGroup) *QueryBuilder {
	if len(groups) == 0 {
		return qb
	}

	var filterConditions []string
	for _, group := range groups {
		var conditions []string
		for property, values := range group.Filters {
			if len(values) == 0 {
				continue
			}
			var condition string
			if len(values) == 1 {
				condition = "JSONExtractString(properties, ?) = ?"
				qb.args = append(qb.args, property, values[0])
			} else {
				quotedValues := make([]string, len(values))
				for i := range values {
					quotedValues[i] = "?"
				}
				condition = fmt.Sprintf(
					"JSONExtractString(properties, ?) IN (%s)",
					strings.Join(quotedValues, ","),
				)
				qb.args = append(qb.args, property)
				for _, v := range values {
					qb.args = append(qb.args, v)
				}
			}
			conditions = append(conditions, condition)
		}

		// Only add the filter group if it has conditions
		if len(conditions) > 0 {
			filterConditions = append(filterConditions, fmt.Sprintf(
				"(?, %d, (%s))",
				group.Priority,
				strings.Join(conditions, " AND "),
			))
			qb.args = append(qb.args, group.ID)
		} else {
			// For empty filter groups, use a constant true condition
			filterConditions = append(filterConditions, fmt.Sprintf(
				"(?, %d, 1)",
				group.Priority,
			))
			qb.args = append(qb.args, group.ID)
		}
	}

	qb.filterQuery = fmt.Sprintf(`filter_matches AS (
		SELECT 
			id,
			timestamp,
			properties,
			arrayMap(x -> (
				x.1,
				x.2,
				x.3
			), [%s]) as group_matches
		FROM base_events
	)`, strings.Join(filterConditions, ",\n\t\t\t"))

	qb.matchedQuery = `matched_events AS (
		SELECT
			id,
			timestamp,
			properties,
			arrayJoin(group_matches) as matched_group,
			matched_group.1 as group_id,
			matched_group.2 as total_filters,
			matched_group.3 as matches
		FROM filter_matches
	),
	best_matches AS (
		SELECT
			id,
			properties,
			argMax(group_id, (total_filters, group_id)) as best_match_group
		FROM matched_events
		WHERE matches = 1
		GROUP BY id, properties
	)`

	qb.filterGroups = groups

	return qb
}

func (qb *QueryBuilder) WithAggregation(ctx context.Context, aggType types.AggregationType, propertyName string) *QueryBuilder {
	var aggClause string
	switch aggType {
	case types.AggregationCount:
		aggClause = "COUNT(*)"
	case types.AggregationSum:
		aggClause = "SUM(CAST(JSONExtractString(properties, ?) AS Float64))"
		qb.args = append(qb.args, propertyName)
	case types.AggregationAvg:
		aggClause = "AVG(CAST(JSONExtractString(properties, ?) AS Float64))"
		qb.args = append(qb.args, propertyName)
	case types.AggregationCountUnique:
		aggClause = "COUNT(DISTINCT JSONExtractString(properties, ?))"
		qb.args = append(qb.args, propertyName)
	}

	qb.finalQuery = fmt.Sprintf("SELECT best_match_group as filter_group_id, %s as value FROM best_matches GROUP BY best_match_group ORDER BY best_match_group", aggClause)

	return qb
}

func (qb *QueryBuilder) Build() (string, []interface{}) {
	var ctes []string

	// Add base query without WITH
	if qb.baseQuery != "" {
		ctes = append(ctes, strings.TrimPrefix(qb.baseQuery, "WITH "))
	}

	// Add filter query without WITH
	if qb.filterQuery != "" {
		ctes = append(ctes, strings.TrimPrefix(qb.filterQuery, "WITH "))
	}

	// Add matched query without WITH
	if qb.matchedQuery != "" {
		// Split the matched query into individual CTEs
		matchedCTEs := strings.Split(strings.TrimPrefix(qb.matchedQuery, "WITH "), ",")
		ctes = append(ctes, matchedCTEs...)
	}

	// Join CTEs with commas
	ctePart := strings.Join(ctes, ",\n")

	// Combine CTEs with final query
	query := fmt.Sprintf("WITH %s\n%s", ctePart, qb.finalQuery)

	return query, qb.args
}

func parseTimeConditions(params *events.UsageParams) ([]string, []interface{}) {
	var conditions []string
	var args []interface{}

	if !params.StartTime.IsZero() {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, params.StartTime)
	}

	if !params.EndTime.IsZero() {
		conditions = append(conditions, "timestamp < ?")
		args = append(args, params.EndTime)
	}

	return conditions, args
}

/*

---------Sample Query with Filter Groups---------------------------------------------

WITH base_events AS (
    SELECT
        id,
        timestamp,
        properties
    FROM events
    WHERE event_name = 'images_processed'
      AND tenant_id = '00000000-0000-0000-0000-000000000000'
      AND timestamp >= toDateTime64('2024-12-01 08:03:02.000', 3)
      AND timestamp < toDateTime64('2025-01-01 08:03:02.000', 3)
      AND external_customer_id = 'cus_loadtest_1'
      AND JSONExtractString(properties, 'image_size') IN ('512x512','768x768','1024x1024')
),
filter_matches AS (
    SELECT
        id,
        timestamp,
        properties,
        arrayMap(x -> (
            x.1,
            x.2,
            x.3
        ), [
            ('3759afd0-588d-4a15-a6d8-8278901ab610', 3, (JSONExtractString(properties, 'image_size') IN ('1024x1024'))),
            ('1f04fd3a-33ce-494c-9498-70e47de97fc5', 2, (JSONExtractString(properties, 'image_size') IN ('512x512'))),
            ('2715dab7-1045-4a3c-bad1-8d6d837ce491', 1, (JSONExtractString(properties, 'image_size') IN ('768x768')))
        ]) AS group_matches
    FROM base_events
),
matched_events AS (
    SELECT
        id,
        timestamp,
        properties,
        arrayJoin(group_matches) AS matched_group,
        matched_group.1 AS group_id,
        matched_group.2 AS total_filters,
        matched_group.3 AS matches
    FROM filter_matches
),
best_matches AS (
    SELECT
        id,
        properties,
        argMax(group_id, (total_filters, group_id)) AS best_match_group
    FROM matched_events
    WHERE matches = 1
    GROUP BY id, properties
)
SELECT
    best_match_group AS filter_group_id,
    COUNT(*) AS value
FROM best_matches
GROUP BY best_match_group
ORDER BY best_match_group;

*/
