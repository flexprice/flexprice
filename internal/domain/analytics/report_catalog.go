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
	Category        string
	Kind            string
	ValueType       string
	HigherIsBetter  bool
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
		Key:            types.MetricInvoiceTotal,
		Name:           "Total billing",
		Description:    "Finalized invoice totals by billing period",
		Category:       "billing",
		Kind:           "flow",
		ValueType:      "money",
		HigherIsBetter: true,
		Shapes:         []types.Shape{types.ShapeScalar, types.ShapeTimeseries, types.ShapeBreakdown},
		DefaultShape:   types.ShapeTimeseries,
		Dimensions: []ReportDimension{
			{Key: "customer_id", Label: "Customer"},
			{Key: "invoice_type", Label: "Invoice type"},
		},
		Grains:          []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
		SupportsCompare: true,
		VariantLabel:    "Billing period",
	},
	{
		Key:            types.MetricInvoiceTotalIssued,
		Name:           "Total billing (invoice date)",
		Description:    "Finalized invoice totals by invoice date",
		Category:       "billing",
		Kind:           "flow",
		ValueType:      "money",
		HigherIsBetter: true,
		Shapes:         []types.Shape{types.ShapeScalar, types.ShapeTimeseries, types.ShapeBreakdown},
		DefaultShape:   types.ShapeTimeseries,
		Dimensions: []ReportDimension{
			{Key: "customer_id", Label: "Customer"},
			{Key: "invoice_type", Label: "Invoice type"},
		},
		Grains:          []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
		SupportsCompare: true,
		VariantOf:       types.MetricInvoiceTotal,
		VariantLabel:    "Invoice date",
	},
	{
		Key:          types.MetricTotalReceivables,
		Name:         "Total receivables",
		Description:  "Outstanding amount on finalized invoices right now",
		Category:     "receivables",
		Kind:         "snapshot",
		ValueType:    "money",
		Shapes:       []types.Shape{types.ShapeScalar},
		DefaultShape: types.ShapeScalar,
	},
	{
		Key:          types.MetricReceivablesAging,
		Name:         "Receivables aging",
		Description:  "Outstanding amount by days past due",
		Category:     "receivables",
		Kind:         "snapshot",
		ValueType:    "money",
		Shapes:       []types.Shape{types.ShapeBreakdown},
		DefaultShape: types.ShapeBreakdown,
		Dimensions: []ReportDimension{
			{Key: "aging_bucket", Label: "Aging bucket"},
			{Key: "customer_id", Label: "Customer"},
		},
	},
	{
		Key:          types.MetricDaysToPay,
		Name:         "Days to pay",
		Description:  "Average days from issue to payment for paid invoices",
		Category:     "receivables",
		Kind:         "ratio",
		ValueType:    "days",
		Shapes:       []types.Shape{types.ShapeScalar, types.ShapeTimeseries, types.ShapeBreakdown},
		DefaultShape: types.ShapeTimeseries,
		Dimensions: []ReportDimension{
			{Key: "customer_id", Label: "Customer"},
		},
		Grains:          []types.Grain{types.GrainWeek, types.GrainMonth},
		SupportsCompare: true,
	},
	{
		Key:             types.MetricARPU,
		SupportsCompare: true,
		Name:            "ARPU",
		Description:     "Billed revenue per billed customer",
		Category:        "customers",
		Kind:            "ratio",
		ValueType:       "money",
		HigherIsBetter:  true,
		Shapes:          []types.Shape{types.ShapeScalar, types.ShapeTimeseries},
		DefaultShape:    types.ShapeTimeseries,
		Grains:          []types.Grain{types.GrainMonth},
		VariantLabel:    "Billed",
	},
	{
		Key:             types.MetricARPUPaid,
		SupportsCompare: true,
		Name:            "ARPU (paid)",
		Description:     "Paid revenue per paying customer",
		Category:        "customers",
		Kind:            "ratio",
		ValueType:       "money",
		HigherIsBetter:  true,
		Shapes:          []types.Shape{types.ShapeScalar, types.ShapeTimeseries},
		DefaultShape:    types.ShapeTimeseries,
		Grains:          []types.Grain{types.GrainMonth},
		VariantOf:       types.MetricARPU,
		VariantLabel:    "Paid",
	},
	{
		Key:            types.MetricCreditConsumption,
		Name:           "Credit consumption",
		Description:    "Wallet credits applied to invoices",
		Category:       "credits",
		Kind:           "flow",
		ValueType:      "money",
		HigherIsBetter: true,
		Shapes:         []types.Shape{types.ShapeScalar, types.ShapeTimeseries, types.ShapeBreakdown},
		DefaultShape:   types.ShapeTimeseries,
		Dimensions: []ReportDimension{
			{Key: "customer_id", Label: "Customer"},
		},
		Grains:          []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
		SupportsCompare: true,
	},
	{
		Key:            types.MetricCreditTopups,
		Name:           "Credit top-ups",
		Description:    "Purchased, free and subscription credit grants",
		Category:       "credits",
		Kind:           "flow",
		ValueType:      "money",
		HigherIsBetter: true,
		Shapes:         []types.Shape{types.ShapeScalar, types.ShapeTimeseries, types.ShapeBreakdown},
		DefaultShape:   types.ShapeTimeseries,
		Dimensions: []ReportDimension{
			{Key: "transaction_reason", Label: "Reason"},
			{Key: "customer_id", Label: "Customer"},
		},
		Grains:          []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
		SupportsCompare: true,
	},
	{
		Key:            types.MetricNetBilling,
		Name:           "Net billing",
		Description:    "Billing by period minus finalized credit notes",
		Category:       "billing",
		Kind:           "flow",
		ValueType:      "money",
		HigherIsBetter: true,
		Shapes:         []types.Shape{types.ShapeScalar, types.ShapeTimeseries},
		DefaultShape:   types.ShapeTimeseries,
		Grains:         []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
		VariantLabel:   "Billing period",
	},
	{
		Key:            types.MetricNetBillingIssued,
		Name:           "Net billing (invoice date)",
		Description:    "Invoiced by issue date minus finalized credit notes",
		Category:       "billing",
		Kind:           "flow",
		ValueType:      "money",
		HigherIsBetter: true,
		Shapes:         []types.Shape{types.ShapeScalar, types.ShapeTimeseries},
		DefaultShape:   types.ShapeTimeseries,
		Grains:         []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
		VariantOf:      types.MetricNetBilling,
		VariantLabel:   "Invoice date",
	},
	{
		Key:            types.MetricActiveCustomers,
		Name:           "Active customers",
		Description:    "Customers with an active or trialing subscription in the period",
		Category:       "customers",
		Kind:           "ratio",
		ValueType:      "count",
		HigherIsBetter: true,
		Shapes:         []types.Shape{types.ShapeScalar},
		DefaultShape:   types.ShapeScalar,
	},
	{
		Key:          types.MetricEventsVolume,
		Name:         "Events volume",
		Description:  "Metered events per meter",
		Category:     "usage",
		Kind:         "flow",
		ValueType:    "count",
		Shapes:       []types.Shape{types.ShapeTimeseries, types.ShapeBreakdown},
		DefaultShape: types.ShapeTimeseries,
		Dimensions: []ReportDimension{
			{Key: "meter_id", Label: "Meter"},
		},
		Grains:       []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth},
		MaxRangeDays: 7,
	},
}

func ReportCatalog() []ReportCatalogEntry {
	out := make([]ReportCatalogEntry, len(reportCatalog))
	for i, e := range reportCatalog {
		e.Shapes = slices.Clone(e.Shapes)
		e.Dimensions = slices.Clone(e.Dimensions)
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
