package analytics

import (
	"slices"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportCatalog_BreakdownNeedsDimension(t *testing.T) {
	for _, e := range ReportCatalog() {
		if slices.Contains(e.Shapes, types.ShapeBreakdown) {
			assert.NotEmpty(t, e.Dimensions, "breakdown of %q", e.Key)
		}
	}
}

func TestReportCatalog_InvoiceTotal(t *testing.T) {
	e, ok := LookupReport("invoice_total")
	require.True(t, ok)
	assert.Equal(t, "Total billing", e.Name)
	assert.Equal(t, "flow", e.Kind)
	assert.Equal(t, "money", e.ValueType)
	assert.True(t, e.HigherIsBetter)
	assert.True(t, e.SupportsCompare)
	assert.Equal(t, types.ShapeTimeseries, e.DefaultShape)
	assert.True(t, slices.ContainsFunc(e.Dimensions, func(d ReportDimension) bool { return d.Key == "customer_id" }))
}

func TestReportCatalog_ARPUVariants(t *testing.T) {
	billed, ok := LookupReport("arpu")
	require.True(t, ok)
	paid, ok := LookupReport("arpu_paid")
	require.True(t, ok)
	_, ok = LookupReport("arpu_billed")
	assert.False(t, ok)
	assert.Equal(t, types.Metric(""), billed.VariantOf)
	assert.Equal(t, "Billed", billed.VariantLabel)
	assert.Equal(t, types.MetricARPU, paid.VariantOf)
	assert.Equal(t, "Paid", paid.VariantLabel)
	assert.Equal(t, billed.Category, paid.Category)
	assert.Equal(t, billed.Kind, paid.Kind)
	assert.Equal(t, billed.ValueType, paid.ValueType)
	assert.True(t, paid.SupportsCompare)
	assert.True(t, billed.SupportsCompare)
	for _, e := range ReportCatalog() {
		if e.VariantOf != "" {
			_, ok := LookupReport(string(e.VariantOf))
			assert.True(t, ok, "variant_of of %q", e.Key)
		}
	}
}

func TestReportCatalog_UnknownAndCopy(t *testing.T) {
	_, ok := LookupReport("nope")
	assert.False(t, ok)
	a := ReportCatalog()
	a[0].Name = "mutated"
	b := ReportCatalog()
	assert.NotEqual(t, "mutated", b[0].Name)
}
