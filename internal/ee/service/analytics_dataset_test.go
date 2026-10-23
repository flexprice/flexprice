package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func datasetSvc(repo *testutil.InMemoryReportsRepo) (AnalyticsService, *testutil.InMemoryAnalyticsViewStore) {
	views := testutil.NewInMemoryAnalyticsViewStore()
	return NewAnalyticsService(ServiceParams{ReportsRepo: repo, AnalyticsViewRepo: views}), views
}

func datasetDef() *analytics.ViewDefinition {
	return &analytics.ViewDefinition{
		Family:   types.FamilyDataset,
		Dataset:  "invoices",
		Measures: []*analytics.Measure{{Agg: "count"}, {Agg: "sum", Field: "total"}},
		Time:     analytics.TimeSpecRaw{Range: "2026-09-01..2026-10-01", Field: "issue_date"},
	}
}

func datasetRepo() *testutil.InMemoryReportsRepo {
	return &testutil.InMemoryReportsRepo{
		On: true, Max: 500,
		Columns: []analytics.ReportColumn{
			{Name: "invoice_status", Type: types.ColumnTypeString, Role: types.ColumnRoleDimension},
			{Name: "m0", Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric},
			{Name: "m1", Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric},
			{Name: "m0_prev", Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric},
			{Name: "m1_prev", Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric},
		},
		Rows: [][]string{{"FINALIZED", "5", "150", "4", "100"}, {"DRAFT", "9", "10", "9", "0"}},
	}
}

func TestRunDataset_PassesWindowAndShapesRows(t *testing.T) {
	repo := datasetRepo()
	svc, _ := datasetSvc(repo)
	def := datasetDef()
	def.Dimensions = []string{"invoice_status"}
	def.Compare = "previous_period"
	def.Sort = []*analytics.SortSpec{{Field: "m1", Dir: types.SortDirectionDesc}}
	def.Limit = 1

	res, err := svc.RunDataset(billingCtx(), def)
	require.NoError(t, err)

	require.NotNil(t, repo.LastDataset)
	require.Equal(t, "2026-09-01", repo.LastDataset.From.Format("2006-01-02"))
	require.Equal(t, "2026-10-01", repo.LastDataset.To.Format("2006-01-02"))
	require.Equal(t, "previous_period", repo.LastDataset.Compare)
	require.Equal(t, "invoices", repo.LastDataset.Def.Dataset)
	require.Equal(t, "tenant_1", repo.LastTenantID)

	require.Len(t, res.Rows, 1)
	require.Equal(t, "FINALIZED", res.Rows[0][0])
	require.Equal(t, "25", res.Rows[0][len(res.Rows[0])-2])
	require.Equal(t, "50", res.Rows[0][len(res.Rows[0])-1])
	names := []string{}
	for _, c := range res.Columns {
		names = append(names, c.Name)
	}
	require.Contains(t, names, "m0_change_pct")
	require.Contains(t, names, "m1_change_pct")
	require.Equal(t, map[string]string{"m0": "Count", "m1": "Sum of Total"}, res.Meta.MeasureLabels)
	require.NotEmpty(t, res.Meta.PrevFrom)
}

func TestRunDataset_InvalidDoesNotHitRepo(t *testing.T) {
	repo := datasetRepo()
	svc, _ := datasetSvc(repo)
	def := datasetDef()
	def.Measures = nil
	_, err := svc.RunDataset(billingCtx(), def)
	require.True(t, ierr.IsValidation(err))
	require.Nil(t, repo.LastDataset)
}

func TestRunDataset_ReportsDisabled(t *testing.T) {
	svc, _ := datasetSvc(&testutil.InMemoryReportsRepo{On: false})
	_, err := svc.RunDataset(billingCtx(), datasetDef())
	require.True(t, ierr.IsPermissionDenied(err))
	_, err = svc.ListDatasets(billingCtx())
	require.True(t, ierr.IsPermissionDenied(err))
}

func TestListDatasets(t *testing.T) {
	svc, _ := datasetSvc(datasetRepo())
	res, err := svc.ListDatasets(billingCtx())
	require.NoError(t, err)
	require.Len(t, res.Items, len(analytics.Datasets()))
}

func TestCreateView_DatasetValidatedAndQueryViewRejects(t *testing.T) {
	svc, _ := datasetSvc(datasetRepo())
	ctx := userCtx("env_1")

	bad := datasetDef()
	bad.Dataset = "nope"
	require.True(t, ierr.IsValidation(svc.CreateView(ctx, &analytics.View{Name: "Bad", Definition: bad})))

	v := &analytics.View{Name: "Good", Definition: datasetDef()}
	require.NoError(t, svc.CreateView(ctx, v))

	_, err := svc.QueryView(ctx, v.ID, nil)
	require.True(t, ierr.IsValidation(err))
}

func TestExecuteView_RoutesDataset(t *testing.T) {
	repo := datasetRepo()
	svc, _ := datasetSvc(repo)
	_, err := svc.ExecuteView(billingCtx(), datasetDef(), nil)
	require.NoError(t, err)
	require.NotNil(t, repo.LastDataset)
}
