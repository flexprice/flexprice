package clickhouse

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
)

// MeterUsageRepositoryOption configures a MeterUsageRepository.
type MeterUsageRepositoryOption func(*MeterUsageRepository)

// WithRollupTable enables hourly rollup reads.
func WithRollupTable(table string) MeterUsageRepositoryOption {
	return func(r *MeterUsageRepository) {
		if validRollupTable.MatchString(table) {
			r.rollupTable = table
		}
	}
}

var validRollupTable = regexp.MustCompile(`^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+)?$`)

// IsValidRollupTable reports whether table is a safe identifier.
func IsValidRollupTable(table string) bool {
	return validRollupTable.MatchString(table)
}

// rollupPlan splits [start, end) into rollup hours and raw edges.
type rollupPlan struct {
	bucketStart time.Time
	bucketEnd   time.Time
	edges       []events.TimeRange
}

func ceilHour(t time.Time) time.Time {
	h := t.UTC().Truncate(time.Hour)
	if h.Before(t.UTC()) {
		return h.Add(time.Hour)
	}
	return h
}

func floorHour(t time.Time) time.Time {
	return t.UTC().Truncate(time.Hour)
}

// planRollup returns false when the rollup cannot serve params.
func planRollup(params *events.MeterUsageQueryParams) (rollupPlan, bool) {
	if params == nil || params.UseFinal || params.WindowSize != "" ||
		len(params.GroupBy) > 0 || len(params.PropertyFilters) > 0 ||
		len(params.Sources) > 0 || len(params.IDs) > 0 || len(params.TimeRanges) > 1 {
		return rollupPlan{}, false
	}

	start, end := params.StartTime, params.EndTime
	if len(params.TimeRanges) == 1 {
		start, end = params.TimeRanges[0].Start, params.TimeRanges[0].End
	}

	var plan rollupPlan
	if !start.IsZero() {
		plan.bucketStart = ceilHour(start)
	}
	if !end.IsZero() {
		plan.bucketEnd = floorHour(end)
	}
	if !start.IsZero() && !end.IsZero() && !plan.bucketStart.Before(plan.bucketEnd) {
		return rollupPlan{}, false
	}
	if !start.IsZero() && start.UTC().Before(plan.bucketStart) {
		plan.edges = append(plan.edges, events.TimeRange{Start: start.UTC(), End: plan.bucketStart})
	}
	if !end.IsZero() && plan.bucketEnd.Before(end.UTC()) {
		plan.edges = append(plan.edges, events.TimeRange{Start: plan.bucketEnd, End: end.UTC()})
	}
	return plan, true
}

// rollupValueExprs returns rollup and raw value expressions.
func rollupValueExprs(agg types.AggregationType) (rollupExpr, rawExpr string, ok bool) {
	switch agg {
	case types.AggregationSum, types.AggregationSumWithMultiplier, types.AggregationWeightedSum:
		return "toDecimal128(sum(qty_sum), 15)", "toDecimal128(SUM(qty_total), 15)", true
	case types.AggregationCount:
		return "toDecimal128(sum(event_count), 15)", "toDecimal128(COUNT(DISTINCT id), 15)", true
	default:
		return "", "", false
	}
}

// rollupScope builds tenant, customer and meter filters.
func rollupScope(params *events.MeterUsageQueryParams) ([]string, []interface{}) {
	conds := []string{"tenant_id = ?", "environment_id = ?"}
	args := []interface{}{params.TenantID, params.EnvironmentID}

	if params.ExternalCustomerID != "" {
		conds = append(conds, "external_customer_id = ?")
		args = append(args, params.ExternalCustomerID)
	} else if len(params.ExternalCustomerIDs) > 0 {
		conds = append(conds, "external_customer_id IN ("+placeholders(len(params.ExternalCustomerIDs))+")")
		for _, id := range params.ExternalCustomerIDs {
			args = append(args, id)
		}
	}

	if params.MeterID != "" {
		conds = append(conds, "meter_id = ?")
		args = append(args, params.MeterID)
	} else if len(params.MeterIDs) > 0 {
		conds = append(conds, "meter_id IN ("+placeholders(len(params.MeterIDs))+")")
		for _, id := range params.MeterIDs {
			args = append(args, id)
		}
	}
	return conds, args
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// rollupBranches builds the rollup and raw-edge SELECTs.
func rollupBranches(params *events.MeterUsageQueryParams, plan rollupPlan, table, selectCols, rawCols, groupBy string) ([]string, []interface{}) {
	scope, scopeArgs := rollupScope(params)

	bucketConds := append([]string{}, scope...)
	bucketArgs := append([]interface{}{}, scopeArgs...)
	if !plan.bucketStart.IsZero() {
		bucketConds = append(bucketConds, "bucket >= ?")
		bucketArgs = append(bucketArgs, plan.bucketStart)
	}
	if !plan.bucketEnd.IsZero() {
		bucketConds = append(bucketConds, "bucket < ?")
		bucketArgs = append(bucketArgs, plan.bucketEnd)
	}

	branches := []string{fmt.Sprintf("SELECT %s FROM %s WHERE %s%s", selectCols, table, strings.Join(bucketConds, " AND "), groupBy)}
	args := bucketArgs

	if len(plan.edges) > 0 {
		edgeConds := append([]string{}, scope...)
		args = append(args, scopeArgs...)
		ranges := make([]string, 0, len(plan.edges))
		for _, e := range plan.edges {
			ranges = append(ranges, "(timestamp >= ? AND timestamp < ?)")
			args = append(args, e.Start, e.End)
		}
		edgeConds = append(edgeConds, "("+strings.Join(ranges, " OR ")+")")
		branches = append(branches, fmt.Sprintf("SELECT %s FROM meter_usage WHERE %s%s", rawCols, strings.Join(edgeConds, " AND "), groupBy))
	}
	return branches, args
}

// buildRollupScalarQuery returns (value, event_count) for one meter.
func buildRollupScalarQuery(params *events.MeterUsageQueryParams, table string) (string, []interface{}, bool) {
	if table == "" {
		return "", nil, false
	}
	rollupExpr, rawExpr, ok := rollupValueExprs(params.AggregationType)
	if !ok {
		return "", nil, false
	}
	plan, ok := planRollup(params)
	if !ok {
		return "", nil, false
	}
	branches, args := rollupBranches(params, plan, table,
		rollupExpr+" AS v, toUInt64(sum(event_count)) AS n",
		rawExpr+" AS v, toUInt64(COUNT(DISTINCT id)) AS n",
		"")
	query := fmt.Sprintf(`
		SELECT sum(v) AS value, toUInt64(sum(n)) AS event_count
		FROM (%s)
		SETTINGS %s
	`, strings.Join(branches, " UNION ALL "), maxMemoryUsageSetting)
	return query, args, true
}

// buildRollupMultiMeterQuery returns (meter_id, value, event_count) rows.
func buildRollupMultiMeterQuery(params *events.MeterUsageQueryParams, table string) (string, []interface{}, bool) {
	if table == "" || len(params.MeterIDs) == 0 {
		return "", nil, false
	}
	rollupExpr, rawExpr, ok := rollupValueExprs(params.AggregationType)
	if !ok {
		return "", nil, false
	}
	plan, ok := planRollup(params)
	if !ok {
		return "", nil, false
	}
	branches, args := rollupBranches(params, plan, table,
		"meter_id, "+rollupExpr+" AS v, toUInt64(sum(event_count)) AS n",
		"meter_id, "+rawExpr+" AS v, toUInt64(COUNT(DISTINCT id)) AS n",
		" GROUP BY meter_id")
	query := fmt.Sprintf(`
		SELECT meter_id, sum(v) AS value, toUInt64(sum(n)) AS event_count
		FROM (%s)
		GROUP BY meter_id
		SETTINGS %s
	`, strings.Join(branches, " UNION ALL "), maxMemoryUsageSetting)
	return query, args, true
}

// buildRollupDistinctMetersQuery lists meters with usage in range.
func buildRollupDistinctMetersQuery(params *events.MeterUsageQueryParams, table string) (string, []interface{}, bool) {
	if table == "" || params.AggregationType == types.AggregationCountUnique {
		return "", nil, false
	}
	plan, ok := planRollup(params)
	if !ok {
		return "", nil, false
	}
	branches, args := rollupBranches(params, plan, table, "DISTINCT meter_id", "DISTINCT meter_id", "")
	query := fmt.Sprintf(`
		SELECT DISTINCT meter_id
		FROM (%s)
		ORDER BY meter_id
		SETTINGS %s
	`, strings.Join(branches, " UNION ALL "), maxMemoryUsageSetting)
	return query, args, true
}
