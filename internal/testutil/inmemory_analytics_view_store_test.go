package testutil

import (
	"context"
	"testing"
	"time"

	domainAnalytics "github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCtx(tenantID string) context.Context {
	ctx := context.Background()
	return context.WithValue(ctx, types.CtxTenantID, tenantID)
}

func TestInMemoryAnalyticsViewStore_CreateGet(t *testing.T) {
	store := NewInMemoryAnalyticsViewStore()
	ctx := testCtx("tenant_1")

	view := &domainAnalytics.View{
		ID:      "view_1",
		Name:    "My View",
		Version: 1,
		Definition: &domainAnalytics.ViewDefinition{
			Shape:   types.ShapeBreakdown,
			Metrics: []types.Metric{types.MetricUsageQuantity},
		},
	}

	err := store.Create(ctx, view)
	require.NoError(t, err)
	assert.Equal(t, "tenant_1", view.TenantID)
	assert.Equal(t, types.StatusPublished, view.Status)

	got, err := store.Get(ctx, "view_1")
	require.NoError(t, err)
	assert.Equal(t, "My View", got.Name)
	assert.Equal(t, types.ShapeBreakdown, got.Definition.Shape)
}

func TestInMemoryAnalyticsViewStore_CreateDuplicate(t *testing.T) {
	store := NewInMemoryAnalyticsViewStore()
	ctx := testCtx("tenant_1")

	view := &domainAnalytics.View{ID: "view_1", Name: "v1"}
	require.NoError(t, store.Create(ctx, view))

	err := store.Create(ctx, &domainAnalytics.View{ID: "view_1", Name: "v2"})
	require.Error(t, err)
	assert.True(t, ierr.IsAlreadyExists(err))
}

func TestInMemoryAnalyticsViewStore_GetNotFound(t *testing.T) {
	store := NewInMemoryAnalyticsViewStore()
	ctx := testCtx("tenant_1")

	_, err := store.Get(ctx, "missing")
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
}

func TestInMemoryAnalyticsViewStore_GetCrossTenantNotFound(t *testing.T) {
	store := NewInMemoryAnalyticsViewStore()

	require.NoError(t, store.Create(testCtx("tenant_1"), &domainAnalytics.View{ID: "v1", Name: "a"}))

	// Owning tenant can fetch it.
	got, err := store.Get(testCtx("tenant_1"), "v1")
	require.NoError(t, err)
	assert.Equal(t, "v1", got.ID)

	// A different tenant must not be able to read it, even by ID.
	_, err = store.Get(testCtx("tenant_2"), "v1")
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
}

func TestInMemoryAnalyticsViewStore_GetFiltersUnpublishedStatus(t *testing.T) {
	store := NewInMemoryAnalyticsViewStore()
	ctx := testCtx("tenant_1")

	view := &domainAnalytics.View{ID: "v1", Name: "a"}
	require.NoError(t, store.Create(ctx, view))

	// Simulate an archived view: same tenant, non-published status.
	view.Status = types.StatusArchived

	_, err := store.Get(ctx, "v1")
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
}

func TestInMemoryAnalyticsViewStore_ListFiltersByTenant(t *testing.T) {
	store := NewInMemoryAnalyticsViewStore()

	require.NoError(t, store.Create(testCtx("tenant_1"), &domainAnalytics.View{ID: "v1", Name: "a"}))
	require.NoError(t, store.Create(testCtx("tenant_2"), &domainAnalytics.View{ID: "v2", Name: "b"}))

	views, err := store.List(testCtx("tenant_1"))
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "v1", views[0].ID)
}

func TestInMemoryAnalyticsViewStore_Clear(t *testing.T) {
	store := NewInMemoryAnalyticsViewStore()
	ctx := testCtx("tenant_1")

	require.NoError(t, store.Create(ctx, &domainAnalytics.View{ID: "v1", Name: "a"}))
	store.Clear()

	views, err := store.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, views)
}

func viewCtx(tenant, env, user string) context.Context {
	ctx := context.WithValue(context.Background(), types.CtxTenantID, tenant)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, env)
	return context.WithValue(ctx, types.CtxUserID, user)
}

func reportView(id, name string) *domainAnalytics.View {
	return &domainAnalytics.View{ID: id, Name: name, Version: 1, Definition: &domainAnalytics.ViewDefinition{
		Family: types.FamilyReport, Shape: types.ShapeScalar, Metrics: []types.Metric{types.MetricInvoiceTotal},
		Time: domainAnalytics.TimeSpecRaw{Range: "this_month"},
	}}
}

func TestInMemoryAnalyticsViewStore_UpdateDeleteScoped(t *testing.T) {
	store := NewInMemoryAnalyticsViewStore()
	own := viewCtx("tenant_1", "env_1", "user_1")
	other := viewCtx("tenant_1", "env_2", "user_2")

	v := reportView("view_1", "A")
	require.NoError(t, store.Create(own, v))
	assert.Equal(t, "user_1", v.CreatedBy)

	upd := reportView("view_1", "B")
	upd.Version = 2
	err := store.Update(other, upd)
	assert.True(t, ierr.IsNotFound(err))
	assert.True(t, ierr.IsNotFound(store.Delete(other, "view_1")))

	require.NoError(t, store.Update(own, upd))
	got, err := store.Get(own, "view_1")
	require.NoError(t, err)
	assert.Equal(t, "B", got.Name)
	assert.Equal(t, 2, got.Version)
	assert.Equal(t, "user_1", got.CreatedBy)

	require.NoError(t, store.Delete(own, "view_1"))
	_, err = store.Get(own, "view_1")
	assert.True(t, ierr.IsNotFound(err))
	list, err := store.List(own)
	require.NoError(t, err)
	assert.Empty(t, list)
	assert.True(t, ierr.IsNotFound(store.Update(own, upd)))
}

func TestInMemoryAnalyticsViewStore_ListNewestFirst(t *testing.T) {
	store := NewInMemoryAnalyticsViewStore()
	ctx := viewCtx("tenant_1", "env_1", "user_1")
	a, b := reportView("view_a", "A"), reportView("view_b", "B")
	require.NoError(t, store.Create(ctx, a))
	b.CreatedAt = a.CreatedAt.Add(time.Second)
	require.NoError(t, store.Create(ctx, b))
	list, err := store.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "view_b", list[0].ID)
}
