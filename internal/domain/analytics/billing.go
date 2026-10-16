package analytics

import (
	"github.com/flexprice/flexprice/internal/types"
)

// BillingMetric describes one billing-family aggregate.
type BillingMetric struct {
	Metric     types.Metric
	Dimensions []string
	Shapes     []types.Shape
	// Forces currency into the group-by.
	Currency bool
	// Point-in-time metric; no time window.
	Snapshot bool
}

var allShapes = []types.Shape{types.ShapeScalar, types.ShapeTimeseries, types.ShapeBreakdown}

func invoiceTotalMetric(m types.Metric) *BillingMetric {
	return &BillingMetric{
		Metric:     m,
		Dimensions: []string{"currency", "customer_id", "invoice_type"},
		Shapes:     allShapes,
		Currency:   true,
	}
}

func currencyMetric(m types.Metric, shapes ...types.Shape) *BillingMetric {
	return &BillingMetric{
		Metric:     m,
		Dimensions: []string{"currency"},
		Shapes:     shapes,
		Currency:   true,
	}
}

var billingMetrics = map[types.Metric]*BillingMetric{
	types.MetricTotalReceivables: {
		Metric:     types.MetricTotalReceivables,
		Dimensions: []string{"currency"},
		Shapes:     []types.Shape{types.ShapeScalar, types.ShapeBreakdown},
		Currency:   true,
		Snapshot:   true,
	},
	types.MetricReceivablesAging: {
		Metric:     types.MetricReceivablesAging,
		Dimensions: []string{"aging_bucket", "customer_id", "currency"},
		Shapes:     []types.Shape{types.ShapeBreakdown},
		Currency:   true,
		Snapshot:   true,
	},
	types.MetricDaysToPay: {
		Metric:     types.MetricDaysToPay,
		Dimensions: []string{"customer_id", "currency"},
		Shapes:     allShapes,
		Currency:   true,
	},
	types.MetricARPUPaid: currencyMetric(types.MetricARPUPaid, types.ShapeScalar, types.ShapeTimeseries),
	types.MetricARPU:     currencyMetric(types.MetricARPU, types.ShapeScalar, types.ShapeTimeseries),
	types.MetricCreditConsumption: {
		Metric:     types.MetricCreditConsumption,
		Dimensions: []string{"customer_id", "currency"},
		Shapes:     allShapes,
		Currency:   true,
	},
	types.MetricCreditTopups: {
		Metric:     types.MetricCreditTopups,
		Dimensions: []string{"transaction_reason", "customer_id", "currency"},
		Shapes:     allShapes,
		Currency:   true,
	},
	types.MetricInvoiceTotal:       invoiceTotalMetric(types.MetricInvoiceTotal),
	types.MetricInvoiceTotalIssued: invoiceTotalMetric(types.MetricInvoiceTotalIssued),
	types.MetricNetBilling:         currencyMetric(types.MetricNetBilling, types.ShapeScalar, types.ShapeTimeseries),
	types.MetricNetBillingIssued:   currencyMetric(types.MetricNetBillingIssued, types.ShapeScalar, types.ShapeTimeseries),
	types.MetricActiveCustomers:    currencyMetric(types.MetricActiveCustomers, types.ShapeScalar),
	types.MetricEventsVolume: {
		Metric:     types.MetricEventsVolume,
		Dimensions: []string{"meter_id"},
		Shapes:     []types.Shape{types.ShapeTimeseries, types.ShapeBreakdown},
	},
}

func LookupBillingMetric(m types.Metric) (*BillingMetric, bool) {
	bm, ok := billingMetrics[m]
	return bm, ok
}

// BillingMetrics lists every registered metric key.
func BillingMetrics() []types.Metric {
	out := make([]types.Metric, 0, len(billingMetrics))
	for m := range billingMetrics {
		out = append(out, m)
	}
	return out
}
