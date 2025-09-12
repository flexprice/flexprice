package builder

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
)

type ParameterizedQuery struct {
	Query string
	Args  []interface{}
}

type CTEComponent struct {
	Name  string
	Query string
	Args  []interface{}
}

type QueryBuilder struct {
	baseQuery     *ParameterizedQuery
	filterQuery   *ParameterizedQuery
	matchedQuery  *ParameterizedQuery
	finalQuery    *ParameterizedQuery
	cteComponents []CTEComponent
	params        *events.UsageParams
}

func NewQueryBuilder() *QueryBuilder {
	return &QueryBuilder{
		baseQuery:     &ParameterizedQuery{},
		filterQuery:   &ParameterizedQuery{},
		matchedQuery:  &ParameterizedQuery{},
		finalQuery:    &ParameterizedQuery{},
		cteComponents: make([]CTEComponent, 0),
	}
}

// getDeduplicationKey returns the columns used for deduplication
func (qb *QueryBuilder) getDeduplicationKey() string {
	return "tenant_id, environment_id, timestamp, id"
}

func (qb *QueryBuilder) WithBaseFilters(ctx context.Context, params *events.UsageParams) *QueryBuilder {
	var conditions []string
	var args []interface{}
	argIndex := 1

	// Event name parameter
	conditions = append(conditions, fmt.Sprintf("event_name = ?%d", argIndex))
	args = append(args, params.EventName)
	argIndex++

	// Tenant ID parameter
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" {
		conditions = append(conditions, fmt.Sprintf("tenant_id = ?%d", argIndex))
		args = append(args, tenantID)
		argIndex++
	}

	// Environment ID parameter
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		conditions = append(conditions, fmt.Sprintf("environment_id = ?%d", argIndex))
		args = append(args, environmentID)
		argIndex++
	}

	// Time conditions (now parameterized with proper indexing)
	timeConditions, timeArgs, newArgIndex := qb.parseTimeConditionsWithIndex(params, argIndex)
	conditions = append(conditions, timeConditions...)
	args = append(args, timeArgs...)
	argIndex = newArgIndex

	// Customer ID parameters
	if params.ExternalCustomerID != "" {
		conditions = append(conditions, fmt.Sprintf("external_customer_id = ?%d", argIndex))
		args = append(args, params.ExternalCustomerID)
		argIndex++
	}
	if params.CustomerID != "" {
		conditions = append(conditions, fmt.Sprintf("customer_id = ?%d", argIndex))
		args = append(args, params.CustomerID)
		argIndex++
	}

	// Filter parameters
	if params.Filters != nil {
		for property, values := range params.Filters {
			if len(values) > 0 {
				var condition string
				if len(values) == 1 {
					condition = fmt.Sprintf("JSONExtractString(properties, ?%d) = ?%d", argIndex, argIndex+1)
					args = append(args, property, values[0])
					argIndex += 2
				} else {
					placeholders := make([]string, len(values))
					for i := range values {
						placeholders[i] = fmt.Sprintf("?%d", argIndex+i+1)
						args = append(args, values[i])
					}
					condition = fmt.Sprintf(
						"JSONExtractString(properties, ?%d) IN (%s)",
						argIndex,
						strings.Join(placeholders, ","),
					)
					args = append(args, property)
					argIndex += len(values) + 1
				}
				conditions = append(conditions, condition)
			}
		}
	}

	qb.params = params

	whereClause := strings.Join(conditions, " AND ")
	query := fmt.Sprintf(`base_events AS (
			SELECT * FROM (
				SELECT DISTINCT ON (%s) * FROM events WHERE %s ORDER BY %s DESC
			)
		)`,
		qb.getDeduplicationKey(),
		whereClause,
		qb.getDeduplicationKey(),
	)

	qb.baseQuery = &ParameterizedQuery{
		Query: query,
		Args:  args,
	}

	qb.cteComponents = append(qb.cteComponents, CTEComponent{
		Name:  "base_events",
		Query: query,
		Args:  args,
	})

	return qb
}

func (qb *QueryBuilder) WithFilterGroups(ctx context.Context, groups []events.FilterGroup) *QueryBuilder {
	if len(groups) == 0 {
		return qb
	}

	var filterConditions []string
	var allArgs []interface{}
	argIndex := 1

	for _, group := range groups {
		var conditions []string
		var groupArgs []interface{}
		localArgIndex := argIndex

		for property, values := range group.Filters {
			if len(values) == 0 {
				continue
			}
			var condition string
			if len(values) == 1 {
				condition = fmt.Sprintf("JSONExtractString(properties, ?%d) = ?%d", localArgIndex, localArgIndex+1)
				groupArgs = append(groupArgs, property, values[0])
				localArgIndex += 2
			} else {
				placeholders := make([]string, len(values))
				for i := range values {
					placeholders[i] = fmt.Sprintf("?%d", localArgIndex+i+1)
					groupArgs = append(groupArgs, values[i])
				}
				condition = fmt.Sprintf(
					"JSONExtractString(properties, ?%d) IN (%s)",
					localArgIndex,
					strings.Join(placeholders, ","),
				)
				groupArgs = append(groupArgs, property)
				localArgIndex += len(values) + 1
			}
			conditions = append(conditions, condition)
		}

		// Update the global argument index
		argIndex = localArgIndex

		// Only add the filter group if it has conditions
		if len(conditions) > 0 {
			// Parameterize group ID and priority
			filterConditions = append(filterConditions, fmt.Sprintf(
				"(?%d, ?%d, (%s))",
				argIndex, argIndex+1, strings.Join(conditions, " AND "),
			))
			allArgs = append(allArgs, group.ID, group.Priority)
			allArgs = append(allArgs, groupArgs...)
			argIndex += 2
		} else {
			// For empty filter groups, use a constant true condition
			filterConditions = append(filterConditions, fmt.Sprintf(
				"(?%d, ?%d, 1)",
				argIndex, argIndex+1,
			))
			allArgs = append(allArgs, group.ID, group.Priority)
			argIndex += 2
		}
	}

	query := fmt.Sprintf(`filter_matches AS (
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

	qb.filterQuery = &ParameterizedQuery{
		Query: query,
		Args:  allArgs,
	}

	qb.cteComponents = append(qb.cteComponents, CTEComponent{
		Name:  "filter_matches",
		Query: query,
		Args:  allArgs,
	})

	matchedQuery := `matched_events AS (
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

	qb.matchedQuery = &ParameterizedQuery{
		Query: matchedQuery,
		Args:  []interface{}{},
	}

	// Split matched query into CTE components
	matchedCTEs := strings.Split(matchedQuery, ",")
	for i, cte := range matchedCTEs {
		cte = strings.TrimSpace(cte)
		if cte != "" {
			if i == 0 {
				cte = strings.TrimPrefix(cte, "matched_events AS (")
			} else {
				cte = strings.TrimSpace(cte)
			}
			qb.cteComponents = append(qb.cteComponents, CTEComponent{
				Name:  fmt.Sprintf("matched_cte_%d", i),
				Query: cte,
				Args:  []interface{}{},
			})
		}
	}

	return qb
}

func (qb *QueryBuilder) WithAggregation(ctx context.Context, aggType types.AggregationType, propertyName string) *QueryBuilder {
	var aggClause string
	var args []interface{}
	argIndex := 1

	switch aggType {
	case types.AggregationCount:
		aggClause = "COUNT(*)"
	case types.AggregationSum:
		aggClause = fmt.Sprintf("SUM(CAST(JSONExtractString(properties, ?%d) AS Float64))", argIndex)
		args = append(args, propertyName)
		argIndex++
	case types.AggregationAvg:
		aggClause = fmt.Sprintf("AVG(CAST(JSONExtractString(properties, ?%d) AS Float64))", argIndex)
		args = append(args, propertyName)
		argIndex++
	case types.AggregationCountUnique:
		aggClause = fmt.Sprintf("COUNT(DISTINCT JSONExtractString(properties, ?%d))", argIndex)
		args = append(args, propertyName)
		argIndex++
	}

	query := fmt.Sprintf("SELECT best_match_group as filter_group_id, %s as value FROM best_matches GROUP BY best_match_group ORDER BY best_match_group", aggClause)

	qb.finalQuery = &ParameterizedQuery{
		Query: query,
		Args:  args,
	}

	return qb
}

func (qb *QueryBuilder) Build() (string, []interface{}) {
	var cteParts []string
	var allArgs []interface{}

	// Collect all CTE components
	for _, component := range qb.cteComponents {
		cteParts = append(cteParts, component.Query)
		allArgs = append(allArgs, component.Args...)
	}

	// Join CTEs with commas
	ctePart := strings.Join(cteParts, ",\n")

	// Combine CTEs with final query
	finalQuery := fmt.Sprintf("WITH %s\n%s", ctePart, qb.finalQuery.Query)
	allArgs = append(allArgs, qb.finalQuery.Args...)

	return finalQuery, allArgs
}

func (qb *QueryBuilder) parseTimeConditionsWithIndex(params *events.UsageParams, startIndex int) ([]string, []interface{}, int) {
	var conditions []string
	var args []interface{}
	argIndex := startIndex

	if !params.StartTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("timestamp >= toDateTime64(?%d, 3)", argIndex))
		args = append(args, params.StartTime)
		argIndex++
	}

	if !params.EndTime.IsZero() {
		conditions = append(conditions, fmt.Sprintf("timestamp < toDateTime64(?%d, 3)", argIndex))
		args = append(args, params.EndTime)
		argIndex++
	}

	return conditions, args, argIndex
}

func formatClickHouseDateTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.000")
}
