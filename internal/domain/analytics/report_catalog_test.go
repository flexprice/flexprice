package analytics

import (
	"slices"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestReportCatalog_UnknownAndCopy(t *testing.T) {
	_, ok := LookupReport("nope")
	assert.False(t, ok)
	a := ReportCatalog()
	a[0].Name = "mutated"
	b := ReportCatalog()
	assert.NotEqual(t, "mutated", b[0].Name)
}
