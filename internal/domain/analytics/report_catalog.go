package analytics

import (
	"slices"

	"github.com/flexprice/flexprice/internal/types"
)

type ReportDimension struct {
	Key   string
	Label string
}

// ReportCatalogEntry describes one canned report.
type ReportCatalogEntry struct {
	Key             types.Metric
	Name            string
	Description     string
	Category        types.ReportCategory
	Kind            types.ReportKind
	ValueType       types.ReportValueType
	Improvement     types.ImprovementDirection
	Shapes          []types.Shape
	DefaultShape    types.Shape
	Dimensions      []ReportDimension
	Grains          []types.Grain
	SupportsCompare bool
	VariantOf       types.Metric
	VariantLabel    string
	MaxRangeDays    int
}

var reportCatalog = []ReportCatalogEntry{
	{
		Key:             types.MetricInvoiceTotal,
		Name:            "Total billing",
		Description:     "Finalized invoice totals by finalization date",
		Category:        types.ReportCategoryBilling,
		Kind:            types.ReportKindFlow,
		ValueType:       types.ReportValueTypeMoney,
		Improvement:     types.ImprovementHigher,
		DefaultShape:    types.ShapeTimeseries,
		Grains:          []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
		SupportsCompare: true,
		VariantLabel:    "Finalized date",
	},
	{
		Key:             types.MetricInvoiceTotalIssued,
		Name:            "Total billing (finalized date)",
		Description:     "Finalized invoice totals by finalization date",
		Category:        types.ReportCategoryBilling,
		Kind:            types.ReportKindFlow,
		ValueType:       types.ReportValueTypeMoney,
		Improvement:     types.ImprovementHigher,
		DefaultShape:    types.ShapeTimeseries,
		Grains:          []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
		SupportsCompare: true,
		VariantOf:       types.MetricInvoiceTotal,
		VariantLabel:    "Finalized date",
	},
	{
		Key:          types.MetricTotalReceivables,
		Name:         "Total receivables",
		Description:  "Outstanding amount on finalized invoices right now",
		Category:     types.ReportCategoryReceivables,
		Kind:         types.ReportKindSnapshot,
		ValueType:    types.ReportValueTypeMoney,
		DefaultShape: types.ShapeScalar,
	},
	{
		Key:          types.MetricReceivablesAging,
		Name:         "Receivables aging",
		Description:  "Outstanding amount by days past due",
		Category:     types.ReportCategoryReceivables,
		Kind:         types.ReportKindSnapshot,
		ValueType:    types.ReportValueTypeMoney,
		DefaultShape: types.ShapeBreakdown,
	},
	{
		Key:             types.MetricDaysToPay,
		Name:            "Days to pay",
		Description:     "Average days from issue to payment for paid invoices",
		Category:        types.ReportCategoryReceivables,
		Kind:            types.ReportKindRatio,
		ValueType:       types.ReportValueTypeDays,
		DefaultShape:    types.ShapeTimeseries,
		Grains:          []types.Grain{types.GrainWeek, types.GrainMonth},
		SupportsCompare: true,
	},
	{
		Key:             types.MetricARPU,
		SupportsCompare: true,
		Name:            "ARPU",
		Description:     "Billed revenue per billed customer",
		Category:        types.ReportCategoryCustomers,
		Kind:            types.ReportKindRatio,
		ValueType:       types.ReportValueTypeMoney,
		Improvement:     types.ImprovementHigher,
		DefaultShape:    types.ShapeTimeseries,
		Grains:          []types.Grain{types.GrainMonth},
		VariantLabel:    "Billed",
	},
	{
		Key:             types.MetricARPUPaid,
		SupportsCompare: true,
		Name:            "ARPU (paid)",
		Description:     "Paid revenue per paying customer",
		Category:        types.ReportCategoryCustomers,
		Kind:            types.ReportKindRatio,
		ValueType:       types.ReportValueTypeMoney,
		Improvement:     types.ImprovementHigher,
		DefaultShape:    types.ShapeTimeseries,
		Grains:          []types.Grain{types.GrainMonth},
		VariantOf:       types.MetricARPU,
		VariantLabel:    "Paid",
	},
	{
		Key:             types.MetricCreditConsumption,
		Name:            "Credit consumption",
		Description:     "Invoice debits split by free and paid source credits, valued at the debit conversion rate",
		Category:        types.ReportCategoryCredits,
		Kind:            types.ReportKindFlow,
		ValueType:       types.ReportValueTypeMoney,
		Improvement:     types.ImprovementHigher,
		DefaultShape:    types.ShapeTimeseries,
		Grains:          []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
		SupportsCompare: false,
	},
	{
		Key:             types.MetricCreditTopups,
		Name:            "Credit top-ups",
		Description:     "Purchased, free and subscription credit grants",
		Category:        types.ReportCategoryCredits,
		Kind:            types.ReportKindFlow,
		ValueType:       types.ReportValueTypeMoney,
		Improvement:     types.ImprovementHigher,
		DefaultShape:    types.ShapeTimeseries,
		Grains:          []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
		SupportsCompare: true,
	},
	{
		Key:          types.MetricNetBilling,
		Name:         "Net billing",
		Description:  "Finalized invoices minus finalized credit notes by finalization date",
		Category:     types.ReportCategoryBilling,
		Kind:         types.ReportKindFlow,
		ValueType:    types.ReportValueTypeMoney,
		Improvement:  types.ImprovementHigher,
		DefaultShape: types.ShapeTimeseries,
		Grains:       []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
		VariantLabel: "Finalized date",
	},
	{
		Key:          types.MetricNetBillingIssued,
		Name:         "Net billing (finalized date)",
		Description:  "Finalized invoices minus finalized credit notes by finalization date",
		Category:     types.ReportCategoryBilling,
		Kind:         types.ReportKindFlow,
		ValueType:    types.ReportValueTypeMoney,
		Improvement:  types.ImprovementHigher,
		DefaultShape: types.ShapeTimeseries,
		Grains:       []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
		VariantOf:    types.MetricNetBilling,
		VariantLabel: "Finalized date",
	},
	{
		Key:          types.MetricActiveCustomers,
		Name:         "Active customers",
		Description:  "Customers with an active or trialing subscription in the period",
		Category:     types.ReportCategoryCustomers,
		Kind:         types.ReportKindRatio,
		ValueType:    types.ReportValueTypeCount,
		Improvement:  types.ImprovementHigher,
		DefaultShape: types.ShapeScalar,
	},
	{
		Key:          types.MetricEventsVolume,
		Name:         "Events volume",
		Description:  "Metered events per meter",
		Category:     types.ReportCategoryUsage,
		Kind:         types.ReportKindFlow,
		ValueType:    types.ReportValueTypeCount,
		DefaultShape: types.ShapeTimeseries,
		Grains:       []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
	},
}

var dimensionLabels = map[string]string{
	"customer_id":        "Customer",
	"invoice_type":       "Invoice type",
	"aging_bucket":       "Aging bucket",
	"transaction_reason": "Reason",
	"meter_id":           "Meter",
	"credit_type":        "Credit type",
}

// Shapes and dimensions come from the billing metric.
func ReportCatalog() []ReportCatalogEntry {
	out := make([]ReportCatalogEntry, len(reportCatalog))
	for i, e := range reportCatalog {
		bm := billingMetrics[e.Key]
		e.Shapes = slices.Clone(bm.Shapes)
		e.Dimensions = nil
		for _, d := range bm.Dimensions {
			if label, ok := dimensionLabels[d]; ok {
				e.Dimensions = append(e.Dimensions, ReportDimension{Key: d, Label: label})
			}
		}
		e.MaxRangeDays = bm.MaxRangeDays
		e.Grains = slices.Clone(e.Grains)
		out[i] = e
	}
	return out
}

func LookupReport(key string) (*ReportCatalogEntry, bool) {
	for _, e := range ReportCatalog() {
		if string(e.Key) == key {
			return &e, true
		}
	}
	return nil, false
}
