package admin

import (
	"context"
	"testing"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const settingsTenantID = "tenant_acme"

type settingsTestDeps struct {
	tenants  *testutil.InMemoryTenantStore
	settings *testutil.InMemorySettingsStore
	params   service.ServiceParams
}

func newSettingsTestDeps(t *testing.T) *settingsTestDeps {
	t.Helper()
	d := &settingsTestDeps{
		tenants:  testutil.NewInMemoryTenantStore(),
		settings: testutil.NewInMemorySettingsStore(),
	}
	require.NoError(t, d.tenants.Create(context.Background(), &tenant.Tenant{ID: settingsTenantID, Name: "Acme"}))
	d.params = service.ServiceParams{
		Logger:       logger.NewNoopLogger(),
		TenantRepo:   d.tenants,
		SettingsRepo: d.settings,
	}
	return d
}

// storeTenantConfig saves a tenant_config for the tenant, as if its limits had been set before.
func (d *settingsTestDeps) storeTenantConfig(t *testing.T, cfg types.TenantConfig) {
	t.Helper()
	value, err := utils.ToMap(cfg)
	require.NoError(t, err)
	require.NoError(t, d.settings.Create(context.Background(), &settings.Setting{
		ID:        "setting_tenant_config",
		Key:       types.SettingKeyTenantConfig,
		Value:     value,
		BaseModel: types.BaseModel{TenantID: settingsTenantID, Status: types.StatusPublished},
	}))
}

// storedTenantConfig reads the tenant_config saved for a tenant.
func (d *settingsTestDeps) storedTenantConfig(t *testing.T, tenantID string) (types.TenantConfig, error) {
	t.Helper()
	setting, err := d.settings.GetTenantLevelSettingByKey(types.SetTenantID(context.Background(), tenantID), types.SettingKeyTenantConfig)
	if err != nil {
		return types.TenantConfig{}, err
	}
	cfg, err := utils.ToStruct[types.TenantConfig](setting.Value)
	require.NoError(t, err)
	return cfg, nil
}

func TestUpdateTenantConfig(t *testing.T) {
	ctx := context.Background()
	existing := types.TenantConfig{Production: 1, Development: 3, MaxUsers: 20}

	tests := []struct {
		name       string
		stored     *types.TenantConfig
		value      admindto.TenantConfigValue
		wantResp   admindto.TenantConfigResponse
		wantStored types.TenantConfig
	}{
		{
			name:       "changes only development when only development is sent",
			stored:     &existing,
			value:      admindto.TenantConfigValue{Development: lo.ToPtr(5)},
			wantResp:   admindto.TenantConfigResponse{TenantID: settingsTenantID, Production: 1, Development: 5, MaxUsers: 20},
			wantStored: types.TenantConfig{Production: 1, Development: 5, MaxUsers: 20},
		},
		{
			name:       "changes only production when only production is sent",
			stored:     &existing,
			value:      admindto.TenantConfigValue{Production: lo.ToPtr(4)},
			wantResp:   admindto.TenantConfigResponse{TenantID: settingsTenantID, Production: 4, Development: 3, MaxUsers: 20},
			wantStored: types.TenantConfig{Production: 4, Development: 3, MaxUsers: 20},
		},
		{
			name:       "changes only max_users when only max_users is sent",
			stored:     &existing,
			value:      admindto.TenantConfigValue{MaxUsers: lo.ToPtr(50)},
			wantResp:   admindto.TenantConfigResponse{TenantID: settingsTenantID, Production: 1, Development: 3, MaxUsers: 50},
			wantStored: types.TenantConfig{Production: 1, Development: 3, MaxUsers: 50},
		},
		{
			name:       "changes all three when all are sent",
			stored:     &existing,
			value:      admindto.TenantConfigValue{Production: lo.ToPtr(2), Development: lo.ToPtr(5), MaxUsers: lo.ToPtr(30)},
			wantResp:   admindto.TenantConfigResponse{TenantID: settingsTenantID, Production: 2, Development: 5, MaxUsers: 30},
			wantStored: types.TenantConfig{Production: 2, Development: 5, MaxUsers: 30},
		},
		{
			name:       "saves zero when zero is sent",
			stored:     &existing,
			value:      admindto.TenantConfigValue{Production: lo.ToPtr(0)},
			wantResp:   admindto.TenantConfigResponse{TenantID: settingsTenantID, Production: 0, Development: 3, MaxUsers: 20},
			wantStored: types.TenantConfig{Production: 0, Development: 3, MaxUsers: 20},
		},
		{
			name:       "accepts max_users of 1",
			stored:     &existing,
			value:      admindto.TenantConfigValue{MaxUsers: lo.ToPtr(1)},
			wantResp:   admindto.TenantConfigResponse{TenantID: settingsTenantID, Production: 1, Development: 3, MaxUsers: 1},
			wantStored: types.TenantConfig{Production: 1, Development: 3, MaxUsers: 1},
		},
		{
			name:       "keeps the defaults for limits never set",
			value:      admindto.TenantConfigValue{MaxUsers: lo.ToPtr(25)},
			wantResp:   admindto.TenantConfigResponse{TenantID: settingsTenantID, Production: 0, Development: 2, MaxUsers: 25},
			wantStored: types.TenantConfig{Production: 0, Development: 2, MaxUsers: 25},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newSettingsTestDeps(t)
			if tt.stored != nil {
				d.storeTenantConfig(t, *tt.stored)
			}

			resp, err := newTestSettingsService(d.params).UpdateTenantConfig(ctx, admindto.UpdateTenantConfigRequest{TenantID: settingsTenantID, Value: tt.value})
			require.NoError(t, err)
			assert.Equal(t, &tt.wantResp, resp)

			stored, err := d.storedTenantConfig(t, settingsTenantID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStored, stored)
		})
	}

	t.Run("returns the error when the setting cannot be saved", func(t *testing.T) {
		d := newSettingsTestDeps(t)
		d.params.SettingsRepo = failingSettingsStore{d.settings}

		resp, err := newTestSettingsService(d.params).UpdateTenantConfig(ctx, admindto.UpdateTenantConfigRequest{
			TenantID: settingsTenantID,
			Value:    admindto.TenantConfigValue{Development: lo.ToPtr(5)},
		})
		require.Error(t, err)
		assert.True(t, ierr.IsDatabase(err))
		assert.Nil(t, resp)
	})

	t.Run("refuses a tenant that does not exist", func(t *testing.T) {
		d := newSettingsTestDeps(t)

		_, err := newTestSettingsService(d.params).UpdateTenantConfig(ctx, admindto.UpdateTenantConfigRequest{
			TenantID: "tenant_missing",
			Value:    admindto.TenantConfigValue{Development: lo.ToPtr(5)},
		})
		require.Error(t, err)

		_, err = d.storedTenantConfig(t, "tenant_missing")
		assert.True(t, ierr.IsNotFound(err), "nothing may be saved for an unknown tenant")
	})
}

func TestUpdateTenantConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		req  admindto.UpdateTenantConfigRequest
	}{
		{name: "missing tenant id", req: admindto.UpdateTenantConfigRequest{Value: admindto.TenantConfigValue{Development: lo.ToPtr(5)}}},
		{name: "no limits sent", req: admindto.UpdateTenantConfigRequest{TenantID: settingsTenantID}},
		{name: "negative production", req: admindto.UpdateTenantConfigRequest{TenantID: settingsTenantID, Value: admindto.TenantConfigValue{Production: lo.ToPtr(-1)}}},
		{name: "negative development", req: admindto.UpdateTenantConfigRequest{TenantID: settingsTenantID, Value: admindto.TenantConfigValue{Development: lo.ToPtr(-1)}}},
		{name: "max_users of 0", req: admindto.UpdateTenantConfigRequest{TenantID: settingsTenantID, Value: admindto.TenantConfigValue{MaxUsers: lo.ToPtr(0)}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newSettingsTestDeps(t)

			resp, err := newTestSettingsService(d.params).UpdateTenantConfig(context.Background(), tt.req)
			require.Error(t, err)
			assert.True(t, ierr.IsValidation(err))
			assert.Nil(t, resp)

			_, err = d.storedTenantConfig(t, settingsTenantID)
			assert.True(t, ierr.IsNotFound(err), "nothing may be saved for a rejected request")
		})
	}
}

// newTestSettingsService wires the admin settings service to the real shared settings service, as fx does.
func newTestSettingsService(params service.ServiceParams) SettingsService {
	return NewSettingsService(params, service.NewSettingsService(params))
}

// failingSettingsStore fails every create, like a database error while saving the setting.
type failingSettingsStore struct {
	*testutil.InMemorySettingsStore
}

func (failingSettingsStore) Create(context.Context, *settings.Setting) error {
	return ierr.NewError("database unavailable").Mark(ierr.ErrDatabase)
}
