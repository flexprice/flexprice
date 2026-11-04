package clickhouse

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"
)

// decimalScale is the decimal scale analytics v2 money math is rounded to.
const decimalScale = 18

// sqlBuilder collects SQL text and its values as typed server-side parameters, so no value is ever
// written into the SQL and large arrays (line windows, rates, page rows) don't count against the query size.
type sqlBuilder struct {
	sql    strings.Builder
	params ch.Parameters
}

// add appends sql, replacing each "?" with a typed parameter for the next arg.
func (q *sqlBuilder) add(sql string, args ...interface{}) {
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
func (q *sqlBuilder) value(arg interface{}) string {
	switch v := arg.(type) {
	case string:
		return q.param("String", strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`).Replace(v))
	case []string:
		return q.param("Array(String)", stringArrayLiteral(v))
	case time.Time:
		// Event timestamps are whole seconds, so a fractional bound rounds up: ">= 12:00:00.5" and
		// "< 12:00:00.5" select the same events as ">= 12:00:01" and "< 12:00:01".
		if t := v.UTC(); t.Truncate(time.Second).Equal(t) {
			v = t
		} else {
			v = t.Truncate(time.Second).Add(time.Second)
		}
		return q.param("DateTime('UTC')", v.UTC().Format("2006-01-02 15:04:05"))
	default:
		panic(fmt.Sprintf("analytics v2 query: unsupported parameter type %T", arg))
	}
}

// param registers a server-side parameter and returns its placeholder, e.g. {p3:Array(String)}.
func (q *sqlBuilder) param(typ, value string) string {
	if q.params == nil {
		q.params = ch.Parameters{}
	}
	name := fmt.Sprintf("p%d", len(q.params))
	q.params[name] = value
	return "{" + name + ":" + typ + "}"
}

func (q *sqlBuilder) context(ctx context.Context) context.Context {
	if len(q.params) == 0 {
		return ctx
	}
	return ch.Context(ctx, ch.WithParameters(q.params))
}

// stringArrayLiteral formats values as a ClickHouse array literal for a parameter.
func stringArrayLiteral(values []string) string {
	quoted := lo.Map(values, func(v string, _ int) string {
		return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
	})
	return "[" + strings.Join(quoted, ",") + "]"
}

func timeArrayLiteral(values []time.Time) string {
	return stringArrayLiteral(lo.Map(values, func(t time.Time, _ int) string { return t.UTC().Format("2006-01-02 15:04:05") }))
}

func numberArrayLiteral[T any](values []T) string {
	return "[" + strings.Join(lo.Map(values, func(v T, _ int) string { return fmt.Sprint(v) }), ",") + "]"
}

// lineLayers splits lines so that no two lines of the same meter overlap within a layer.
// Each layer is joined to events separately, so an event inside two overlapping lines counts for both.
func lineLayers(lines []events.AnalyticsV2Line) [][]int {
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

// readSettings bounds every analytics v2 query's memory and, with FINAL, keeps the merge per partition.
func readSettings(useFinal bool) (finalClause, settings string) {
	if useFinal {
		return "FINAL", "SETTINGS " + maxMemoryUsageSetting + ", do_not_merge_across_partitions_select_final = 1"
	}
	return "", "SETTINGS " + maxMemoryUsageSetting
}

// lineIndexes picks which lines a scan joins events to; idx keeps each line's position in the request.
func lineIndexes(lines []events.AnalyticsV2Line, keep func(events.AnalyticsV2Line) bool) []int {
	var out []int
	for i, l := range lines {
		if keep == nil || keep(l) {
			out = append(out, i)
		}
	}
	return out
}

// bucketScan is a set of bucketed MAX lines that share a billing bucket and group property, so one scan can
// compute both as columns (lbucket, grp).
type bucketScan struct {
	bucket string // SQL bucket start for an event, as billing computes it
	group  string // the meter's group_by property, "" for none
	lines  []int
}

// splitBucketed separates the lines at include into additive lines and scans of bucketed MAX lines.
func splitBucketed(lines []events.AnalyticsV2Line, include []int) (additive []int, scans []bucketScan) {
	byKey := map[string]int{}
	for _, i := range include {
		l := lines[i]
		if !l.IsBucketed() {
			additive = append(additive, i)
			continue
		}
		// Same bucket expression as billing's bucketed query (UTC, monthly buckets on the billing anchor).
		anchor := l.BillingAnchor
		expr := formatWindowSizeWithBillingAnchor(l.BucketSize, &anchor, "")
		key := expr + "\x00" + l.BucketGroup
		j, ok := byKey[key]
		if !ok {
			j = len(scans)
			byKey[key] = j
			scans = append(scans, bucketScan{bucket: expr, group: l.BucketGroup})
		}
		scans[j].lines = append(scans[j].lines, i)
	}
	return additive, scans
}

// scanColumns are the scan's extra columns for a bucket scan: the billing bucket, plus any chart window.
func (b bucketScan) scanColumns(extra string) string {
	return extra + ", " + b.bucket + " AS lbucket"
}

// props adds the bucket's group property to the properties a scan extracts.
func (b bucketScan) props(props []string) []string {
	if b.group == "" {
		return props
	}
	return lo.Uniq(append(append([]string{}, props...), b.group))
}

// writeBucketColumns selects the event's billing bucket and group value after the join.
func (b bucketScan) writeBucketColumns(q *sqlBuilder) {
	q.add("e.lbucket AS lbucket, ")
	if b.group == "" {
		q.add("'' AS grp, ")
	} else {
		q.add("e." + propertyAlias(b.group) + " AS grp, ")
	}
}

// writePass selects whether an event passes the request's event filters, as pass. Bucketed lines pick each
// bucket's peak from every event and only then drop filtered rows, so filtering out one app never hands its
// peaks to another.
func writePass(q *sqlBuilder, params *events.AnalyticsV2RankParams) {
	q.add("(1")
	writeEventFilters(q, "e.", params)
	q.add(") AS pass, ")
}

// writeBucketPeaks writes, for one bucket scan, every (row cell, line) of each bucket: the cell holding the
// bucket's peak event carries the peak as q (ties go to any one of them), and every cell carries its own
// passing event count as n. key is the tuple identifying a cell, with the pass flag at position passAt;
// cells of filtered-out events are dropped only after the peak is chosen. Output columns: cell, idx, q, n.
func writeBucketPeaks(q *sqlBuilder, key string, passAt int, join func()) {
	q.add(`SELECT m.1 AS cell, idx, if(m.1 = owner, toDecimal128(peak, 15), toDecimal128(0, 15)) AS q, m.2 AS n
FROM (
	SELECT idx, lbucket, grp, argMax(key, rq) AS owner, max(rq) AS peak, groupArray((key, rn)) AS members
	FROM (
		SELECT idx, lbucket, grp, ` + key + ` AS key, max(qty_total) AS rq, countIf(pass) AS rn
		FROM (
`)
	join()
	q.add(`
		)
		GROUP BY idx, lbucket, grp, key
	)
	GROUP BY idx, lbucket, grp
)
ARRAY JOIN members AS m
WHERE tupleElement(m.1, ` + fmt.Sprint(passAt) + `)`)
}

// writeEventLineJoin writes the events-to-lines join for the lines at include: one UNION ALL branch per
// layer, each event tagged with its line index (1-based, for ClickHouse arrays). selectExprs see the event as
// e and the line as r.
func writeEventLineJoin(
	q *sqlBuilder,
	scope events.AnalyticsV2Scope,
	lines []events.AnalyticsV2Line,
	include []int,
	from, to time.Time,
	scanExtra string,
	props []string,
	selectExprs func(q *sqlBuilder),
	eventFilters func(q *sqlBuilder),
	joinedFilters func(q *sqlBuilder),
) {
	finalClause, _ := readSettings(scope.UseFinal)
	subset := lo.Map(include, func(i int, _ int) events.AnalyticsV2Line { return lines[i] })

	for n, layer := range lineLayers(subset) {
		if n > 0 {
			q.add("\n\t\tUNION ALL\n")
		}
		// Each layer reads only its own meters, so overlapping lines don't re-read every meter per layer.
		meterIDs := q.param("Array(String)", stringArrayLiteral(lo.Uniq(lo.Map(layer, func(s int, _ int) string { return subset[s].MeterID }))))
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
		// Property keys are read from the JSON inside the scan, so the join carries short values,
		// not the whole properties string.
		q.add("\n\t\tFROM (\n\t\t\tSELECT meter_id, timestamp, qty_total, source")
		for _, name := range props {
			q.add(", JSONExtractString(properties, ?) AS "+propertyAlias(name), name)
		}
		q.add(fmt.Sprintf(`%s
			FROM meter_usage %s
			WHERE tenant_id = ? AND environment_id = ? AND external_customer_id IN ?
				AND meter_id IN %s AND timestamp >= ? AND timestamp < ? AND ingested_at <= %s`, scanExtra, finalClause, meterIDs,
			q.param("DateTime64(3, 'UTC')", scope.DataThrough.UTC().Format("2006-01-02 15:04:05.000"))),
			scope.TenantID, scope.EnvironmentID, scope.ExternalCustomerIDs,
			from.UTC(), to.UTC())
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
			q.param("Array(String)", stringArrayLiteral(meters)),
			q.param("Array(DateTime('UTC'))", timeArrayLiteral(starts)),
			q.param("Array(DateTime('UTC'))", timeArrayLiteral(ends)),
			q.param("Array(UInt32)", numberArrayLiteral(idx)),
			q.param("Array(String)", stringArrayLiteral(lineIDs))))
		if joinedFilters != nil {
			joinedFilters(q)
		}
	}
}

// unionWriter returns a func that writes UNION ALL before every branch but the first.
func unionWriter(q *sqlBuilder) func() {
	first := true
	return func() {
		if !first {
			q.add("\n\tUNION ALL\n")
		}
		first = false
	}
}

func linesWindow(lines []events.AnalyticsV2Line) (time.Time, time.Time) {
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

	from, to := linesWindow(params.Lines)
	_, settings := readSettings(params.Scope.UseFinal)
	rs, re := params.RangeStart.UTC(), params.RangeEnd.UTC()
	additive, scans := splitBucketed(params.Lines, lineIndexes(params.Lines, nil))

	q := &sqlBuilder{}
	q.add("SELECT idx, sum(quantity), sum(range_quantity), sum(events) FROM (\n")
	union := unionWriter(q)
	if len(additive) > 0 {
		union()
		q.add("\tSELECT idx, sum(qty_total) AS quantity, sumIf(qty_total, ts >= ? AND ts < ?) AS range_quantity, count() AS events\n\tFROM (\n", rs, re)
		writeEventLineJoin(q, params.Scope, params.Lines, additive, from, to, "", nil,
			func(q *sqlBuilder) { q.add("r.idx AS idx, e.qty_total AS qty_total, e.timestamp AS ts") }, nil, nil)
		q.add("\n\t)\n\tGROUP BY idx")
	}
	// Bucketed MAX lines: usage is the sum of each (bucket, group) peak; the range part sums each bucket's
	// peak among the events inside the range.
	for _, b := range scans {
		union()
		q.add(`	SELECT idx, sum(m) AS quantity, sum(mr) AS range_quantity, sum(n) AS events FROM (
		SELECT idx, lbucket, grp, max(qty_total) AS m, maxIf(qty_total, ts >= ? AND ts < ?) AS mr, count() AS n
		FROM (
`, rs, re)
		writeEventLineJoin(q, params.Scope, params.Lines, b.lines, from, to, b.scanColumns(""), b.props(nil),
			func(q *sqlBuilder) {
				b.writeBucketColumns(q)
				q.add("r.idx AS idx, e.qty_total AS qty_total, e.timestamp AS ts")
			}, nil, nil)
		q.add("\n\t\t)\n\t\tGROUP BY idx, lbucket, grp\n\t)\n\tGROUP BY idx")
	}
	q.add("\n)\nGROUP BY idx\n" + settings)

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

// keyExpr returns the SQL for a row, column or filter key. prefix is "e." after the join
// and "" inside the event scan; sub_line_item_id reads the joined line r and is only valid after the join.
func keyExpr(q *sqlBuilder, key, prefix string, params *events.AnalyticsV2RankParams) {
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
		q.add("formatDateTime(toTimeZone("+prefix+"timestamp, ?), '%Y-%m')", normalizeCHTimezone(params.Timezone))
	default:
		name := strings.TrimPrefix(key, "properties.")
		if prefix == "" {
			q.add("JSONExtractString(properties, ?)", name)
		} else {
			// After the join the key is a column the scan already extracted (see propertyNames).
			q.add(prefix + propertyAlias(name))
		}
	}
}

// propertyNames lists the property names among keys, for the scan to extract once.
func propertyNames(keys ...string) []string {
	return lo.Uniq(lo.FilterMap(keys, func(k string, _ int) (string, bool) {
		return strings.TrimPrefix(k, "properties."), strings.HasPrefix(k, "properties.")
	}))
}

// propertyAlias names the scan column holding a property; hex keeps any property name a valid identifier.
func propertyAlias(name string) string {
	return "p_" + hex.EncodeToString([]byte(name))
}

// scanFilters pushes line-independent filters into the event scan.
func scanFilters(params *events.AnalyticsV2RankParams) func(q *sqlBuilder) {
	return func(q *sqlBuilder) { writeEventFilters(q, "", params) }
}

// writeEventFilters writes the filters that depend only on the event (every key but sub_line_item_id).
func writeEventFilters(q *sqlBuilder, prefix string, params *events.AnalyticsV2RankParams) {
	p := *params
	p.Filters = lo.Filter(params.Filters, func(f events.AnalyticsV2Filter, _ int) bool { return f.Key != "sub_line_item_id" })
	writeFilters(q, prefix, &p)
}

// filterProps lists the properties the event filters read, so a scan can extract them for writePass.
func filterProps(params *events.AnalyticsV2RankParams) []string {
	return propertyNames(lo.Map(params.Filters, func(f events.AnalyticsV2Filter, _ int) string { return f.Key })...)
}

// lineFilters applies line filters after events are joined to their lines.
func lineFilters(params *events.AnalyticsV2RankParams) func(q *sqlBuilder) {
	return func(q *sqlBuilder) {
		p := *params
		p.Filters = lo.Filter(params.Filters, func(f events.AnalyticsV2Filter, _ int) bool { return f.Key == "sub_line_item_id" })
		writeFilters(q, "e.", &p)
	}
}

func writeFilters(q *sqlBuilder, prefix string, params *events.AnalyticsV2RankParams) {
	for _, f := range params.Filters {
		q.add("\n\t\t\tAND ")
		keyExpr(q, f.Key, prefix, params)
		q.add(lo.Ternary(f.Exclude, " NOT IN ?", " IN ?"), f.Values)
	}
}

// costComponents are the per-line rate arrays, in the order the queries read them.
type costComponent struct {
	rate, out string
	pick      func(events.AnalyticsV2Amounts) decimal.Decimal
}

var costComponents = []costComponent{
	{"r_usage", "usage", func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.Usage }},
	{"r_utilized", "utilized", func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.CommitmentUtilized }},
	{"r_overage", "overage", func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.Overage }},
	{"r_true_up", "true_up", func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.TrueUp }},
	{"r_discount", "discount", func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.Discount }},
	{"r_free", "free", func(a events.AnalyticsV2Amounts) decimal.Decimal { return a.Free }},
}

// pointComponents are the amounts a chart point carries; free units are not shown per point.
var pointComponents = lo.Filter(costComponents, func(c costComponent, _ int) bool { return c.out != "free" })

// writeRates writes the WITH clause binding each rate array as a constant parameter.
func writeRates(q *sqlBuilder, rates []events.AnalyticsV2Amounts) {
	q.add("WITH\n")
	for i, c := range costComponents {
		values := lo.Map(rates, func(a events.AnalyticsV2Amounts, _ int) string { return c.pick(a).String() })
		q.add(fmt.Sprintf("\t%s AS %s%s\n", q.param("Array(Decimal256(20))", "["+strings.Join(values, ",")+"]"), c.rate,
			lo.Ternary(i < len(costComponents)-1, ",", "")))
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

	q := buildRankQuery(params)
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
		var m, t scanMeasure
		var lines []uint32
		var colCost, colQty, totColCost, totColQty map[string]decimal.Decimal
		var colEvents map[string]uint64
		var total uint64

		dest := make([]interface{}, 0, keys+24)
		for i := range group {
			dest = append(dest, &group[i])
		}
		dest = append(dest, m.targets()...)
		dest = append(dest, &lines, &colCost, &colQty, &colEvents, &total)
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
			row.Columns = buildColumns(colCost, colQty, colEvents)
		}
		result.Rows = append(result.Rows, row)
		result.TotalCount = int64(total)
		result.Totals = t.measure()
		if params.Column != "" {
			result.ColumnTotals = buildColumns(totColCost, totColQty, nil)
		}
	}
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to read analytics rows").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return result, nil
}

// scanMeasure receives one measure's columns in the order the rank query selects them.
type scanMeasure struct {
	usage, utilized, overage, trueUp, discount, free, quantity decimal.Decimal
	events                                                     uint64
}

func (m *scanMeasure) targets() []interface{} {
	return []interface{}{&m.usage, &m.utilized, &m.overage, &m.trueUp, &m.discount, &m.free, &m.quantity, &m.events}
}

func (m *scanMeasure) measure() events.AnalyticsV2Measure {
	return events.AnalyticsV2Measure{
		Amounts: events.AnalyticsV2Amounts{
			Usage: m.usage, CommitmentUtilized: m.utilized, Overage: m.overage,
			TrueUp: m.trueUp, Discount: m.discount, Free: m.free,
		},
		Quantity:   m.quantity,
		EventCount: m.events,
	}
}

// buildColumns builds column measures; a column's amounts hold its cost as Usage.
func buildColumns(cost, quantity map[string]decimal.Decimal, eventCounts map[string]uint64) map[string]events.AnalyticsV2Measure {
	out := make(map[string]events.AnalyticsV2Measure, len(cost))
	for name, c := range cost {
		out[name] = events.AnalyticsV2Measure{
			Amounts:    events.AnalyticsV2Amounts{Usage: c},
			Quantity:   quantity[name],
			EventCount: eventCounts[name],
		}
	}
	return out
}

// writeRowSelect writes the row keys (k1..kn) and the column (col) for one event.
func writeRowSelect(q *sqlBuilder, params *events.AnalyticsV2RankParams) {
	for i, k := range params.GroupBy {
		keyExpr(q, k, "e.", params)
		q.add(fmt.Sprintf(" AS k%d, ", i+1))
	}
	if params.Column != "" {
		keyExpr(q, params.Column, "e.", params)
		q.add(" AS col, ")
	} else {
		q.add("'' AS col, ")
	}
}

// buildRankQuery prices events by line, rolls them up per row (and column) and returns one page, with
// window totals over every matching row.
func buildRankQuery(params *events.AnalyticsV2RankParams) *sqlBuilder {
	keys := make([]string, len(params.GroupBy))
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", i+1)
	}
	keyList := strings.Join(keys, ", ")
	rowProps := propertyNames(append([]string{params.Column}, params.GroupBy...)...)
	_, settings := readSettings(params.Scope.UseFinal)

	q := &sqlBuilder{}
	writeRates(q, params.Rates)
	q.add(`SELECT ` + keyList + `,
	a_usage, a_utilized, a_overage, a_true_up, a_discount, a_free, quantity, events,
	lines, c_cost, c_quantity, c_events,
	count() OVER () AS total_rows,
	sum(a_usage) OVER (), sum(a_utilized) OVER (), sum(a_overage) OVER (), sum(a_true_up) OVER (),
	sum(a_discount) OVER (), sum(a_free) OVER (), sum(quantity) OVER (), sum(events) OVER (),
	sumMap(c_cost) OVER (), sumMap(c_quantity) OVER ()
FROM (
	SELECT ` + keyList + `,
		sum(b_usage) AS a_usage, sum(b_utilized) AS a_utilized, sum(b_overage) AS a_overage,
		sum(b_true_up) AS a_true_up, sum(b_discount) AS a_discount, sum(b_free) AS a_free,
		sum(b_quantity) AS quantity, sum(b_events) AS events,
		groupUniqArrayArray(b_lines) AS lines,
		sumMap(map(col, b_usage + b_utilized + b_overage + b_true_up - b_discount)) AS c_cost,
		sumMap(map(col, b_quantity)) AS c_quantity,
		sumMap(map(col, b_events)) AS c_events`)
	if params.Sort.By == events.AnalyticsV2SortByColumn {
		q.add(",\n\t\tsumIf(b_usage + b_utilized + b_overage + b_true_up - b_discount, col = ?) AS sort_column", params.Sort.Column)
	}
	// Quantities are summed per (row, column, line) first, so the exact rate multiplications run once per
	// cell instead of once per event.
	q.add("\n\tFROM (\n\t\tSELECT " + keyList + ", col")
	for _, c := range costComponents {
		q.add(fmt.Sprintf(",\n\t\t\ttoDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(%s, idx)), %d), %d) AS b_%s",
			c.rate, decimalScale, decimalScale, c.out))
	}
	q.add(`,
			sum(q) AS b_quantity, sum(n) AS b_events, groupUniqArray(idx) AS b_lines
		FROM (
`)
	additive, scans := splitBucketed(params.Lines, lineIndexes(params.Lines, nil))
	union := unionWriter(q)
	if len(additive) > 0 {
		union()
		q.add("\t\t\tSELECT " + keyList + ", col, idx, toDecimal128(sum(qty_total), 15) AS q, count() AS n\n\t\t\tFROM (\n")
		writeEventLineJoin(q, params.Scope, params.Lines, additive, params.StartTime, params.EndTime, "", rowProps,
			func(q *sqlBuilder) {
				writeRowSelect(q, params)
				q.add("r.idx AS idx, e.qty_total AS qty_total")
			},
			scanFilters(params), lineFilters(params))
		q.add("\n\t\t\t)\n\t\t\tGROUP BY " + keyList + ", col, idx")
	}
	// Bucketed MAX lines: each bucket's peak event carries the bucket for its row.
	cellKey := "(" + keyList + ", col, pass)"
	for _, b := range scans {
		union()
		q.add("\t\t\tSELECT ")
		for i, k := range keys {
			q.add(fmt.Sprintf("cell.%d AS %s, ", i+1, k))
		}
		q.add(fmt.Sprintf("cell.%d AS col, idx, q, n FROM (\n", len(keys)+1))
		writeBucketPeaks(q, cellKey, len(keys)+2, func() {
			writeEventLineJoin(q, params.Scope, params.Lines, b.lines, params.StartTime, params.EndTime, b.scanColumns(""),
				b.props(lo.Uniq(append(rowProps, filterProps(params)...))),
				func(q *sqlBuilder) {
					writeRowSelect(q, params)
					writePass(q, params)
					b.writeBucketColumns(q)
					q.add("r.idx AS idx, e.qty_total AS qty_total")
				},
				nil, lineFilters(params))
		})
		q.add("\n\t\t\t)")
	}
	q.add("\n\t\t)\n\t\tGROUP BY " + keyList + ", col\n")
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
		orderBy = "quantity " + dir
	case events.AnalyticsV2SortByEventCount:
		orderBy = "events " + dir
	default:
		orderBy = "a_usage + a_utilized + a_overage + a_true_up - a_discount " + dir
	}
	q.add(fmt.Sprintf("\nORDER BY %s, %s\nLIMIT %d OFFSET %d\n%s", orderBy, keyList, params.Limit, params.Offset, settings))
	return q
}

// rowKeySep joins a row's key values into one string; Go and SQL must agree on it.
const rowKeySep = "\x1f"

// writeRowKey writes the row key (all group values joined) as rk and their space-joined text as row_text.
func writeRowKey(q *sqlBuilder, groupBy []string, params *events.AnalyticsV2RankParams) {
	for i, k := range groupBy {
		if i > 0 {
			q.add(", char(31), ")
		} else {
			q.add("concat('', ")
		}
		keyExpr(q, k, "e.", params)
	}
	q.add(") AS rk, ")
	for i, k := range groupBy {
		if i > 0 {
			q.add(", ' ', ")
		} else {
			q.add("concat('', ")
		}
		keyExpr(q, k, "e.", params)
	}
	q.add(") AS row_text, ")
}

func (r *MeterUsageRepository) GetAnalyticsV2Detail(ctx context.Context, params *events.AnalyticsV2DetailParams) (*events.AnalyticsV2Detail, error) {
	detail := &events.AnalyticsV2Detail{Distinct: make([]map[string]events.AnalyticsV2Distinct, len(params.Rows))}
	// With no page rows (offset past the end) only the all-rows points are read.
	if len(params.Rank.Lines) == 0 || (len(params.Rows) == 0 && params.WindowSize == "") {
		return detail, nil
	}
	span := StartRepositorySpan(ctx, "meter_usage", "get_analytics_v2_detail", map[string]interface{}{
		"rows": len(params.Rows), "window_size": params.WindowSize, "distinct_keys": params.DistinctKeys,
	})
	defer FinishSpan(span)

	// Points and distinct metrics are independent reads of the same events; run them side by side.
	g, gctx := errgroup.WithContext(ctx)
	if params.WindowSize != "" {
		g.Go(func() error {
			points, err := r.queryPoints(gctx, params)
			detail.Points = points
			return err
		})
	}
	if len(params.DistinctKeys) > 0 && len(params.Rows) > 0 {
		g.Go(func() error { return r.queryDistinct(gctx, params, detail.Distinct) })
	}
	if err := g.Wait(); err != nil {
		SetSpanError(span, err)
		return nil, err
	}
	SetSpanSuccess(span)
	return detail, nil
}

// pageRowExpr is page_row for an event: 1..N for page rows, max for other rows (dropped);
// the all-rows grouping set reports 0.
func pageRowExpr(pageKeys string) string {
	return fmt.Sprintf("if(indexOf(%[1]s, rk) = 0, toUInt32(4294967295), toUInt32(indexOf(%[1]s, rk)))", pageKeys)
}

// queryPoints returns priced (page row, window, column) points, plus the same points summed over every
// matching row (Row -1), using grouping sets. Rates are applied in ClickHouse, so the result has no line
// dimension and its size does not grow with the number of meters.
func (r *MeterUsageRepository) queryPoints(ctx context.Context, params *events.AnalyticsV2DetailParams) ([]events.AnalyticsV2PointCell, error) {
	p := &params.Rank
	bucket := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, p.Timezone)
	_, settings := readSettings(p.Scope.UseFinal)

	q := &sqlBuilder{}
	writeRates(q, p.Rates)
	var amounts []string
	for _, c := range pointComponents {
		amounts = append(amounts, fmt.Sprintf("toDecimal256(round(sum(toDecimal256(q, 15) * arrayElement(%s, idx)), %d), %d) AS a_%s",
			c.rate, decimalScale, decimalScale, c.out))
	}
	pageKeys := q.param("Array(String)", stringArrayLiteral(lo.Map(params.Rows, func(row []string, _ int) string {
		return strings.Join(row, rowKeySep)
	})))

	rowProps := propertyNames(append([]string{p.Column}, p.GroupBy...)...)
	writeCell := func(q *sqlBuilder) {
		writeRowKey(q, p.GroupBy, p)
		q.add("e.bucket AS w, ")
		if p.Column != "" {
			keyExpr(q, p.Column, "e.", p)
			q.add(" AS col, ")
		} else {
			q.add("'' AS col, ")
		}
	}
	search := func(textCol string) {
		if p.Search != "" {
			q.add("\n\tWHERE positionCaseInsensitiveUTF8("+textCol+", ?) > 0", p.Search)
		}
	}

	// Quantities are summed per (page row, window, column, line) first, so the rate multiplications run
	// once per cell instead of once per event.
	q.add(fmt.Sprintf(`SELECT page_row, w, col,
	%s,
	sum(q) AS quantity, sum(n) AS events
FROM (
`, strings.Join(amounts, ",\n\t")))
	additive, scans := splitBucketed(p.Lines, lineIndexes(p.Lines, nil))
	union := unionWriter(q)
	if len(additive) > 0 {
		union()
		q.add("\tSELECT " + pageRowExpr(pageKeys) + " AS page_row, w, col, idx, toDecimal128(sum(qty_total), 15) AS q, count() AS n\n\tFROM (\n")
		writeEventLineJoin(q, p.Scope, p.Lines, additive, p.StartTime, p.EndTime, ", "+bucket+" AS bucket", rowProps,
			func(q *sqlBuilder) {
				writeCell(q)
				q.add("r.idx AS idx, e.qty_total AS qty_total")
			},
			scanFilters(p), lineFilters(p))
		q.add("\n\t)")
		search("row_text")
		q.add("\n\tGROUP BY page_row, w, col, idx")
	}
	// Bucketed MAX lines: each bucket's peak lands in its row and in the chart window of the peak event.
	for _, b := range scans {
		union()
		q.add("\tSELECT " + pageRowExpr(pageKeys) + " AS page_row, w, col, idx, q, n FROM (\n")
		q.add("\tSELECT cell.1 AS rk, cell.2 AS row_text, cell.3 AS w, cell.4 AS col, idx, q, n FROM (\n")
		writeBucketPeaks(q, "(rk, row_text, w, col, pass)", 5, func() {
			writeEventLineJoin(q, p.Scope, p.Lines, b.lines, p.StartTime, p.EndTime, b.scanColumns(", "+bucket+" AS bucket"),
				b.props(lo.Uniq(append(rowProps, filterProps(p)...))),
				func(q *sqlBuilder) {
					writeCell(q)
					writePass(q, p)
					b.writeBucketColumns(q)
					q.add("r.idx AS idx, e.qty_total AS qty_total")
				},
				nil, lineFilters(p))
		})
		q.add("\n\t))")
		search("row_text")
	}
	q.add(`
)
GROUP BY GROUPING SETS ((page_row, w, col), (w, col))
HAVING page_row != 4294967295
` + settings)

	rows, err := r.store.GetConn().Query(q.context(ctx), q.sql.String())
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to query analytics points").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var cells []events.AnalyticsV2PointCell
	for rows.Next() {
		var pageRow uint32
		var w time.Time
		var col string
		var a events.AnalyticsV2Amounts
		var quantity decimal.Decimal
		var n uint64
		if err := rows.Scan(&pageRow, &w, &col, &a.Usage, &a.CommitmentUtilized, &a.Overage, &a.TrueUp, &a.Discount,
			&quantity, &n); err != nil {
			return nil, ierr.WithError(err).WithHint("Failed to scan analytics points").Mark(ierr.ErrDatabase)
		}
		cells = append(cells, events.AnalyticsV2PointCell{
			Row: int(pageRow) - 1, WindowStart: w.UTC(), Column: col,
			Amounts: a, Quantity: quantity, EventCount: n,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to read analytics points").Mark(ierr.ErrDatabase)
	}
	return cells, nil
}

// queryDistinct fills each page row's distinct count per key, with values when there are few.
func (r *MeterUsageRepository) queryDistinct(ctx context.Context, params *events.AnalyticsV2DetailParams, out []map[string]events.AnalyticsV2Distinct) error {
	p := &params.Rank
	_, settings := readSettings(p.Scope.UseFinal)

	q := &sqlBuilder{}
	pageKeys := q.param("Array(String)", stringArrayLiteral(lo.Map(params.Rows, func(row []string, _ int) string {
		return strings.Join(row, rowKeySep)
	})))
	q.add("SELECT indexOf(" + pageKeys + ", rk) AS page_row")
	for i := range params.DistinctKeys {
		q.add(fmt.Sprintf(", uniqExact(d%[1]d), groupUniqArray(%[2]d)(d%[1]d)", i, distinctSampleSize+1))
	}
	q.add("\nFROM (\n")
	writeEventLineJoin(q, p.Scope, p.Lines, lineIndexes(p.Lines, nil), p.StartTime, p.EndTime, "",
		propertyNames(append(append([]string{}, p.GroupBy...), params.DistinctKeys...)...),
		func(q *sqlBuilder) {
			writeRowKey(q, p.GroupBy, p)
			for i, k := range params.DistinctKeys {
				keyExpr(q, k, "e.", p)
				q.add(fmt.Sprintf(" AS d%d, ", i))
			}
			q.add("r.idx AS idx")
		},
		scanFilters(p), lineFilters(p))
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
			if counts[i] <= distinctSampleSize {
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

// distinctSampleSize is how many distinct values are listed before only the count is returned.
const distinctSampleSize = 5
