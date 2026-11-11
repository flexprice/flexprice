package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func exportSvc(repo *testutil.InMemoryReportsRepo) (AnalyticsService, *testutil.InMemoryAnalyticsViewStore) {
	if repo.Max == 0 {
		repo.Max = 500
	}
	views := testutil.NewInMemoryAnalyticsViewStore()
	return NewAnalyticsService(ServiceParams{ReportsRepo: repo, AnalyticsViewRepo: views}), views
}

func TestValidateReportSource(t *testing.T) {
	ok := &types.ReportParams{Shape: "scalar"}

	svc, _ := exportSvc(&testutil.InMemoryReportsRepo{On: true})
	assert.NoError(t, svc.ValidateReportSource(billingCtx(), "invoice_total", ok))

	err := svc.ValidateReportSource(billingCtx(), "nope", ok)
	assert.True(t, ierr.IsNotFound(err))

	err = svc.ValidateReportSource(billingCtx(), "invoice_total", &types.ReportParams{Shape: "histogram"})
	assert.True(t, ierr.IsValidation(err))

	off, _ := exportSvc(&testutil.InMemoryReportsRepo{On: false})
	err = off.ValidateReportSource(billingCtx(), "invoice_total", ok)
	assert.True(t, ierr.IsPermissionDenied(err))
}

func TestValidateViewSource(t *testing.T) {
	svc, _ := exportSvc(&testutil.InMemoryReportsRepo{On: true})
	ctx := userCtx("env_1")

	err := svc.ValidateViewSource(ctx, "missing", 1)
	assert.True(t, ierr.IsNotFound(err))

	good := &analytics.View{Name: "ok", Definition: billingView(types.ShapeScalar)}
	require.NoError(t, svc.CreateView(ctx, good))
	assert.NoError(t, svc.ValidateViewSource(ctx, good.ID, 1))

	withVar := billingView(types.ShapeScalar)
	withVar.Filters = []*analytics.Filter{{Field: "currency", Op: types.EQUAL, Value: []string{"{{cur}}"}}}
	withVar.Variables = []*analytics.Variable{{Name: "cur", Type: types.VariableTypeString, Required: true}}
	v := &analytics.View{Name: "var", Definition: withVar}
	require.NoError(t, svc.CreateView(ctx, v))
	err = svc.ValidateViewSource(ctx, v.ID, 1)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestRunViewForExport_OverridesRangeWithoutMutatingStore(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: true, Columns: reportCols("currency", "invoice_total"), Rows: [][]string{{"usd", "1"}}}
	svc, _ := exportSvc(repo)
	ctx := userCtx("env_1")

	v := &analytics.View{Name: "b", Definition: billingView(types.ShapeScalar)}
	require.NoError(t, svc.CreateView(ctx, v))

	res, updatedAt, err := svc.RunViewForExport(ctx, v.ID, "2026-11-09..2026-11-10")
	require.NoError(t, err)
	assert.Len(t, res.Rows, 1)
	assert.True(t, updatedAt.Equal(v.UpdatedAt))
	require.NotNil(t, repo.Last)
	assert.Equal(t, "2026-11-09", repo.Last.Time.From.Format("2006-01-02"))
	assert.Equal(t, "2026-11-10", repo.Last.Time.To.Format("2006-01-02"))

	stored, err := svc.GetView(ctx, v.ID)
	require.NoError(t, err)
	assert.Equal(t, "last_30_days", stored.Definition.Time.Range)
}

func TestRunViewForExport_DeletedViewNotFound(t *testing.T) {
	svc, _ := exportSvc(&testutil.InMemoryReportsRepo{On: true})
	ctx := userCtx("env_1")
	v := &analytics.View{Name: "b", Definition: billingView(types.ShapeScalar)}
	require.NoError(t, svc.CreateView(ctx, v))
	require.NoError(t, svc.DeleteView(ctx, v.ID))

	_, _, err := svc.RunViewForExport(ctx, v.ID, "2026-11-09..2026-11-10")
	assert.True(t, ierr.IsNotFound(err))
}

func TestRunViewForExport_ReportFamilyRoutesToRunReport(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: true, Columns: reportCols("currency", "invoice_total"), Rows: [][]string{{"usd", "1"}}}
	svc, _ := exportSvc(repo)
	ctx := userCtx("env_1")

	def := reportDef(types.MetricInvoiceTotal, types.ShapeScalar, "this_month")
	def.Filters = []*analytics.Filter{{Field: "currency", Op: types.EQUAL, Value: []string{"usd"}}}
	v := &analytics.View{Name: "r", Definition: def}
	require.NoError(t, svc.CreateView(ctx, v))

	res, _, err := svc.RunViewForExport(ctx, v.ID, "2026-11-09..2026-11-10")
	require.NoError(t, err)
	assert.Len(t, res.Rows, 1)
	require.NotNil(t, repo.Last)
	assert.Equal(t, []types.Metric{types.MetricInvoiceTotal}, repo.Last.Metrics)
	assert.Equal(t, "2026-11-09", repo.Last.Time.From.Format("2006-01-02"))
	require.Len(t, repo.Last.Filters, 1)
	assert.Equal(t, "currency", repo.Last.Filters[0].Field)
}

func TestRunViewForExport_ReportFamilyUnsupportedFilterOp(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: true}
	svc, views := exportSvc(repo)
	ctx := userCtx("env_1")

	def := reportDef(types.MetricInvoiceTotal, types.ShapeScalar, "this_month")
	def.Filters = []*analytics.Filter{{Field: "currency", Op: types.CONTAINS, Value: []string{"usd"}}}
	v := &analytics.View{ID: "view_x", Name: "r", Definition: def, EnvironmentID: "env_1"}
	v.TenantID, v.Status = "tenant_1", types.StatusPublished
	require.NoError(t, views.Create(ctx, v))

	_, _, err := svc.RunViewForExport(ctx, v.ID, "2026-11-09..2026-11-10")
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
	assert.Nil(t, repo.Last)
}

func TestReportColumnsForExport_MatchesRunReport(t *testing.T) {
	cols := reportCols("customer_id", "currency", "invoice_total", "invoice_total_prev")
	repo := &testutil.InMemoryReportsRepo{On: true, Columns: cols, Rows: [][]string{{"c", "usd", "2", "1"}}}
	svc, _ := exportSvc(repo)
	s := svc.(*analyticsService)
	rng := "2026-11-09..2026-11-10"

	for _, compare := range []string{"", "previous_period"} {
		req := &dto.ReportQueryRequest{Shape: "breakdown", Dimensions: []string{"customer_id"}, Range: rng, Compare: compare}
		ran, err := svc.RunReport(billingCtx(), "invoice_total", req)
		require.NoError(t, err)
		want := lo.Map(ran.Columns, func(c *dto.AnalyticsColumn, _ int) string { return c.Name })

		got, err := s.reportColumns(billingCtx(), "invoice_total", req)

		require.NoError(t, err)
		assert.Equal(t, want, got, "compare=%q", compare)
	}

	got, err := svc.ReportColumnsForExport(billingCtx(), "invoice_total", &types.ReportParams{Shape: "breakdown", Dimensions: []string{"customer_id"}}, rng)
	require.NoError(t, err)
	assert.Equal(t, []string{"customer_id", "currency", "invoice_total", "invoice_total_prev"}, got)
}

func TestRunReportForExport_RepoOverflowPropagates(t *testing.T) {
	boom := ierr.NewError("too many rows").Mark(ierr.ErrValidation)
	svc, _ := exportSvc(&testutil.InMemoryReportsRepo{On: true, Err: boom})
	_, err := svc.RunReportForExport(billingCtx(), "invoice_total", &types.ReportParams{Shape: "scalar", Currency: "usd"}, "2026-11-09..2026-11-10")
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestRunViewForExport_RowCap(t *testing.T) {
	rows := make([][]string, maxExportRows+1)
	for i := range rows {
		rows[i] = []string{"usd", "1"}
	}
	repo := &testutil.InMemoryReportsRepo{On: true, Columns: reportCols("currency", "invoice_total"), Rows: rows}
	svc, _ := exportSvc(repo)
	ctx := userCtx("env_1")
	v := &analytics.View{Name: "b", Definition: billingView(types.ShapeScalar)}
	require.NoError(t, svc.CreateView(ctx, v))

	_, _, err := svc.RunViewForExport(ctx, v.ID, "2026-11-09..2026-11-10")
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestValidateViewSource_WindowCap(t *testing.T) {
	svc, _ := exportSvc(&testutil.InMemoryReportsRepo{On: true})
	ctx := userCtx("env_1")

	billing := billingView(types.ShapeTimeseries)
	billing.Metrics = []types.Metric{types.MetricEventsVolume}
	billing.Time.Grain = types.GrainDay
	v := &analytics.View{Name: "vol", Definition: billing}
	require.NoError(t, svc.CreateView(ctx, v))

	assert.NoError(t, svc.ValidateViewSource(ctx, v.ID, 7))
	err := svc.ValidateViewSource(ctx, v.ID, 31)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestReportViewVariables(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: true, Columns: reportCols("customer_id", "invoice_total"), Rows: [][]string{{"c", "1"}}}
	svc, _ := exportSvc(repo)
	ctx := userCtx("env_1")

	def := reportDef(types.MetricInvoiceTotal, types.ShapeBreakdown, "last_7_days")
	def.Dimensions = []string{"customer_id"}
	def.Filters = []*analytics.Filter{{Field: "invoice_type", Op: types.EQUAL, Value: []string{"{{kind}}"}}, nil}
	def.Sort = []*analytics.SortSpec{nil}
	def.Variables = []*analytics.Variable{{Name: "kind", Type: types.VariableTypeString, Required: true}}
	noDefault := &analytics.View{Name: "nd", Definition: def}
	require.NoError(t, svc.CreateView(ctx, noDefault))
	assert.True(t, ierr.IsValidation(svc.ValidateViewSource(ctx, noDefault.ID, 7)))

	withDefault := *def
	withDefault.Variables = []*analytics.Variable{{Name: "kind", Type: types.VariableTypeString, Required: true, Default: []string{"SUBSCRIPTION"}}}
	ok := &analytics.View{Name: "wd", Definition: &withDefault}
	require.NoError(t, svc.CreateView(ctx, ok))
	require.NoError(t, svc.ValidateViewSource(ctx, ok.ID, 7))

	_, _, err := svc.RunViewForExport(ctx, ok.ID, "2026-11-09..2026-11-10")
	require.NoError(t, err)
	require.Len(t, repo.Last.Filters, 1)
	assert.Equal(t, []string{"SUBSCRIPTION"}, repo.Last.Filters[0].Value)
}
