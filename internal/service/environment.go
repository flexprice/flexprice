package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/environment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type EnvironmentService interface {
	CreateEnvironment(ctx context.Context, req dto.CreateEnvironmentRequest) (*dto.EnvironmentResponse, error)
	GetEnvironment(ctx context.Context, id string) (*dto.EnvironmentResponse, error)
	GetEnvironments(ctx context.Context, filter types.Filter) (*dto.ListEnvironmentsResponse, error)
	UpdateEnvironment(ctx context.Context, id string, req dto.UpdateEnvironmentRequest) (*dto.EnvironmentResponse, error)
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
		// Get env config with defaults (tenant-level, no environment_id)

		config, err := s.settingsService.GetSettingWithDefaults(ctx, types.SettingKeyEnvConfig)
		if err != nil {
			return nil, err
		}

		// Get current count of environments of this type
		currentCount, err := s.repo.CountByType(ctx, envType)
		if err != nil {
			return nil, err
		}

		// Determine the limit based on environment type
		envTypeKey := string(envType)
		limit, exists := config.Value[envTypeKey]
		if !exists {
			return nil, ierr.NewErrorf("environment limit not configured for type: %s", envTypeKey).
				WithHintf("Environment limit configuration missing for type: %s", envTypeKey).
				Mark(ierr.ErrValidation)
		}

		// Check if limit is reached
		if currentCount >= limit.(int) {
			envTypeName := string(envType)
			return nil, ierr.NewErrorf("environment limit reached: maximum %d %s environment(s) allowed", limit, envTypeName).
				WithHintf("You have reached the maximum limit of %d %s environment(s)", limit, envTypeName).
				Mark(ierr.ErrValidation)
		}
	}

	if err := s.repo.Create(ctx, env); err != nil {
		return nil, err
	}

	return dto.NewEnvironmentResponse(env), nil
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

	return dto.NewEnvironmentResponse(env), nil
}

func (s *environmentService) GetEnvironments(ctx context.Context, filter types.Filter) (*dto.ListEnvironmentsResponse, error) {
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
		response.Environments[i] = *dto.NewEnvironmentResponse(env)
	}

	return response, nil
}

func (s *environmentService) UpdateEnvironment(ctx context.Context, id string, req dto.UpdateEnvironmentRequest) (*dto.EnvironmentResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	env, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		env.Name = req.Name
	}
	if req.Type != "" {
		env.Type = types.EnvironmentType(req.Type)
	}
	env.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, env); err != nil {
		return nil, err
	}

	return dto.NewEnvironmentResponse(env), nil
}
