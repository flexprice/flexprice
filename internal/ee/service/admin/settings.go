package admin

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

// SettingsService reads and changes a tenant's settings for the admin portal. Only tenant_config can be changed.
type SettingsService interface {
	UpdateTenantConfig(ctx context.Context, req admindto.UpdateTenantConfigRequest) (*admindto.TenantConfigResponse, error)
	// ListTenantSettings returns tenant_config, and saml_config when this deployment offers SAML.
	ListTenantSettings(ctx context.Context, tenantID string) (*admindto.ListSettingsResponse, error)
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

func (s *settingsService) ListTenantSettings(ctx context.Context, tenantID string) (*admindto.ListSettingsResponse, error) {
	if _, err := s.TenantRepo.GetByID(ctx, tenantID); err != nil {
		return nil, err
	}
	ctx = types.SetTenantID(ctx, tenantID)

	keys := []types.SettingKey{types.SettingKeyTenantConfig}
	if s.Config != nil && s.Config.Auth.SAML.Enabled {
		keys = append(keys, types.SettingKeySAMLConfig)
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
