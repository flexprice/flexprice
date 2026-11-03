package clickhouse

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// analyticsV2Scale is the decimal scale analytics v2 money math is rounded to.
const analyticsV2Scale = 18

// analyticsV2Query collects SQL text and its values as typed server-side parameters, so no value is ever
// written into the SQL and large arrays (line windows, rates, page rows) don't count against the query size.
type analyticsV2Query struct {
	sql    strings.Builder
	params ch.Parameters
}

// add appends sql, replacing each "?" with a typed parameter for the next arg.
func (q *analyticsV2Query) add(sql string, args ...interface{}) {
	for _, arg := range args {
		i := strings.Index(sql, "?")
		if i < 0 {
			panic("analytics v2 query: more args than placeholders")
		}
		q.sql.WriteString(sql[:i])
		q.sql.WriteString(q.value(arg))
		sql = sql[i+1:]
	}
	q.sql.WriteString(sql)
}

// value registers one Go value as a parameter of the matching ClickHouse type.
func (q *analyticsV2Query) value(arg interface{}) string {
	switch v := arg.(type) {
	case string:
		return q.param("String", strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`).Replace(v))
	case []string:
		return q.param("Array(String)", analyticsV2StringArray(v))
	case time.Time:
		return q.param("DateTime('UTC')", v.UTC().Format("2006-01-02 15:04:05"))
	default:
		panic(fmt.Sprintf("analytics v2 query: unsupported parameter type %T", arg))
	}
}

// param registers a server-side parameter and returns its placeholder, e.g. {p3:Array(String)}.
func (q *analyticsV2Query) param(typ, value string) string {
	if q.params == nil {
		q.params = ch.Parameters{}
	}
	name := fmt.Sprintf("p%d", len(q.params))
	q.params[name] = value
	return "{" + name + ":" + typ + "}"
}

func (q *analyticsV2Query) context(ctx context.Context) context.Context {
	if len(q.params) == 0 {
		return ctx
	}
	return ch.Context(ctx, ch.WithParameters(q.params))
}

// analyticsV2StringArray formats values as a ClickHouse array literal for a parameter.
func analyticsV2StringArray(values []string) string {
	quoted := lo.Map(values, func(v string, _ int) string {
		return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
	})
	return "[" + strings.Join(quoted, ",") + "]"
}

func analyticsV2TimeArray(values []time.Time) string {
	return analyticsV2StringArray(lo.Map(values, func(t time.Time, _ int) string { return t.UTC().Format("2006-01-02 15:04:05") }))
}

func analyticsV2NumberArray[T any](values []T) string {
	return "[" + strings.Join(lo.Map(values, func(v T, _ int) string { return fmt.Sprint(v) }), ",") + "]"
}

// analyticsV2Layers splits lines so that no two lines of the same meter overlap within a layer.
// Each layer is joined to events separately, so an event inside two overlapping lines counts for both.
func analyticsV2Layers(lines []events.AnalyticsV2Line) [][]int {
	order := make([]int, len(lines))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return lines[order[a]].Start.Before(lines[order[b]].Start) })

	var layers [][]int
	// layerEnd[l][meter] is the latest end of that meter's lines already placed in layer l.
	var layerEnd []map[string]time.Time
	for _, i := range order {
		line := lines[i]
		placed := false
		for l := range layers {
			if end, ok := layerEnd[l][line.MeterID]; ok && line.Start.Before(end) {
				continue
			}
			layers[l] = append(layers[l], i)
			layerEnd[l][line.MeterID] = line.End
			placed = true
			break
		}
		if !placed {
			layers = append(layers, []int{i})
			layerEnd = append(layerEnd, map[string]time.Time{line.MeterID: line.End})
		}
	}
	return layers
}

// analyticsV2Settings bounds every analytics v2 query's memory and, with FINAL, keeps the merge per partition.
func analyticsV2Settings(useFinal bool) (finalClause, settings string) {
	if useFinal {
		return "FINAL", "SETTINGS " + maxMemoryUsageSetting + ", do_not_merge_across_partitions_select_final = 1"
	}
	return "", "SETTINGS " + maxMemoryUsageSetting
}

// analyticsV2Lines picks which lines a scan joins events to; idx keeps each line's position in the request.
func analyticsV2Lines(lines []events.AnalyticsV2Line, keep func(events.AnalyticsV2Line) bool) []int {
	var out []int
	for i, l := range lines {
		if keep == nil || keep(l) {
			out = append(out, i)
		}
	}
	return out
}

// writeAnalyticsV2Events writes the events-to-lines join for the lines at include: one UNION ALL branch per
// layer, each event tagged with its line index (1-based, for ClickHouse arrays). selectExprs see the event as
// e and the line as r.
func writeAnalyticsV2Events(
	q *analyticsV2Query,
	scope events.AnalyticsV2Scope,
	lines []events.AnalyticsV2Line,
	include []int,
	from, to time.Time,
	scanExtra string,
	selectExprs func(q *analyticsV2Query),
	eventFilters func(q *analyticsV2Query),
	joinedFilters func(q *analyticsV2Query),
) {
	finalClause, _ := analyticsV2Settings(scope.UseFinal)
	subset := lo.Map(include, func(i int, _ int) events.AnalyticsV2Line { return lines[i] })
	meterIDs := q.param("Array(String)", analyticsV2StringArray(lo.Uniq(lo.Map(subset, func(l events.AnalyticsV2Line, _ int) string { return l.MeterID }))))

	for n, layer := range analyticsV2Layers(subset) {
		if n > 0 {
			q.add("\n\t\tUNION ALL\n")
		}
		meters := make([]string, len(layer))
		starts := make([]time.Time, len(layer))
		ends := make([]time.Time, len(layer))
		idx := make([]uint32, len(layer))
		lineIDs := make([]string, len(layer))
		for k, s := range layer {
			i := include[s]
			meters[k], starts[k], ends[k] = lines[i].MeterID, lines[i].Start, lines[i].End
			idx[k], lineIDs[k] = uint32(i+1), lines[i].ID
		}

		q.add("\t\tSELECT ")
		selectExprs(q)
		q.add(fmt.Sprintf(`
		FROM (
			SELECT meter_id, timestamp, qty_total, source, properties%s
			FROM meter_usage %s
			WHERE tenant_id = ? AND environment_id = ? AND external_customer_id IN ?
				AND meter_id IN %s AND timestamp >= ? AND timestamp < ? AND ingested_at <= ?`, scanExtra, finalClause, meterIDs),
			scope.TenantID, scope.EnvironmentID, scope.ExternalCustomerIDs,
			from.UTC(), to.UTC(), scope.DataThrough.UTC())
		if eventFilters != nil {
			eventFilters(q)
		}
		q.add(fmt.Sprintf(`
		) AS e
		ASOF INNER JOIN (
			SELECT t.1 AS meter_id, t.2 AS start_time, t.3 AS end_time, t.4 AS idx, t.5 AS line_id
			FROM (SELECT arrayJoin(arrayZip(%s, %s, %s, %s, %s)) AS t)
		) AS r ON e.meter_id = r.meter_id AND e.timestamp >= r.start_time
		WHERE e.timestamp < r.end_time`,
			q.param("Array(String)", analyticsV2StringArray(meters)),
			q.param("Array(DateTime('UTC'))", analyticsV2TimeArray(starts)),
			q.param("Array(DateTime('UTC'))", analyticsV2TimeArray(ends)),
			q.param("Array(UInt32)", analyticsV2NumberArray(idx)),
			q.param("Array(String)", analyticsV2StringArray(lineIDs))))
		if joinedFilters != nil {
			joinedFilters(q)
		}
	}
}

func analyticsV2Window(lines []events.AnalyticsV2Line) (time.Time, time.Time) {
	from, to := lines[0].Start, lines[0].End
	for _, l := range lines[1:] {
		from, to = lo.Earliest(from, l.Start), lo.Latest(to, l.End)
	}
	return from, to
}

func (r *MeterUsageRepository) GetAnalyticsV2LineUsage(ctx context.Context, params *events.AnalyticsV2LineUsageParams) ([]events.AnalyticsV2LineUsage, error) {
	if params == nil || len(params.Lines) == 0 {
		return nil, nil
	}
	span := StartRepositorySpan(ctx, "meter_usage", "get_analytics_v2_line_usage", map[string]interface{}{
		"lines": len(params.Lines),
	})
	defer FinishSpan(span)

	from, to := analyticsV2Window(params.Lines)
	_, settings := analyticsV2Settings(params.Scope.UseFinal)
	q := &analyticsV2Query{}
	q.add("SELECT idx, sum(qty_total) AS quantity, sumIf(qty_total, ts >= ? AND ts < ?) AS range_quantity, count() AS events\n\tFROM (\n",
		params.RangeStart.UTC(), params.RangeEnd.UTC())
	writeAnalyticsV2Events(q, params.Scope, params.Lines, analyticsV2Lines(params.Lines, nil), from, to, "",
		func(q *analyticsV2Query) { q.add("r.idx AS idx, e.qty_total AS qty_total, e.timestamp AS ts") }, nil, nil)
	q.add("\n\t)\n\tGROUP BY idx\n\t" + settings)

	rows, err := r.store.GetConn().Query(q.context(ctx), q.sql.String())
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to query analytics line usage").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	out := make([]events.AnalyticsV2LineUsage, len(params.Lines))
	for rows.Next() {
		var idx uint32
		var quantity, rangeQuantity decimal.Decimal
		var n uint64
		if err := rows.Scan(&idx, &quantity, &rangeQuantity, &n); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).WithHint("Failed to scan analytics line usage").Mark(ierr.ErrDatabase)
		}
		out[idx-1] = events.AnalyticsV2LineUsage{Quantity: quantity, RangeQuantity: rangeQuantity, EventCount: n}
	}
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to read analytics line usage").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return out, nil
}

// analyticsV2KeyExpr returns the SQL for a row, column or filter key. prefix is "e." after the join
// and "" inside the event scan; sub_line_item_id reads the joined line r and is only valid after the join.
func analyticsV2KeyExpr(q *analyticsV2Query, key, prefix string, params *events.AnalyticsV2RankParams) {
	switch {
	case key == "source":
		q.add(prefix + "source")
	case key == "meter_id":
		q.add(prefix + "meter_id")
	case key == "sub_line_item_id":
		q.add("r.line_id")
	case key == "feature_id":
		meters := lo.Keys(params.MeterFeatures)
		sort.Strings(meters)
		features := lo.Map(meters, func(m string, _ int) string { return params.MeterFeatures[m] })
		q.add("transform("+prefix+"meter_id, ?, ?, '')", meters, features)
	case key == events.AnalyticsV2KeyMonth:
		q.add("formatDateTime(toTimeZone("+prefix+"timestamp, ?), '%Y-%m')", lo.Ternary(params.Timezone == "", "UTC", params.Timezone))
	default:
		q.add("JSONExtractString("+prefix+"properties, ?)", strings.TrimPrefix(key, "properties."))
	}
}

// analyticsV2ScanFilters pushes line-independent filters into the event scan.
func analyticsV2ScanFilters(params *events.AnalyticsV2RankParams) func(q *analyticsV2Query) {
	return func(q *analyticsV2Query) {
		p := *params
		p.Filters = lo.Filter(params.Filters, func(f events.AnalyticsV2Filter, _ int) bool { return f.Key != "sub_line_item_id" })
		analyticsV2Filters(q, "", &p)
	}
}

// analyticsV2JoinedFilters applies line filters after events are joined to their lines.
func analyticsV2JoinedFilters(params *events.AnalyticsV2RankParams) func(q *analyticsV2Query) {
	return func(q *analyticsV2Query) {
		p := *params
		p.Filters = lo.Filter(params.Filters, func(f events.AnalyticsV2Filter, _ int) bool { return f.Key == "sub_line_item_id" })
		analyticsV2Filters(q, "e.", &p)
	}
}

func analyticsV2Filters(q *analyticsV2Query, prefix string, params *events.AnalyticsV2RankParams) {
	for _, f := range params.Filters {
		q.add("\n\t\t\tAND ")
		analyticsV2KeyExpr(q, f.Key, prefix, params)
		q.add(lo.Ternary(f.Exclude, " NOT IN ?", " IN ?"), f.Values)
	}
}

// analyticsV2Components are the per-line rate arrays, in the order the queries read them.
var analyticsV2Components = []struct {
	rate, out string
	pick      func(events.AnalyticsV2Amounts) decimal.Decimal
}{
	{"r_usage", "usage", func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.Usage }},
	{"r_utilized", "utilized", func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.CommitmentUtilized }},
	{"r_overage", "overage", func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.Overage }},
	{"r_true_up", "true_up", func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.TrueUp }},
	{"r_discount", "discount", func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.Discount }},
	{"r_free", "free", func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.Free }},
}

// writeAnalyticsV2Rates writes the WITH clause binding each rate array as a constant parameter.
func writeAnalyticsV2Rates(q *analyticsV2Query, rates []events.AnalyticsV2Amounts) {
	q.add("WITH\n")
	for i, c := range analyticsV2Components {
		values := lo.Map(rates, func(a events.AnalyticsV2Amounts, _ int) string { return c.pick(a).String() })
		q.add(fmt.Sprintf("\t%s AS %s%s\n", q.param("Array(Decimal256(20))", "["+strings.Join(values, ",")+"]"), c.rate,
			lo.Ternary(i < len(analyticsV2Components)-1, ",", "")))
	}
}

func (r *MeterUsageRepository) RankAnalyticsV2Rows(ctx context.Context, params *events.AnalyticsV2RankParams) (*events.AnalyticsV2RankResult, error) {
	if params == nil || len(params.Lines) == 0 || len(params.GroupBy) == 0 {
		return &events.AnalyticsV2RankResult{}, nil
	}
	if len(params.Rates) != len(params.Lines) {
		return nil, ierr.NewError("rates must align with lines").Mark(ierr.ErrValidation)
	}
	span := StartRepositorySpan(ctx, "meter_usage", "rank_analytics_v2_rows", map[string]interface{}{
		"lines": len(params.Lines), "group_by": params.GroupBy, "column": params.Column,
	})
	defer FinishSpan(span)

	q := buildAnalyticsV2RankQuery(params)
	rows, err := r.store.GetConn().Query(q.context(ctx), q.sql.String())
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to rank analytics rows").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	result := &events.AnalyticsV2RankResult{}
	keys := len(params.GroupBy)
	for rows.Next() {
		group := make([]string, keys)
		var m, t analyticsV2ScanMeasure
		var lines []uint32
		var colCost, colQty, colPeak, totColCost, totColQty, totColPeak map[string]decimal.Decimal
		var colEvents map[string]uint64
		var total uint64

		dest := make([]interface{}, 0, keys+24)
		for i := range group {
			dest = append(dest, &group[i])
		}
		dest = append(dest, m.targets()...)
		dest = append(dest, &lines, &colCost, &colQty, &colPeak, &colEvents, &total)
		dest = append(dest, t.targets()...)
		dest = append(dest, &totColCost, &totColQty, &totColPeak)
		if err := rows.Scan(dest...); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).WithHint("Failed to scan analytics rows").Mark(ierr.ErrDatabase)
		}

		row := events.AnalyticsV2Row{
			Group:       group,
			Measure:     m.measure(),
			LineIndexes: lo.Map(lines, func(i uint32, _ int) int { return int(i) - 1 }),
		}
		sort.Ints(row.LineIndexes)
		if params.Column != "" {
			row.Columns = analyticsV2Columns(colCost, colQty, colPeak, colEvents)
		}
		result.Rows = append(result.Rows, row)
		result.TotalCount = int64(total)
		result.Totals = t.measure()
		if params.Column != "" {
			result.ColumnTotals = analyticsV2Columns(totColCost, totColQty, totColPeak, nil)
		}
	}
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to read analytics rows").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return result, nil
}

// analyticsV2ScanMeasure receives one measure's columns in the order the rank query selects them.
type analyticsV2ScanMeasure struct {
	usage, utilized, overage, trueUp, discount, free, quantity, peak decimal.Decimal
	events                                                           uint64
}

func (m *analyticsV2ScanMeasure) targets() []interface{} {
	return []interface{}{&m.usage, &m.utilized, &m.overage, &m.trueUp, &m.discount, &m.free, &m.quantity, &m.peak, &m.events}
}

func (m *analyticsV2ScanMeasure) measure() events.AnalyticsV2Measure {
	return events.AnalyticsV2Measure{
		Amounts: events.AnalyticsV2Amounts{
			Usage: m.usage, CommitmentUtilized: m.utilized, Overage: m.overage,
			TrueUp: m.trueUp, Discount: m.discount, Free: m.free,
		},
		Quantity:   m.quantity,
		Peak:       m.peak,
		EventCount: m.events,
	}
}

// analyticsV2Columns builds column measures; a column's amounts hold its cost as Usage.
func analyticsV2Columns(cost, quantity, peak map[string]decimal.Decimal, eventCounts map[string]uint64) map[string]events.AnalyticsV2Measure {
	out := make(map[string]events.AnalyticsV2Measure, len(cost))
	for name, c := range cost {
		out[name] = events.AnalyticsV2Measure{
			Amounts:    events.AnalyticsV2Amounts{Usage: c},
			Quantity:   quantity[name],
			Peak:       peak[name],
			EventCount: eventCounts[name],
		}
	}
	return out
}

// analyticsV2RowSelect writes the row keys (k1..kn) and the column (col) for one event.
func analyticsV2RowSelect(q *analyticsV2Query, params *events.AnalyticsV2RankParams) {
	for i, k := range params.GroupBy {
		analyticsV2KeyExpr(q, k, "e.", params)
		q.add(fmt.Sprintf(" AS k%d, ", i+1))
	}
	if params.Column != "" {
		analyticsV2KeyExpr(q, params.Column, "e.", params)
		q.add(" AS col, ")
	} else {
		q.add("'' AS col, ")
	}
}

// buildAnalyticsV2RankQuery prices events by line, rolls them up per row (and column) and returns one page.
// Additive lines are priced per event and summed straight into (row, column); only MAX lines need the
// (row, column, line) peaks. Both branches are merged per row, then the page carries window totals over
// every matching row.
func buildAnalyticsV2RankQuery(params *events.AnalyticsV2RankParams) *analyticsV2Query {
	keys := make([]string, len(params.GroupBy))
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", i+1)
	}
	keyList := strings.Join(keys, ", ")
	additive := analyticsV2Lines(params.Lines, func(l events.AnalyticsV2Line) bool { return !l.IsMax })
	maxLines := analyticsV2Lines(params.Lines, func(l events.AnalyticsV2Line) bool { return l.IsMax })
	_, settings := analyticsV2Settings(params.Scope.UseFinal)

	q := &analyticsV2Query{}
	writeAnalyticsV2Rates(q, params.Rates)
	q.add(`SELECT ` + keyList + `,
	a_usage, a_utilized, a_overage, a_true_up, a_discount, a_free, quantity, peak, events,
	lines, c_cost, c_quantity, c_peak, c_events,
	count() OVER () AS total_rows,
	sum(a_usage) OVER (), sum(a_utilized) OVER (), sum(a_overage) OVER (), sum(a_true_up) OVER (),
	sum(a_discount) OVER (), sum(a_free) OVER (), sum(quantity) OVER (), max(peak) OVER (), sum(events) OVER (),
	sumMap(c_cost) OVER (), sumMap(c_quantity) OVER (), maxMap(c_peak) OVER ()
FROM (
	SELECT ` + keyList + `,
		sum(b_usage) AS a_usage, sum(b_utilized) AS a_utilized, sum(b_overage) AS a_overage,
		sum(b_true_up) AS a_true_up, sum(b_discount) AS a_discount, sum(b_free) AS a_free,
		sum(b_quantity) AS quantity, max(b_peak) AS peak, sum(b_events) AS events,
		groupUniqArrayArray(b_lines) AS lines,
		sumMap(map(col, b_usage + b_utilized + b_overage + b_true_up - b_discount)) AS c_cost,
		sumMap(map(col, b_quantity)) AS c_quantity,
		maxMap(map(col, b_peak)) AS c_peak,
		sumMap(map(col, b_events)) AS c_events`)
	if params.Sort.By == events.AnalyticsV2SortByColumn {
		q.add(",\n\t\tsumIf(b_usage + b_utilized + b_overage + b_true_up - b_discount, col = ?) AS sort_column", params.Sort.Column)
	}
	q.add("\n\tFROM (\n")

	if len(additive) > 0 {
		// Quantities are summed per (row, column, line) first, so the exact rate multiplications run once per
		// cell instead of once per event.
		q.add("\t\tSELECT " + keyList + ", col")
		for _, c := range analyticsV2Components {
			q.add(fmt.Sprintf(",\n\t\t\ttoDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(%s, idx)), %d), %d) AS b_%s",
				c.rate, analyticsV2Scale, analyticsV2Scale, c.out))
		}
		q.add(`,
			sum(q) AS b_quantity, toDecimal128(0, 15) AS b_peak, sum(n) AS b_events, groupUniqArray(idx) AS b_lines
		FROM (
			SELECT ` + keyList + `, col, idx, sum(qty_total) AS q, count() AS n
			FROM (
`)
		writeAnalyticsV2Events(q, params.Scope, params.Lines, additive, params.StartTime, params.EndTime, "",
			func(q *analyticsV2Query) {
				analyticsV2RowSelect(q, params)
				q.add("r.idx AS idx, e.qty_total AS qty_total")
			},
			analyticsV2ScanFilters(params), analyticsV2JoinedFilters(params))
		q.add("\n\t\t\t)\n\t\t\tGROUP BY " + keyList + ", col, idx\n\t\t)\n\t\tGROUP BY " + keyList + ", col\n")
	}
	if len(additive) > 0 && len(maxLines) > 0 {
		q.add("\t\tUNION ALL\n")
	}
	if len(maxLines) > 0 {
		q.add("\t\tSELECT " + keyList + ", col")
		for _, c := range analyticsV2Components {
			q.add(fmt.Sprintf(",\n\t\t\tsum(toDecimal256(round(arrayElement(%s, idx) * share, %d), %d)) AS b_%s",
				c.rate, analyticsV2Scale, analyticsV2Scale, c.out))
		}
		q.add(`,
			toDecimal128(0, 15) AS b_quantity, toDecimal128(max(pk), 15) AS b_peak, sum(n) AS b_events, groupUniqArray(idx) AS b_lines
		FROM (
			SELECT *,
				if(col_peak_sum = 0, toDecimal256(0, 30),
					toDecimal256(toDecimal256(row_peak, 30) * toDecimal256(pk, 15) / col_peak_sum, 30)) AS share
			FROM (
				SELECT *,
					max(pk) OVER (PARTITION BY ` + keyList + `, idx) AS row_peak,
					sum(pk) OVER (PARTITION BY ` + keyList + `, idx) AS col_peak_sum
				FROM (
					SELECT ` + keyList + `, col, idx, max(qty_total) AS pk, count() AS n
					FROM (
`)
		writeAnalyticsV2Events(q, params.Scope, params.Lines, maxLines, params.StartTime, params.EndTime, "",
			func(q *analyticsV2Query) {
				analyticsV2RowSelect(q, params)
				q.add("r.idx AS idx, e.qty_total AS qty_total")
			},
			analyticsV2ScanFilters(params), analyticsV2JoinedFilters(params))
		q.add(`
					)
					GROUP BY ` + keyList + `, col, idx
				)
			)
		)
		GROUP BY ` + keyList + `, col
`)
	}
	q.add("\t)\n\tGROUP BY " + keyList + "\n)")

	if params.Search != "" {
		q.add("\nWHERE positionCaseInsensitiveUTF8(concat("+strings.Join(keys, ", ' ', ")+"), ?) > 0", params.Search)
	}

	dir := lo.Ternary(params.Sort.Desc, "DESC", "ASC")
	var orderBy string
	switch params.Sort.By {
	case events.AnalyticsV2SortByColumn:
		orderBy = "sort_column " + dir
	case events.AnalyticsV2SortByName:
		orderBy = strings.Join(lo.Map(keys, func(k string, _ int) string { return k + " " + dir }), ", ")
	case events.AnalyticsV2SortByUsage:
		orderBy = "quantity + peak " + dir
	case events.AnalyticsV2SortByEventCount:
		orderBy = "events " + dir
	default:
		orderBy = "a_usage + a_utilized + a_overage + a_true_up - a_discount " + dir
	}
	q.add(fmt.Sprintf("\nORDER BY %s, %s\nLIMIT %d OFFSET %d\n%s", orderBy, keyList, params.Limit, params.Offset, settings))
	return q
}

// analyticsV2RowSep joins a row's key values into one string; Go and SQL must agree on it.
const analyticsV2RowSep = "\x1f"

// writeAnalyticsV2RowKey writes the row key (all group values joined) as rk and their space-joined text as row_text.
func writeAnalyticsV2RowKey(q *analyticsV2Query, groupBy []string, params *events.AnalyticsV2RankParams) {
	for i, k := range groupBy {
		if i > 0 {
			q.add(", char(31), ")
		} else {
			q.add("concat('', ")
		}
		analyticsV2KeyExpr(q, k, "e.", params)
	}
	q.add(") AS rk, ")
	for i, k := range groupBy {
		if i > 0 {
			q.add(", ' ', ")
		} else {
			q.add("concat('', ")
		}
		analyticsV2KeyExpr(q, k, "e.", params)
	}
	q.add(") AS row_text, ")
}

func (r *MeterUsageRepository) GetAnalyticsV2LinePeaks(ctx context.Context, params *events.AnalyticsV2LinePeakParams) ([]decimal.Decimal, error) {
	out := make([]decimal.Decimal, len(params.Lines))
	maxLines := analyticsV2Lines(params.Lines, func(l events.AnalyticsV2Line) bool { return l.IsMax })
	if len(maxLines) == 0 || len(params.GroupBy) == 0 {
		return out, nil
	}
	span := StartRepositorySpan(ctx, "meter_usage", "get_analytics_v2_line_peaks", map[string]interface{}{"max_lines": len(maxLines)})
	defer FinishSpan(span)

	keyParams := &events.AnalyticsV2RankParams{MeterFeatures: params.MeterFeatures}
	_, settings := analyticsV2Settings(params.Scope.UseFinal)
	q := &analyticsV2Query{}
	q.add("SELECT idx, sum(row_peak) FROM (\n\tSELECT rk, idx, max(qty_total) AS row_peak FROM (\n")
	writeAnalyticsV2Events(q, params.Scope, params.Lines, maxLines, params.StartTime, params.EndTime, "",
		func(q *analyticsV2Query) {
			writeAnalyticsV2RowKey(q, params.GroupBy, keyParams)
			q.add("r.idx AS idx, e.qty_total AS qty_total")
		}, nil, nil)
	q.add("\n\t)\n\tGROUP BY rk, idx\n)\nGROUP BY idx\n" + settings)

	rows, err := r.store.GetConn().Query(q.context(ctx), q.sql.String())
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to query analytics line peaks").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()
	for rows.Next() {
		var idx uint32
		var peaks decimal.Decimal
		if err := rows.Scan(&idx, &peaks); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).WithHint("Failed to scan analytics line peaks").Mark(ierr.ErrDatabase)
		}
		out[idx-1] = peaks
	}
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to read analytics line peaks").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return out, nil
}

func (r *MeterUsageRepository) GetAnalyticsV2Detail(ctx context.Context, params *events.AnalyticsV2DetailParams) (*events.AnalyticsV2Detail, error) {
	detail := &events.AnalyticsV2Detail{Distinct: make([]map[string]events.AnalyticsV2Distinct, len(params.Rows))}
	if len(params.Rows) == 0 || len(params.Rank.Lines) == 0 {
		return detail, nil
	}
	span := StartRepositorySpan(ctx, "meter_usage", "get_analytics_v2_detail", map[string]interface{}{
		"rows": len(params.Rows), "window_size": params.WindowSize, "distinct_keys": params.DistinctKeys,
	})
	defer FinishSpan(span)

	if params.WindowSize != "" {
		points, err := r.analyticsV2Points(ctx, params)
		if err != nil {
			SetSpanError(span, err)
			return nil, err
		}
		detail.Points = points
	}
	if len(params.DistinctKeys) > 0 {
		if err := r.analyticsV2Distinct(ctx, params, detail.Distinct); err != nil {
			SetSpanError(span, err)
			return nil, err
		}
	}
	SetSpanSuccess(span)
	return detail, nil
}

// analyticsV2PageRow is page_row for an event: 1..N for page rows, max for other rows (dropped);
// the all-rows grouping set reports 0.
func analyticsV2PageRow(pageKeys string) string {
	return fmt.Sprintf("if(indexOf(%[1]s, rk) = 0, toUInt32(4294967295), toUInt32(indexOf(%[1]s, rk)))", pageKeys)
}

// analyticsV2Points returns per (page row, window, column, line) cells, plus the same cells summed over every
// matching row (Row -1), using grouping sets. Additive lines need no per-row work for the all-rows cells;
// only MAX lines are grouped by row to weight each cell by its row's peak.
func (r *MeterUsageRepository) analyticsV2Points(ctx context.Context, params *events.AnalyticsV2DetailParams) ([]events.AnalyticsV2PointCell, error) {
	p := &params.Rank
	bucket := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, p.Timezone)
	additive := analyticsV2Lines(p.Lines, func(l events.AnalyticsV2Line) bool { return !l.IsMax })
	maxLines := analyticsV2Lines(p.Lines, func(l events.AnalyticsV2Line) bool { return l.IsMax })
	_, settings := analyticsV2Settings(p.Scope.UseFinal)

	q := &analyticsV2Query{}
	pageKeys := q.param("Array(String)", analyticsV2StringArray(lo.Map(params.Rows, func(row []string, _ int) string {
		return strings.Join(row, analyticsV2RowSep)
	})))
	scan := func(include []int) {
		writeAnalyticsV2Events(q, p.Scope, p.Lines, include, p.StartTime, p.EndTime, ", "+bucket+" AS bucket",
			func(q *analyticsV2Query) {
				writeAnalyticsV2RowKey(q, p.GroupBy, p)
				q.add("e.bucket AS w, ")
				if p.Column != "" {
					analyticsV2KeyExpr(q, p.Column, "e.", p)
					q.add(" AS col, ")
				} else {
					q.add("'' AS col, ")
				}
				q.add("r.idx AS idx, e.qty_total AS qty_total")
			},
			analyticsV2ScanFilters(p), analyticsV2JoinedFilters(p))
	}
	search := func() {
		if p.Search != "" {
			q.add("\n\t\tWHERE positionCaseInsensitiveUTF8(row_text, ?) > 0", p.Search)
		}
	}

	if len(additive) > 0 {
		q.add(`SELECT page_row, w, col, idx, sum(qty_total) AS quantity, toDecimal256(0, 30) AS peak_weight,
	toDecimal128(max(qty_total), 15) AS peak, count() AS events
FROM (
	SELECT ` + analyticsV2PageRow(pageKeys) + ` AS page_row, w, col, idx, qty_total
	FROM (
`)
		scan(additive)
		q.add("\n\t)")
		search()
		q.add(`
)
GROUP BY GROUPING SETS ((page_row, w, col, idx), (w, col, idx))
HAVING page_row != 4294967295
`)
	}
	if len(additive) > 0 && len(maxLines) > 0 {
		q.add("UNION ALL\n")
	}
	if len(maxLines) > 0 {
		q.add(`SELECT page_row, w, col, idx, sum(q) AS quantity, sum(weight) AS peak_weight, toDecimal128(max(pk), 15) AS peak, sum(n) AS events
FROM (
	SELECT page_row, w, col, idx, q, pk, n,
		if(cell_peak_sum = 0, toDecimal256(0, 30),
			toDecimal256(toDecimal256(row_peak, 30) * toDecimal256(pk, 15) / cell_peak_sum, 30)) AS weight
	FROM (
		SELECT *,
			max(pk) OVER (PARTITION BY rk, idx) AS row_peak,
			sum(pk) OVER (PARTITION BY rk, idx) AS cell_peak_sum
		FROM (
			SELECT rk, ` + analyticsV2PageRow(pageKeys) + ` AS page_row,
				w, col, idx, sum(qty_total) AS q, max(qty_total) AS pk, count() AS n
			FROM (
`)
		scan(maxLines)
		q.add("\n\t\t\t)")
		search()
		q.add(`
			GROUP BY rk, w, col, idx
		)
	)
)
GROUP BY GROUPING SETS ((page_row, w, col, idx), (w, col, idx))
HAVING page_row != 4294967295
`)
	}
	q.add(settings)

	rows, err := r.store.GetConn().Query(q.context(ctx), q.sql.String())
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to query analytics points").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var cells []events.AnalyticsV2PointCell
	for rows.Next() {
		var pageRow, idx uint32
		var w time.Time
		var col string
		var quantity, weight, peak decimal.Decimal
		var n uint64
		if err := rows.Scan(&pageRow, &w, &col, &idx, &quantity, &weight, &peak, &n); err != nil {
			return nil, ierr.WithError(err).WithHint("Failed to scan analytics points").Mark(ierr.ErrDatabase)
		}
		cells = append(cells, events.AnalyticsV2PointCell{
			Row: int(pageRow) - 1, WindowStart: w.UTC(), Column: col, Line: int(idx) - 1,
			Quantity: quantity, PeakWeight: weight, Peak: peak, EventCount: n,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to read analytics points").Mark(ierr.ErrDatabase)
	}
	return cells, nil
}

// analyticsV2Distinct fills each page row's distinct count per key, with values when there are few.
func (r *MeterUsageRepository) analyticsV2Distinct(ctx context.Context, params *events.AnalyticsV2DetailParams, out []map[string]events.AnalyticsV2Distinct) error {
	p := &params.Rank
	_, settings := analyticsV2Settings(p.Scope.UseFinal)

	q := &analyticsV2Query{}
	pageKeys := q.param("Array(String)", analyticsV2StringArray(lo.Map(params.Rows, func(row []string, _ int) string {
		return strings.Join(row, analyticsV2RowSep)
	})))
	q.add("SELECT indexOf(" + pageKeys + ", rk) AS page_row")
	for i := range params.DistinctKeys {
		q.add(fmt.Sprintf(", uniqExact(d%[1]d), groupUniqArray(%[2]d)(d%[1]d)", i, analyticsV2DistinctValues+1))
	}
	q.add("\nFROM (\n")
	writeAnalyticsV2Events(q, p.Scope, p.Lines, analyticsV2Lines(p.Lines, nil), p.StartTime, p.EndTime, "",
		func(q *analyticsV2Query) {
			writeAnalyticsV2RowKey(q, p.GroupBy, p)
			for i, k := range params.DistinctKeys {
				analyticsV2KeyExpr(q, k, "e.", p)
				q.add(fmt.Sprintf(" AS d%d, ", i))
			}
			q.add("r.idx AS idx")
		},
		analyticsV2ScanFilters(p), analyticsV2JoinedFilters(p))
	q.add("\n)\nWHERE indexOf(" + pageKeys + ", rk) > 0\nGROUP BY page_row\n" + settings)

	rows, err := r.store.GetConn().Query(q.context(ctx), q.sql.String())
	if err != nil {
		return ierr.WithError(err).WithHint("Failed to query analytics metrics").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()
	for rows.Next() {
		var pageRow uint64
		counts := make([]uint64, len(params.DistinctKeys))
		values := make([][]string, len(params.DistinctKeys))
		dest := []interface{}{&pageRow}
		for i := range params.DistinctKeys {
			dest = append(dest, &counts[i], &values[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return ierr.WithError(err).WithHint("Failed to scan analytics metrics").Mark(ierr.ErrDatabase)
		}
		m := make(map[string]events.AnalyticsV2Distinct, len(params.DistinctKeys))
		for i, k := range params.DistinctKeys {
			d := events.AnalyticsV2Distinct{Count: counts[i]}
			if counts[i] <= analyticsV2DistinctValues {
				d.Values = values[i]
				sort.Strings(d.Values)
			}
			m[k] = d
		}
		out[pageRow-1] = m
	}
	if err := rows.Err(); err != nil {
		return ierr.WithError(err).WithHint("Failed to read analytics metrics").Mark(ierr.ErrDatabase)
	}
	return nil
}

// analyticsV2DistinctValues is how many distinct values are listed before only the count is returned.
const analyticsV2DistinctValues = 5
