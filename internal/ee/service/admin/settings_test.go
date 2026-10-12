package admin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/config"
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

// storeSetting saves a tenant-level setting for a tenant, last changed at updatedAt.
func (d *settingsTestDeps) storeSetting(t *testing.T, tenantID string, key types.SettingKey, value map[string]interface{}, updatedAt time.Time) {
	t.Helper()
	require.NoError(t, d.settings.Create(context.Background(), &settings.Setting{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		Key:       key,
		Value:     value,
		BaseModel: types.BaseModel{TenantID: tenantID, Status: types.StatusPublished, UpdatedAt: updatedAt},
	}))
}

// withSAML switches SAML on for the deployment, as auth.saml.enabled does.
func (d *settingsTestDeps) withSAML() {
	d.params.Config = &config.Configuration{}
	d.params.Config.Auth.SAML.Enabled = true
}

func TestListTenantSettings(t *testing.T) {
	changed := time.Date(2026, 10, 9, 8, 15, 0, 0, time.UTC)

	tests := []struct {
		name     string
		setup    func(t *testing.T, d *settingsTestDeps)
		wantJSON string
	}{
		{
			name:     "nothing stored gives the default limits and no saml_config",
			wantJSON: `{"items": [{"key": "tenant_config", "value": {"production": 0, "development": 2, "max_users": 10}, "is_default": true}]}`,
		},
		{
			name: "a stored tenant_config is merged over the defaults",
			setup: func(t *testing.T, d *settingsTestDeps) {
				d.storeSetting(t, settingsTenantID, types.SettingKeyTenantConfig, map[string]interface{}{"max_users": 15}, changed)
			},
			wantJSON: `{"items": [{"key": "tenant_config", "value": {"production": 0, "development": 2, "max_users": 15}, "is_default": false, "updated_at": "2026-10-09T08:15:00Z"}]}`,
		},
		{
			name: "another tenant's settings are not shown",
			setup: func(t *testing.T, d *settingsTestDeps) {
				d.storeSetting(t, "tenant_other", types.SettingKeyTenantConfig, map[string]interface{}{"max_users": 99}, changed)
			},
			wantJSON: `{"items": [{"key": "tenant_config", "value": {"production": 0, "development": 2, "max_users": 10}, "is_default": true}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newSettingsTestDeps(t)
			if tt.setup != nil {
				tt.setup(t, d)
			}

			resp, err := newTestSettingsService(d.params).ListTenantSettings(context.Background(), settingsTenantID)

			require.NoError(t, err)
			body, err := json.Marshal(resp)
			require.NoError(t, err)
			assert.JSONEq(t, tt.wantJSON, string(body))
		})
	}
}

func TestListTenantSettingsWithSAML(t *testing.T) {
	samlItem := func(t *testing.T, resp *admindto.ListSettingsResponse) *admindto.SettingResponse {
		t.Helper()
		require.NotNil(t, resp)
		keys := lo.Map(resp.Items, func(item *admindto.SettingResponse, _ int) types.SettingKey { return item.Key })
		require.Equal(t, []types.SettingKey{types.SettingKeyTenantConfig, types.SettingKeySAMLConfig}, keys)
		return resp.Items[1]
	}

	t.Run("nothing stored gives the SAML default", func(t *testing.T) {
		d := newSettingsTestDeps(t)
		d.withSAML()

		resp, err := newTestSettingsService(d.params).ListTenantSettings(context.Background(), settingsTenantID)

		require.NoError(t, err)
		saml := samlItem(t, resp)
		assert.True(t, saml.IsDefault)
		assert.Equal(t, false, saml.Value["enabled"])
	})

	t.Run("a stored saml_config is read without a super admin on the request", func(t *testing.T) {
		d := newSettingsTestDeps(t)
		d.withSAML()
		d.storeSetting(t, settingsTenantID, types.SettingKeySAMLConfig,
			map[string]interface{}{"enabled": true, "default_role": "all_writer"}, time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC))

		resp, err := newTestSettingsService(d.params).ListTenantSettings(context.Background(), settingsTenantID)

		require.NoError(t, err)
		saml := samlItem(t, resp)
		assert.False(t, saml.IsDefault)
		assert.Equal(t, "2026-06-02T12:00:00Z", saml.UpdatedAt)
		assert.Equal(t, true, saml.Value["enabled"])
		assert.Equal(t, "all_writer", saml.Value["default_role"])
		assert.Equal(t, false, saml.Value["enforce_sso"], "a field that was never stored comes from the default")
	})
}

func TestListTenantSettingsUnknownTenant(t *testing.T) {
	d := newSettingsTestDeps(t)
	d.params.TenantRepo = notFoundTenantStore{InMemoryTenantStore: d.tenants}

	resp, err := newTestSettingsService(d.params).ListTenantSettings(context.Background(), "tenant_missing")

	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
	assert.Nil(t, resp)
}

// failingSettingsReadStore fails every tenant-level read, like a database error while loading a setting.
type failingSettingsReadStore struct {
	*testutil.InMemorySettingsStore
}

func (failingSettingsReadStore) GetTenantLevelSettingByKey(context.Context, types.SettingKey) (*settings.Setting, error) {
	return nil, ierr.NewError("database unavailable").Mark(ierr.ErrDatabase)
}

func TestListTenantSettingsReadError(t *testing.T) {
	d := newSettingsTestDeps(t)
	d.params.SettingsRepo = failingSettingsReadStore{InMemorySettingsStore: d.settings}

	resp, err := newTestSettingsService(d.params).ListTenantSettings(context.Background(), settingsTenantID)

	require.Error(t, err)
	assert.True(t, ierr.IsDatabase(err))
	assert.Nil(t, resp)
}
