// Package clickhouse provides ClickHouse-specific aggregators for usage data.
//
// Custom Monthly Billing Periods
// ==============================
//
// This package supports custom monthly billing periods through the BillingAnchor parameter.
// This allows usage data to be aggregated by custom monthly periods (e.g., 5th to 5th of each month)
// instead of standard calendar months (1st to 1st of each month).
//
// How it works:
// 1. When WindowSize = "MONTH" and BillingAnchor is provided, events are grouped by custom monthly periods
// 2. The BillingAnchor timestamp defines the reference day for monthly periods (time is ignored)
// 3. Day-level granularity is used for simplicity and predictability
// 4. All other window sizes (DAY, HOUR, WEEK, etc.) ignore BillingAnchor and use standard windows
//
// Example:
//   BillingAnchor = 2024-03-05 (any time on March 5th)
//   - March period: 2024-03-05 to 2024-04-05
//   - April period: 2024-04-05 to 2024-05-05
//
// Use cases:
// - Subscription billing that doesn't align with calendar months
// - Customer-specific billing cycles (e.g., signed up on 15th)
// - Multi-tenant systems with different billing anchor dates
// - Custom business cycles (fiscal months, quarterly periods)
//
// Implementation:
// The custom monthly logic generates ClickHouse expressions that:
// 1. Shift timestamps by the day offset from the billing anchor
// 2. Apply toStartOfMonth() to get calendar month boundaries
// 3. Shift back by the same day offset to create custom monthly periods
//
// This ensures that events are correctly grouped into the appropriate billing periods
// using day-level granularity for better predictability and user understanding.

package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

func GetAggregator(aggregationType types.AggregationType) events.Aggregator {
	switch aggregationType {
	case types.AggregationCount:
		return &CountAggregator{}
	case types.AggregationSum:
		return &SumAggregator{}
	case types.AggregationAvg:
		return &AvgAggregator{}
	case types.AggregationCountUnique:
		return &CountUniqueAggregator{}
	case types.AggregationLatest:
		return &LatestAggregator{}
	case types.AggregationSumWithMultiplier:
		return &SumWithMultiAggregator{}
	case types.AggregationMax:
		return &MaxAggregator{}
	case types.AggregationWeightedSum:
		return &WeightedSumAggregator{}
	}
	return nil
}

func getDeduplicationKey() string {
	return "id"
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
	case types.WindowSizeWeek:
		return "toStartOfWeek(timestamp)"
	case types.WindowSize15Min:
		return "toStartOfInterval(timestamp, INTERVAL 15 MINUTE)"
	case types.WindowSize30Min:
		return "toStartOfInterval(timestamp, INTERVAL 30 MINUTE)"
	case types.WindowSize3Hour:
		return "toStartOfInterval(timestamp, INTERVAL 3 HOUR)"
	case types.WindowSize6Hour:
		return "toStartOfInterval(timestamp, INTERVAL 6 HOUR)"
	case types.WindowSize12Hour:
		return "toStartOfInterval(timestamp, INTERVAL 12 HOUR)"
	case types.WindowSizeMonth:
		return "toStartOfMonth(timestamp)"
	default:
		return ""
	}
}

// formatWindowSizeWithBillingAnchor formats window size with custom billing anchor for monthly periods.
//
// This function handles custom billing periods for monthly aggregations, allowing events to be grouped
// by custom monthly periods (e.g., 5th to 5th of each month) instead of calendar months (1st to 1st).
//
// Parameters:
//   - windowSize: The aggregation window size (MONTH, DAY, HOUR, etc.)
//   - billingAnchor: The reference timestamp for custom monthly periods (only used for MONTH window size)
//
// Behavior by window size:
//   - MONTH + billingAnchor: Creates custom monthly periods using day-level granularity
//   - MONTH + nil: Falls back to standard calendar months (toStartOfMonth)
//   - DAY/HOUR/WEEK/etc: Ignores billing anchor, uses standard window functions
//
// Example: billingAnchor = 2024-03-05 (any time on March 5th)
//   - Events from March 5 to April 5 → March billing period
//   - Events from April 5 to May 5 → April billing period
//
// The generated ClickHouse expression shifts timestamps by the day offset,
// applies toStartOfMonth(), then shifts back to create the correct billing periods.
// This uses day-level granularity for simplicity and predictability.
func formatWindowSizeWithBillingAnchor(windowSize types.WindowSize, billingAnchor *time.Time) string {
	if windowSize == types.WindowSizeMonth && billingAnchor != nil {
		// Extract only the day component from billing anchor for simplicity
		anchorDay := billingAnchor.Day()

		// Generate the custom monthly window expression using day-level granularity
		// This shifts the timestamp by the day offset, then uses toStartOfMonth,
		// then shifts back by the same day offset to get the correct billing period
		return fmt.Sprintf(`
			addDays(
				toStartOfMonth(addDays(timestamp, -%d)),
				%d
			)`, anchorDay-1, anchorDay-1)
	}

	// Fall back to standard window size formatting
	return formatWindowSize(windowSize)
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
	conditions := parseTimeConditions(params)

	if len(conditions) == 0 {
		return ""
	}

	return "AND " + strings.Join(conditions, " AND ")
}

func parseTimeConditions(params *events.UsageParams) []string {
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

	return conditions
}

// SumAggregator implements sum aggregation
type SumAggregator struct{}

func (a *SumAggregator) GetQuery(ctx context.Context, params *events.UsageParams) string {
	// If bucket_size is specified, use windowed aggregation
	if params.BucketSize != "" {
		return a.getWindowedQuery(ctx, params)
	}
	// Otherwise use simple SUM aggregation
	return a.getNonWindowedQuery(ctx, params)
}

func (a *SumAggregator) getNonWindowedQuery(ctx context.Context, params *events.UsageParams) string {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
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

	externalCustomerFilter := ""
	if params.ExternalCustomerID != "" {
		externalCustomerFilter = fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID)
	}

	customerFilter := ""
	if params.CustomerID != "" {
		customerFilter = fmt.Sprintf("AND customer_id = '%s'", params.CustomerID)
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
            PREWHERE tenant_id = '%s'
				AND environment_id = '%s'
				AND event_name = '%s'
				%s
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
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		params.EventName,
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		getDeduplicationKey(),
		windowGroupBy,
		groupByClause)
}

func (a *SumAggregator) getWindowedQuery(ctx context.Context, params *events.UsageParams) string {
	bucketWindow := formatWindowSizeWithBillingAnchor(params.BucketSize, params.BillingAnchor)

	externalCustomerFilter := ""
	if params.ExternalCustomerID != "" {
		externalCustomerFilter = fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID)
	}

	customerFilter := ""
	if params.CustomerID != "" {
		customerFilter = fmt.Sprintf("AND customer_id = '%s'", params.CustomerID)
	}

	filterConditions := buildFilterConditions(params.Filters)
	timeConditions := buildTimeConditions(params)

	// Get sum values per bucket, return each bucket's sum separately
	return fmt.Sprintf(`
		WITH bucket_sums AS (
			SELECT
				%s as bucket_start,
				sum(JSONExtractFloat(assumeNotNull(properties), '%s')) as bucket_sum
			FROM events
			PREWHERE tenant_id = '%s'
				AND environment_id = '%s'
				AND event_name = '%s'
				%s
				%s
				%s
				%s
			GROUP BY bucket_start
			ORDER BY bucket_start
		)
		SELECT
			(SELECT sum(bucket_sum) FROM bucket_sums) as total,
			bucket_start as timestamp,
			bucket_sum as value
		FROM bucket_sums
		ORDER BY bucket_start
	`,
		bucketWindow,
		params.PropertyName,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		params.EventName,
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions)
}

func (a *SumAggregator) GetType() types.AggregationType {
	return types.AggregationSum
}

// CountAggregator implements count aggregation
type CountAggregator struct{}

func (a *CountAggregator) GetQuery(ctx context.Context, params *events.UsageParams) string {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
	selectClause := ""
	groupByClause := ""

	if windowSize != "" {
		selectClause = fmt.Sprintf("%s AS window_size,", windowSize)
		groupByClause = "GROUP BY window_size ORDER BY window_size"
	}

	externalCustomerFilter := ""
	if params.ExternalCustomerID != "" {
		externalCustomerFilter = fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID)
	}

	customerFilter := ""
	if params.CustomerID != "" {
		customerFilter = fmt.Sprintf("AND customer_id = '%s'", params.CustomerID)
	}

	filterConditions := buildFilterConditions(params.Filters)
	timeConditions := buildTimeConditions(params)

	return fmt.Sprintf(`
        SELECT 
            %s count(DISTINCT %s) as total
        FROM events
        PREWHERE tenant_id = '%s'
			AND environment_id = '%s'
			AND event_name = '%s'
			%s
			%s
            %s
            %s
        %s
    `,
		selectClause,
		getDeduplicationKey(),
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		params.EventName,
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		groupByClause)
}

func (a *CountAggregator) GetType() types.AggregationType {
	return types.AggregationCount
}

// CountUniqueAggregator implements count unique aggregation
type CountUniqueAggregator struct{}

func (a *CountUniqueAggregator) GetQuery(ctx context.Context, params *events.UsageParams) string {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
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

	externalCustomerFilter := ""
	if params.ExternalCustomerID != "" {
		externalCustomerFilter = fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID)
	}

	customerFilter := ""
	if params.CustomerID != "" {
		customerFilter = fmt.Sprintf("AND customer_id = '%s'", params.CustomerID)
	}

	filterConditions := buildFilterConditions(params.Filters)
	timeConditions := buildTimeConditions(params)

	return fmt.Sprintf(`
        SELECT 
            %s count(DISTINCT property_value) as total
        FROM (
            SELECT
                %s JSONExtractString(assumeNotNull(properties), '%s') as property_value
            FROM events
            PREWHERE tenant_id = '%s'
				AND environment_id = '%s'
				AND event_name = '%s'
				%s
				%s
                %s
                %s
            GROUP BY %s, property_value %s
        )
        %s
    `,
		selectClause,
		windowClause,
		params.PropertyName,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		params.EventName,
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		getDeduplicationKey(),
		windowGroupBy,
		groupByClause)
}

func (a *CountUniqueAggregator) GetType() types.AggregationType {
	return types.AggregationCountUnique
}

// AvgAggregator implements avg aggregation
type AvgAggregator struct{}

func (a *AvgAggregator) GetQuery(ctx context.Context, params *events.UsageParams) string {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
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

	externalCustomerFilter := ""
	if params.ExternalCustomerID != "" {
		externalCustomerFilter = fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID)
	}

	customerFilter := ""
	if params.CustomerID != "" {
		customerFilter = fmt.Sprintf("AND customer_id = '%s'", params.CustomerID)
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
            PREWHERE tenant_id = '%s'
				AND environment_id = '%s'
				AND event_name = '%s' 
				%s
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
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		params.EventName,
		externalCustomerFilter,
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

// LatestAggregator implements latest value aggregation
type LatestAggregator struct{}

func (a *LatestAggregator) GetQuery(ctx context.Context, params *events.UsageParams) string {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
	windowClause := ""
	groupByClause := ""

	if windowSize != "" {
		windowClause = fmt.Sprintf("%s AS window_size,", windowSize)
		groupByClause = "GROUP BY window_size ORDER BY window_size"
	}

	externalCustomerFilter := ""
	if params.ExternalCustomerID != "" {
		externalCustomerFilter = fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID)
	}

	customerFilter := ""
	if params.CustomerID != "" {
		customerFilter = fmt.Sprintf("AND customer_id = '%s'", params.CustomerID)
	}

	filterConditions := buildFilterConditions(params.Filters)
	timeConditions := buildTimeConditions(params)

	return fmt.Sprintf(`
        SELECT 
            %s argMax(JSONExtractFloat(assumeNotNull(properties), '%s'), timestamp) as total
        FROM 
			events	PREWHERE tenant_id = '%s'
                AND environment_id = '%s'
                AND event_name = '%s'
                %s
                %s
                %s
                %s
        %s
    `,
		windowClause,
		params.PropertyName,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		params.EventName,
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		groupByClause)
}

func (a *LatestAggregator) GetType() types.AggregationType {
	return types.AggregationLatest
}

// SumWithMultiAggregator implements sum with multiplier aggregation
type SumWithMultiAggregator struct{}

func (a *SumWithMultiAggregator) GetQuery(ctx context.Context, params *events.UsageParams) string {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
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

	externalCustomerFilter := ""
	if params.ExternalCustomerID != "" {
		externalCustomerFilter = fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID)
	}

	customerFilter := ""
	if params.CustomerID != "" {
		customerFilter = fmt.Sprintf("AND customer_id = '%s'", params.CustomerID)
	}

	filterConditions := buildFilterConditions(params.Filters)
	timeConditions := buildTimeConditions(params)

	multiplier := decimal.NewFromInt(1)
	if params.Multiplier != nil {
		multiplier = *params.Multiplier
	}

	return fmt.Sprintf(`
        SELECT 
            %s (sum(value) * %f) as total
        FROM (
            SELECT
                %s anyLast(JSONExtractFloat(assumeNotNull(properties), '%s')) as value
            FROM events
            PREWHERE tenant_id = '%s'
				AND environment_id = '%s'
				AND event_name = '%s'
				%s
				%s
                %s
                %s
            GROUP BY %s %s
        )
        %s
    `,
		selectClause,
		multiplier.InexactFloat64(),
		windowClause,
		params.PropertyName,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		params.EventName,
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		getDeduplicationKey(),
		windowGroupBy,
		groupByClause)
}

func (a *SumWithMultiAggregator) GetType() types.AggregationType {
	return types.AggregationSumWithMultiplier
}

// MaxAggregator implements max aggregation
type MaxAggregator struct{}

func (a *MaxAggregator) GetQuery(ctx context.Context, params *events.UsageParams) string {
	// If bucket_size is specified, use windowed aggregation
	if params.BucketSize != "" {
		return a.getWindowedQuery(ctx, params)
	}
	// Otherwise use simple MAX aggregation
	return a.getNonWindowedQuery(ctx, params)
}

func (a *MaxAggregator) getNonWindowedQuery(ctx context.Context, params *events.UsageParams) string {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
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

	externalCustomerFilter := ""
	if params.ExternalCustomerID != "" {
		externalCustomerFilter = fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID)
	}

	customerFilter := ""
	if params.CustomerID != "" {
		customerFilter = fmt.Sprintf("AND customer_id = '%s'", params.CustomerID)
	}

	filterConditions := buildFilterConditions(params.Filters)
	timeConditions := buildTimeConditions(params)

	return fmt.Sprintf(`
		SELECT 
			%s max(value) as total
		FROM (
			SELECT
				%s anyLast(JSONExtractFloat(assumeNotNull(properties), '%s')) as value
			FROM events
			PREWHERE tenant_id = '%s'
				AND environment_id = '%s'
				AND event_name = '%s'
				%s
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
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		params.EventName,
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		getDeduplicationKey(),
		windowGroupBy,
		groupByClause)
}

func (a *MaxAggregator) getWindowedQuery(ctx context.Context, params *events.UsageParams) string {
	bucketWindow := formatWindowSizeWithBillingAnchor(params.BucketSize, params.BillingAnchor)

	externalCustomerFilter := ""
	if params.ExternalCustomerID != "" {
		externalCustomerFilter = fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID)
	}

	customerFilter := ""
	if params.CustomerID != "" {
		customerFilter = fmt.Sprintf("AND customer_id = '%s'", params.CustomerID)
	}

	filterConditions := buildFilterConditions(params.Filters)
	timeConditions := buildTimeConditions(params)

	// First get max values per bucket, then get the max across all buckets
	return fmt.Sprintf(`
		WITH bucket_maxes AS (
			SELECT
				%s as bucket_start,
				max(JSONExtractFloat(assumeNotNull(properties), '%s')) as bucket_max
			FROM events
			PREWHERE tenant_id = '%s'
				AND environment_id = '%s'
				AND event_name = '%s'
				%s
				%s
				%s
				%s
			GROUP BY bucket_start
			ORDER BY bucket_start
		)
		SELECT
			(SELECT sum(bucket_max) FROM bucket_maxes) as total,
			bucket_start as timestamp,
			bucket_max as value
		FROM bucket_maxes
		ORDER BY bucket_start
	`,
		bucketWindow,
		params.PropertyName,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		params.EventName,
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions)
}

func (a *MaxAggregator) GetType() types.AggregationType {
	return types.AggregationMax
}

// WeightedSumAggregator implements weighted sum aggregation
type WeightedSumAggregator struct{}

func (a *WeightedSumAggregator) GetQuery(ctx context.Context, params *events.UsageParams) string {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
	selectClause := ""
	windowClause := ""
	groupByClause := ""

	if windowSize != "" {
		selectClause = "window_size,"
		windowClause = fmt.Sprintf("%s AS window_size,", windowSize)
		groupByClause = "GROUP BY window_size ORDER BY window_size"
	}

	externalCustomerFilter := ""
	if params.ExternalCustomerID != "" {
		externalCustomerFilter = fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID)
	}

	customerFilter := ""
	if params.CustomerID != "" {
		customerFilter = fmt.Sprintf("AND customer_id = '%s'", params.CustomerID)
	}

	filterConditions := buildFilterConditions(params.Filters)
	timeConditions := buildTimeConditions(params)

	return fmt.Sprintf(`
        WITH
            toDateTime64('%s', 3) AS period_start,
            toDateTime64('%s', 3) AS period_end,
            dateDiff('second', period_start, period_end) AS total_seconds
        SELECT 
            %s sum(
                (JSONExtractFloat(assumeNotNull(properties), '%s') / nullIf(total_seconds, 0)) *
                dateDiff('second', timestamp, period_end)
            ) AS total
        FROM (
            SELECT
                %s timestamp,
                properties
            FROM events
            PREWHERE tenant_id = '%s'
				AND environment_id = '%s'
				AND event_name = '%s'
				%s
				%s
                %s
                %s
        )
        %s
    `,
		formatClickHouseDateTime(params.StartTime),
		formatClickHouseDateTime(params.EndTime),
		selectClause,
		params.PropertyName,
		windowClause,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		params.EventName,
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		groupByClause,
	)
}

func (a *WeightedSumAggregator) GetType() types.AggregationType {
	return types.AggregationWeightedSum
}

// weighted sum final query without window size
// WITH
//             toDateTime64('2025-07-31 18:30:00.000', 3) AS period_start,
//             toDateTime64('2025-08-31 18:30:00.000', 3) AS period_end,
//             dateDiff('second', period_start, period_end) AS total_seconds
//         SELECT
//              sum(
//                 (JSONExtractFloat(assumeNotNull(properties), 'value') / nullIf(total_seconds, 0)) *
//                 dateDiff('second', timestamp, period_end)
//             ) AS total
//         FROM (
//             SELECT
//                  timestamp,
//                 properties
//             FROM events
//             PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
//                                 AND environment_id = 'env_01K3G204ZZS7F1CAPE32TS0X9W'
//                                 AND event_name = 'weighted_sum_event'
//                                 AND external_customer_id = 'cust-wc-5'

//                 AND timestamp >= toDateTime64('2025-07-31 18:30:00.000', 3) AND timestamp < toDateTime64('2025-08-31 18:30:00.000', 3)
//         )

// weighted sum final query with window size
// WITH
//             toDateTime64('2025-09-12 11:08:50.630', 3) AS period_start,
//             toDateTime64('2025-09-19 11:08:50.630', 3) AS period_end,
//             dateDiff('second', period_start, period_end) AS total_seconds
//         SELECT
//             window_size, sum(
//                 (JSONExtractFloat(assumeNotNull(properties), 'value') / nullIf(total_seconds, 0)) *
//                 dateDiff('second', timestamp, period_end)
//             ) AS total
//         FROM (
//             SELECT
//                 toStartOfInterval(timestamp, INTERVAL 15 MINUTE) AS window_size, timestamp,
//                 properties
//             FROM events
//             PREWHERE tenant_id = '00000000-0000-0000-0000-000000000000'
//                                 AND environment_id = 'env_01K3G204ZZS7F1CAPE32TS0X9W'
//                                 AND event_name = 'weighted_sum_event'
//                                 AND external_customer_id = 'cust-wc-5'
//                                 AND customer_id = 'cust_01K5GHAV4R787PVZTYW5G9BMGD'

//                 AND timestamp >= toDateTime64('2025-09-12 11:08:50.630', 3) AND timestamp < toDateTime64('2025-09-19 11:08:50.630', 3)
//         )
//         GROUP BY window_size ORDER BY window_size

// // buildFilterGroupsQuery builds a query that matches events to the most specific filter group
// func buildFilterGroupsQuery(params *events.UsageWithFiltersParams) string {
//     var queryBuilder strings.Builder

//     // Base query with CTE for filter matches
//     queryBuilder.WriteString(fmt.Sprintf(`
//         WITH
//         base_events AS (
//             SELECT
//                 %s,
//                 timestamp,
//                 properties
//             FROM events
//             WHERE event_name = $1
//             %s
//         ),
//         filter_matches AS (
//             SELECT
//                 *,
//                 -- Calculate matches for each filter group
//                 ARRAY[
//     `, getDeduplicationKey(), buildTimeAndCustomerConditions(params.UsageParams)))

//     // Generate array of filter group matches
//     for i, group := range params.FilterGroups {
//         if i > 0 {
//             queryBuilder.WriteString(",")
//         }

//         // Default group case (no filters)
//         if len(group.Filters) == 0 {
//             queryBuilder.WriteString(fmt.Sprintf(`
//                 (%d, %d, 1)  -- group_id, priority, always matches
//             `, group.ID, group.Priority))
//             continue
//         }

//         // Build filter conditions
//         var conditions []string
//         for property, values := range group.Filters {
//             quotedValues := make([]string, len(values))
//             for i, v := range values {
//                 quotedValues[i] = fmt.Sprintf("'%s'", v)
//             }
//             conditions = append(conditions, fmt.Sprintf(
//                 "JSONExtractString(properties, '%s') IN (%s)",
//                 property,
//                 strings.Join(quotedValues, ","),
//             ))
//         }

//         queryBuilder.WriteString(fmt.Sprintf(`
//             (%d, %d, %s)  -- group_id, priority, filter conditions
//         `, group.ID, group.Priority, strings.Join(conditions, " AND ")))
//     }

//     // Complete filter_matches CTE and add matched_events CTE
//     queryBuilder.WriteString(`
//         ] as group_matches
//         ),
//         matched_events AS (
//             SELECT
//                 *,
//                 -- Select group with highest priority that matches
//                 (
//                     SELECT group_id
//                     FROM arrayJoin(group_matches) AS g
//                     WHERE g.3 = 1  -- where matches = true
//                     ORDER BY g.2 DESC, group_id  -- order by priority desc, then group_id
//                     LIMIT 1
//                 ) as best_match_group
//             FROM base_events
//         )
//     `)

//     // Add final aggregation based on params
//     queryBuilder.WriteString(`
//         SELECT
//             best_match_group as filter_group_id,
//     `)

//     // Add aggregation function
//     switch params.AggregationType {
//     case types.AggregationCount:
//         queryBuilder.WriteString(`
//             COUNT(*) as value
//         `)
//     case types.AggregationSum:
//         queryBuilder.WriteString(fmt.Sprintf(`
//             SUM(CAST(JSONExtractString(properties, '%s') AS Float64)) as value
//         `, params.PropertyName))
//     case types.AggregationAvg:
//         queryBuilder.WriteString(fmt.Sprintf(`
//             AVG(CAST(JSONExtractString(properties, '%s') AS Float64)) as value
//         `, params.PropertyName))
//     }

//     // Add grouping and ordering
//     queryBuilder.WriteString(`
//         FROM matched_events
//         GROUP BY best_match_group
//         ORDER BY best_match_group
//     `)

//     return queryBuilder.String()
// }

// // buildTimeAndCustomerConditions builds the WHERE clause for time and customer filters
// func buildTimeAndCustomerConditions(params *events.UsageParams) string {
//     var conditions []string

//     // Add time range conditions
//     if !params.StartTime.IsZero() {
//         conditions = append(conditions,
//             fmt.Sprintf("AND timestamp >= '%s'", params.StartTime.Format(time.RFC3339)))
//     }
//     if !params.EndTime.IsZero() {
//         conditions = append(conditions,
//             fmt.Sprintf("AND timestamp < '%s'", params.EndTime.Format(time.RFC3339)))
//     }

//     // Add customer conditions
//     if params.ExternalCustomerID != "" {
//         conditions = append(conditions,
//             fmt.Sprintf("AND external_customer_id = '%s'", params.ExternalCustomerID))
//     }
//     if params.CustomerID != "" {
//         conditions = append(conditions,
//             fmt.Sprintf("AND customer_id = '%s'", params.CustomerID))
//     }

//     return strings.Join(conditions, " ")
// }
