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

func buildCreditConsumption(in buildInput) (*reportQuery, error) {
	rv := in.RV
	if rv.Compare != "" {
		return nil, ierr.NewError("credit consumption does not support compare").Mark(ierr.ErrValidation)
	}
	grainFn, ok := billingGrainFn[rv.Time.Grain]
	if !ok {
		return nil, ierr.NewErrorf("grain %q not supported for credit consumption", rv.Time.Grain).Mark(ierr.ErrValidation)
	}
	timeseries := rv.Shape == types.ShapeTimeseries
	if rv.Shape != types.ShapeScalar && !timeseries && rv.Shape != types.ShapeBreakdown {
		return nil, ierr.NewErrorf("shape %q not supported for credit consumption", rv.Shape).Mark(ierr.ErrValidation)
	}

	dims := []string{"credit_type"}
	for _, d := range rv.Dimensions {
		if d != "credit_type" && d != "customer_id" && d != "currency" {
			return nil, ierr.NewErrorf("illegal dimension %q for credit consumption", d).Mark(ierr.ErrValidation)
		}
		if d != "currency" && !slices.Contains(dims, d) {
			dims = append(dims, d)
		}
	}
	if !slices.Contains(dims, "currency") {
		dims = append(dims, "currency")
	}
	if rv.Shape == types.ShapeBreakdown && len(rv.Dimensions) == 0 {
		return nil, ierr.NewError("credit consumption breakdown requires a dimension").Mark(ierr.ErrValidation)
	}

	var params = in.Params
	var b strings.Builder
	var cols []analytics.ReportColumn
	var selects, groups []string
	if timeseries {
		selects = append(selects, "ifNull(formatDateTime(toDateTime("+grainFn+"(d.created_at, 'UTC'), 'UTC'), "+bucketFormat+"), '') AS bucket")
		groups = append(groups, "bucket")
		cols = append(cols, analytics.ReportColumn{Name: "bucket", Type: types.ColumnTypeDatetime, Role: types.ColumnRoleDimension})
	}
	for _, d := range dims {
		expr := ""
		switch d {
		case "credit_type":
			expr = "if(ifNull(nullIf(g.transaction_reason, ''), 'PURCHASED_CREDIT_DIRECT') IN ('PURCHASED_CREDIT_DIRECT', 'PURCHASED_CREDIT_INVOICED'), 'paid', 'free')"
		case "customer_id":
			expr = "d.customer_id"
		case "currency":
			expr = "d.currency"
		}
		alias := "d_" + d
		selects = append(selects, "ifNull(toString("+expr+"), '') AS "+alias)
		groups = append(groups, alias)
		cols = append(cols, analytics.ReportColumn{Name: d, Type: types.ColumnTypeString, Role: types.ColumnRoleDimension})
	}
	selects = append(selects, "ifNull(toString(sum(if(item = '', d.credit_amount, toDecimal128OrZero(if(empty(JSONExtractString(item, 'credits')), JSONExtractRaw(item, 'credits'), JSONExtractString(item, 'credits')), 9)) * ifNull(d.conversion_rate, 1))), '') AS m_value")
	cols = append(cols, analytics.ReportColumn{Name: string(types.MetricCreditConsumption), Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric})

	b.WriteString("SELECT ")
	b.WriteString(strings.Join(selects, ", "))
	b.WriteString(" FROM (SELECT d.*, JSONExtractArrayRaw(ifNull(d.consumption_breakdown, '[]')) AS parts FROM " + in.DB + ".wallet_transactions AS d WHERE " + in.Scope("d"))
	b.WriteString(" AND d.type = 'debit' AND d.transaction_status = 'completed' AND d.transaction_reason = 'INVOICE_PAYMENT'")
	b.WriteString(" AND " + in.TimeCond("d.created_at"))
	b.WriteString(") AS d ARRAY JOIN if(empty(d.parts), [''], d.parts) AS item LEFT JOIN " + in.DB + ".wallet_transactions AS g ON g.id = JSONExtractString(item, 'credit_transaction_id') AND " + in.Scope("g"))
	var filterConds []string
	for i, f := range rv.Filters {
		if f == nil {
			continue
		}
		if f.Field != "customer_id" && f.Field != "currency" && f.Field != "credit_type" {
			return nil, ierr.NewErrorf("illegal filter field %q for credit consumption", f.Field).Mark(ierr.ErrValidation)
		}
		if len(f.Value) == 0 {
			filterConds = append(filterConds, "1 = 0")
			continue
		}
		placeholders := make([]string, len(f.Value))
		for j, v := range f.Value {
			name := "f" + strconv.Itoa(i) + "_" + strconv.Itoa(j)
			params[name] = v
			placeholders[j] = "{" + name + ":String}"
		}
		field := map[string]string{"customer_id": "d.customer_id", "currency": "d.currency", "credit_type": "if(ifNull(nullIf(g.transaction_reason, ''), 'PURCHASED_CREDIT_DIRECT') IN ('PURCHASED_CREDIT_DIRECT', 'PURCHASED_CREDIT_INVOICED'), 'paid', 'free')"}[f.Field]
		filterConds = append(filterConds, field+" IN ("+strings.Join(placeholders, ", ")+")")
	}
	if len(filterConds) > 0 {
		b.WriteString(" WHERE " + strings.Join(filterConds, " AND "))
	}
	b.WriteString(" GROUP BY " + strings.Join(groups, ", ") + " ORDER BY " + strings.Join(groups, ", ") + limitClause(in.MaxRows))
	return &reportQuery{SQL: b.String(), Params: params, Columns: cols}, nil
}

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
	b.WriteString(" FROM " + in.DB + ".invoices AS i WHERE " + in.Scope("i"))
	b.WriteString(" AND i.invoice_status = '" + string(types.InvoiceStatusFinalized) + "'")
	b.WriteString(" AND " + in.TimeCond(invoiceTime("i.")) + currencyCond(in, "i"))
	b.WriteString(" UNION ALL ")
	b.WriteString("SELECT nullIf(finalized_at, " + chEpoch + "), currency, -toDecimal128(total_amount, 9)")
	b.WriteString(" FROM " + in.DB + ".credit_notes AS n WHERE " + in.Scope("n"))
	b.WriteString(" AND n.credit_note_status = '" + string(types.CreditNoteStatusFinalized) + "'")
	b.WriteString(" AND " + in.TimeCond("n.finalized_at") + currencyCond(in, "n"))
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
	b.WriteString(" FROM " + in.DB + ".subscriptions AS s WHERE " + in.Scope("s"))
	b.WriteString(" AND s.subscription_status IN ('" + string(types.SubscriptionStatusActive) + "', '" + string(types.SubscriptionStatusTrialing) + "', '" + string(types.SubscriptionStatusCancelled) + "')")
	b.WriteString(" AND s.start_date < toDateTime64({to:String}, 6, 'UTC')")
	endExpr := "coalesce(nullIf(s.end_date, " + chEpoch + "), if(s.subscription_status = '" + string(types.SubscriptionStatusCancelled) + "', nullIf(s.cancelled_at, " + chEpoch + "), NULL))"
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

var macComponents = []string{"committed", "consumed", "overage", "true_up"}

func macDecimal(field string) string {
	return "toDecimal128OrZero(JSONExtractString(il.commitment_info, '" + field + "'), 9)"
}

// Per-customer commitment amounts from invoice line items.
func buildMACConsumption(in buildInput) (*reportQuery, error) {
	if err := rejectCustomDims(in, types.MetricMACConsumption, []string{"component", "customer_id", "currency"}, types.ShapeBreakdown); err != nil {
		return nil, err
	}
	filterConds, err := macFilters(in)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString("SELECT ifNull(toString(customer_id), '') AS d_customer_id, ifNull(toString(currency), '') AS d_currency, comp.1 AS d_component, ifNull(toString(sum(comp.2)), '') AS m_value FROM (")
	b.WriteString("SELECT il.customer_id AS customer_id, il.currency AS currency, ")
	b.WriteString(macDecimal("computed_commitment_utilized_amount") + " AS utilized, ")
	b.WriteString(macDecimal("computed_overage_amount") + " AS overage_amount, ")
	b.WriteString(macDecimal("computed_true_up_amount") + " AS true_up_amount, ")
	b.WriteString("if(JSONExtractString(il.commitment_info, 'type') = 'amount' AND " + macDecimal("amount") + " > 0, " + macDecimal("amount") + ", utilized + true_up_amount) AS committed_amount")
	b.WriteString(" FROM " + in.DB + ".invoice_line_items AS il INNER JOIN (SELECT id FROM " + in.DB + ".invoices AS i WHERE " + in.Scope("i"))
	b.WriteString(" AND i.invoice_status = '" + string(types.InvoiceStatusFinalized) + "') AS inv ON inv.id = il.invoice_id")
	b.WriteString(" WHERE " + in.Scope("il") + " AND il.commitment_info IS NOT NULL AND il.commitment_info NOT IN ('', 'null')")
	b.WriteString(" AND " + in.TimeCond("il.period_start"))
	b.WriteString(") ARRAY JOIN [('committed', committed_amount), ('consumed', utilized), ('overage', overage_amount), ('true_up', true_up_amount)] AS comp")
	if len(filterConds) > 0 {
		b.WriteString(" WHERE " + strings.Join(filterConds, " AND "))
	}
	b.WriteString(" GROUP BY d_customer_id, d_currency, d_component ORDER BY d_customer_id, d_currency, d_component" + limitClause(in.MaxRows))

	return &reportQuery{SQL: b.String(), Params: in.Params, Columns: []analytics.ReportColumn{
		{Name: "customer_id", Type: types.ColumnTypeString, Role: types.ColumnRoleDimension},
		{Name: "currency", Type: types.ColumnTypeString, Role: types.ColumnRoleDimension},
		{Name: "component", Type: types.ColumnTypeString, Role: types.ColumnRoleDimension},
		{Name: string(types.MetricMACConsumption), Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric},
	}}, nil
}

func rejectCustomDims(in buildInput, metric types.Metric, dims []string, shape types.Shape) error {
	rv := in.RV
	if rv.Compare != "" {
		return ierr.NewErrorf("metric %q does not support compare", metric).Mark(ierr.ErrValidation)
	}
	if rv.Shape != shape {
		return ierr.NewErrorf("shape %q not supported for metric %q", rv.Shape, metric).Mark(ierr.ErrValidation)
	}
	for _, d := range rv.Dimensions {
		if !slices.Contains(dims, d) {
			return ierr.NewErrorf("illegal dimension %q for metric %q", d, metric).Mark(ierr.ErrValidation)
		}
	}
	return nil
}

func macFilters(in buildInput) ([]string, error) {
	var conds []string
	for i, f := range in.RV.Filters {
		if f == nil {
			continue
		}
		if f.Field != "customer_id" && f.Field != "currency" && f.Field != "component" {
			return nil, ierr.NewErrorf("illegal filter field %q for mac consumption", f.Field).Mark(ierr.ErrValidation)
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
		field := map[string]string{"customer_id": "customer_id", "currency": "currency", "component": "comp.1"}[f.Field]
		conds = append(conds, field+" IN ("+strings.Join(ph, ", ")+")")
	}
	return conds, nil
}
