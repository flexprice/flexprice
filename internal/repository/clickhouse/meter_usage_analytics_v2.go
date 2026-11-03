package clickhouse

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// analyticsV2Scale is the decimal scale analytics v2 money math is rounded to.
const analyticsV2Scale = 18

// analyticsV2Query collects SQL text and its positional arguments in order.
type analyticsV2Query struct {
	sql  strings.Builder
	args []interface{}
}

func (q *analyticsV2Query) add(sql string, args ...interface{}) {
	q.sql.WriteString(sql)
	q.args = append(q.args, args...)
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

// writeAnalyticsV2Events writes the events-to-lines join: one UNION ALL branch per layer, each event tagged
// with its line index (1-based, for ClickHouse arrays). selectExprs see the event as e and the line as r.
func writeAnalyticsV2Events(
	q *analyticsV2Query,
	scope events.AnalyticsV2Scope,
	lines []events.AnalyticsV2Line,
	from, to time.Time,
	selectExprs func(q *analyticsV2Query),
	eventFilters func(q *analyticsV2Query),
	joinedFilters func(q *analyticsV2Query),
) {
	finalClause, _ := analyticsV2Settings(scope.UseFinal)
	meterIDs := lo.Uniq(lo.Map(lines, func(l events.AnalyticsV2Line, _ int) string { return l.MeterID }))

	for n, layer := range analyticsV2Layers(lines) {
		if n > 0 {
			q.add("\n\t\tUNION ALL\n")
		}
		meters := make([]string, len(layer))
		starts := make([]time.Time, len(layer))
		ends := make([]time.Time, len(layer))
		idx := make([]uint32, len(layer))
		lineIDs := make([]string, len(layer))
		for k, i := range layer {
			meters[k], starts[k], ends[k] = lines[i].MeterID, lines[i].Start.UTC(), lines[i].End.UTC()
			idx[k], lineIDs[k] = uint32(i+1), lines[i].ID
		}

		q.add("\t\tSELECT ")
		selectExprs(q)
		q.add(fmt.Sprintf(`
		FROM (
			SELECT meter_id, timestamp, qty_total, source, properties
			FROM meter_usage %s
			WHERE tenant_id = ? AND environment_id = ? AND external_customer_id IN ?
				AND meter_id IN ? AND timestamp >= ? AND timestamp < ? AND ingested_at <= ?`, finalClause),
			scope.TenantID, scope.EnvironmentID, scope.ExternalCustomerIDs,
			meterIDs, from.UTC(), to.UTC(), scope.DataThrough.UTC())
		if eventFilters != nil {
			eventFilters(q)
		}
		q.add(`
		) AS e
		ASOF INNER JOIN (
			SELECT t.1 AS meter_id, t.2 AS start_time, t.3 AS end_time, toUInt32(t.4) AS idx, t.5 AS line_id
			FROM (SELECT arrayJoin(arrayZip(?, ?, ?, ?, ?)) AS t)
		) AS r ON e.meter_id = r.meter_id AND e.timestamp >= r.start_time
		WHERE e.timestamp < r.end_time`, meters, starts, ends, idx, lineIDs)
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
	q.add("SELECT idx, sum(qty_total) AS quantity, count() AS events\n\tFROM (\n")
	writeAnalyticsV2Events(q, params.Scope, params.Lines, from, to,
		func(q *analyticsV2Query) { q.add("r.idx AS idx, e.qty_total AS qty_total") }, nil, nil)
	q.add("\n\t)\n\tGROUP BY idx\n\t" + settings)

	rows, err := r.store.GetConn().Query(ctx, q.sql.String(), q.args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to query analytics line usage").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	out := make([]events.AnalyticsV2LineUsage, len(params.Lines))
	for rows.Next() {
		var idx uint32
		var quantity decimal.Decimal
		var n uint64
		if err := rows.Scan(&idx, &quantity, &n); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).WithHint("Failed to scan analytics line usage").Mark(ierr.ErrDatabase)
		}
		out[idx-1] = events.AnalyticsV2LineUsage{Quantity: quantity, EventCount: n}
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

func analyticsV2Filters(q *analyticsV2Query, prefix string, params *events.AnalyticsV2RankParams) {
	for _, f := range params.Filters {
		q.add("\n\t\t\tAND ")
		analyticsV2KeyExpr(q, f.Key, prefix, params)
		q.add(lo.Ternary(f.Exclude, " NOT IN ?", " IN ?"), f.Values)
	}
}

// analyticsV2Amount is one cost component per (row, column, line): quantity x rate for additive lines,
// the line amount x the row's peak share for MAX lines. Rounded to analyticsV2Scale.
func analyticsV2Amount(rates string) string {
	return fmt.Sprintf(`if(arrayElement(l_max, idx) = 1,
				toDecimal256(round(arrayElement(%[1]s, idx) * share, %[2]d), %[2]d),
				toDecimal256(round(toDecimal256(q, 15) * arrayElement(%[1]s, idx), %[2]d), %[2]d))`, rates, analyticsV2Scale)
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
	rows, err := r.store.GetConn().Query(ctx, q.sql.String(), q.args...)
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
		var colCost, colQty, colPeak, totColCost, totColQty map[string]decimal.Decimal
		var colEvents map[string]uint64
		var total uint64

		dest := make([]interface{}, 0, keys+24)
		for i := range group {
			dest = append(dest, &group[i])
		}
		dest = append(dest, m.targets()...)
		dest = append(dest, &lines, &colCost, &colQty, &colPeak, &colEvents, &total)
		dest = append(dest, t.targets()...)
		dest = append(dest, &totColCost, &totColQty)
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
			result.ColumnTotals = analyticsV2Columns(totColCost, totColQty, nil, nil)
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

// buildAnalyticsV2RankQuery prices events by line, rolls them up per row (and column) and returns one page.
// Levels: per (row, column, line) quantities and peaks -> MAX peak shares -> per (row, column) amounts ->
// per row -> page with window totals over every matching row.
func buildAnalyticsV2RankQuery(params *events.AnalyticsV2RankParams) *analyticsV2Query {
	keys := make([]string, len(params.GroupBy))
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", i+1)
	}
	keyList := strings.Join(keys, ", ")
	hasColumn := params.Column != ""

	rate := func(pick func(events.AnalyticsV2Amounts) decimal.Decimal) []string {
		return lo.Map(params.Rates, func(a events.AnalyticsV2Amounts, _ int) string { return pick(a).String() })
	}
	isMax := lo.Map(params.Lines, func(l events.AnalyticsV2Line, _ int) uint8 { return lo.Ternary[uint8](l.IsMax, 1, 0) })
	_, settings := analyticsV2Settings(params.Scope.UseFinal)

	q := &analyticsV2Query{}
	q.add(`WITH
	? AS l_max,
	CAST(? AS Array(Decimal256(20))) AS r_usage,
	CAST(? AS Array(Decimal256(20))) AS r_utilized,
	CAST(? AS Array(Decimal256(20))) AS r_overage,
	CAST(? AS Array(Decimal256(20))) AS r_true_up,
	CAST(? AS Array(Decimal256(20))) AS r_discount,
	CAST(? AS Array(Decimal256(20))) AS r_free
SELECT `, isMax,
		rate(func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.Usage }),
		rate(func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.CommitmentUtilized }),
		rate(func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.Overage }),
		rate(func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.TrueUp }),
		rate(func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.Discount }),
		rate(func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.Free }))

	// Page: row values, measure, lines, columns, total count, totals.
	q.add(keyList + `,
	a_usage, a_utilized, a_overage, a_true_up, a_discount, a_free, quantity, peak, events,
	lines, c_cost, c_quantity, c_peak, c_events,
	count() OVER () AS total_rows,
	sum(a_usage) OVER (), sum(a_utilized) OVER (), sum(a_overage) OVER (), sum(a_true_up) OVER (),
	sum(a_discount) OVER (), sum(a_free) OVER (), sum(quantity) OVER (), max(peak) OVER (), sum(events) OVER (),
	sumMap(c_cost) OVER (), sumMap(c_quantity) OVER ()
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
	q.add(`
	FROM (
		SELECT ` + keyList + `, col,
			sum(` + analyticsV2Amount("r_usage") + `) AS b_usage,
			sum(` + analyticsV2Amount("r_utilized") + `) AS b_utilized,
			sum(` + analyticsV2Amount("r_overage") + `) AS b_overage,
			sum(` + analyticsV2Amount("r_true_up") + `) AS b_true_up,
			sum(` + analyticsV2Amount("r_discount") + `) AS b_discount,
			sum(` + analyticsV2Amount("r_free") + `) AS b_free,
			sumIf(q, arrayElement(l_max, idx) = 0) AS b_quantity,
			maxIf(pk, arrayElement(l_max, idx) = 1) AS b_peak,
			sum(n) AS b_events,
			groupUniqArray(idx) AS b_lines
		FROM (
			SELECT *,
				if(line_peak = 0 OR col_peak_sum = 0, toDecimal256(0, 30),
					toDecimal256(toDecimal256(row_peak, 30) / line_peak * toDecimal256(pk, 15) / col_peak_sum, 30)) AS share
			FROM (
				SELECT *,
					sum(if(col_peak_sum = 0, toDecimal256(0, 30),
						toDecimal256(toDecimal256(row_peak, 30) * toDecimal256(pk, 15) / col_peak_sum, 30)))
						OVER (PARTITION BY idx) AS line_peak
				FROM (
					SELECT *,
						max(pk) OVER (PARTITION BY ` + keyList + `, idx) AS row_peak,
						sum(pk) OVER (PARTITION BY ` + keyList + `, idx) AS col_peak_sum
					FROM (
						SELECT ` + keyList + `, col, idx, sum(qty_total) AS q, max(qty_total) AS pk, count() AS n
						FROM (
`)
	writeAnalyticsV2Events(q, params.Scope, params.Lines, params.StartTime, params.EndTime,
		func(q *analyticsV2Query) {
			for i, k := range params.GroupBy {
				analyticsV2KeyExpr(q, k, "e.", params)
				q.add(fmt.Sprintf(" AS k%d, ", i+1))
			}
			if hasColumn {
				analyticsV2KeyExpr(q, params.Column, "e.", params)
				q.add(" AS col, ")
			} else {
				q.add("'' AS col, ")
			}
			q.add("r.idx AS idx, e.qty_total AS qty_total")
		},
		func(q *analyticsV2Query) {
			// Line-independent filters are pushed into the event scan.
			p := *params
			p.Filters = lo.Filter(params.Filters, func(f events.AnalyticsV2Filter, _ int) bool { return f.Key != "sub_line_item_id" })
			analyticsV2Filters(q, "", &p)
		},
		func(q *analyticsV2Query) {
			p := *params
			p.Filters = lo.Filter(params.Filters, func(f events.AnalyticsV2Filter, _ int) bool { return f.Key == "sub_line_item_id" })
			analyticsV2Filters(q, "e.", &p)
		})
	q.add(`
						)
						GROUP BY ` + keyList + `, col, idx
					)
				)
			)
		)
		GROUP BY ` + keyList + `, col
	)
	GROUP BY ` + keyList + `
)`)

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
