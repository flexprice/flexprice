package clickhouse

import (
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/flexprice/flexprice/internal/domain/costanalytics"
	"github.com/samber/lo"
)

// Cost is computed in Decimal256: qty_total (scale 15) × unit price (scale 20) has scale 35, which
// would overflow Decimal128 above about 1,000 units.
const (
	costPriceScale = 20
	costQtyScale   = 15
)

// costSQL is a built cost breakdown query and its named arguments.
type costSQL struct {
	query string
	args  []any
	// keyAliases maps each key used by the query to its column alias in the priced CTE.
	keyAliases map[costanalytics.Key]string
}

// costSQLBuilder builds the cost breakdown query. The inner "priced" CTE costs every event inside a
// line window; the outer levels filter, group, sort, page and total, and never depend on pricing.
type costSQLBuilder struct {
	params *costanalytics.QueryParams
	query  *costanalytics.Query
	args   []any
	keys   []costanalytics.Key // distinct keys in first-use order; alias k<i>
}

func buildCostBreakdownSQL(params *costanalytics.QueryParams) *costSQL {
	b := &costSQLBuilder{params: params, query: params.Query}
	b.collectKeys()

	var sb strings.Builder
	sb.WriteString("WITH\n")
	sb.WriteString(b.linesCTE())
	sb.WriteString(",\n")
	sb.WriteString(b.pricedCTE())
	sb.WriteString(",\n")
	sb.WriteString(b.groupedCTE())
	sb.WriteString("\n")
	sb.WriteString(b.outerSelect())
	if b.query.Deduplicate {
		sb.WriteString("\nSETTINGS do_not_merge_across_partitions_select_final = 1")
	}

	aliases := make(map[costanalytics.Key]string, len(b.keys))
	for i, k := range b.keys {
		aliases[k] = keyAlias(i)
	}
	return &costSQL{query: sb.String(), args: b.args, keyAliases: aliases}
}

func (b *costSQLBuilder) bind(name string, value any) string {
	b.args = append(b.args, clickhouse.Named(name, value))
	return "@" + name
}

func (b *costSQLBuilder) collectKeys() {
	add := func(k costanalytics.Key) {
		if !lo.Contains(b.keys, k) {
			b.keys = append(b.keys, k)
		}
	}
	for _, k := range b.query.GroupBy {
		add(k)
	}
	if b.query.Columns != nil {
		add(*b.query.Columns)
	}
	for _, f := range b.query.Filters {
		add(f.Key)
	}
	for _, m := range b.query.Metrics {
		add(m.Key)
	}
}

func keyAlias(i int) string { return fmt.Sprintf("k%d", i) }

func (b *costSQLBuilder) alias(k costanalytics.Key) string {
	return keyAlias(lo.IndexOf(b.keys, k))
}

// keyExpr reads a key from an event row (mu) joined to its line window (l).
func (b *costSQLBuilder) keyExpr(k costanalytics.Key) string {
	switch k.Kind {
	case costanalytics.KeyProperty:
		// JSONExtractString returns '' for numbers, so non-strings are read raw.
		name := sqlStringLiteral(k.Property)
		return fmt.Sprintf("if(JSONType(mu.properties, %[1]s) = 'String', JSONExtractString(mu.properties, %[1]s), JSONExtractRaw(mu.properties, %[1]s))", name)
	case costanalytics.KeySource:
		return "toString(mu.source)"
	case costanalytics.KeyMeterID:
		return "toString(mu.meter_id)"
	case costanalytics.KeyFeatureID:
		return "l.feature_id"
	case costanalytics.KeySubLineItemID:
		return "l.line_id"
	case costanalytics.KeyMonth:
		return fmt.Sprintf("formatDateTime(mu.timestamp, '%%Y-%%m', %s)", sqlStringLiteral(b.query.Timezone.String()))
	}
	panic(fmt.Sprintf("costanalytics: unhandled key kind %q", k.Kind))
}

// linesCTE expands the line windows into one row per (line × customer).
func (b *costSQLBuilder) linesCTE() string {
	var lineIDs, meterIDs, featureIDs, customerIDs, prices []string
	var starts, ends []int64
	for _, l := range b.params.Lines {
		for _, c := range l.ExternalCustomerIDs {
			lineIDs = append(lineIDs, l.LineItemID)
			meterIDs = append(meterIDs, l.MeterID)
			featureIDs = append(featureIDs, l.FeatureID)
			customerIDs = append(customerIDs, c)
			starts = append(starts, l.Start.Unix())
			ends = append(ends, l.End.Unix())
			prices = append(prices, l.UnitPrice.String())
		}
	}
	return fmt.Sprintf(`lines AS (
    SELECT
        l.1 AS line_id,
        l.2 AS meter_id,
        l.3 AS feature_id,
        l.4 AS customer_id,
        toDateTime(l.5, 'UTC') AS w_start,
        toDateTime(l.6, 'UTC') AS w_end,
        toDecimal256(l.7, %d) AS unit_price
    FROM (SELECT arrayJoin(arrayZip(%s, %s, %s, %s, %s, %s, %s)) AS l)
)`, costPriceScale,
		b.bind("line_ids", lineIDs), b.bind("line_meter_ids", meterIDs), b.bind("line_feature_ids", featureIDs),
		b.bind("line_customer_ids", customerIDs), b.bind("line_starts", starts), b.bind("line_ends", ends),
		b.bind("line_unit_prices", prices))
}

// pricedCTE has one row per event × matching line window, with its cost and every key the query uses.
func (b *costSQLBuilder) pricedCTE() string {
	customers := lo.Uniq(lo.FlatMap(b.params.Lines, func(l costanalytics.LineWindow, _ int) []string { return l.ExternalCustomerIDs }))
	meters := lo.Uniq(lo.Map(b.params.Lines, func(l costanalytics.LineWindow, _ int) string { return l.MeterID }))
	minStart := lo.MinBy(b.params.Lines, func(a, b costanalytics.LineWindow) bool { return a.Start.Before(b.Start) }).Start
	maxEnd := lo.MaxBy(b.params.Lines, func(a, b costanalytics.LineWindow) bool { return a.End.After(b.End) }).End

	cols := []string{
		"mu.qty_total AS qty",
		fmt.Sprintf("toDecimal256(mu.qty_total, %d) * l.unit_price AS cost", costQtyScale),
	}
	for i, k := range b.keys {
		cols = append(cols, fmt.Sprintf("%s AS %s", b.keyExpr(k), keyAlias(i)))
	}

	final := ""
	if b.query.Deduplicate {
		final = " FINAL"
	}
	return fmt.Sprintf(`priced AS (
    SELECT
        %s
    FROM meter_usage AS mu%s
    INNER JOIN lines AS l ON mu.meter_id = l.meter_id AND mu.external_customer_id = l.customer_id
    WHERE mu.tenant_id = %s
      AND mu.environment_id = %s
      AND mu.external_customer_id IN %s
      AND mu.meter_id IN %s
      AND mu.timestamp >= %s
      AND mu.timestamp < %s
      AND mu.timestamp >= l.w_start
      AND mu.timestamp < l.w_end
)`, strings.Join(cols, ",\n        "), final,
		b.bind("tenant_id", b.params.TenantID), b.bind("environment_id", b.params.EnvironmentID),
		b.bind("customer_ids", customers), b.bind("meter_ids", meters),
		b.bind("min_start", minStart.UTC().Truncate(time.Second)), b.bind("max_end", maxEnd.UTC().Truncate(time.Second)))
}

// groupedCTE filters the priced events, then aggregates per group.
func (b *costSQLBuilder) groupedCTE() string {
	groupCols := b.groupAliases()
	cols := append([]string{}, groupCols...)
	cols = append(cols, "sum(cost) AS g_cost", "sum(qty) AS g_qty", "count() AS g_events")
	if b.query.Columns != nil {
		cols = append(cols, fmt.Sprintf("sumMap(map(%s, cost)) AS g_columns", b.alias(*b.query.Columns)))
	}
	for i, m := range b.query.Metrics {
		a := b.alias(m.Key)
		cols = append(cols,
			fmt.Sprintf("uniqExactIf(%[1]s, %[1]s != '') AS m%[2]d_count", a, i),
			fmt.Sprintf("groupUniqArrayIf(%[3]d)(%[1]s, %[1]s != '') AS m%[2]d_values", a, i, costanalytics.MaxDistinctValues+1))
	}

	var conds []string
	for i, f := range b.query.Filters {
		cond := fmt.Sprintf("has(%s, %s)", b.bind(fmt.Sprintf("filter_%d", i), f.Values), b.alias(f.Key))
		if f.Op == costanalytics.FilterNotIn {
			cond = "NOT " + cond
		}
		conds = append(conds, cond)
	}
	if b.query.Search != "" {
		search := b.bind("search", b.query.Search)
		matches := lo.Map(groupCols, func(a string, _ int) string {
			return fmt.Sprintf("positionCaseInsensitiveUTF8(%s, %s) > 0", a, search)
		})
		conds = append(conds, "("+strings.Join(matches, " OR ")+")")
	}
	where := ""
	if len(conds) > 0 {
		where = "\n    WHERE " + strings.Join(conds, "\n      AND ")
	}

	return fmt.Sprintf(`grouped AS (
    SELECT
        %s
    FROM priced%s
    GROUP BY %s
)`, strings.Join(cols, ",\n        "), where, strings.Join(groupCols, ", "))
}

func (b *costSQLBuilder) groupAliases() []string {
	return lo.Map(b.query.GroupBy, func(k costanalytics.Key, _ int) string { return b.alias(k) })
}

// outerSelect returns the page of groups, with totals over every group as window aggregates so rows,
// totals and the count come from the same read.
func (b *costSQLBuilder) outerSelect() string {
	groupCols := b.groupAliases()
	cols := append([]string{}, groupCols...)
	cols = append(cols, "toString(g_cost)", "toString(g_qty)", "g_events")
	if b.query.Columns != nil {
		cols = append(cols, "mapApply((k, v) -> (k, toString(v)), g_columns)")
	}
	for i := range b.query.Metrics {
		cols = append(cols, fmt.Sprintf("m%d_count", i), fmt.Sprintf("m%d_values", i))
	}
	cols = append(cols,
		"count() OVER () AS total_count",
		"toString(sum(g_cost) OVER ())",
		"toString(sum(g_qty) OVER ())",
		"sum(g_events) OVER ()")
	if b.query.Columns != nil {
		cols = append(cols, "mapApply((k, v) -> (k, toString(v)), sumMap(g_columns) OVER ())")
	}

	return fmt.Sprintf(`SELECT
    %s
FROM grouped
ORDER BY %s
LIMIT %s OFFSET %s`, strings.Join(cols, ",\n    "), strings.Join(b.orderBy(groupCols), ", "),
		b.bind("limit", b.query.Limit), b.bind("offset", b.query.Offset))
}

// orderBy sorts by the requested field, then by the group values so equal rows keep a stable order.
func (b *costSQLBuilder) orderBy(groupCols []string) []string {
	dir := "ASC"
	if b.query.Sort.Desc {
		dir = "DESC"
	}
	var primary string
	switch b.query.Sort.Field {
	case costanalytics.SortTotal:
		primary = "g_cost"
	case costanalytics.SortUsage:
		primary = "g_qty"
	case costanalytics.SortEventCount:
		primary = "g_events"
	case costanalytics.SortColumn:
		primary = fmt.Sprintf("g_columns[%s]", b.bind("sort_column", b.query.Sort.Column))
	case costanalytics.SortName:
		return lo.Map(groupCols, func(a string, _ int) string { return a + " " + dir })
	}
	return append([]string{primary + " " + dir}, lo.Map(groupCols, func(a string, _ int) string { return a + " ASC" })...)
}

// sqlStringLiteral quotes s as a ClickHouse string literal.
func sqlStringLiteral(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + r.Replace(s) + "'"
}
