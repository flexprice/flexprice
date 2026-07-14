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
//
// Query Parameterization
// =======================
//
// All queries built in this file use `?` positional placeholders for any value that
// originates from user/API input (filter property names/values, customer IDs, event
// name, timezone, group-by property, timestamps). Each query-building function returns
// (query string, []interface{} args) — the args slice must be passed to the ClickHouse
// driver's Query(ctx, query, args...) call in the same order the placeholders appear in
// the concatenated query string, since ClickHouse binds positionally. This mirrors the
// pattern already used in meter_usage_query_builder.go.

package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

// buildUsageEventCustomerFilters returns PREWHERE fragments (with `?` placeholders) for
// external_customer_id and FlexPrice customer_id, plus the args to bind against them, in
// the order the two returned fragments are concatenated into the final query. Params may
// be nil. External merges ExternalCustomerID and ExternalCustomerIDs (deduped); internal
// uses CustomerID only.
func buildUsageEventCustomerFilters(params *events.UsageParams) (externalCustomerFilter string, customerFilter string, args []interface{}) {
	if params == nil {
		return "", "", nil
	}

	extIDs := make([]string, 0)
	if params.ExternalCustomerID != "" {
		extIDs = append(extIDs, params.ExternalCustomerID)
	}
	if len(params.ExternalCustomerIDs) > 0 {
		extIDs = append(extIDs, params.ExternalCustomerIDs...)
	}
	extUnique := lo.Uniq(extIDs)
	switch len(extUnique) {
	case 0:
		externalCustomerFilter = ""
	case 1:
		externalCustomerFilter = "AND external_customer_id = ?"
		args = append(args, extUnique[0])
	default:
		placeholders := make([]string, len(extUnique))
		for i := range extUnique {
			placeholders[i] = "?"
		}
		externalCustomerFilter = fmt.Sprintf("AND external_customer_id IN (%s)", strings.Join(placeholders, ", "))
		for _, id := range extUnique {
			args = append(args, id)
		}
	}

	if params.CustomerID != "" {
		customerFilter = "AND customer_id = ?"
		args = append(args, params.CustomerID)
	}
	return externalCustomerFilter, customerFilter, args
}

func formatClickHouseDateTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.000")
}

// normalizeCHTimezone returns a valid IANA timezone name for ClickHouse
// toStartOf* functions, defaulting to UTC for empty, "UTC", or unresolvable
// values. Passing an invalid name straight to ClickHouse would error the query,
// so this is the single guard shared by both window formatters. Since the return
// value is validated against time.LoadLocation, it is safe to interpolate directly.
func normalizeCHTimezone(tz string) string {
	if tz == "" || tz == types.DefaultTimezone {
		return types.DefaultTimezone
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return types.DefaultTimezone
	}
	return tz
}

// formatWindowSize returns the ClickHouse expression that buckets `timestamp`
// into the given window in the given timezone. "UTC" is a valid ClickHouse
// timezone argument and yields identical results to omitting it, so the tz is
// always passed — there is no UTC special-casing. The tz value has already been
// validated by normalizeCHTimezone (constrained to resolvable IANA names via
// time.LoadLocation), so it is safe to interpolate directly here rather than bind
// as a query arg — these window expressions are used inside larger fmt.Sprintf
// templates that are themselves fully parameterized for user-controlled values.
func formatWindowSize(windowSize types.WindowSize, tz string) string {
	if windowSize == "" {
		return ""
	}
	// Minute buckets are timezone-invariant (all real offsets are whole minutes).
	if windowSize == types.WindowSizeMinute {
		return "toStartOfMinute(timestamp)"
	}

	tz = normalizeCHTimezone(tz)
	switch windowSize {
	case types.WindowSize15Min:
		return fmt.Sprintf("toStartOfInterval(timestamp, INTERVAL 15 MINUTE, '%s')", tz)
	case types.WindowSize30Min:
		return fmt.Sprintf("toStartOfInterval(timestamp, INTERVAL 30 MINUTE, '%s')", tz)
	case types.WindowSize3Hour:
		return fmt.Sprintf("toStartOfInterval(timestamp, INTERVAL 3 HOUR, '%s')", tz)
	case types.WindowSize6Hour:
		return fmt.Sprintf("toStartOfInterval(timestamp, INTERVAL 6 HOUR, '%s')", tz)
	case types.WindowSize12Hour:
		return fmt.Sprintf("toStartOfInterval(timestamp, INTERVAL 12 HOUR, '%s')", tz)
	case types.WindowSizeHour:
		return fmt.Sprintf("toStartOfHour(timestamp, '%s')", tz)
	case types.WindowSizeWeek:
		return fmt.Sprintf("toStartOfWeek(timestamp, 0, '%s')", tz)
	case types.WindowSizeMonth:
		return fmt.Sprintf("toStartOfMonth(timestamp, '%s')", tz)
	case types.WindowSizeDay:
		return fmt.Sprintf("toStartOfDay(timestamp, '%s')", tz)
	default:
		return fmt.Sprintf("toStartOfDay(timestamp, '%s')", tz)
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
func formatWindowSizeWithBillingAnchor(windowSize types.WindowSize, billingAnchor *time.Time, tz string) string {
	if windowSize == types.WindowSizeMonth && billingAnchor != nil {
		// Extract only the day component from billing anchor for simplicity
		anchorDay := billingAnchor.Day()
		tz = normalizeCHTimezone(tz)

		// Custom monthly window anchored on anchorDay, computed in the customer's
		// timezone. Shift by the day offset, snap to month start, shift back.
		// "UTC" is a valid tz argument and reproduces the legacy (UTC) result.
		return fmt.Sprintf("addDays(toStartOfMonth(addDays(toTimezone(timestamp, '%s'), -%d), '%s'), %d)", tz, anchorDay-1, tz, anchorDay-1)
	}

	// Fall back to standard window size formatting
	return formatWindowSize(windowSize, tz)
}

// buildFilterConditions builds JSONExtractString filter fragments using positional `?`
// placeholders for both the property name and its value(s) — both are user-controlled
// (request body `filters` map) and must never be interpolated into the query string.
// Returns the WHERE-fragment (e.g. "AND JSONExtractString(properties, ?) IN (?,?) AND ...")
// and the args to bind, in the exact order the placeholders appear.
func buildFilterConditions(filters map[string][]string) (string, []interface{}) {
	if len(filters) == 0 {
		return "", nil
	}

	var conditions []string
	var args []interface{}
	for key, values := range filters {
		if len(values) == 0 {
			continue
		}

		placeholders := make([]string, len(values))
		for i := range values {
			placeholders[i] = "?"
		}

		conditions = append(conditions, fmt.Sprintf(
			"JSONExtractString(properties, ?) IN (%s)",
			strings.Join(placeholders, ","),
		))
		args = append(args, key)
		for _, v := range values {
			args = append(args, v)
		}
	}

	if len(conditions) == 0 {
		return "", nil
	}

	return "AND " + strings.Join(conditions, " AND "), args
}

// buildTimeConditions builds the time-range WHERE fragment using `?` placeholders.
func buildTimeConditions(params *events.UsageParams) (string, []interface{}) {
	conditions, args := parseTimeConditions(params)

	if len(conditions) == 0 {
		return "", nil
	}

	return "AND " + strings.Join(conditions, " AND "), args
}

func parseTimeConditions(params *events.UsageParams) ([]string, []interface{}) {
	var conditions []string
	var args []interface{}

	if !params.StartTime.IsZero() {
		conditions = append(conditions, "timestamp >= toDateTime64(?, 3)")
		args = append(args, formatClickHouseDateTime(params.StartTime))
	}

	if !params.EndTime.IsZero() {
		conditions = append(conditions, "timestamp < toDateTime64(?, 3)")
		args = append(args, formatClickHouseDateTime(params.EndTime))
	}

	return conditions, args
}

// SumAggregator implements sum aggregation
type SumAggregator struct{}

func (a *SumAggregator) GetQuery(ctx context.Context, params *events.UsageParams) (string, []interface{}) {
	// If bucket_size is specified, use windowed aggregation
	if params.BucketSize != "" {
		return a.getWindowedQuery(ctx, params)
	}
	// Otherwise use simple SUM aggregation
	return a.getNonWindowedQuery(ctx, params)
}

func (a *SumAggregator) getNonWindowedQuery(ctx context.Context, params *events.UsageParams) (string, []interface{}) {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
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

	externalCustomerFilter, customerFilter, customerArgs := buildUsageEventCustomerFilters(params)

	filterConditions, filterArgs := buildFilterConditions(params.Filters)
	timeConditions, timeArgs := buildTimeConditions(params)

	query := fmt.Sprintf(`
        SELECT
            %s sum(value) as total
        FROM (
            SELECT
                %s anyLast(JSONExtractFloat(assumeNotNull(properties), ?)) as value
            FROM events
            PREWHERE tenant_id = ?
				AND environment_id = ?
				AND event_name = ?
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
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		getDeduplicationKey(),
		windowGroupBy,
		groupByClause)

	args := []interface{}{params.PropertyName, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.EventName}
	args = append(args, customerArgs...)
	args = append(args, filterArgs...)
	args = append(args, timeArgs...)

	return query, args
}

func (a *SumAggregator) getWindowedQuery(ctx context.Context, params *events.UsageParams) (string, []interface{}) {
	bucketWindow := formatWindowSizeWithBillingAnchor(params.BucketSize, params.BillingAnchor, params.Timezone)

	externalCustomerFilter, customerFilter, customerArgs := buildUsageEventCustomerFilters(params)

	filterConditions, filterArgs := buildFilterConditions(params.Filters)
	timeConditions, timeArgs := buildTimeConditions(params)

	// Get sum values per bucket, return each bucket's sum separately
	query := fmt.Sprintf(`
		WITH bucket_sums AS (
			SELECT
				%s as bucket_start,
				sum(JSONExtractFloat(assumeNotNull(properties), ?)) as bucket_sum
			FROM events FINAL
			PREWHERE tenant_id = ?
				AND environment_id = ?
				AND event_name = ?
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
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions)

	args := []interface{}{params.PropertyName, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.EventName}
	args = append(args, customerArgs...)
	args = append(args, filterArgs...)
	args = append(args, timeArgs...)

	return query, args
}

func (a *SumAggregator) GetType() types.AggregationType {
	return types.AggregationSum
}

// CountAggregator implements count aggregation
type CountAggregator struct{}

func (a *CountAggregator) GetQuery(ctx context.Context, params *events.UsageParams) (string, []interface{}) {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
	selectClause := ""
	groupByClause := ""

	if windowSize != "" {
		selectClause = fmt.Sprintf("%s AS window_size,", windowSize)
		groupByClause = "GROUP BY window_size ORDER BY window_size"
	}

	externalCustomerFilter, customerFilter, customerArgs := buildUsageEventCustomerFilters(params)

	filterConditions, filterArgs := buildFilterConditions(params.Filters)
	timeConditions, timeArgs := buildTimeConditions(params)

	query := fmt.Sprintf(`
        SELECT
            %s count(DISTINCT %s) as total
        FROM events
        PREWHERE tenant_id = ?
			AND environment_id = ?
			AND event_name = ?
			%s
			%s
            %s
            %s
        %s
    `,
		selectClause,
		getDeduplicationKey(),
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		groupByClause)

	args := []interface{}{types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.EventName}
	args = append(args, customerArgs...)
	args = append(args, filterArgs...)
	args = append(args, timeArgs...)

	return query, args
}

func (a *CountAggregator) GetType() types.AggregationType {
	return types.AggregationCount
}

// CountUniqueAggregator implements count unique aggregation
type CountUniqueAggregator struct{}

func (a *CountUniqueAggregator) GetQuery(ctx context.Context, params *events.UsageParams) (string, []interface{}) {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
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

	externalCustomerFilter, customerFilter, customerArgs := buildUsageEventCustomerFilters(params)

	filterConditions, filterArgs := buildFilterConditions(params.Filters)
	timeConditions, timeArgs := buildTimeConditions(params)

	query := fmt.Sprintf(`
        SELECT
            %s count(DISTINCT property_value) as total
        FROM (
            SELECT
                %s JSONExtractString(assumeNotNull(properties), ?) as property_value
            FROM events
            PREWHERE tenant_id = ?
				AND environment_id = ?
				AND event_name = ?
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
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		getDeduplicationKey(),
		windowGroupBy,
		groupByClause)

	args := []interface{}{params.PropertyName, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.EventName}
	args = append(args, customerArgs...)
	args = append(args, filterArgs...)
	args = append(args, timeArgs...)

	return query, args
}

func (a *CountUniqueAggregator) GetType() types.AggregationType {
	return types.AggregationCountUnique
}

// AvgAggregator implements avg aggregation
type AvgAggregator struct{}

func (a *AvgAggregator) GetQuery(ctx context.Context, params *events.UsageParams) (string, []interface{}) {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
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

	externalCustomerFilter, customerFilter, customerArgs := buildUsageEventCustomerFilters(params)

	filterConditions, filterArgs := buildFilterConditions(params.Filters)
	timeConditions, timeArgs := buildTimeConditions(params)

	query := fmt.Sprintf(`
        SELECT
            %s avg(value) as total
        FROM (
            SELECT
                %s anyLast(JSONExtractFloat(assumeNotNull(properties), ?)) as value
            FROM events
            PREWHERE tenant_id = ?
				AND environment_id = ?
				AND event_name = ?
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
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		getDeduplicationKey(),
		windowGroupBy,
		groupByClause)

	args := []interface{}{params.PropertyName, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.EventName}
	args = append(args, customerArgs...)
	args = append(args, filterArgs...)
	args = append(args, timeArgs...)

	return query, args
}

func (a *AvgAggregator) GetType() types.AggregationType {
	return types.AggregationAvg
}

// LatestAggregator implements latest value aggregation
type LatestAggregator struct{}

func (a *LatestAggregator) GetQuery(ctx context.Context, params *events.UsageParams) (string, []interface{}) {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
	windowClause := ""
	groupByClause := ""

	if windowSize != "" {
		windowClause = fmt.Sprintf("%s AS window_size,", windowSize)
		groupByClause = "GROUP BY window_size ORDER BY window_size"
	}

	externalCustomerFilter, customerFilter, customerArgs := buildUsageEventCustomerFilters(params)

	filterConditions, filterArgs := buildFilterConditions(params.Filters)
	timeConditions, timeArgs := buildTimeConditions(params)

	query := fmt.Sprintf(`
        SELECT
            %s argMax(JSONExtractFloat(assumeNotNull(properties), ?), timestamp) as total
        FROM
			events
			PREWHERE tenant_id = ?
                AND environment_id = ?
                AND event_name = ?
                %s
                %s
                %s
                %s
        %s
    `,
		windowClause,
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		groupByClause)

	args := []interface{}{params.PropertyName, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.EventName}
	args = append(args, customerArgs...)
	args = append(args, filterArgs...)
	args = append(args, timeArgs...)

	return query, args
}

func (a *LatestAggregator) GetType() types.AggregationType {
	return types.AggregationLatest
}

// SumWithMultiAggregator implements sum with multiplier aggregation
type SumWithMultiAggregator struct{}

func (a *SumWithMultiAggregator) GetQuery(ctx context.Context, params *events.UsageParams) (string, []interface{}) {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
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

	externalCustomerFilter, customerFilter, customerArgs := buildUsageEventCustomerFilters(params)

	filterConditions, filterArgs := buildFilterConditions(params.Filters)
	timeConditions, timeArgs := buildTimeConditions(params)

	// Multiplier is a decimal.Decimal value computed server-side (not raw user string
	// input) — safe to interpolate as a numeric literal via .String(), same as before.
	multiplier := decimal.NewFromInt(1)
	if params.Multiplier != nil {
		multiplier = *params.Multiplier
	}

	query := fmt.Sprintf(`
        SELECT
            %s (sum(value) * %s) as total
        FROM (
            SELECT
                %s anyLast(JSONExtractFloat(assumeNotNull(properties), ?)) as value
            FROM events
            PREWHERE tenant_id = ?
				AND environment_id = ?
				AND event_name = ?
				%s
				%s
                %s
                %s
            GROUP BY %s %s
        )
        %s
    `,
		selectClause,
		multiplier.String(),
		windowClause,
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		getDeduplicationKey(),
		windowGroupBy,
		groupByClause)

	args := []interface{}{params.PropertyName, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.EventName}
	args = append(args, customerArgs...)
	args = append(args, filterArgs...)
	args = append(args, timeArgs...)

	return query, args
}

func (a *SumWithMultiAggregator) GetType() types.AggregationType {
	return types.AggregationSumWithMultiplier
}

// MaxAggregator implements max aggregation
type MaxAggregator struct{}

func (a *MaxAggregator) GetQuery(ctx context.Context, params *events.UsageParams) (string, []interface{}) {
	// If bucket_size is specified, use windowed aggregation
	if params.BucketSize != "" {
		return a.getWindowedQuery(ctx, params)
	}
	// Otherwise use simple MAX aggregation
	return a.getNonWindowedQuery(ctx, params)
}

func (a *MaxAggregator) getNonWindowedQuery(ctx context.Context, params *events.UsageParams) (string, []interface{}) {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
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

	externalCustomerFilter, customerFilter, customerArgs := buildUsageEventCustomerFilters(params)

	filterConditions, filterArgs := buildFilterConditions(params.Filters)
	timeConditions, timeArgs := buildTimeConditions(params)

	query := fmt.Sprintf(`
		SELECT
			%s max(value) as total
		FROM (
			SELECT
				%s anyLast(JSONExtractFloat(assumeNotNull(properties), ?)) as value
			FROM events
			PREWHERE tenant_id = ?
				AND environment_id = ?
				AND event_name = ?
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
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		getDeduplicationKey(),
		windowGroupBy,
		groupByClause)

	args := []interface{}{params.PropertyName, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.EventName}
	args = append(args, customerArgs...)
	args = append(args, filterArgs...)
	args = append(args, timeArgs...)

	return query, args
}

func (a *MaxAggregator) getWindowedQuery(ctx context.Context, params *events.UsageParams) (string, []interface{}) {
	bucketWindow := formatWindowSizeWithBillingAnchor(params.BucketSize, params.BillingAnchor, params.Timezone)

	externalCustomerFilter, customerFilter, customerArgs := buildUsageEventCustomerFilters(params)

	filterConditions, filterArgs := buildFilterConditions(params.Filters)
	timeConditions, timeArgs := buildTimeConditions(params)

	// When GroupBy has a "properties.X" entry, return per-group rows so tiered pricing
	// can be applied per group (e.g. per KRN). The events-table SQL only supports
	// a single property dim here; multi-dim grouping is on the bucketed-meter path.
	// 1. per_group CTE: max per group per bucket (e.g., MAX per krn per hour)
	// 2. Return each group's value with group_key; total is sum of all group values for backward compat
	if groupByProperty := events.FirstGroupByProperty(params.GroupBy); groupByProperty != "" && validateGroupByProperty(groupByProperty) == nil {
		// groupByProperty has already been validated by validateGroupByProperty
		// (alphanumeric/underscore/dot only), so it is safe to interpolate directly
		// into the JSONExtractString key argument here — it cannot carry SQL metacharacters.
		groupByExpr := fmt.Sprintf("JSONExtractString(assumeNotNull(properties), '%s')", groupByProperty)

		query := fmt.Sprintf(`
			WITH per_group AS (
				SELECT
					%s as bucket_start,
					%s as group_key,
					max(JSONExtractFloat(assumeNotNull(properties), ?)) as group_value
				FROM events FINAL
				PREWHERE tenant_id = ?
					AND environment_id = ?
					AND event_name = ?
					%s
					%s
					%s
					%s
				GROUP BY bucket_start, group_key
			)
			SELECT
				(SELECT sum(group_value) FROM per_group) as total,
				bucket_start as timestamp,
				group_value as value,
				group_key
			FROM per_group
			ORDER BY bucket_start, group_key
		`,
			bucketWindow,
			groupByExpr,
			externalCustomerFilter,
			customerFilter,
			filterConditions,
			timeConditions)

		args := []interface{}{params.PropertyName, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.EventName}
		args = append(args, customerArgs...)
		args = append(args, filterArgs...)
		args = append(args, timeArgs...)

		return query, args
	}

	// First get max values per bucket, then sum across all buckets
	query := fmt.Sprintf(`
		WITH bucket_maxes AS (
			SELECT
				%s as bucket_start,
				max(JSONExtractFloat(assumeNotNull(properties), ?)) as bucket_max
			FROM events FINAL
			PREWHERE tenant_id = ?
				AND environment_id = ?
				AND event_name = ?
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
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions)

	args := []interface{}{params.PropertyName, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.EventName}
	args = append(args, customerArgs...)
	args = append(args, filterArgs...)
	args = append(args, timeArgs...)

	return query, args
}

func (a *MaxAggregator) GetType() types.AggregationType {
	return types.AggregationMax
}

// WeightedSumAggregator implements weighted sum aggregation
type WeightedSumAggregator struct{}

func (a *WeightedSumAggregator) GetQuery(ctx context.Context, params *events.UsageParams) (string, []interface{}) {
	windowSize := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
	selectClause := ""
	windowClause := ""
	groupByClause := ""

	if windowSize != "" {
		selectClause = "window_size,"
		windowClause = fmt.Sprintf("%s AS window_size,", windowSize)
		groupByClause = "GROUP BY window_size ORDER BY window_size"
	}

	externalCustomerFilter, customerFilter, customerArgs := buildUsageEventCustomerFilters(params)

	filterConditions, filterArgs := buildFilterConditions(params.Filters)
	timeConditions, timeArgs := buildTimeConditions(params)

	query := fmt.Sprintf(`
        WITH
            toDateTime64(?, 3) AS period_start,
            toDateTime64(?, 3) AS period_end,
            dateDiff('second', period_start, period_end) AS total_seconds
        SELECT
            %s sum(
                (JSONExtractFloat(assumeNotNull(properties), ?) / nullIf(total_seconds, 0)) *
                dateDiff('second', timestamp, period_end)
            ) AS total
        FROM (
            SELECT
                %s timestamp,
                properties
            FROM events
            PREWHERE tenant_id = ?
				AND environment_id = ?
				AND event_name = ?
				%s
				%s
                %s
                %s
        )
        %s
    `,
		selectClause,
		windowClause,
		externalCustomerFilter,
		customerFilter,
		filterConditions,
		timeConditions,
		groupByClause,
	)

	args := []interface{}{
		formatClickHouseDateTime(params.StartTime),
		formatClickHouseDateTime(params.EndTime),
		params.PropertyName,
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		params.EventName,
	}
	args = append(args, customerArgs...)
	args = append(args, filterArgs...)
	args = append(args, timeArgs...)

	return query, args
}

func (a *WeightedSumAggregator) GetType() types.AggregationType {
	return types.AggregationWeightedSum
}
