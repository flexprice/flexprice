package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func billingCtx() context.Context {
	ctx := context.WithValue(context.Background(), types.CtxTenantID, "tenant_1")
	return context.WithValue(ctx, types.CtxEnvironmentID, "env_1")
}

func newBillingSvc(repo *testutil.InMemoryReportsRepo) AnalyticsService {
	if repo.Max == 0 {
		repo.Max = 500
	}
	return NewAnalyticsService(ServiceParams{ReportsRepo: repo})
}

func billingView(shape types.Shape, dims ...string) *analytics.ViewDefinition {
	return &analytics.ViewDefinition{
		Family:     types.FamilyBilling,
		Shape:      shape,
		Metrics:    []types.Metric{types.MetricInvoiceTotal},
		Dimensions: dims,
		Time:       analytics.TimeSpecRaw{Range: "last_30_days", Grain: types.GrainMonth},
	}
}

func reportCols(names ...string) []analytics.ReportColumn {
	cols := make([]analytics.ReportColumn, len(names))
	for i, n := range names {
		cols[i] = analytics.ReportColumn{Name: n, Type: types.ColumnTypeString, Role: types.ColumnRoleDimension}
	}
	if len(cols) > 0 {
		cols[len(cols)-1].Type, cols[len(cols)-1].Role = types.ColumnTypeDecimal, types.ColumnRoleMetric
	}
	return cols
}

func TestExecuteView_BillingDisabledIs403(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: false}
	_, err := newBillingSvc(repo).ExecuteView(billingCtx(), billingView(types.ShapeScalar), nil)
	require.Error(t, err)
	assert.True(t, ierr.IsPermissionDenied(err))
	assert.Nil(t, repo.Last)
}

func TestExecuteView_BillingScopesFromContext(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: true, Columns: reportCols("currency", "invoice_total"), Rows: [][]string{{"usd", "10"}}}
	res, err := newBillingSvc(repo).ExecuteView(billingCtx(), billingView(types.ShapeScalar), nil)
	require.NoError(t, err)
	require.NotNil(t, repo.Last)
	assert.Equal(t, "tenant_1", repo.LastTenantID)
	assert.Equal(t, "env_1", repo.LastEnvironmentID)
	assert.Equal(t, types.ShapeScalar, repo.Last.Shape)
	assert.Equal(t, []types.Metric{types.MetricInvoiceTotal}, repo.Last.Metrics)
	assert.Equal(t, "billing", res.Meta.QuerySource)
	require.Len(t, res.Columns, 2)
	assert.Equal(t, "currency", res.Columns[0].Name)
	assert.Equal(t, "", res.Columns[1].Currency)
	assert.Equal(t, [][]string{{"usd", "10"}}, res.Rows)
}

func TestExecuteView_BillingRepoErrorPassesThrough(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: true, Err: ierr.NewError("denied").Mark(ierr.ErrPermissionDenied)}
	_, err := newBillingSvc(repo).ExecuteView(context.Background(), billingView(types.ShapeScalar), nil)
	require.Error(t, err)
	assert.True(t, ierr.IsPermissionDenied(err))
	assert.Equal(t, "", repo.LastTenantID)
}

func TestExecuteView_BillingBreakdownSortAndLimit(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: true, Columns: reportCols("customer_id", "currency", "invoice_total"), Rows: [][]string{
		{"c1", "usd", "5"}, {"c2", "usd", "50"}, {"c3", "usd", "20"},
	}}
	v := billingView(types.ShapeBreakdown, "customer_id")
	v.Sort = []*analytics.SortSpec{{Field: "invoice_total", Dir: types.SortDirectionDesc}}
	v.Limit = 2
	res, err := newBillingSvc(repo).ExecuteView(billingCtx(), v, nil)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"c2", "usd", "50"}, {"c3", "usd", "20"}}, res.Rows)
}

func TestExecuteView_BillingOverflowIsValidationError(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: true, Err: ierr.NewErrorf("result exceeds %d rows", 2).Mark(ierr.ErrValidation)}
	_, err := newBillingSvc(repo).ExecuteView(billingCtx(), billingView(types.ShapeScalar), nil)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestExecuteView_BillingRunErrorWithNilLogger(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: true, Err: ierr.NewError("boom").Mark(ierr.ErrDatabase)}
	_, err := newBillingSvc(repo).ExecuteView(billingCtx(), billingView(types.ShapeScalar), nil)
	require.Error(t, err)
}

func TestExecuteView_BillingEmptyRowsNotNil(t *testing.T) {
	repo := &testutil.InMemoryReportsRepo{On: true}
	res, err := newBillingSvc(repo).ExecuteView(billingCtx(), billingView(types.ShapeScalar), nil)
	require.NoError(t, err)
	assert.NotNil(t, res.Rows)
}
