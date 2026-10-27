package clickhouse

import (
	"strings"

	"github.com/flexprice/flexprice/internal/types"
)

// billingSQL holds a metric's ClickHouse fragments.
type billingSQL struct {
	Table   string
	TimeCol string
	Measure string
	// Aggregate name and argument.
	Agg string
	Arg string
	// Conditional Measure for ratio metrics.
	CompareMeasure func(cond string) string
	Where          []string
	// Dimension expressions differing from name.
	DimExprs map[string]string
	// Replaces the generic builder.
	Build func(buildInput) (*reportQuery, error)
}

func (s *billingSQL) dimExpr(d string) string {
	if e, ok := s.DimExprs[d]; ok {
		return e
	}
	return d
}

const agingBucketExpr = "multiIf(nullIf(due_date, toDateTime64(0, 6)) IS NULL, 'No due date', " +
	"dateDiff('day', due_date, now64(6, 'UTC'), 'UTC') <= 0, 'Current', " +
	"dateDiff('day', due_date, now64(6, 'UTC'), 'UTC') <= 30, '1-30', " +
	"dateDiff('day', due_date, now64(6, 'UTC'), 'UTC') <= 60, '31-60', " +
	"dateDiff('day', due_date, now64(6, 'UTC'), 'UTC') <= 90, '61-90', '90+')"

func invoiceFinalizedTime(prefix string) string { return prefix + "finalized_at" }

func invoiceTotalSQL(timeCol string) *billingSQL {
	return &billingSQL{
		Table:   "invoices",
		TimeCol: timeCol,
		Measure: "sum(total)",
		Agg:     "sum",
		Arg:     "total",
		Where:   []string{"invoice_status = '" + string(types.InvoiceStatusFinalized) + "'"},
	}
}

func netBillingSQL(m types.Metric, invoiceTime func(prefix string) string) *billingSQL {
	return &billingSQL{
		Build: func(in buildInput) (*reportQuery, error) {
			return buildNetBilling(m, invoiceTime, in)
		},
	}
}

var (
	paidIn = "payment_status IN ('" + string(types.PaymentStatusSucceeded) + "','" + string(types.PaymentStatusOverpaid) + "')"
	// Open invoices with a balance.
	receivableWhere = []string{
		"invoice_status = '" + string(types.InvoiceStatusFinalized) + "'",
		"amount_remaining > 0",
	}
	creditTopupReasons = []types.TransactionReason{
		types.TransactionReasonPurchasedCreditDirect,
		types.TransactionReasonPurchasedCreditInvoiced,
		types.TransactionReasonPurchasedCreditBonus,
		types.TransactionReasonFreeCredit,
		types.TransactionReasonSubscriptionCredit,
	}
)

func ratioIf(col, cond string) string {
	return "if(uniqExactIf(customer_id, " + cond + ") = 0, NULL, sumIf(" + col + ", " + cond + ") / uniqExactIf(customer_id, " + cond + "))"
}

func quotedList[T ~string](vs []T) string {
	q := make([]string, len(vs))
	for i, v := range vs {
		q[i] = "'" + string(v) + "'"
	}
	return strings.Join(q, ",")
}

var billingSQLs = map[types.Metric]*billingSQL{
	types.MetricTotalReceivables: {
		Table:   "invoices",
		TimeCol: "finalized_at",
		Measure: "sum(amount_remaining)",
		Agg:     "sum",
		Arg:     "amount_remaining",
		Where:   receivableWhere,
	},
	types.MetricReceivablesAging: {
		Table:    "invoices",
		TimeCol:  "finalized_at",
		Measure:  "sum(amount_remaining)",
		Agg:      "sum",
		Arg:      "amount_remaining",
		Where:    receivableWhere,
		DimExprs: map[string]string{"aging_bucket": agingBucketExpr},
	},
	types.MetricDaysToPay: {
		Table:   "invoices",
		TimeCol: "paid_at",
		Measure: "avg(dateDiff('day', issue_date, paid_at, 'UTC'))",
		Agg:     "avg",
		Arg:     "dateDiff('day', issue_date, paid_at, 'UTC')",
		Where: []string{
			paidIn,
			"paid_at > " + chEpoch,
			"issue_date > " + chEpoch,
		},
	},
	types.MetricARPUPaid: {
		Table:   "invoices",
		TimeCol: "paid_at",
		Measure: "sum(amount_paid) / uniqExact(customer_id)",
		CompareMeasure: func(c string) string {
			return ratioIf("amount_paid", c)
		},
		Where: []string{paidIn, "paid_at > " + chEpoch},
	},
	types.MetricARPU: {
		Table:   "invoices",
		TimeCol: "finalized_at",
		Measure: "sum(total) / uniqExact(customer_id)",
		CompareMeasure: func(c string) string {
			return ratioIf("total", c)
		},
		Where: []string{"invoice_status = '" + string(types.InvoiceStatusFinalized) + "'"},
	},
	types.MetricCreditConsumption: {
		Build: buildCreditConsumption,
	},
	types.MetricCreditTopups: {
		Table:   "wallet_transactions",
		TimeCol: "created_at",
		Measure: "sum(amount)",
		Agg:     "sum",
		Arg:     "amount",
		Where: []string{
			"type = '" + string(types.TransactionTypeCredit) + "'",
			"transaction_status = '" + string(types.TransactionStatusCompleted) + "'",
			"transaction_reason IN (" + quotedList(creditTopupReasons) + ")",
		},
	},
	types.MetricInvoiceTotal:       invoiceTotalSQL("finalized_at"),
	types.MetricInvoiceTotalIssued: invoiceTotalSQL("finalized_at"),
	types.MetricNetBilling:         netBillingSQL(types.MetricNetBilling, invoiceFinalizedTime),
	types.MetricNetBillingIssued:   netBillingSQL(types.MetricNetBillingIssued, invoiceFinalizedTime),
	types.MetricActiveCustomers:    {Build: buildActiveCustomers},
	types.MetricEventsVolume:       {Build: buildEventsVolume},
}
