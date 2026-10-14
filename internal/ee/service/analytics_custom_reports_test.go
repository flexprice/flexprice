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

func customSvc() (AnalyticsService, *testutil.InMemoryAnalyticsViewStore) {
	views := testutil.NewInMemoryAnalyticsViewStore()
	return NewAnalyticsService(ServiceParams{
		ReportsRepo:       &testutil.InMemoryReportsRepo{On: true, Max: 500},
		AnalyticsViewRepo: views,
	}), views
}

func userCtx(env string) context.Context {
	ctx := context.WithValue(billingCtx(), types.CtxEnvironmentID, env)
	return context.WithValue(ctx, types.CtxUserID, "user_1")
}

func reportDef(key types.Metric, shape types.Shape, rng string) *analytics.ViewDefinition {
	return &analytics.ViewDefinition{Family: types.FamilyReport, Shape: shape, Metrics: []types.Metric{key}, Time: analytics.TimeSpecRaw{Range: rng}}
}

func TestCreateView_CustomReportValidation(t *testing.T) {
	svc, _ := customSvc()
	ctx := userCtx("env_1")

	ok := reportDef(types.MetricInvoiceTotal, types.ShapeBreakdown, "this_quarter")
	ok.Dimensions = []string{"customer_id"}
	ok.Filters = []*analytics.Filter{{Field: "invoice_type", Op: types.EQUAL, Value: []string{"SUBSCRIPTION"}}}
	ok.Compare, ok.Display, ok.Currency = "previous_period", "chart", "USD"
	require.NoError(t, svc.CreateView(ctx, &analytics.View{Name: "Acme", Definition: ok}))

	variant := reportDef(types.MetricInvoiceTotalIssued, types.ShapeScalar, "last_month")
	require.NoError(t, svc.CreateView(ctx, &analytics.View{Name: "Issued", Definition: variant}))

	bad := map[string]func(d *analytics.ViewDefinition){
		"filter not allowed": func(d *analytics.ViewDefinition) {
			d.Filters = []*analytics.Filter{{Field: "meter_id", Op: types.EQUAL, Value: []string{"m"}}}
		},
		"dimension not allowed": func(d *analytics.ViewDefinition) { d.Shape, d.Dimensions = types.ShapeBreakdown, []string{"meter_id"} },
		"grain on scalar":       func(d *analytics.ViewDefinition) { d.Time.Grain = types.GrainDay },
		"bad compare":           func(d *analytics.ViewDefinition) { d.Compare = "previous_week" },
		"shape not allowed":     func(d *analytics.ViewDefinition) { d.Shape = "pie" },
		"empty filter values": func(d *analytics.ViewDefinition) {
			d.Filters = []*analytics.Filter{{Field: "customer_id", Op: types.IN}}
		},
	}
	for name, mut := range bad {
		d := reportDef(types.MetricInvoiceTotal, types.ShapeScalar, "this_month")
		mut(d)
		err := svc.CreateView(ctx, &analytics.View{Name: name, Definition: d})
		require.Error(t, err, name)
		assert.True(t, ierr.IsValidation(err), name)
	}

	err := svc.CreateView(ctx, &analytics.View{Name: "  ", Definition: reportDef(types.MetricInvoiceTotal, types.ShapeScalar, "this_month")})
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))

	ev, _ := analytics.LookupReport(string(types.MetricEventsVolume))
	tooLong := reportDef(types.MetricEventsVolume, ev.DefaultShape, "last_30_days")
	err = svc.CreateView(ctx, &analytics.View{Name: "events", Definition: tooLong})
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
	require.NoError(t, svc.CreateView(ctx, &analytics.View{Name: "events", Definition: reportDef(types.MetricEventsVolume, ev.DefaultShape, "last_7_days")}))

	noCur := reportDef(types.MetricEventsVolume, ev.DefaultShape, "last_7_days")
	noCur.Currency = "USD"
	err = svc.CreateView(ctx, &analytics.View{Name: "events cur", Definition: noCur})
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))

	withCur := reportDef(types.MetricInvoiceTotal, types.ShapeScalar, "this_month")
	withCur.Currency = "USD"
	require.NoError(t, svc.CreateView(ctx, &analytics.View{Name: "inv cur", Definition: withCur}))
}

func TestCustomReport_CRUDScopedAndVersioned(t *testing.T) {
	svc, _ := customSvc()
	ctx, other := userCtx("env_1"), userCtx("env_2")

	v := &analytics.View{Name: "A", Definition: reportDef(types.MetricInvoiceTotal, types.ShapeScalar, "this_month")}
	require.NoError(t, svc.CreateView(ctx, v))
	require.NoError(t, svc.CreateView(ctx, &analytics.View{Name: "usage", Definition: &analytics.ViewDefinition{
		Shape: types.ShapeBreakdown, Metrics: []types.Metric{types.MetricUsageQuantity}, Dimensions: []string{"meter_id"},
	}}))

	list, err := svc.ListViews(ctx, types.FamilyReport)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, v.ID, list[0].ID)
	otherList, err := svc.ListViews(other, types.FamilyReport)
	require.NoError(t, err)
	assert.Empty(t, otherList)

	_, err = svc.GetView(other, v.ID)
	assert.True(t, ierr.IsNotFound(err))

	upd := &analytics.View{ID: v.ID, Name: "B", Definition: reportDef(types.MetricInvoiceTotal, types.ShapeTimeseries, "this_year")}
	upd.Definition.Time.Grain = types.GrainMonth
	require.NoError(t, svc.UpdateView(ctx, upd))
	got, err := svc.GetView(ctx, v.ID)
	require.NoError(t, err)
	assert.Equal(t, "B", got.Name)
	assert.Equal(t, 2, got.Version)

	assert.True(t, ierr.IsNotFound(svc.UpdateView(other, upd)))
	assert.True(t, ierr.IsNotFound(svc.DeleteView(other, v.ID)))

	toUsage := &analytics.View{ID: v.ID, Name: "B", Definition: &analytics.ViewDefinition{
		Shape: types.ShapeBreakdown, Metrics: []types.Metric{types.MetricUsageQuantity}, Dimensions: []string{"meter_id"},
	}}
	err = svc.UpdateView(ctx, toUsage)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))

	_, err = svc.QueryView(ctx, v.ID, nil)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))

	require.NoError(t, svc.DeleteView(ctx, v.ID))
	_, err = svc.GetView(ctx, v.ID)
	assert.True(t, ierr.IsNotFound(err))
}

func TestCustomReport_DisabledReportsIs403(t *testing.T) {
	views := testutil.NewInMemoryAnalyticsViewStore()
	svc := NewAnalyticsService(ServiceParams{ReportsRepo: &testutil.InMemoryReportsRepo{On: false}, AnalyticsViewRepo: views})
	err := svc.CreateView(userCtx("env_1"), &analytics.View{Name: "A", Definition: reportDef(types.MetricInvoiceTotal, types.ShapeScalar, "this_month")})
	require.Error(t, err)
	assert.True(t, ierr.IsPermissionDenied(err))
}
