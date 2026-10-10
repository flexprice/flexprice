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

// SettingsService changes a tenant's settings for the admin portal. Only tenant_config is exposed.
type SettingsService interface {
	UpdateTenantConfig(ctx context.Context, req admindto.UpdateTenantConfigRequest) (*admindto.TenantConfigResponse, error)
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
