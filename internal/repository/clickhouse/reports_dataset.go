package clickhouse

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

var datasetAggFn = map[string]string{
	"count":          "count",
	"count_distinct": "uniqExact",
	"sum":            "sum",
	"avg":            "avg",
	"median":         "quantileExact",
	"min":            "min",
	"max":            "max",
}

var datasetCmpOp = map[types.FilterOperatorType]string{
	types.EQUAL:              "=",
	types.GREATER_THAN:       ">",
	types.GREATER_THAN_EQUAL: ">=",
	types.LESS_THAN:          "<",
	analytics.FilterOpLTE:    "<=",
}

func datasetInvalid(format string, args ...any) error {
	return ierr.NewErrorf(format, args...).Mark(ierr.ErrValidation)
}

func datasetParamType(k analytics.FieldKind) string {
	switch k {
	case analytics.FieldKindNumber:
		return "Decimal(38, 9)"
	case analytics.FieldKindBool:
		return "Bool"
	}
	return "String"
}

func buildDatasetQuery(q *analytics.DatasetQuery, db, usageDB, tenantID, environmentID string, maxRows int) (*reportQuery, error) {
	usageDB, err := checkReportScope(db, usageDB, tenantID, environmentID)
	if err != nil {
		return nil, err
	}
	if q == nil || q.Def == nil {
		return nil, datasetInvalid("dataset query is required")
	}
	def := q.Def
	ds, ok := analytics.LookupDataset(def.Dataset)
	if !ok {
		return nil, datasetInvalid("unknown dataset %q", def.Dataset)
	}
	if ds.MaxRangeDays > 0 && q.To.Sub(q.From).Hours() > float64(24*ds.MaxRangeDays) {
		return nil, ierr.NewErrorf("range exceeds %d days", ds.MaxRangeDays).
			WithHint(fmt.Sprintf("dataset %s allows at most %d days", ds.Key, ds.MaxRangeDays)).
			Mark(ierr.ErrValidation)
	}
	tf, ok := ds.Field(def.Time.Field)
	if !ok || tf.Kind != analytics.FieldKindTime {
		return nil, datasetInvalid("invalid time field %q", def.Time.Field)
	}
	shape := analytics.DatasetShape(*def)
	grainFn, ok := billingGrainFn[def.Time.Grain]
	if !ok {
		return nil, datasetInvalid("grain %q not supported", def.Time.Grain)
	}
	if len(def.Measures) == 0 {
		return nil, datasetInvalid("at least one measure is required")
	}

	params := map[string]string{
		"tenant_id":      tenantID,
		"environment_id": environmentID,
		"from":           q.From.UTC().Format(chTimeLayout),
		"to":             q.To.UTC().Format(chTimeLayout),
	}
	timeExpr := tf.Expr
	// Null times are stored as epoch.
	if !ds.Usage {
		timeExpr = fmt.Sprintf("nullIf(%s, %s)", tf.Expr, chEpoch)
	}
	win, err := buildTimeWindow(tf.Expr, timeExpr, q.From, q.To, q.Compare, params)
	if err != nil {
		return nil, err
	}
	used := []string{timeExpr}

	var selects, groups []string
	var cols []analytics.ReportColumn
	if shape == types.ShapeTimeseries {
		selects = append(selects, bucketSelect(grainFn, win.Bucket))
		groups = append(groups, "bucket")
		cols = append(cols, analytics.ReportColumn{Name: "bucket", Type: types.ColumnTypeDatetime, Role: types.ColumnRoleDimension})
	}
	dims := slices.Clone(def.Dimensions)
	var curMeasures, prevMeasures []string
	money := false
	for _, m := range def.Measures {
		if m == nil {
			return nil, datasetInvalid("empty measure")
		}
		fn, ok := datasetAggFn[m.Agg]
		if !ok {
			return nil, datasetInvalid("unknown aggregation %q", m.Agg)
		}
		arg := ""
		if m.Agg != "count" {
			f, ok := ds.Field(m.Field)
			if !ok || (!f.Measure && !f.Group) {
				return nil, datasetInvalid("invalid measure field %q", m.Field)
			}
			if m.Agg != "count_distinct" && (!f.Measure || f.Kind != analytics.FieldKindNumber) {
				return nil, datasetInvalid("%s not allowed on field %q", m.Agg, m.Field)
			}
			arg = f.Expr
			used = append(used, arg)
			money = money || (f.Money && m.Agg != "count_distinct")
		}
		if q.Compare != "" {
			curMeasures = append(curMeasures, datasetAgg(m.Agg, fn, arg, win.Cur))
			prevMeasures = append(prevMeasures, datasetAgg(m.Agg, fn, arg, win.Prev))
			continue
		}
		curMeasures = append(curMeasures, datasetAgg(m.Agg, fn, arg, ""))
	}
	if money && !slices.Contains(dims, "currency") {
		dims = append(dims, "currency")
	}
	for _, d := range dims {
		f, ok := ds.Field(d)
		if !ok || !f.Group {
			return nil, datasetInvalid("invalid dimension %q", d)
		}
		selects = append(selects, fmt.Sprintf("ifNull(toString(%s), '') AS d_%s", f.Expr, d))
		groups = append(groups, "d_"+d)
		cols = append(cols, analytics.ReportColumn{Name: d, Type: types.ColumnTypeString, Role: types.ColumnRoleDimension})
		used = append(used, f.Expr)
		if f.LabelExpr != "" {
			selects = append(selects, fmt.Sprintf("ifNull(toString(any(%s)), '') AS d_%s_label", f.LabelExpr, d))
			cols = append(cols, analytics.ReportColumn{Name: d + "_label", Type: types.ColumnTypeString, Role: types.ColumnRoleDimension})
			used = append(used, f.LabelExpr)
		}
	}
	for i, m := range curMeasures {
		selects = append(selects, fmt.Sprintf("ifNull(toString(%s), '') AS m%d", m, i))
		cols = append(cols, analytics.ReportColumn{Name: fmt.Sprintf("m%d", i), Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric})
	}
	for i, m := range prevMeasures {
		selects = append(selects, fmt.Sprintf("ifNull(toString(%s), '') AS m%d_prev", m, i))
		cols = append(cols, analytics.ReportColumn{Name: fmt.Sprintf("m%d_prev", i), Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric})
	}

	var where []string
	if ds.Usage {
		where = []string{ds.Alias + ".tenant_id = {tenant_id:String}", ds.Alias + ".environment_id = {environment_id:String}"}
	} else {
		where = mirroredScope(ds.Alias + ".")
	}
	where = append(where, win.Where)
	for i, flt := range def.Filters {
		if flt == nil {
			continue
		}
		f, ok := ds.Field(flt.Field)
		if !ok || !f.Filter {
			return nil, datasetInvalid("invalid filter field %q", flt.Field)
		}
		used = append(used, f.Expr)
		ph := make([]string, len(flt.Value))
		for j, v := range flt.Value {
			name := fmt.Sprintf("f%d_%d", i, j)
			params[name] = v
			ph[j] = "{" + name + ":" + datasetParamType(f.Kind) + "}"
		}
		cond, err := datasetFilterCond(f.Expr, flt.Op, ph)
		if err != nil {
			return nil, err
		}
		where = append(where, cond)
	}

	var b strings.Builder
	b.WriteString("SELECT " + strings.Join(selects, ", "))
	if ds.Usage {
		b.WriteString(" FROM " + usageDB + "." + ds.Table + " AS " + ds.Alias)
	} else {
		b.WriteString(" FROM " + db + "." + ds.Table + " AS " + ds.Alias)
	}
	for _, j := range ds.Joins {
		if !slices.ContainsFunc(used, regexp.MustCompile(`\b`+j.Alias+`\.`).MatchString) {
			continue
		}
		b.WriteString(" LEFT JOIN (" + joinSubquery(db, j) + ") AS " + j.Alias + " ON " + j.On)
	}
	b.WriteString(" WHERE " + strings.Join(where, " AND "))
	if len(groups) > 0 {
		b.WriteString(" GROUP BY " + strings.Join(groups, ", "))
	}
	if shape == types.ShapeBreakdown {
		b.WriteString(" ORDER BY " + curMeasures[0] + " DESC")
	} else if len(groups) > 0 {
		b.WriteString(" ORDER BY " + strings.Join(groups, ", "))
	}
	topN := sqlTopN(shape, def.Limit, def.Sort, "m0")
	b.WriteString(topNLimit(topN, def.Limit, maxRows))

	return &reportQuery{SQL: b.String(), Params: params, Columns: cols, TopN: topN}, nil
}

func datasetFilterCond(expr string, op types.FilterOperatorType, ph []string) (string, error) {
	switch op {
	case types.IN:
		return fmt.Sprintf("%s IN (%s)", expr, strings.Join(ph, ", ")), nil
	case types.NOT_IN:
		return fmt.Sprintf("%s NOT IN (%s)", expr, strings.Join(ph, ", ")), nil
	case analytics.FilterOpBetween:
		if len(ph) != 2 {
			return "", datasetInvalid("between takes 2 values")
		}
		return fmt.Sprintf("(%s >= %s AND %s <= %s)", expr, ph[0], expr, ph[1]), nil
	}
	sym, ok := datasetCmpOp[op]
	if !ok || len(ph) != 1 {
		return "", datasetInvalid("invalid filter operator %q", op)
	}
	return fmt.Sprintf("%s %s %s", expr, sym, ph[0]), nil
}

// joinSubquery dedups non-id joins.
func joinSubquery(db string, j analytics.DatasetJoin) string {
	scope := strings.Join(mirroredScope(""), " AND ")
	lhs, _, _ := strings.Cut(j.On, " = ")
	key := strings.TrimPrefix(lhs, j.Alias+".")
	if key == "id" {
		return fmt.Sprintf("SELECT %s FROM %s.%s WHERE %s", strings.Join(j.Cols, ", "), db, j.Table, scope)
	}
	sel := []string{key}
	for _, c := range j.Cols {
		if c != key {
			sel = append(sel, fmt.Sprintf("any(%s) AS %s", c, c))
		}
	}
	return fmt.Sprintf("SELECT %s FROM %s.%s WHERE %s GROUP BY %s", strings.Join(sel, ", "), db, j.Table, scope, key)
}

func datasetAgg(agg, fn, arg, cond string) string {
	params := ""
	if agg == "median" {
		params = "(0.5)"
	}
	if cond == "" {
		return fmt.Sprintf("%s%s(%s)", fn, params, arg)
	}
	if arg == "" {
		return fmt.Sprintf("%sIf%s(%s)", fn, params, cond)
	}
	return fmt.Sprintf("%sIf%s(%s, %s)", fn, params, arg, cond)
}
