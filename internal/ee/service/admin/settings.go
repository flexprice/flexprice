package admin

import (
	"context"
	"slices"

	"github.com/flexprice/flexprice/internal/api/dto"
	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
)

// SettingsService reads and changes a tenant's settings for the admin portal. Only tenant_config can be changed.
type SettingsService interface {
	UpdateTenantConfig(ctx context.Context, req admindto.UpdateTenantConfigRequest) (*admindto.TenantConfigResponse, error)
	// ListTenantSettings returns tenant_config, saml_config when this deployment offers SAML,
	// and every setting of the environment when environmentID is set.
	ListTenantSettings(ctx context.Context, tenantID, environmentID string) (*admindto.ListSettingsResponse, error)
	// GetTenantSetting returns one setting; environmentID is required for keys set per environment.
	GetTenantSetting(ctx context.Context, tenantID, environmentID string, key types.SettingKey) (*admindto.SettingResponse, error)
}

type settingsService struct {
	service.ServiceParams
	settings service.SettingsService
}

func NewSettingsService(params service.ServiceParams, settings service.SettingsService) SettingsService {
	return &settingsService{ServiceParams: params, settings: settings}
}

func (s *settingsService) UpdateTenantConfig(ctx context.Context, req admindto.UpdateTenantConfigRequest) (*admindto.TenantConfigResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.TenantRepo.GetByID(ctx, req.TenantID); err != nil {
		return nil, err
	}

	ctx = types.SetTenantID(ctx, req.TenantID)

	// Same write as PUT /v1/settings/tenant_config: merged with what is stored, so limits not sent stay as they are.
	setting, err := s.settings.UpdateSettingByKey(ctx, types.SettingKeyTenantConfig, &dto.UpdateSettingRequest{
		Value: req.ToSettingValue(),
	})
	if err != nil {
		return nil, err
	}

	cfg, err := utils.ToStruct[types.TenantConfig](setting.Value)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to read the tenant's settings").
			Mark(ierr.ErrInternal)
	}

	s.Logger.Info(ctx, "updated tenant config", "tenant_id", req.TenantID,
		"production", cfg.Production, "development", cfg.Development, "max_users", cfg.MaxUsers)
	return admindto.NewTenantConfigResponse(req.TenantID, cfg), nil
}

func (s *settingsService) ListTenantSettings(ctx context.Context, tenantID, environmentID string) (*admindto.ListSettingsResponse, error) {
	keys := []types.SettingKey{types.SettingKeyTenantConfig}
	if s.Config != nil && s.Config.Auth.SAML.Enabled {
		keys = append(keys, types.SettingKeySAMLConfig)
	}

	if environmentID == "" {
		if _, err := s.TenantRepo.GetByID(ctx, tenantID); err != nil {
			return nil, err
		}
		ctx = types.SetTenantID(ctx, tenantID)
	} else {
		var err error
		if ctx, err = environmentContext(ctx, s.ServiceParams, tenantID, environmentID); err != nil {
			return nil, err
		}
		defaults, err := types.GetDefaultSettings()
		if err != nil {
			return nil, err
		}
		// Tenant-wide keys are already listed.
		envKeys := lo.Without(lo.Keys(defaults), types.SettingKeyTenantConfig, types.SettingKeySAMLConfig)
		slices.Sort(envKeys)
		keys = append(keys, envKeys...)
	}

	found := make([]*dto.SettingResponse, 0, len(keys))
	for _, key := range keys {
		// Unchecked: its checks are for logged-in tenant users; operators are authorized by the admin secret.
		setting, err := s.settings.GetSettingByKeyUnchecked(ctx, key)
		if err != nil {
			return nil, err
		}
		found = append(found, setting)
	}
	return admindto.NewListSettingsResponse(found), nil
}

func (s *settingsService) GetTenantSetting(ctx context.Context, tenantID, environmentID string, key types.SettingKey) (*admindto.SettingResponse, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	if key == types.SettingKeySAMLConfig && (s.Config == nil || !s.Config.Auth.SAML.Enabled) {
		return nil, ierr.NewError("saml is not enabled for this deployment").
			WithHint("SAML single sign-on is not available on this deployment").
			Mark(ierr.ErrNotFound)
	}

	ctx, err := settingContext(ctx, s.ServiceParams, tenantID, environmentID, key)
	if err != nil {
		return nil, err
	}
	setting, err := s.settings.GetSettingByKeyUnchecked(ctx, key)
	if err != nil {
		return nil, err
	}
	return admindto.NewSettingResponse(setting), nil
}
