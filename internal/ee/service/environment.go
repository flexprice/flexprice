package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

type EnvironmentService interface {
	CreateEnvironment(ctx context.Context, req dto.CreateEnvironmentRequest) (*dto.EnvironmentResponse, error)
	GetEnvironment(ctx context.Context, id string) (*dto.EnvironmentResponse, error)
	GetEnvironments(ctx context.Context, filter types.Filter) (*dto.ListEnvironmentsResponse, error)
	UpdateEnvironment(ctx context.Context, id string, req dto.UpdateEnvironmentRequest) (*dto.EnvironmentResponse, error)
	// UploadBrandingAsset stores the environment's logo or icon.
	UploadBrandingAsset(ctx context.Context, id string, req dto.UploadBrandingAssetRequest) (*dto.EnvironmentResponse, error)
	// RemoveBrandingAsset clears the environment's logo or icon; the stored image stays.
	RemoveBrandingAsset(ctx context.Context, id string, asset types.BrandingAsset) (*dto.EnvironmentResponse, error)
	// CheckCloneAllowed rejects cloning from or into an environment with billing entities.
	CheckCloneAllowed(ctx context.Context, environmentIDs ...string) error
}

type environmentService struct {
	repo             environment.Repository
	envAccessService EnvAccessService
	settingsService  SettingsService
	ServiceParams
}

func NewEnvironmentService(repo environment.Repository, envAccessService EnvAccessService, settingsService SettingsService, params ServiceParams) EnvironmentService {
	return &environmentService{
		repo:             repo,
		envAccessService: envAccessService,
		settingsService:  settingsService,
		ServiceParams:    params,
	}
}

func (s *environmentService) CreateEnvironment(ctx context.Context, req dto.CreateEnvironmentRequest) (*dto.EnvironmentResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	env := req.ToEnvironment(ctx)
	envType := types.EnvironmentType(req.Type)

	// Check environment limits for prod and sandbox environments
	if envType == types.EnvironmentProduction || envType == types.EnvironmentDevelopment {
		// Get tenant config with defaults (tenant-level, no environment_id)
		tenantConfig, err := GetSetting[types.TenantConfig](
			s.settingsService.(*settingsService),
			ctx,
			types.SettingKeyTenantConfig,
		)
		if err != nil {
			return nil, err
		}

		// Get current count of environments of this type
		currentCount, err := s.repo.CountByType(ctx, envType)
		if err != nil {
			return nil, err
		}

		// Determine the limit based on environment type
		var limit int
		if envType == types.EnvironmentProduction {
			limit = tenantConfig.Production
		} else {
			limit = tenantConfig.Development
		}

		// Check if limit is reached
		if currentCount >= limit {
			envTypeName := string(envType)
			return nil, ierr.NewErrorf("environment limit reached: maximum %d %s environment(s) allowed", limit, envTypeName).
				WithHintf("You have reached the maximum limit of %d %s environment(s)", limit, envTypeName).
				Mark(ierr.ErrValidation)
		}
	}

	if err := s.repo.Create(ctx, env); err != nil {
		return nil, err
	}

	return s.environmentResponse(ctx, env)
}

func (s *environmentService) CheckCloneAllowed(ctx context.Context, environmentIDs ...string) error {
	for _, id := range environmentIDs {
		mode, err := NewBillingEntityService(s.ServiceParams).Status(types.SetEnvironmentID(ctx, id))
		if err != nil {
			return err
		}
		if mode != types.BillingEntityModeOff {
			return ierr.NewErrorf("environment %s has billing entities", id).
				WithHint("Environments with billing entities cannot be cloned from or into").
				WithReportableDetails(map[string]any{"environment_id": id}).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

func (s *environmentService) GetEnvironment(ctx context.Context, id string) (*dto.EnvironmentResponse, error) {
	// First get the environment
	env, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Check if user has access to this environment
	userID := types.GetUserID(ctx)
	tenantID := types.GetTenantID(ctx)

	if userID != "" && tenantID != "" {
		hasAccess := s.envAccessService.HasEnvironmentAccess(ctx, userID, tenantID, id)
		if !hasAccess {
			return nil, fmt.Errorf("access denied to environment %s", id)
		}
	}

	return s.environmentResponse(ctx, env)
}

func (s *environmentService) GetEnvironments(ctx context.Context, filter types.Filter) (*dto.ListEnvironmentsResponse, error) {

	// explicitly set status to published
	filter.Status = types.StatusPublished

	environments, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Filter environments based on user access
	userID := types.GetUserID(ctx)
	tenantID := types.GetTenantID(ctx)

	var allowedEnvironments []*environment.Environment

	if userID != "" && tenantID != "" {
		// Get user's allowed environments
		userAllowedEnvs := s.envAccessService.GetAllowedEnvironments(ctx, userID, tenantID)

		if len(userAllowedEnvs) == 0 {
			// Empty list means super admin - can see all environments
			allowedEnvironments = environments
		} else {
			// Filter environments based on user's allowed environments
			allowedEnvMap := make(map[string]bool)
			for _, envID := range userAllowedEnvs {
				allowedEnvMap[envID] = true
			}

			for _, env := range environments {
				if allowedEnvMap[env.ID] {
					allowedEnvironments = append(allowedEnvironments, env)
				}
			}
		}
	} else {
		// No user context, return all environments (for backwards compatibility)
		allowedEnvironments = environments
	}

	response := &dto.ListEnvironmentsResponse{
		Environments: make([]dto.EnvironmentResponse, len(allowedEnvironments)),
		Total:        len(allowedEnvironments),
		Offset:       filter.Offset,
		Limit:        filter.Limit,
	}

	for i, env := range allowedEnvironments {
		resp, err := s.environmentResponse(ctx, env)
		if err != nil {
			return nil, err
		}
		response.Environments[i] = *resp
	}

	return response, nil
}

func (s *environmentService) UpdateEnvironment(ctx context.Context, id string, req dto.UpdateEnvironmentRequest) (*dto.EnvironmentResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	env, err := s.writableEnvironment(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		env.Name = req.Name
	}
	if req.Type != "" && types.EnvironmentType(req.Type) != env.Type {
		return nil, ierr.NewError("environment type cannot be changed").
			WithHintf("type is immutable; current type is %q", env.Type).
			Mark(ierr.ErrValidation)
	}
	env.UpdatedAt = clock.Now()
	env.UpdatedBy = types.GetUserID(ctx)

	if err := s.repo.Update(ctx, env); err != nil {
		return nil, err
	}

	return s.environmentResponse(ctx, env)
}

// writableEnvironment loads the environment named in the path and checks the caller may write it.
// It authorises the path's environment, not the caller's selected one: the two differ whenever
// the request omits X-Environment-ID.
func (s *environmentService) writableEnvironment(ctx context.Context, id string) (*environment.Environment, error) {
	env, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	userID := types.GetUserID(ctx)
	tenantID := types.GetTenantID(ctx)
	if userID != "" && tenantID != "" {
		if !s.envAccessService.HasEnvironmentAccess(ctx, userID, tenantID, id) {
			return nil, ierr.NewErrorf("access denied to environment %s", id).
				WithHint("You don't have access to this environment").
				Mark(ierr.ErrPermissionDenied)
		}
	}
	return env, nil
}

func (s *environmentService) UploadBrandingAsset(ctx context.Context, id string, req dto.UploadBrandingAssetRequest) (*dto.EnvironmentResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	env, err := s.writableEnvironment(ctx, id)
	if err != nil {
		return nil, err
	}

	key, err := NewBrandingService(s.ServiceParams).StoreImage(ctx, env.ID, req.Asset, req.Data)
	if err != nil {
		return nil, err
	}
	return s.saveBrandingKey(ctx, env, req.Asset, &key)
}

func (s *environmentService) RemoveBrandingAsset(ctx context.Context, id string, asset types.BrandingAsset) (*dto.EnvironmentResponse, error) {
	if err := asset.Validate(); err != nil {
		return nil, err
	}
	env, err := s.writableEnvironment(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.saveBrandingKey(ctx, env, asset, nil)
}

// saveBrandingKey sets one image key in the environment's branding_config. The lock and the uncached
// read inside the transaction keep a concurrent write of the other asset from being lost.
func (s *environmentService) saveBrandingKey(ctx context.Context, env *environment.Environment, asset types.BrandingAsset, key *string) (*dto.EnvironmentResponse, error) {
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.DB.LockWithWait(txCtx, postgres.LockRequest{Key: "branding_config:" + env.TenantID + ":" + env.ID}); err != nil {
			return err
		}
		stored, err := s.SettingsRepo.GetByKeyUncached(txCtx, env.ID, types.SettingKeyBrandingConfig)
		if err != nil && !ierr.IsNotFound(err) {
			return err
		}
		cfg := types.BrandingConfig{}
		if stored != nil {
			if cfg, err = utils.ToStruct[types.BrandingConfig](stored.Value); err != nil {
				return err
			}
		}
		if asset == types.BrandingAssetLogo {
			cfg.LogoKey = key
		} else {
			cfg.IconKey = key
		}
		value, err := utils.ToMap(cfg)
		if err != nil {
			return err
		}
		if stored == nil {
			return s.SettingsRepo.Create(txCtx, &settings.Setting{
				ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
				EnvironmentID: env.ID,
				Key:           types.SettingKeyBrandingConfig,
				Value:         value,
				BaseModel:     types.GetDefaultBaseModel(txCtx),
			})
		}
		stored.Value = value
		return s.SettingsRepo.Update(txCtx, stored)
	})
	if err != nil {
		return nil, err
	}
	return s.environmentResponse(ctx, env)
}

func (s *environmentService) environmentResponse(ctx context.Context, env *environment.Environment) (*dto.EnvironmentResponse, error) {
	cfg, err := environmentBranding(ctx, s.SettingsRepo, env.ID)
	if err != nil {
		return nil, err
	}
	resp := dto.NewEnvironmentResponse(env)
	resp.LogoURL = brandingImageURL(s.Config, cfg.LogoKey)
	resp.IconURL = brandingImageURL(s.Config, cfg.IconKey)
	return resp, nil
}
