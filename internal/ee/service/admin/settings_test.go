package admin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	_ "github.com/flexprice/flexprice/internal/ee/auth/saml" // registers the saml_config validator
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

// withEnvironment saves an environment owned by tenantID.
func (d *settingsTestDeps) withEnvironment(t *testing.T, tenantID, environmentID string) {
	t.Helper()
	envs, ok := d.params.EnvironmentRepo.(*testutil.InMemoryEnvironmentStore)
	if !ok {
		envs = testutil.NewInMemoryEnvironmentStore()
		d.params.EnvironmentRepo = envs
	}
	require.NoError(t, envs.Create(types.SetTenantID(context.Background(), tenantID), &environment.Environment{ID: environmentID, Name: "Production",
		BaseModel: types.BaseModel{TenantID: tenantID, Status: types.StatusPublished}}))
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

			resp, err := newTestSettingsService(d.params).ListTenantSettings(context.Background(), settingsTenantID, "")

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

		resp, err := newTestSettingsService(d.params).ListTenantSettings(context.Background(), settingsTenantID, "")

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

		resp, err := newTestSettingsService(d.params).ListTenantSettings(context.Background(), settingsTenantID, "")

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

	resp, err := newTestSettingsService(d.params).ListTenantSettings(context.Background(), "tenant_missing", "")

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

	resp, err := newTestSettingsService(d.params).ListTenantSettings(context.Background(), settingsTenantID, "")

	require.Error(t, err)
	assert.True(t, ierr.IsDatabase(err))
	assert.Nil(t, resp)
}

func TestListTenantSettingsForEnvironment(t *testing.T) {
	t.Run("lists the tenant-wide keys first, then every setting of the environment", func(t *testing.T) {
		d := newSettingsTestDeps(t)
		d.withEnvironment(t, settingsTenantID, revrecEnvironmentID)
		ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), settingsTenantID), revrecEnvironmentID)
		require.NoError(t, d.settings.Create(ctx, &settings.Setting{
			ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING), Key: types.SettingKeyInvoiceConfig, EnvironmentID: revrecEnvironmentID,
			Value:     map[string]interface{}{"prefix": "ACME"},
			BaseModel: types.BaseModel{TenantID: settingsTenantID, Status: types.StatusPublished},
		}))

		resp, err := newTestSettingsService(d.params).ListTenantSettings(context.Background(), settingsTenantID, revrecEnvironmentID)

		require.NoError(t, err)
		defaults, err := types.GetDefaultSettings()
		require.NoError(t, err)
		keys := lo.Map(resp.Items, func(item *admindto.SettingResponse, _ int) types.SettingKey { return item.Key })
		assert.Len(t, keys, len(defaults)-1, "every key but saml_config, which is off")
		assert.Equal(t, types.SettingKeyTenantConfig, keys[0])
		invoice, ok := lo.Find(resp.Items, func(item *admindto.SettingResponse) bool { return item.Key == types.SettingKeyInvoiceConfig })
		require.True(t, ok)
		assert.False(t, invoice.IsDefault)
		assert.Equal(t, "ACME", invoice.Value["prefix"])
	})

	t.Run("an environment of another tenant is not found", func(t *testing.T) {
		d := newSettingsTestDeps(t)
		d.withEnvironment(t, "tenant_other", "env_other")

		resp, err := newTestSettingsService(d.params).ListTenantSettings(context.Background(), settingsTenantID, "env_other")

		require.Error(t, err)
		assert.True(t, ierr.IsNotFound(err))
		assert.Nil(t, resp)
	})
}

func TestGetTenantSetting(t *testing.T) {
	tests := []struct {
		name          string
		environmentID string
		key           types.SettingKey
		wantErr       func(error) bool
	}{
		{name: "a tenant-wide key needs no environment", key: types.SettingKeyTenantConfig},
		{name: "an environment key is read from the environment", environmentID: revrecEnvironmentID, key: types.SettingKeyRevenueAnalyticsConfig},
		{name: "an environment key without an environment", key: types.SettingKeyInvoiceConfig, wantErr: ierr.IsValidation},
		{name: "an unknown key", environmentID: revrecEnvironmentID, key: "no_such_key", wantErr: ierr.IsValidation},
		{name: "saml_config while SAML is off", key: types.SettingKeySAMLConfig, wantErr: ierr.IsNotFound},
		{name: "an environment of another tenant", environmentID: "env_other", key: types.SettingKeyInvoiceConfig, wantErr: ierr.IsNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newSettingsTestDeps(t)
			d.withEnvironment(t, settingsTenantID, revrecEnvironmentID)
			d.withEnvironment(t, "tenant_other", "env_other")

			resp, err := newTestSettingsService(d.params).GetTenantSetting(context.Background(), settingsTenantID, tt.environmentID, tt.key)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.True(t, tt.wantErr(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.key, resp.Key)
			assert.True(t, resp.IsDefault)
		})
	}
}

func TestUpdateTenantSetting(t *testing.T) {
	changed := time.Date(2026, 10, 9, 8, 15, 0, 0, time.UTC)

	t.Run("a never-stored setting is created with its whole nested value", func(t *testing.T) {
		d := newSettingsTestDeps(t)
		d.withEnvironment(t, settingsTenantID, revrecEnvironmentID)
		svc := newTestSettingsService(d.params)
		value := map[string]interface{}{"sections": []interface{}{map[string]interface{}{
			"id": "usage", "enabled": true, "tabs": []interface{}{map[string]interface{}{"id": "graph", "type": "usage_graph", "enabled": true}},
		}}}

		resp, err := svc.UpdateTenantSetting(context.Background(), settingsTenantID, revrecEnvironmentID, types.SettingKeyCustomerPortalConfig,
			admindto.UpdateSettingRequest{Value: value})

		require.NoError(t, err)
		assert.False(t, resp.IsDefault)
		got, err := svc.GetTenantSetting(context.Background(), settingsTenantID, revrecEnvironmentID, types.SettingKeyCustomerPortalConfig)
		require.NoError(t, err)
		body, err := json.Marshal(got.Value["sections"])
		require.NoError(t, err)
		assert.JSONEq(t, `[{"id": "usage", "enabled": true, "tabs": [{"id": "graph", "type": "usage_graph", "enabled": true}]}]`, string(body))
	})

	t.Run("updated_at must match what is stored", func(t *testing.T) {
		d := newSettingsTestDeps(t)
		d.storeSetting(t, settingsTenantID, types.SettingKeyTenantConfig, map[string]interface{}{"max_users": 15}, changed)
		svc := newTestSettingsService(d.params)

		_, err := svc.UpdateTenantSetting(context.Background(), settingsTenantID, "", types.SettingKeyTenantConfig,
			admindto.UpdateSettingRequest{Value: map[string]interface{}{"max_users": 20}})
		assert.True(t, ierr.IsVersionConflict(err), "an empty updated_at means nothing was stored")

		resp, err := svc.UpdateTenantSetting(context.Background(), settingsTenantID, "", types.SettingKeyTenantConfig,
			admindto.UpdateSettingRequest{Value: map[string]interface{}{"max_users": 20}, UpdatedAt: "2026-10-09T08:15:00Z"})
		require.NoError(t, err)
		assert.EqualValues(t, 20, resp.Value["max_users"])
	})

	t.Run("an operator turns revenue recognition on", func(t *testing.T) {
		d := newSettingsTestDeps(t)
		d.withEnvironment(t, settingsTenantID, revrecEnvironmentID)

		resp, err := newTestSettingsService(d.params).UpdateTenantSetting(context.Background(), settingsTenantID, revrecEnvironmentID,
			types.SettingKeyRevenueRecognitionConfig, admindto.UpdateSettingRequest{Value: map[string]interface{}{"enabled": true}})

		require.NoError(t, err)
		assert.Equal(t, true, resp.Value["enabled"])
	})

	t.Run("an operator approves SAML, which tenants cannot", func(t *testing.T) {
		d := newSettingsTestDeps(t)
		d.withSAML()

		resp, err := newTestSettingsService(d.params).UpdateTenantSetting(context.Background(), settingsTenantID, "",
			types.SettingKeySAMLConfig, admindto.UpdateSettingRequest{Value: map[string]interface{}{"active": true}})

		require.NoError(t, err)
		assert.Equal(t, true, resp.Value["active"])
	})

	refused := []struct {
		name    string
		key     types.SettingKey
		req     admindto.UpdateSettingRequest
		wantErr func(error) bool
	}{
		{name: "billing_entity_config", key: types.SettingKeyBillingEntityConfig,
			req: admindto.UpdateSettingRequest{Value: map[string]interface{}{"mode": "enabled"}}, wantErr: ierr.IsValidation},
		{name: "an empty value", key: types.SettingKeyTenantConfig, wantErr: ierr.IsValidation},
		{name: "an unknown key", key: "no_such_key",
			req: admindto.UpdateSettingRequest{Value: map[string]interface{}{"x": 1}}, wantErr: ierr.IsValidation},
	}
	for _, tt := range refused {
		t.Run("refuses "+tt.name, func(t *testing.T) {
			d := newSettingsTestDeps(t)
			d.withEnvironment(t, settingsTenantID, revrecEnvironmentID)

			resp, err := newTestSettingsService(d.params).UpdateTenantSetting(context.Background(), settingsTenantID, revrecEnvironmentID, tt.key, tt.req)

			require.Error(t, err)
			assert.True(t, tt.wantErr(err))
			assert.Nil(t, resp)
		})
	}
}
