package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/environment"
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
}

func NewEnvironmentService(repo environment.Repository, envAccessService EnvAccessService) EnvironmentService {
	return &environmentService{
		repo:             repo,
		envAccessService: envAccessService,
	}
}

func (s *environmentService) CreateEnvironment(ctx context.Context, req dto.CreateEnvironmentRequest) (*dto.EnvironmentResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	env := req.ToEnvironment(ctx)

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
