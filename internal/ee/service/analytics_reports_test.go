package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func reportsSvc(repo *testutil.InMemoryReportsRepo) AnalyticsService {
	if repo.Max == 0 {
		repo.Max = 500
	}
	return NewAnalyticsService(ServiceParams{ReportsRepo: repo})
}

func TestListReports(t *testing.T) {
	res, err := reportsSvc(&testutil.InMemoryReportsRepo{On: true}).ListReports(billingCtx())
	require.NoError(t, err)
	require.NotEmpty(t, res.Items)
	assert.Equal(t, "invoice_total", res.Items[0].Key)
	assert.Equal(t, "Total billing", res.Items[0].Name)
}

func TestListReports_DisabledIs403(t *testing.T) {
	_, err := reportsSvc(&testutil.InMemoryReportsRepo{On: false}).ListReports(billingCtx())
	require.Error(t, err)
	assert.True(t, ierr.IsPermissionDenied(err))
}

func TestRunReport_UnknownIs404(t *testing.T) {
	_, err := reportsSvc(&testutil.InMemoryReportsRepo{On: true}).RunReport(billingCtx(), "nope", &dto.ReportQueryRequest{Shape: "scalar", Range: "2026-09-01..2026-10-01"})
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
}

func TestRunReport_RejectsDisallowed(t *testing.T) {
	svc := reportsSvc(&testutil.InMemoryReportsRepo{On: true})
	for _, req := range []*dto.ReportQueryRequest{
		{Shape: "histogram", Range: "2026-09-01..2026-10-01"},
		{Shape: "breakdown", Dimensions: []string{"meter_id"}, Range: "2026-09-01..2026-10-01"},
		{Shape: "timeseries", Grain: "hour", Range: "2026-09-01..2026-10-01"},
		{Shape: "scalar", Compare: "last_decade", Range: "2026-09-01..2026-10-01"},
		{Shape: "scalar", Range: "2026-09-01..2026-10-01", Filters: []dto.ReportFilter{{Field: "tenant_id", Op: "eq", Value: []string{"x"}}}},
	} {
		_, err := svc.RunReport(billingCtx(), "invoice_total", req)
		require.Error(t, err, "%+v", req)
		assert.True(t, ierr.IsValidation(err), "%+v", req)
	}
}

func TestRunReport_CompareAddsChangeAndMeta(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: true, Columns: reportCols("currency", "invoice_total", "invoice_total_prev"), Rows: [][]string{{"usd", "110", "100"}, {"eur", "5", "0"}, {"inr", "x", "1"}, {"usd", "0", "4"}}}
	res, err := reportsSvc(repo).RunReport(billingCtx(), "invoice_total", &dto.ReportQueryRequest{Shape: "scalar", Range: "2026-09-01..2026-10-01", Compare: "previous_period"})
	require.NoError(t, err)
	names := []string{}
	for _, c := range res.Columns {
		names = append(names, c.Name)
	}
	assert.Equal(t, []string{"currency", "invoice_total", "invoice_total_prev", "invoice_total_change_pct"}, names)
	assert.Equal(t, []string{"usd", "110", "100", "10"}, res.Rows[0])
	assert.Equal(t, "", res.Rows[1][3])
	assert.Equal(t, "", res.Rows[2][3])
	assert.Equal(t, "-100", res.Rows[3][3])
	assert.Equal(t, "2026-09-01T00:00:00Z", res.Meta.From)
	assert.Equal(t, "2026-10-01T00:00:00Z", res.Meta.To)
	assert.Equal(t, "2026-08-01T00:00:00Z", res.Meta.PrevFrom)
	assert.Equal(t, "2026-09-01T00:00:00Z", res.Meta.PrevTo)
	assert.Equal(t, "previous_period", repo.Last.Compare)
}

func TestRunReport_CurrencyBecomesFilter(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: true}
	_, err := reportsSvc(repo).RunReport(billingCtx(), "invoice_total", &dto.ReportQueryRequest{Shape: "scalar", Range: "2026-09-01..2026-10-01", Currency: "usd"})
	require.NoError(t, err)
	require.Len(t, repo.Last.Filters, 1)
	assert.Equal(t, "currency", repo.Last.Filters[0].Field)
	assert.Equal(t, []string{"usd"}, repo.Last.Filters[0].Value)
}

func TestRunReport_NoCompareNoMetaPrev(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: true, Columns: reportCols("currency", "invoice_total"), Rows: [][]string{{"usd", "1"}}}
	res, err := reportsSvc(repo).RunReport(billingCtx(), "invoice_total", &dto.ReportQueryRequest{Shape: "scalar", Range: "2026-09-01..2026-10-01"})
	require.NoError(t, err)
	assert.Empty(t, res.Meta.PrevFrom)
	assert.Len(t, res.Columns, 2)
}

func TestRunReport_ValidationRules(t *testing.T) {
	const rng = "2026-09-01..2026-10-01"
	cols := append(reportCols("customer_id", "currency", "invoice_total"), analytics.ReportColumn{Name: "invoice_total_prev", Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric})
	svc := reportsSvc(&testutil.InMemoryReportsRepo{On: true, Max: 100, Columns: cols})
	cases := map[string]*dto.ReportQueryRequest{
		"negative limit":      {Shape: "breakdown", Range: rng, Limit: -1},
		"limit over max":      {Shape: "breakdown", Range: rng, Limit: 101},
		"sort on scalar":      {Shape: "scalar", Range: rng, Sort: []dto.ReportSort{{Field: "invoice_total", Dir: "asc"}}},
		"sort bad dir":        {Shape: "breakdown", Range: rng, Sort: []dto.ReportSort{{Field: "invoice_total", Dir: "up"}}},
		"sort bad field":      {Shape: "breakdown", Range: rng, Sort: []dto.ReportSort{{Field: "tenant_id", Dir: "asc"}}},
		"sort prev w/o cmp":   {Shape: "breakdown", Range: rng, Sort: []dto.ReportSort{{Field: "invoice_total_prev", Dir: "asc"}}},
		"sort change_pct":     {Shape: "breakdown", Range: rng, Compare: "previous_period", Sort: []dto.ReportSort{{Field: "invoice_total_change_pct", Dir: "asc"}}},
		"grain on scalar":     {Shape: "scalar", Range: rng, Grain: "day"},
		"grain on breakdown":  {Shape: "breakdown", Range: rng, Grain: "day"},
		"eq two values":       {Shape: "scalar", Range: rng, Filters: []dto.ReportFilter{{Field: "customer_id", Op: "eq", Value: []string{"a", "b"}}}},
		"eq no values":        {Shape: "scalar", Range: rng, Filters: []dto.ReportFilter{{Field: "customer_id", Op: "eq"}}},
		"in no values":        {Shape: "scalar", Range: rng, Filters: []dto.ReportFilter{{Field: "customer_id", Op: "in"}}},
		"bad op":              {Shape: "scalar", Range: rng, Filters: []dto.ReportFilter{{Field: "customer_id", Op: "gt", Value: []string{"a"}}}},
		"limit on scalar":     {Shape: "scalar", Range: rng, Limit: 5},
		"limit on timeseries": {Shape: "timeseries", Range: rng, Limit: 5},
		"filter field bad":    {Shape: "scalar", Range: rng, Filters: []dto.ReportFilter{{Field: "tenant_id", Op: "eq", Value: []string{"a"}}}},
	}
	for name, req := range cases {
		_, err := svc.RunReport(billingCtx(), "invoice_total", req)
		require.Error(t, err, name)
		assert.True(t, ierr.IsValidation(err), name)
	}

	ok := []*dto.ReportQueryRequest{
		{Shape: "breakdown", Dimensions: []string{"customer_id"}, Range: rng, Limit: 100, Sort: []dto.ReportSort{{Field: "invoice_total", Dir: "desc"}}},
		{Shape: "breakdown", Dimensions: []string{"customer_id"}, Range: rng, Compare: "previous_period", Sort: []dto.ReportSort{{Field: "invoice_total_prev", Dir: "asc"}}},
		{Shape: "scalar", Range: rng, Filters: []dto.ReportFilter{{Field: "customer_id", Op: "in", Value: []string{"a", "b"}}}},
	}
	for _, req := range ok {
		_, err := svc.RunReport(billingCtx(), "invoice_total", req)
		require.NoError(t, err, "%+v", req)
	}
}

func TestValidateReportRequest_UnsupportedCompareAndNoCurrency(t *testing.T) {
	e := &analytics.ReportCatalogEntry{
		Key:          types.Metric("no_such_metric"),
		Shapes:       []types.Shape{types.ShapeScalar},
		DefaultShape: types.ShapeScalar,
	}
	err := validateReportRequest(e, &dto.ReportQueryRequest{Shape: "scalar", Compare: "previous_period"}, 0)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))

	err = validateReportRequest(e, &dto.ReportQueryRequest{Shape: "scalar", Currency: "usd"}, 0)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}
