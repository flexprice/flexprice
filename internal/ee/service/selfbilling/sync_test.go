package selfbilling

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncStatuses(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	ctx := context.Background()

	seedTenant(t, d, "paid", types.TenantInternalStatusTrialing)
	fc.subs["us:paid"] = &remoteSubscription{id: "s1", status: "active"}
	seedTenant(t, d, "lapsed", types.TenantInternalStatusTrialing)
	fc.subs["us:lapsed"] = &remoteSubscription{id: "s2", status: "incomplete"}
	seedTenant(t, d, "intrial", types.TenantInternalStatusTrialing)
	fc.subs["us:intrial"] = &remoteSubscription{id: "s3", status: "trialing"}
	seedTenant(t, d, "loyal", types.TenantInternalStatusActive)
	fc.subs["us:loyal"] = &remoteSubscription{id: "s4", status: "cancelled"}
	seedTenant(t, d, "orphan", types.TenantInternalStatusTrialing)
	seedTenant(t, d, "cancelledtrial", types.TenantInternalStatusTrialing)
	fc.subs["us:cancelledtrial"] = &remoteSubscription{id: "s5", status: "cancelled"}
	seedTenant(t, d, "returning", types.TenantInternalStatusSuspended)
	fc.subs["us:returning"] = &remoteSubscription{id: "s6", status: "active"}

	require.NoError(t, s.syncStatuses(ctx))

	status := func(id string) types.TenantInternalStatus {
		got, err := d.tenants.GetByID(ctx, id)
		require.NoError(t, err)
		return got.InternalStatus
	}
	assert.Equal(t, types.TenantInternalStatusActive, status("paid"))
	assert.Equal(t, types.TenantInternalStatusSuspended, status("lapsed"))
	assert.Equal(t, types.TenantInternalStatusTrialing, status("intrial"))
	assert.Equal(t, types.TenantInternalStatusActive, status("loyal"), "never auto-suspend active")
	assert.True(t, fc.customers["us:orphan"], "missing customer provisioned")
	assert.Equal(t, "trialing", fc.subs["us:orphan"].status)
	assert.Equal(t, types.TenantInternalStatusSuspended, status("cancelledtrial"), "cancelled trial is suspended")
	assert.Equal(t, "cancelled", fc.subs["us:cancelledtrial"].status, "cancelled trial is not re-provisioned")
	assert.False(t, fc.customers["us:cancelledtrial"])
	assert.Equal(t, 1, fc.trialsStarted, "only the orphan gets a trial")
	assert.Equal(t, types.TenantInternalStatusActive, status("returning"), "suspended tenant paid in Mumbai is re-activated")
}

func TestSyncStatuses_SkipsBillingTenant(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	s.Config.Billing.TenantID = "t_billing"
	seedTenant(t, d, "t_billing", types.TenantInternalStatusTrialing)
	require.NoError(t, s.syncStatuses(context.Background()))
	assert.False(t, fc.customers["us:t_billing"])
}

// failingUpdates refuses Update for one tenant so the sync must carry on with the rest.
type failingUpdates struct {
	*testutil.InMemoryTenantStore
	failID string
}

func (f failingUpdates) Update(ctx context.Context, t *tenant.Tenant) error {
	if t.ID == f.failID {
		return errors.New("db down")
	}
	return f.InMemoryTenantStore.Update(ctx, t)
}

func TestSyncStatuses_OneFailureDoesNotStopOthers(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	seedTenant(t, d, "a", types.TenantInternalStatusTrialing)
	seedTenant(t, d, "b", types.TenantInternalStatusTrialing)
	fc.subs["us:a"] = &remoteSubscription{id: "sa", status: "incomplete"}
	fc.subs["us:b"] = &remoteSubscription{id: "sb", status: "incomplete"}
	s.TenantRepo = failingUpdates{InMemoryTenantStore: d.tenants, failID: "a"}

	require.NoError(t, s.syncStatuses(context.Background()))
	got, err := d.tenants.GetByID(context.Background(), "b")
	require.NoError(t, err)
	assert.Equal(t, types.TenantInternalStatusSuspended, got.InternalStatus)
	got, err = d.tenants.GetByID(context.Background(), "a")
	require.NoError(t, err)
	assert.Equal(t, types.TenantInternalStatusTrialing, got.InternalStatus)
}

func TestSyncStatuses_SkipsUnpublishedTenants(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	ctx := context.Background()
	for _, st := range []types.Status{types.StatusArchived, types.StatusDeleted} {
		id := string(st)
		require.NoError(t, d.tenants.Create(ctx, &tenant.Tenant{ID: id, Name: id, Status: st, InternalStatus: types.TenantInternalStatusTrialing}))
	}
	fc.subs["us:archived"] = &remoteSubscription{id: "sa", status: "incomplete"}

	require.NoError(t, s.syncStatuses(ctx))

	assert.Empty(t, fc.customers, "unpublished tenants are not provisioned")
	assert.Zero(t, fc.trialsStarted)
	got, err := d.tenants.GetByID(ctx, "archived")
	require.NoError(t, err)
	assert.Equal(t, types.TenantInternalStatusTrialing, got.InternalStatus, "unpublished tenants are not suspended")
}

// staleList lists tenants as they were before a concurrent change, as a slow sync would see them.
type staleList struct {
	*testutil.InMemoryTenantStore
	snapshot []*tenant.Tenant
}

func (s staleList) List(context.Context) ([]*tenant.Tenant, error) { return s.snapshot, nil }

func TestSyncStatuses_DecidesFromFreshTenant(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	ctx := context.Background()
	seedTenant(t, d, "manual", types.TenantInternalStatusTrialing)
	fc.subs["us:manual"] = &remoteSubscription{id: "sm", status: "incomplete"}
	listed, err := d.tenants.GetByID(ctx, "manual")
	require.NoError(t, err)
	snapshot := *listed
	require.NoError(t, d.tenants.Update(ctx, tenant.NewBuilder(listed).WithInternalStatus(types.TenantInternalStatusActive).Build()))
	s.TenantRepo = staleList{InMemoryTenantStore: d.tenants, snapshot: []*tenant.Tenant{&snapshot}}

	require.NoError(t, s.syncStatuses(ctx))

	got, err := d.tenants.GetByID(ctx, "manual")
	require.NoError(t, err)
	assert.Equal(t, types.TenantInternalStatusActive, got.InternalStatus, "tenant activated after listing is not suspended")
}

func TestSyncStatuses_StopsWhenCancelled(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	seedTenant(t, d, "orphan", types.TenantInternalStatusTrialing)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, s.syncStatuses(ctx), context.Canceled)
	assert.Empty(t, fc.customers)
}
