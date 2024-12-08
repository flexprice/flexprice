package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
)

func GetAggregator(aggregationType types.AggregationType) events.Aggregator {
	switch aggregationType {
	case types.AggregationCount:
		return &CountAggregator{}
	case types.AggregationSum:
		return &SumAggregator{}
	case types.AggregationAvg:
		return &AvgAggregator{}
	}
	return nil
}

func getDeduplicationKey() string {
	return "id, tenant_id, external_customer_id, customer_id, event_name"
}

func formatClickHouseDateTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.000")
}

func formatWindowSize(windowSize types.WindowSize) string {
	switch windowSize {
	case types.WindowSizeMinute:
		return "toStartOfMinute(timestamp)"
	case types.WindowSizeHour:
		return "toStartOfHour(timestamp)"
	case types.WindowSizeDay:
		return "toStartOfDay(timestamp)"
	default:
		return ""
	}
}

func buildFilterConditions(filters map[string][]string) string {
	if len(filters) == 0 {
		return ""
	}

	var conditions []string
	for key, values := range filters {
		if len(values) == 0 {
			continue
		}

		quotedValues := make([]string, len(values))
		for i, v := range values {
			quotedValues[i] = fmt.Sprintf("'%s'", v)
		}

		conditions = append(conditions, fmt.Sprintf(
			"JSONExtractString(properties, '%s') IN (%s)",
			key,
			strings.Join(quotedValues, ","),
		))
	}

	if len(conditions) == 0 {
		return ""
	}

	return "AND " + strings.Join(conditions, " AND ")
}

func buildTimeConditions(params *events.UsageParams) string {
	var conditions []string

	if !params.StartTime.IsZero() {
		conditions = append(conditions,
			fmt.Sprintf("timestamp >= toDateTime64('%s', 3)",
				formatClickHouseDateTime(params.StartTime)))
	}

	if !params.EndTime.IsZero() {
		conditions = append(conditions,
			fmt.Sprintf("timestamp < toDateTime64('%s', 3)",
				formatClickHouseDateTime(params.EndTime)))
	}

	if len(conditions) == 0 {
		return ""
	}

	return "AND " + strings.Join(conditions, " AND ")
}

// SumAggregator implements sum aggregation
type SumAggregator struct{}

func (a *SumAggregator) GetQuery(ctx context.Context, params *events.UsageParams) string {
	windowSize := formatWindowSize(params.WindowSize)
	selectClause := ""
	windowClause := ""
	groupByClause := ""
	windowGroupBy := ""

	if windowSize != "" {
		selectClause = "window_size,"
		windowClause = fmt.Sprintf("%s AS window_size,", windowSize)
		groupByClause = "GROUP BY window_size ORDER BY window_size"
		windowGroupBy = ", window_size"
	}

	customerFilter := ""
	if params.ExternalCustomerID != "" {
		customerFilter = fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID)
	}

	filterConditions := buildFilterConditions(params.Filters)
	timeConditions := buildTimeConditions(params)

	return fmt.Sprintf(`
        SELECT 
            %s sum(value) as total
        FROM (
            SELECT
                %s anyLast(JSONExtractFloat(assumeNotNull(properties), '%s')) as value
            FROM events
            PREWHERE event_name = '%s' 
                AND tenant_id = '%s'
                %s
                %s
                %s
            GROUP BY %s %s
        )
        %s
    `,
		selectClause,
		windowClause,
		params.PropertyName,
		params.EventName,
		types.GetTenantID(ctx),
		customerFilter,
		filterConditions,
		timeConditions,
		getDeduplicationKey(),
		windowGroupBy,
		groupByClause)
}

func (a *SumAggregator) GetType() types.AggregationType {
	return types.AggregationSum
}

// CountAggregator implements count aggregation
type CountAggregator struct{}

func (a *CountAggregator) GetQuery(ctx context.Context, params *events.UsageParams) string {
	windowSize := formatWindowSize(params.WindowSize)
	selectClause := ""
	groupByClause := ""

	if windowSize != "" {
		selectClause = fmt.Sprintf("%s AS window_size,", windowSize)
		groupByClause = "GROUP BY window_size ORDER BY window_size"
	}

	customerFilter := ""
	if params.ExternalCustomerID != "" {
		customerFilter = fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID)
	}

	filterConditions := buildFilterConditions(params.Filters)
	timeConditions := buildTimeConditions(params)

	return fmt.Sprintf(`
        SELECT 
            %s count(DISTINCT %s) as total
        FROM events
        PREWHERE event_name = '%s'
            AND tenant_id = '%s'
            %s
            %s
            %s
        %s
    `,
		selectClause,
		getDeduplicationKey(),
		params.EventName,
		types.GetTenantID(ctx),
		customerFilter,
		filterConditions,
		timeConditions,
		groupByClause)
}

func (a *CountAggregator) GetType() types.AggregationType {
	return types.AggregationCount
}

// AvgAggregator implements avg aggregation
type AvgAggregator struct{}

func (a *AvgAggregator) GetQuery(ctx context.Context, params *events.UsageParams) string {
	windowSize := formatWindowSize(params.WindowSize)
	selectClause := ""
	windowClause := ""
	groupByClause := ""
	windowGroupBy := ""

	if windowSize != "" {
		selectClause = "window_size,"
		windowClause = fmt.Sprintf("%s AS window_size,", windowSize)
		groupByClause = "GROUP BY window_size ORDER BY window_size"
		windowGroupBy = ", window_size"
	}

	customerFilter := ""
	if params.ExternalCustomerID != "" {
		customerFilter = fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID)
	}

	filterConditions := buildFilterConditions(params.Filters)
	timeConditions := buildTimeConditions(params)

	return fmt.Sprintf(`
        SELECT 
            %s avg(value) as total
        FROM (
            SELECT
                %s anyLast(JSONExtractFloat(assumeNotNull(properties), '%s')) as value
            FROM events
            PREWHERE event_name = '%s' 
                AND tenant_id = '%s'
                %s
                %s
                %s
            GROUP BY %s %s
        )
        %s
    `,
		selectClause,
		windowClause,
		params.PropertyName,
		params.EventName,
		types.GetTenantID(ctx),
		customerFilter,
		filterConditions,
		timeConditions,
		getDeduplicationKey(),
		windowGroupBy,
		groupByClause)
}

func (a *AvgAggregator) GetType() types.AggregationType {
	return types.AggregationAvg
}
