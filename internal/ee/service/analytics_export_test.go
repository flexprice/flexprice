package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
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

	err := svc.ValidateViewSource(ctx, "missing")
	assert.True(t, ierr.IsNotFound(err))

	good := &analytics.View{Name: "ok", Definition: billingView(types.ShapeScalar)}
	require.NoError(t, svc.CreateView(ctx, good))
	assert.NoError(t, svc.ValidateViewSource(ctx, good.ID))

	withVar := billingView(types.ShapeScalar)
	withVar.Filters = []*analytics.Filter{{Field: "currency", Op: types.EQUAL, Value: []string{"{{cur}}"}}}
	withVar.Variables = []*analytics.Variable{{Name: "cur", Type: types.VariableTypeString, Required: true}}
	v := &analytics.View{Name: "var", Definition: withVar}
	require.NoError(t, svc.CreateView(ctx, v))
	err = svc.ValidateViewSource(ctx, v.ID)
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
