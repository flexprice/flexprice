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
			r.rollup.hourly = table
		}
	}
}

// WithDailyRollupTable enables the daily tier.
func WithDailyRollupTable(table string) MeterUsageRepositoryOption {
	return func(r *MeterUsageRepository) {
		if validRollupTable.MatchString(table) {
			r.rollup.daily = table
		}
	}
}

// rollupTables names the rollup tiers.
type rollupTables struct {
	hourly string
	daily  string
}

var validRollupTable = regexp.MustCompile(`^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+)?$`)

// IsValidRollupTable checks the table identifier.
func IsValidRollupTable(table string) bool {
	return validRollupTable.MatchString(table)
}

// rollupPlan holds rollup hours and edges.
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

const oneDay = 24 * time.Hour

func ceilDay(t time.Time) time.Time {
	d := t.UTC().Truncate(oneDay)
	if d.Before(t.UTC()) {
		return d.Add(oneDay)
	}
	return d
}

func floorDay(t time.Time) time.Time {
	return t.UTC().Truncate(oneDay)
}

// tierSplit divides rollup hours into tiers.
type tierSplit struct {
	dayStart time.Time
	dayEnd   time.Time
	hours    []events.TimeRange
}

// splitDays reports whether a full day exists.
func splitDays(plan rollupPlan) (tierSplit, bool) {
	var s tierSplit
	if !plan.bucketStart.IsZero() {
		s.dayStart = ceilDay(plan.bucketStart)
	}
	if !plan.bucketEnd.IsZero() {
		s.dayEnd = floorDay(plan.bucketEnd)
	}
	if !plan.bucketStart.IsZero() && !plan.bucketEnd.IsZero() && !s.dayStart.Before(s.dayEnd) {
		return tierSplit{}, false
	}
	if plan.bucketStart.Before(s.dayStart) {
		s.hours = append(s.hours, events.TimeRange{Start: plan.bucketStart, End: s.dayStart})
	}
	if !plan.bucketEnd.IsZero() && s.dayEnd.Before(plan.bucketEnd) {
		s.hours = append(s.hours, events.TimeRange{Start: s.dayEnd, End: plan.bucketEnd})
	}
	return s, true
}

// planRollup reports whether rollup applies.
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

// rollupValueExprs returns rollup and raw expressions.
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

// rollupScope builds tenant/customer/meter filters.
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

// rollupBranches builds rollup and edge SELECTs.
func rollupBranches(params *events.MeterUsageQueryParams, plan rollupPlan, tables rollupTables, selectCols, rawCols, groupBy string) ([]string, []interface{}) {
	scope, scopeArgs := rollupScope(params)

	var branches []string
	var args []interface{}
	if split, ok := splitDays(plan); tables.daily != "" && ok {
		branches, args = tieredBranches(scope, scopeArgs, split, tables, selectCols, groupBy)
	} else {
		branches, args = hourlyBranch(scope, scopeArgs, plan, tables.hourly, selectCols, groupBy)
	}

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

func hourlyBranch(scope []string, scopeArgs []interface{}, plan rollupPlan, table, selectCols, groupBy string) ([]string, []interface{}) {
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

	return []string{fmt.Sprintf("SELECT %s FROM %s WHERE %s%s", selectCols, table, strings.Join(bucketConds, " AND "), groupBy)}, bucketArgs
}

func tieredBranches(scope []string, scopeArgs []interface{}, split tierSplit, tables rollupTables, selectCols, groupBy string) ([]string, []interface{}) {
	dayConds := append([]string{}, scope...)
	args := append([]interface{}{}, scopeArgs...)
	if !split.dayStart.IsZero() {
		dayConds = append(dayConds, "bucket >= ?")
		args = append(args, split.dayStart)
	}
	if !split.dayEnd.IsZero() {
		dayConds = append(dayConds, "bucket < ?")
		args = append(args, split.dayEnd)
	}
	branches := []string{fmt.Sprintf("SELECT %s FROM %s WHERE %s%s", selectCols, tables.daily, strings.Join(dayConds, " AND "), groupBy)}

	if len(split.hours) > 0 {
		hourConds := append([]string{}, scope...)
		args = append(args, scopeArgs...)
		ranges := make([]string, 0, len(split.hours))
		for _, h := range split.hours {
			ranges = append(ranges, "(bucket >= ? AND bucket < ?)")
			args = append(args, h.Start, h.End)
		}
		hourConds = append(hourConds, "("+strings.Join(ranges, " OR ")+")")
		branches = append(branches, fmt.Sprintf("SELECT %s FROM %s WHERE %s%s", selectCols, tables.hourly, strings.Join(hourConds, " AND "), groupBy))
	}
	return branches, args
}

// buildRollupScalarQuery handles one meter.
func buildRollupScalarQuery(params *events.MeterUsageQueryParams, table string) (string, []interface{}, bool) {
	return buildTieredScalarQuery(params, rollupTables{hourly: table})
}

func buildTieredScalarQuery(params *events.MeterUsageQueryParams, tables rollupTables) (string, []interface{}, bool) {
	if tables.hourly == "" {
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
	branches, args := rollupBranches(params, plan, tables,
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
	return buildTieredMultiMeterQuery(params, rollupTables{hourly: table})
}

func buildTieredMultiMeterQuery(params *events.MeterUsageQueryParams, tables rollupTables) (string, []interface{}, bool) {
	if tables.hourly == "" || len(params.MeterIDs) == 0 {
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
	branches, args := rollupBranches(params, plan, tables,
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

// buildRollupDistinctMetersQuery lists meters with usage.
func buildRollupDistinctMetersQuery(params *events.MeterUsageQueryParams, table string) (string, []interface{}, bool) {
	return buildTieredDistinctMetersQuery(params, rollupTables{hourly: table})
}

func buildTieredDistinctMetersQuery(params *events.MeterUsageQueryParams, tables rollupTables) (string, []interface{}, bool) {
	if tables.hourly == "" || params.AggregationType == types.AggregationCountUnique {
		return "", nil, false
	}
	plan, ok := planRollup(params)
	if !ok {
		return "", nil, false
	}
	branches, args := rollupBranches(params, plan, tables, "DISTINCT meter_id", "DISTINCT meter_id", "")
	query := fmt.Sprintf(`
		SELECT DISTINCT meter_id
		FROM (%s)
		ORDER BY meter_id
		SETTINGS %s
	`, strings.Join(branches, " UNION ALL "), maxMemoryUsageSetting)
	return query, args, true
}
