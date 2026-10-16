package clickhouse

import (
	"github.com/flexprice/flexprice/internal/types"
)

// billingSQL holds a metric's ClickHouse fragments.
type billingSQL struct {
	Table   string
	TimeCol string
	Measure string
	// Aggregate name and argument.
	Agg   string
	Arg   string
	Where []string
	// Dimension expressions differing from name.
	DimExprs map[string]string
}

var billingSQLs = map[types.Metric]*billingSQL{
	types.MetricInvoiceTotal: {
		Table:   "invoices",
		TimeCol: "finalized_at",
		Measure: "sum(total)",
		Agg:     "sum",
		Arg:     "total",
		Where:   []string{"invoice_status = '" + string(types.InvoiceStatusFinalized) + "'"},
	},
}

func (s *billingSQL) dimExpr(d string) string {
	if e, ok := s.DimExprs[d]; ok {
		return e
	}
	return d
}
