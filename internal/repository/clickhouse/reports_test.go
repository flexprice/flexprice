package clickhouse

import (
	"context"
	"slices"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingSQL_InvoiceTotalUsesConstants(t *testing.T) {
	_, sq, ok := lookupBillingMetric(types.MetricInvoiceTotal)
	require.True(t, ok)
	assert.Equal(t, "invoices", sq.Table)
	assert.Contains(t, sq.Where, "invoice_status = '"+string(types.InvoiceStatusFinalized)+"'")
}

func TestBillingSQL_MatchesDomainRegistry(t *testing.T) {
	metrics := analytics.BillingMetrics()
	assert.Len(t, billingSQLs, len(metrics))
	for _, m := range metrics {
		bm, sq, ok := lookupBillingMetric(m)
		require.True(t, ok, "metric %q has no SQL", m)
		for d := range sq.DimExprs {
			assert.True(t, slices.Contains(bm.Dimensions, d), "metric %q: unknown dimension %q", m, d)
		}
	}
}

func TestReportCatalog_AgreesWithRegistry(t *testing.T) {
	entries := analytics.ReportCatalog()
	require.NotEmpty(t, entries)
	for _, e := range entries {
		bm, sq, ok := lookupBillingMetric(e.Key)
		require.True(t, ok, "catalog key %q has no billing metric", e.Key)
		assert.Equal(t, sq.Agg+"("+sq.Arg+")", sq.Measure, "measure of %q", e.Key)
		for _, s := range e.Shapes {
			assert.Contains(t, bm.Shapes, s, "shape %q of %q", s, e.Key)
		}
		assert.Contains(t, e.Shapes, e.DefaultShape)
		for _, d := range e.Dimensions {
			assert.True(t, slices.Contains(bm.Dimensions, d.Key), "dimension %q of %q", d.Key, e.Key)
			assert.NotEmpty(t, d.Label)
		}
		for _, g := range e.Grains {
			assert.Contains(t, []types.Grain{types.GrainDay, types.GrainWeek, types.GrainMonth}, g)
		}
		assert.Contains(t, []string{"billing", "receivables", "customers", "credits", "usage"}, e.Category)
		assert.Contains(t, []string{"flow", "ratio", "snapshot"}, e.Kind)
		assert.Contains(t, []string{"money", "count", "days"}, e.ValueType)
		if e.Kind == "snapshot" {
			assert.False(t, e.SupportsCompare)
			assert.Empty(t, e.Grains)
		}
	}
}

func TestReportResult_OverflowIsValidationError(t *testing.T) {
	q, err := buildBillingQuery(billingRV(types.ShapeScalar), "reports_db", "t", "e", 2)
	require.NoError(t, err)
	_, err = reportResult(q, [][]string{{"usd", "1"}, {"inr", "2"}, {"eur", "3"}}, 2)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestReportResult_TopNSkipsOverflow(t *testing.T) {
	rv := billingRV(types.ShapeBreakdown, "customer_id")
	rv.Limit = 2
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 2)
	require.NoError(t, err)
	res, err := reportResult(q, [][]string{{"c1", "usd", "50"}, {"c2", "usd", "20"}, {"c3", "usd", "5"}}, 2)
	require.NoError(t, err)
	assert.Len(t, res.Rows, 3)
	assert.Equal(t, q.Columns, res.Columns)
}

func TestRunReport_DisabledIs403(t *testing.T) {
	_, err := (&ReportsRepository{}).RunReport(context.Background(), billingRV(types.ShapeScalar), "t", "e")
	require.Error(t, err)
	assert.True(t, ierr.IsPermissionDenied(err))
}
