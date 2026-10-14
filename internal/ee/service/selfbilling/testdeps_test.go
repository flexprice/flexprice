package selfbilling

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type testDeps struct {
	tenants  *testutil.InMemoryTenantStore
	envs     *testutil.InMemoryEnvironmentStore
	settings *testutil.InMemorySettingsStore
}

func newTestService(t *testing.T, fc *fakeClient) (*selfBillingService, *testDeps) {
	t.Helper()
	d := &testDeps{tenants: testutil.NewInMemoryTenantStore(), envs: testutil.NewInMemoryEnvironmentStore(), settings: testutil.NewInMemorySettingsStore()}
	cfg := &config.Configuration{Billing: config.BillingConfig{
		Enabled: true, Region: "us", APIHost: "https://mumbai.test/v1", APIKey: "sk", PlanID: "plan_1", DashboardURL: "https://us.app.test",
	}}
	params := service.ServiceParams{Logger: logger.NewNoopLogger(), Config: cfg, TenantRepo: d.tenants, EnvironmentRepo: d.envs, SettingsRepo: d.settings}
	settings := service.NewSettingsService(params)
	envs := service.NewEnvironmentService(d.envs, nil, settings, params)
	return &selfBillingService{ServiceParams: params, client: fc, envs: envs, settings: settings}, d
}

func seedTenant(t *testing.T, d *testDeps, id string, status types.TenantInternalStatus) {
	t.Helper()
	require.NoError(t, d.tenants.Create(context.Background(), &tenant.Tenant{ID: id, Name: id, InternalStatus: status}))
}

func tenantCtx(id string) context.Context { return types.SetTenantID(context.Background(), id) }
