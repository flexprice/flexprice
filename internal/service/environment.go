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
	repo environment.Repository
}

func NewEnvironmentService(repo environment.Repository) EnvironmentService {
	return &environmentService{repo: repo}
}

func (s *environmentService) CreateEnvironment(ctx context.Context, req dto.CreateEnvironmentRequest) (*dto.EnvironmentResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	env := req.ToEnvironment(ctx)

	if err := s.repo.Create(ctx, env); err != nil {
		return nil, fmt.Errorf("failed to create environment: %w", err)
	}

	return dto.NewEnvironmentResponse(env), nil
}

func (s *environmentService) GetEnvironment(ctx context.Context, id string) (*dto.EnvironmentResponse, error) {
	env, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get environment: %w", err)
	}

	return dto.NewEnvironmentResponse(env), nil
}

func (s *environmentService) GetEnvironments(ctx context.Context, filter types.Filter) (*dto.ListEnvironmentsResponse, error) {
	environments, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get environments: %w", err)
	}

	response := &dto.ListEnvironmentsResponse{
		Environments: make([]dto.EnvironmentResponse, len(environments)),
		Total:        len(environments),
		Offset:       filter.Offset,
		Limit:        filter.Limit,
	}

	for i, env := range environments {
		response.Environments[i] = *dto.NewEnvironmentResponse(env)
	}

	return response, nil
}

func (s *environmentService) UpdateEnvironment(ctx context.Context, id string, req dto.UpdateEnvironmentRequest) (*dto.EnvironmentResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	env, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get environment: %w", err)
	}

	if req.Name != "" {
		env.Name = req.Name
	}
	if req.Type != "" {
		env.Type = environment.EnvironmentType(req.Type)
	}
	if req.Slug != "" {
		env.Slug = req.Slug
	}
	env.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, env); err != nil {
		return nil, fmt.Errorf("failed to update environment: %w", err)
	}

	return dto.NewEnvironmentResponse(env), nil
}
