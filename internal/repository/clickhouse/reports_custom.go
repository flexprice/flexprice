package clickhouse

import (
	"slices"
	"strconv"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

const bucketFormat = "'%Y-%m-%dT%H:%i:%SZ'"

func rejectCustom(in buildInput, metric types.Metric, shapes ...types.Shape) error {
	rv := in.RV
	if rv.Compare != "" {
		return ierr.NewErrorf("metric %q does not support compare", metric).
			WithHint("remove compare for this metric").
			Mark(ierr.ErrValidation)
	}
	if !slices.Contains(shapes, rv.Shape) {
		return ierr.NewErrorf("shape %q not supported for metric %q", rv.Shape, metric).
			Mark(ierr.ErrValidation)
	}
	for _, d := range rv.Dimensions {
		if d != "currency" {
			return ierr.NewErrorf("illegal dimension %q for metric %q", d, metric).Mark(ierr.ErrValidation)
		}
	}
	for _, f := range rv.Filters {
		if f != nil && f.Field != "currency" {
			return ierr.NewErrorf("filter %q not supported for metric %q", f.Field, metric).Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// currencyCond binds currency filters as params.
func currencyCond(in buildInput, alias string) string {
	var conds []string
	for i, f := range in.RV.Filters {
		if f == nil {
			continue
		}
		if len(f.Value) == 0 {
			conds = append(conds, "1 = 0")
			continue
		}
		ph := make([]string, len(f.Value))
		for j, v := range f.Value {
			name := "f" + strconv.Itoa(i) + "_" + strconv.Itoa(j)
			in.Params[name] = v
			ph[j] = "{" + name + ":String}"
		}
		conds = append(conds, alias+".currency IN ("+strings.Join(ph, ", ")+")")
	}
	if len(conds) == 0 {
		return ""
	}
	return " AND " + strings.Join(conds, " AND ")
}

func limitClause(maxRows int) string {
	if maxRows > 0 {
		return " LIMIT " + strconv.Itoa(maxRows+1)
	}
	return ""
}

// Invoices plus negative finalized credit notes.
func buildNetBilling(m types.Metric, invoiceTime func(prefix string) string, in buildInput) (*reportQuery, error) {
	if err := rejectCustom(in, m, types.ShapeScalar, types.ShapeTimeseries); err != nil {
		return nil, err
	}
	grainFn, ok := billingGrainFn[in.RV.Time.Grain]
	if !ok {
		return nil, ierr.NewErrorf("grain %q not supported for billing", in.RV.Time.Grain).Mark(ierr.ErrValidation)
	}
	timeseries := in.RV.Shape == types.ShapeTimeseries

	var b strings.Builder
	var cols []analytics.ReportColumn
	b.WriteString("SELECT ")
	groups := "d_currency"
	if timeseries {
		b.WriteString("ifNull(formatDateTime(toDateTime(" + grainFn + "(b, 'UTC'), 'UTC'), " + bucketFormat + "), '') AS bucket, ")
		groups = "bucket, d_currency"
		cols = append(cols, analytics.ReportColumn{Name: "bucket", Type: types.ColumnTypeDatetime, Role: types.ColumnRoleDimension})
	}
	b.WriteString("ifNull(toString(c), '') AS d_currency, ifNull(toString(sum(v)), '') AS m_value FROM (")
	b.WriteString("SELECT nullIf(" + invoiceTime("") + ", " + chEpoch + ") AS b, currency AS c, toDecimal128(total, 9) AS v")
	b.WriteString(" FROM " + in.DB + ".invoices AS i FINAL WHERE " + in.Scope("i"))
	b.WriteString(" AND i.invoice_status = '" + string(types.InvoiceStatusFinalized) + "'")
	b.WriteString(" AND " + in.TimeCond(invoiceTime("i.")) + currencyCond(in, "i"))
	b.WriteString(" UNION ALL ")
	b.WriteString("SELECT nullIf(created_at, " + chEpoch + "), currency, -toDecimal128(total_amount, 9)")
	b.WriteString(" FROM " + in.DB + ".credit_notes AS n FINAL WHERE " + in.Scope("n"))
	b.WriteString(" AND n.credit_note_status = '" + string(types.CreditNoteStatusFinalized) + "'")
	b.WriteString(" AND " + in.TimeCond("n.created_at") + currencyCond(in, "n"))
	b.WriteString(") GROUP BY " + groups + " ORDER BY " + groups + limitClause(in.MaxRows))

	cols = append(cols,
		analytics.ReportColumn{Name: "currency", Type: types.ColumnTypeString, Role: types.ColumnRoleDimension},
		analytics.ReportColumn{Name: string(m), Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric},
	)
	return &reportQuery{SQL: b.String(), Params: in.Params, Columns: cols}, nil
}

// Customers with subscriptions overlapping window.
func buildActiveCustomers(in buildInput) (*reportQuery, error) {
	if err := rejectCustom(in, types.MetricActiveCustomers, types.ShapeScalar); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("SELECT ifNull(toString(currency), '') AS d_currency, ifNull(toString(uniqExact(customer_id)), '') AS m_value")
	b.WriteString(" FROM " + in.DB + ".subscriptions AS s FINAL WHERE " + in.Scope("s"))
	b.WriteString(" AND s.subscription_status IN ('" + string(types.SubscriptionStatusActive) + "', '" + string(types.SubscriptionStatusTrialing) + "', '" + string(types.SubscriptionStatusCancelled) + "')")
	b.WriteString(" AND s.start_date < toDateTime64({to:String}, 6, 'UTC')")
	endExpr := "coalesce(nullIf(s.cancelled_at, " + chEpoch + "), nullIf(s.end_date, " + chEpoch + "))"
	// Cancelled rows need a real end.
	b.WriteString(" AND (s.subscription_status != '" + string(types.SubscriptionStatusCancelled) + "' OR " + endExpr + " IS NOT NULL)")
	b.WriteString(" AND coalesce(" + endExpr + ", toDateTime64('2299-12-31 00:00:00', 6, 'UTC')) > toDateTime64({from:String}, 6, 'UTC')")
	b.WriteString(currencyCond(in, "s"))
	b.WriteString(" GROUP BY d_currency ORDER BY d_currency" + limitClause(in.MaxRows))

	return &reportQuery{SQL: b.String(), Params: in.Params, Columns: []analytics.ReportColumn{
		{Name: "currency", Type: types.ColumnTypeString, Role: types.ColumnRoleDimension},
		{Name: string(types.MetricActiveCustomers), Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric},
	}}, nil
}
