package clickhouse

import (
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/flexprice/flexprice/internal/domain/usageanalytics"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// Amount is computed in Decimal256: qty_total (scale 15) × unit price (scale 20) has scale 35, which
// would overflow Decimal128 above about 1,000 units.
const (
	usagePriceScale = 20
	usageQtyScale   = 15
)

// breakdownSQL is a built usage breakdown query and its named arguments.
type breakdownSQL struct {
	query string
	args  []any
	// keyAliases maps each key used by the query to its column alias in the priced CTE.
	keyAliases map[usageanalytics.Key]string
}

// breakdownSQLBuilder builds the usage breakdown query. The inner "priced" CTE prices every event inside a
// line window; the outer levels filter, group, sort, page and total, and never depend on pricing.
type breakdownSQLBuilder struct {
	params *usageanalytics.QueryParams
	query  *usageanalytics.Query
	args   []any
	keys   []usageanalytics.Key // distinct keys in first-use order; alias k<i>
	window bool                 // priced rows carry their time window as w
}

func buildBreakdownSQL(params *usageanalytics.QueryParams) *breakdownSQL {
	b := &breakdownSQLBuilder{params: params, query: params.Query}
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
	sb.WriteString(b.settings())

	return b.result(sb.String())
}

// buildPointsSQL returns amount per window for each page row (pageGroups, in GroupBy order) and,
// marked is_total, per window over every row that passes the filters.
func buildPointsSQL(params *usageanalytics.QueryParams, pageGroups [][]string) *breakdownSQL {
	b := &breakdownSQLBuilder{params: params, query: params.Query, window: true}
	b.collectKeys()
	groupCols := b.groupAliases()

	var sb strings.Builder
	sb.WriteString("WITH\n")
	sb.WriteString(b.linesCTE())
	sb.WriteString(",\n")
	sb.WriteString(b.pricedCTE())
	sb.WriteString(",\n")
	sb.WriteString(fmt.Sprintf("filtered AS (\n    SELECT *\n    FROM priced%s\n)\n", b.filterWhere(groupCols)))

	var parts []string
	if len(pageGroups) > 0 {
		parts = append(parts, fmt.Sprintf(`SELECT 0 AS is_total, %[1]s, toString(w), toString(sum(amount)), toString(sum(subtotal)), toString(sum(qty))
FROM filtered
WHERE has(%[2]s, [%[1]s])
GROUP BY %[1]s, w`, strings.Join(groupCols, ", "), b.bind("page_groups", pageGroups)))
	}
	blanks := strings.Join(lo.Map(groupCols, func(a string, _ int) string { return "'' AS " + a }), ", ")
	parts = append(parts, fmt.Sprintf(`SELECT 1 AS is_total, %s, toString(w), toString(sum(amount)), toString(sum(subtotal)), toString(sum(qty))
FROM filtered
GROUP BY w`, blanks))
	sb.WriteString(strings.Join(parts, "\nUNION ALL\n"))
	sb.WriteString(b.settings())
	return b.result(sb.String())
}

func (b *breakdownSQLBuilder) result(query string) *breakdownSQL {
	aliases := make(map[usageanalytics.Key]string, len(b.keys))
	for i, k := range b.keys {
		aliases[k] = keyAlias(i)
	}
	return &breakdownSQL{query: query, args: b.args, keyAliases: aliases}
}

// windowExpr is the local date that starts the event's window, in the query timezone.
func (b *breakdownSQLBuilder) windowExpr() string {
	day := fmt.Sprintf("toDate(mu.timestamp, %s)", sqlStringLiteral(b.query.Timezone.String()))
	switch b.query.WindowSize {
	case types.WindowSizeWeek:
		return fmt.Sprintf("toStartOfWeek(%s, 0)", day) // Sunday, as in usage analytics
	case types.WindowSizeMonth:
		return fmt.Sprintf("toStartOfMonth(%s)", day)
	}
	return day
}

// settings caps memory like every meter_usage query and, with dedupe, keeps FINAL within partitions.
func (b *breakdownSQLBuilder) settings() string {
	if b.query.Deduplicate {
		return "\nSETTINGS " + maxMemoryUsageSetting + ", do_not_merge_across_partitions_select_final = 1"
	}
	return "\nSETTINGS " + maxMemoryUsageSetting
}

func (b *breakdownSQLBuilder) bind(name string, value any) string {
	b.args = append(b.args, clickhouse.Named(name, value))
	return "@" + name
}

func (b *breakdownSQLBuilder) collectKeys() {
	add := func(k usageanalytics.Key) {
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

func (b *breakdownSQLBuilder) alias(k usageanalytics.Key) string {
	return keyAlias(lo.IndexOf(b.keys, k))
}

// keyExpr reads a key from an event row (mu) joined to its line window (l).
func (b *breakdownSQLBuilder) keyExpr(k usageanalytics.Key) string {
	switch k.Kind {
	case usageanalytics.KeyProperty:
		// JSONExtractString returns '' for numbers, so non-strings are read raw.
		name := sqlStringLiteral(k.Property)
		return fmt.Sprintf("if(JSONType(mu.properties, %[1]s) = 'String', JSONExtractString(mu.properties, %[1]s), JSONExtractRaw(mu.properties, %[1]s))", name)
	case usageanalytics.KeySource:
		return "toString(mu.source)"
	case usageanalytics.KeyMeterID:
		return "toString(mu.meter_id)"
	case usageanalytics.KeyFeatureID:
		return "l.feature_id"
	case usageanalytics.KeySubLineItemID:
		return "l.line_id"
	case usageanalytics.KeyMonth:
		return fmt.Sprintf("formatDateTime(mu.timestamp, '%%Y-%%m', %s)", sqlStringLiteral(b.query.Timezone.String()))
	}
	panic(fmt.Sprintf("usageanalytics: unhandled key kind %q", k.Kind))
}

// linesCTE expands the line windows into one row per (line × customer).
func (b *breakdownSQLBuilder) linesCTE() string {
	var lineIDs, meterIDs, featureIDs, customerIDs, prices, listPrices []string
	var starts, ends []int64
	for _, l := range b.params.Lines {
		for _, c := range lo.Uniq(l.ExternalCustomerIDs) {
			lineIDs = append(lineIDs, l.LineItemID)
			meterIDs = append(meterIDs, l.MeterID)
			featureIDs = append(featureIDs, l.FeatureID)
			customerIDs = append(customerIDs, c)
			starts = append(starts, l.Start.Unix())
			ends = append(ends, l.End.Unix())
			prices = append(prices, l.UnitPrice.String())
			listPrices = append(listPrices, l.ListUnitPrice.String())
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
        toDecimal256(l.7, %[1]d) AS unit_price,
        toDecimal256(l.8, %[1]d) AS list_unit_price
    FROM (SELECT arrayJoin(arrayZip(%[2]s, %[3]s, %[4]s, %[5]s, %[6]s, %[7]s, %[8]s, %[9]s)) AS l)
)`, usagePriceScale,
		b.bind("line_ids", lineIDs), b.bind("line_meter_ids", meterIDs), b.bind("line_feature_ids", featureIDs),
		b.bind("line_customer_ids", customerIDs), b.bind("line_starts", starts), b.bind("line_ends", ends),
		b.bind("line_unit_prices", prices), b.bind("line_list_unit_prices", listPrices))
}

// pricedCTE has one row per event × matching line window, with its amount and every key the query uses.
func (b *breakdownSQLBuilder) pricedCTE() string {
	customers := lo.Uniq(lo.FlatMap(b.params.Lines, func(l usageanalytics.LineWindow, _ int) []string { return l.ExternalCustomerIDs }))
	meters := lo.Uniq(lo.Map(b.params.Lines, func(l usageanalytics.LineWindow, _ int) string { return l.MeterID }))
	minStart := lo.MinBy(b.params.Lines, func(a, b usageanalytics.LineWindow) bool { return a.Start.Before(b.Start) }).Start
	maxEnd := lo.MaxBy(b.params.Lines, func(a, b usageanalytics.LineWindow) bool { return a.End.After(b.End) }).End

	cols := []string{
		"mu.qty_total AS qty",
		"l.line_id AS line_id",
		fmt.Sprintf("toDecimal256(mu.qty_total, %d) * l.unit_price AS amount", usageQtyScale),
		fmt.Sprintf("toDecimal256(mu.qty_total, %d) * l.list_unit_price AS subtotal", usageQtyScale),
	}
	for i, k := range b.keys {
		cols = append(cols, fmt.Sprintf("%s AS %s", b.keyExpr(k), keyAlias(i)))
	}
	if b.window {
		cols = append(cols, b.windowExpr()+" AS w")
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
func (b *breakdownSQLBuilder) groupedCTE() string {
	groupCols := b.groupAliases()
	cols := append([]string{}, groupCols...)
	cols = append(cols, "sum(amount) AS g_amount", "sum(subtotal) AS g_subtotal", "sum(qty) AS g_qty", "count() AS g_events")
	if b.query.Columns != nil {
		cols = append(cols, fmt.Sprintf("sumMap(map(%s, amount)) AS g_columns", b.alias(*b.query.Columns)))
	}
	if b.query.WithLineItemIDs {
		cols = append(cols, "groupUniqArray(line_id) AS g_line_ids")
	}
	for i, m := range b.query.Metrics {
		a := b.alias(m.Key)
		cols = append(cols,
			fmt.Sprintf("uniqExactIf(%[1]s, %[1]s != '') AS m%[2]d_count", a, i),
			fmt.Sprintf("groupUniqArrayIf(%[3]d)(%[1]s, %[1]s != '') AS m%[2]d_values", a, i, usageanalytics.MaxDistinctValues+1))
	}

	return fmt.Sprintf(`grouped AS (
    SELECT
        %s
    FROM priced%s
    GROUP BY %s
)`, strings.Join(cols, ",\n        "), b.filterWhere(groupCols), strings.Join(groupCols, ", "))
}

// filterWhere is the WHERE clause applying the filters and search to priced rows, or "".
func (b *breakdownSQLBuilder) filterWhere(groupCols []string) string {
	var conds []string
	for i, f := range b.query.Filters {
		cond := fmt.Sprintf("has(%s, %s)", b.bind(fmt.Sprintf("filter_%d", i), f.Values), b.alias(f.Key))
		if f.Op == usageanalytics.FilterNotIn {
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
	if len(conds) == 0 {
		return ""
	}
	return "\n    WHERE " + strings.Join(conds, "\n      AND ")
}

func (b *breakdownSQLBuilder) groupAliases() []string {
	return lo.Map(b.query.GroupBy, func(k usageanalytics.Key, _ int) string { return b.alias(k) })
}

// outerSelect returns the page of groups, with totals over every group as window aggregates so rows,
// totals and the count come from the same read.
func (b *breakdownSQLBuilder) outerSelect() string {
	groupCols := b.groupAliases()
	cols := append([]string{}, groupCols...)
	cols = append(cols, "toString(g_amount)", "toString(g_subtotal)", "toString(g_qty)", "g_events")
	if b.query.Columns != nil {
		cols = append(cols, "mapApply((k, v) -> (k, toString(v)), g_columns)")
	}
	if b.query.WithLineItemIDs {
		cols = append(cols, "g_line_ids")
	}
	for i := range b.query.Metrics {
		cols = append(cols, fmt.Sprintf("m%d_count", i), fmt.Sprintf("m%d_values", i))
	}
	cols = append(cols,
		"count() OVER () AS total_count",
		"toString(sum(g_amount) OVER ())",
		"toString(sum(g_subtotal) OVER ())",
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
func (b *breakdownSQLBuilder) orderBy(groupCols []string) []string {
	dir := "ASC"
	if b.query.Sort.Desc {
		dir = "DESC"
	}
	var primary string
	switch b.query.Sort.Field {
	case usageanalytics.SortAmount:
		primary = "g_amount"
	case usageanalytics.SortUsage:
		primary = "g_qty"
	case usageanalytics.SortEventCount:
		primary = "g_events"
	case usageanalytics.SortColumn:
		primary = fmt.Sprintf("g_columns[%s]", b.bind("sort_column", b.query.Sort.Column))
	case usageanalytics.SortName:
		return lo.Map(groupCols, func(a string, _ int) string { return a + " " + dir })
	}
	return append([]string{primary + " " + dir}, lo.Map(groupCols, func(a string, _ int) string { return a + " ASC" })...)
}

// sqlStringLiteral quotes s as a ClickHouse string literal.
func sqlStringLiteral(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + r.Replace(s) + "'"
}
