package selfbilling

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// subFailingClient fails LatestSubscription for the listed external IDs only.
type subFailingClient struct {
	*fakeClient
	failFor map[string]error
}

func (c *subFailingClient) LatestSubscription(ctx context.Context, extID string) (*remoteSubscription, error) {
	if err, ok := c.failFor[extID]; ok {
		return nil, err
	}
	return c.fakeClient.LatestSubscription(ctx, extID)
}

func tenantStatus(t *testing.T, d *testDeps, id string) types.TenantInternalStatus {
	t.Helper()
	got, err := d.tenants.GetByID(context.Background(), id)
	require.NoError(t, err)
	return got.InternalStatus
}

func TestSyncStatuses(t *testing.T) {
	tests := []struct {
		name         string
		local        types.TenantInternalStatus
		remote       types.SubscriptionStatus // "" = no Flexprice Cloud subscription
		wantStatus   types.TenantInternalStatus
		wantProdEnvs int
		wantTrial    bool
	}{
		{
			name:         "trialing_paid_becomes_active_with_production",
			local:        types.TenantInternalStatusTrialing,
			remote:       types.SubscriptionStatusActive,
			wantStatus:   types.TenantInternalStatusActive,
			wantProdEnvs: 1,
		},
		{
			name:       "trialing_unpaid_is_suspended",
			local:      types.TenantInternalStatusTrialing,
			remote:     types.SubscriptionStatusIncomplete,
			wantStatus: types.TenantInternalStatusSuspended,
		},
		{
			name:         "active_is_never_suspended",
			local:        types.TenantInternalStatusActive,
			remote:       types.SubscriptionStatusIncomplete,
			wantStatus:   types.TenantInternalStatusActive,
			wantProdEnvs: 1,
		},
		{
			name:         "active_missing_production_env_gets_it",
			local:        types.TenantInternalStatusActive,
			remote:       types.SubscriptionStatusActive,
			wantStatus:   types.TenantInternalStatusActive,
			wantProdEnvs: 1,
		},
		{
			name:       "trialing_without_subscription_is_provisioned",
			local:      types.TenantInternalStatusTrialing,
			wantStatus: types.TenantInternalStatusTrialing,
			wantTrial:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := newFakeClient()
			s, d := newTestService(fc)
			seedTenant(t, d, "t1", tt.local)
			if tt.remote != "" {
				fc.subs["us:t1"] = &remoteSubscription{
					id:     "sub_us:t1",
					status: tt.remote,
				}
			}

			require.NoError(t, s.syncStatuses(context.Background()))

			assert.Equal(t, tt.wantStatus, tenantStatus(t, d, "t1"))
			assert.Equal(t, tt.wantProdEnvs, productionEnvs(t, d, "t1"))
			assert.Equal(t, tt.wantTrial, fc.trials() == 1)
			assert.Equal(t, tt.wantTrial, fc.hasCustomer("us:t1"))
		})
	}
}

func TestSyncStatuses_SkipsUnpublishedAndBillingTenant(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(fc)
	s.Config.Billing.TenantID = "t_billing"

	seedTenant(t, d, "t_billing", types.TenantInternalStatusTrialing)
	require.NoError(t, d.tenants.Create(context.Background(), &tenant.Tenant{
		ID:             "t_archived",
		Name:           "t_archived",
		Status:         types.StatusArchived,
		InternalStatus: types.TenantInternalStatusTrialing,
	}))

	require.NoError(t, s.syncStatuses(context.Background()))

	assert.Equal(t, 0, fc.trials())
	assert.False(t, fc.hasCustomer("us:t_billing"))
	assert.False(t, fc.hasCustomer("us:t_archived"))
}

func TestSyncStatuses_OneFailureDoesNotStopOthers(t *testing.T) {
	cloudDown := errors.New("flexprice cloud down")
	fc := newFakeClient()
	client := &subFailingClient{
		fakeClient: fc,
		failFor:    map[string]error{"us:t_bad": cloudDown},
	}
	s, d := newTestService(client)

	seedTenant(t, d, "t_bad", types.TenantInternalStatusTrialing)
	seedTenant(t, d, "t_good", types.TenantInternalStatusTrialing)
	fc.subs["us:t_good"] = &remoteSubscription{
		id:     "sub_us:t_good",
		status: types.SubscriptionStatusActive,
	}

	err := s.syncStatuses(context.Background())

	assert.ErrorIs(t, err, cloudDown)
	assert.Equal(t, types.TenantInternalStatusActive, tenantStatus(t, d, "t_good"))
	assert.Equal(t, types.TenantInternalStatusTrialing, tenantStatus(t, d, "t_bad"))
}

func TestSyncStatuses_StopsOnCancelledContext(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(fc)
	seedTenant(t, d, "t1", types.TenantInternalStatusTrialing)
	fc.subs["us:t1"] = activeSub

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.syncStatuses(ctx)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, types.TenantInternalStatusTrialing, tenantStatus(t, d, "t1"))
}
