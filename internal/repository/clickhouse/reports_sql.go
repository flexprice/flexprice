package clickhouse

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// reportQuery is compiled SQL plus parameters.
type reportQuery struct {
	SQL     string
	Params  map[string]string
	Columns []analytics.ReportColumn
	// TopN: LIMIT is the view limit.
	TopN bool
}

// buildInput is handed to custom builders.
type buildInput struct {
	RV *analytics.ResolvedView
	DB string
	// Database holding meter_usage.
	UsageDB string
	Params  map[string]string
	// Standard row invariants for alias.
	Scope func(alias string) string
	// Half-open window predicate on col.
	TimeCond func(col string) string
	MaxRows  int
}

const (
	chEpoch      = "toDateTime64(0, 6)"
	chTimeLayout = "2006-01-02 15:04:05.000000"
)

var chIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var billingGrainFn = map[types.Grain]string{
	"":               "toStartOfDay",
	types.GrainDay:   "toStartOfDay",
	types.GrainWeek:  "toMonday",
	types.GrainMonth: "toStartOfMonth",
}

func lookupBillingMetric(m types.Metric) (*analytics.BillingMetric, *billingSQL, bool) {
	bm, ok := analytics.LookupBillingMetric(m)
	if !ok {
		return nil, nil, false
	}
	sq, ok := billingSQLs[m]
	return bm, sq, ok
}

func buildBillingQuery(rv *analytics.ResolvedView, db, tenantID, environmentID string, maxRows int) (*reportQuery, error) {
	return buildBillingQueryWithUsage(rv, db, db, tenantID, environmentID, maxRows)
}

// buildBillingQueryWithUsage reads meter_usage from usageDB.
func buildBillingQueryWithUsage(rv *analytics.ResolvedView, db, usageDB, tenantID, environmentID string, maxRows int) (*reportQuery, error) {
	usageDB, err := checkReportScope(db, usageDB, tenantID, environmentID)
	if err != nil {
		return nil, err
	}
	if rv == nil || len(rv.Metrics) != 1 {
		return nil, ierr.NewError("billing views take exactly one metric").Mark(ierr.ErrValidation)
	}
	bm, sq, ok := lookupBillingMetric(rv.Metrics[0])
	if !ok {
		return nil, ierr.NewErrorf("unknown billing metric %q", rv.Metrics[0]).Mark(ierr.ErrValidation)
	}
	grainFn, ok := billingGrainFn[rv.Time.Grain]
	if !ok {
		return nil, ierr.NewErrorf("grain %q not supported for billing", rv.Time.Grain).Mark(ierr.ErrValidation)
	}

	dims := slices.Clone(rv.Dimensions)
	if bm.Currency && !slices.Contains(dims, "currency") {
		dims = append(dims, "currency")
	}

	params := map[string]string{
		"tenant_id":      tenantID,
		"environment_id": environmentID,
		"from":           rv.Time.From.UTC().Format(chTimeLayout),
		"to":             rv.Time.To.UTC().Format(chTimeLayout),
	}
	if sq.Build != nil {
		if rv.Offset > 0 {
			return nil, ierr.NewErrorf("offset not supported for metric %q", bm.Metric).Mark(ierr.ErrValidation)
		}
		return sq.Build(buildInput{
			RV:      rv,
			DB:      db,
			UsageDB: usageDB,
			Params:  params,
			Scope: func(alias string) string {
				return strings.Join(mirroredScope(alias+"."), " AND ")
			},
			TimeCond: func(col string) string {
				return fmt.Sprintf("%s >= toDateTime64({from:String}, 6, 'UTC') AND %s < toDateTime64({to:String}, 6, 'UTC')", col, col)
			},
			MaxRows: maxRows,
		})
	}
	if bm.Snapshot && (rv.Compare != "" || rv.Shape == types.ShapeTimeseries) {
		return nil, ierr.NewErrorf("metric %q is a snapshot", bm.Metric).
			WithHint("snapshot metrics support neither compare nor timeseries").
			Mark(ierr.ErrValidation)
	}
	if rv.Compare != "" && sq.Agg == "" && sq.CompareMeasure == nil {
		return nil, ierr.NewErrorf("metric %q does not support compare", bm.Metric).
			WithHint("remove compare for this metric").
			Mark(ierr.ErrValidation)
	}
	win, err := buildTimeWindow(sq.TimeCol, fmt.Sprintf("nullIf(%s, %s)", sq.TimeCol, chEpoch), rv.Time.From, rv.Time.To, rv.Compare, params)
	if err != nil {
		return nil, err
	}
	measure := sq.Measure
	prevMeasure := ""
	if rv.Compare != "" {
		if sq.Agg == "" {
			measure = sq.CompareMeasure(win.Cur)
			prevMeasure = sq.CompareMeasure(win.Prev)
		} else {
			measure = aggIf(sq.Agg, sq.Arg, win.Cur)
			prevMeasure = aggIf(sq.Agg, sq.Arg, win.Prev)
		}
	}

	var selects, groups []string
	var cols []analytics.ReportColumn
	if rv.Shape == types.ShapeTimeseries {
		selects = append(selects, bucketSelect(grainFn, win.Bucket))
		groups = append(groups, "bucket")
		cols = append(cols, analytics.ReportColumn{Name: "bucket", Type: types.ColumnTypeDatetime, Role: types.ColumnRoleDimension})
	}
	for _, d := range dims {
		if !slices.Contains(bm.Dimensions, d) {
			return nil, ierr.NewErrorf("illegal dimension %q for metric %q", d, bm.Metric).Mark(ierr.ErrValidation)
		}
		selects = append(selects, fmt.Sprintf("ifNull(toString(%s), '') AS d_%s", sq.dimExpr(d), d))
		groups = append(groups, "d_"+d)
		cols = append(cols, analytics.ReportColumn{Name: d, Type: types.ColumnTypeString, Role: types.ColumnRoleDimension})
	}
	selects = append(selects, fmt.Sprintf("ifNull(toString(%s), '') AS m_value", measure))
	cols = append(cols, analytics.ReportColumn{Name: string(bm.Metric), Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric})
	if prevMeasure != "" {
		selects = append(selects, fmt.Sprintf("ifNull(toString(%s), '') AS m_prev", prevMeasure))
		cols = append(cols, analytics.ReportColumn{Name: string(bm.Metric) + "_prev", Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric})
	}

	where := mirroredScope("")
	where = append(where, sq.Where...)
	if !bm.Snapshot {
		where = append(where, win.Where)
	}
	for i, f := range rv.Filters {
		if f == nil {
			continue
		}
		if !slices.Contains(bm.Dimensions, f.Field) {
			return nil, ierr.NewErrorf("illegal filter field %q for metric %q", f.Field, bm.Metric).Mark(ierr.ErrValidation)
		}
		if len(f.Value) == 0 {
			where = append(where, "1 = 0")
			continue
		}
		placeholders := make([]string, len(f.Value))
		for j, v := range f.Value {
			name := fmt.Sprintf("f%d_%d", i, j)
			params[name] = v
			placeholders[j] = "{" + name + ":String}"
		}
		where = append(where, fmt.Sprintf("%s IN (%s)", sq.dimExpr(f.Field), strings.Join(placeholders, ", ")))
	}

	var b strings.Builder
	b.WriteString("SELECT " + strings.Join(selects, ", "))
	b.WriteString(" FROM " + db + "." + sq.Table)
	b.WriteString(" WHERE " + strings.Join(where, " AND "))
	if len(groups) > 0 {
		b.WriteString(" GROUP BY " + strings.Join(groups, ", "))
	}
	if rv.Shape == types.ShapeBreakdown {
		b.WriteString(" ORDER BY " + measure + " DESC")
		if len(groups) > 0 {
			b.WriteString(", " + strings.Join(groups, ", "))
		}
	} else if len(groups) > 0 {
		b.WriteString(" ORDER BY " + strings.Join(groups, ", "))
	}
	topN := billingTopN(rv, bm.Metric)
	b.WriteString(topNLimit(topN, rv.Limit, rv.Offset, maxRows))

	return &reportQuery{SQL: b.String(), Params: params, Columns: cols, TopN: topN}, nil
}

// billingTopN: SQL order equals view order.
func billingTopN(rv *analytics.ResolvedView, metric types.Metric) bool {
	return sqlTopN(rv.Shape, rv.Limit, rv.Sort, string(metric))
}

func sqlTopN(shape types.Shape, limit int, sort []*analytics.SortSpec, metric string) bool {
	if shape != types.ShapeBreakdown || limit <= 0 {
		return false
	}
	switch len(sort) {
	case 0:
		return true
	case 1:
		s := sort[0]
		return s != nil && s.Field == metric && s.Dir == types.SortDirectionDesc
	}
	return false
}

func topNLimit(topN bool, limit, offset, maxRows int) string {
	skip := ""
	if offset > 0 && topN {
		skip = " OFFSET " + strconv.Itoa(offset)
	}
	switch {
	case topN && maxRows > 0:
		return " LIMIT " + strconv.Itoa(min(limit, maxRows)) + skip
	case topN:
		return " LIMIT " + strconv.Itoa(limit) + skip
	case maxRows > 0:
		// One extra row detects overflow.
		return " LIMIT " + strconv.Itoa(maxRows+1)
	}
	return ""
}

func checkReportScope(db, usageDB, tenantID, environmentID string) (string, error) {
	if tenantID == "" || environmentID == "" {
		return "", ierr.NewError("tenant and environment are required").Mark(ierr.ErrPermissionDenied)
	}
	if !chIdentifier.MatchString(db) {
		return "", ierr.NewErrorf("invalid reports database %q", db).Mark(ierr.ErrSystem)
	}
	if usageDB == "" {
		usageDB = db
	}
	if !chIdentifier.MatchString(usageDB) {
		return "", ierr.NewErrorf("invalid usage database %q", usageDB).Mark(ierr.ErrSystem)
	}
	return usageDB, nil
}

// mirroredScope: prefix is "alias." or empty.
func mirroredScope(prefix string) []string {
	return []string{
		prefix + "tenant_id = {tenant_id:String}",
		prefix + "environment_id = {environment_id:String}",
		prefix + "_peerdb_is_deleted = 0",
		prefix + "status = '" + string(types.StatusPublished) + "'",
	}
}

// timeWindow: Prev empty without compare.
type timeWindow struct {
	Cur, Prev, Bucket, Where string
}

// buildTimeWindow expects from/to params already set.
// Predicates use the raw column so skip indexes apply.
func buildTimeWindow(col, timeExpr string, from, to time.Time, compare string, params map[string]string) (timeWindow, error) {
	cur := fmt.Sprintf("%s >= toDateTime64({from:String}, 6, 'UTC') AND %s < toDateTime64({to:String}, 6, 'UTC')", col, col)
	w := timeWindow{Cur: cur, Bucket: timeExpr, Where: cur}
	if compare == "" {
		return w, nil
	}
	pf, pt, shiftMonths, err := analytics.PreviousWindow(from, to, compare)
	if err != nil {
		return w, err
	}
	params["prev_from"] = pf.UTC().Format(chTimeLayout)
	params["prev_to"] = pt.UTC().Format(chTimeLayout)
	w.Prev = fmt.Sprintf("%s >= toDateTime64({prev_from:String}, 6, 'UTC') AND %s < toDateTime64({prev_to:String}, 6, 'UTC')", col, col)
	shifted := fmt.Sprintf("addYears(%s, 1)", timeExpr)
	switch {
	case compare == "previous_period" && shiftMonths > 0:
		params["shift_m"] = strconv.Itoa(shiftMonths)
		shifted = fmt.Sprintf("addMonths(%s, {shift_m:Int32})", timeExpr)
	case compare == "previous_period":
		params["shift_s"] = strconv.FormatInt(int64(to.Sub(from).Seconds()), 10)
		shifted = fmt.Sprintf("addSeconds(%s, {shift_s:Int64})", timeExpr)
	}
	w.Bucket = fmt.Sprintf("if(%s, %s, %s)", cur, timeExpr, shifted)
	w.Where = fmt.Sprintf("((%s) OR (%s))", cur, w.Prev)
	return w, nil
}

func aggIf(agg, arg, cond string) string {
	if arg == "" {
		return fmt.Sprintf("%sIf(%s)", agg, cond)
	}
	return fmt.Sprintf("%sIf(%s, %s)", agg, arg, cond)
}

func bucketSelect(grainFn, expr string) string {
	return fmt.Sprintf("ifNull(formatDateTime(toDateTime(%s(%s, 'UTC'), 'UTC'), %s), '') AS bucket", grainFn, expr, bucketFormat)
}
