package clickhouse

import (
	"slices"
	"strconv"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

const chSecondLayout = "2006-01-02 15:04:05"

// buildEventsVolume reads raw meter_usage; no FINAL.
func buildEventsVolume(in buildInput) (*reportQuery, error) {
	rv := in.RV
	if rv.Compare != "" {
		return nil, ierr.NewError("events_volume does not support compare").
			WithHint("remove compare for this metric").
			Mark(ierr.ErrValidation)
	}
	grainFn, ok := billingGrainFn[rv.Time.Grain]
	if !ok {
		return nil, ierr.NewErrorf("grain %q not supported for billing", rv.Time.Grain).Mark(ierr.ErrValidation)
	}
	timeseries := rv.Shape == types.ShapeTimeseries
	if !timeseries && rv.Shape != types.ShapeBreakdown {
		return nil, ierr.NewErrorf("shape %q not supported for metric %q", rv.Shape, types.MetricEventsVolume).Mark(ierr.ErrValidation)
	}
	for _, d := range rv.Dimensions {
		if d != "meter_id" {
			return nil, ierr.NewErrorf("illegal dimension %q for metric %q", d, types.MetricEventsVolume).Mark(ierr.ErrValidation)
		}
	}
	byMeter := !timeseries || slices.Contains(rv.Dimensions, "meter_id")

	in.Params["from_ts"] = rv.Time.From.UTC().Format(chSecondLayout)
	in.Params["to_ts"] = rv.Time.To.UTC().Format(chSecondLayout)

	where := []string{
		"tenant_id = {tenant_id:String}",
		"environment_id = {environment_id:String}",
		"timestamp >= toDateTime({from_ts:String}, 'UTC')",
		"timestamp < toDateTime({to_ts:String}, 'UTC')",
	}
	for i, f := range rv.Filters {
		if f == nil {
			continue
		}
		if f.Field != "meter_id" {
			return nil, ierr.NewErrorf("illegal filter field %q for metric %q", f.Field, types.MetricEventsVolume).Mark(ierr.ErrValidation)
		}
		if len(f.Value) == 0 {
			where = append(where, "1 = 0")
			continue
		}
		ph := make([]string, len(f.Value))
		for j, v := range f.Value {
			name := "f" + strconv.Itoa(i) + "_" + strconv.Itoa(j)
			in.Params[name] = v
			ph[j] = "{" + name + ":String}"
		}
		where = append(where, "meter_id IN ("+strings.Join(ph, ", ")+")")
	}

	var selects, groups []string
	var cols []analytics.ReportColumn
	if timeseries {
		selects = append(selects, "formatDateTime("+grainFn+"(timestamp, 'UTC'), "+bucketFormat+", 'UTC') AS bucket")
		groups = append(groups, "bucket")
		cols = append(cols, analytics.ReportColumn{Name: "bucket", Type: types.ColumnTypeDatetime, Role: types.ColumnRoleDimension})
	}
	if byMeter {
		selects = append(selects, "meter_id")
		groups = append(groups, "meter_id")
		cols = append(cols, analytics.ReportColumn{Name: "meter_id", Type: types.ColumnTypeString, Role: types.ColumnRoleDimension})
	}
	selects = append(selects, "toString(count()) AS events_volume")
	cols = append(cols, analytics.ReportColumn{Name: string(types.MetricEventsVolume), Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric})

	var b strings.Builder
	b.WriteString("SELECT " + strings.Join(selects, ", "))
	b.WriteString(" FROM " + in.UsageDB + ".meter_usage WHERE " + strings.Join(where, " AND "))
	b.WriteString(" GROUP BY " + strings.Join(groups, ", "))
	if timeseries {
		b.WriteString(" ORDER BY " + strings.Join(groups, ", "))
	} else {
		b.WriteString(" ORDER BY count() DESC, meter_id")
	}
	topN := billingTopN(rv, types.MetricEventsVolume)
	switch {
	case topN && in.MaxRows > 0:
		b.WriteString(" LIMIT " + strconv.Itoa(min(rv.Limit, in.MaxRows)))
	case topN:
		b.WriteString(" LIMIT " + strconv.Itoa(rv.Limit))
	default:
		b.WriteString(limitClause(in.MaxRows))
	}
	return &reportQuery{SQL: b.String(), Params: in.Params, Columns: cols, TopN: topN}, nil
}
