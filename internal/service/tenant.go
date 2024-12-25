package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/tenant"
)

type TenantService interface {
	CreateTenant(ctx context.Context, req dto.CreateTenantRequest) (*dto.TenantResponse, error)
	GetTenantByID(ctx context.Context, id string) (*dto.TenantResponse, error)
	AssignTenantToUser(ctx context.Context, req dto.AssignTenantRequest) error
}

type tenantService struct {
	repo tenant.Repository
	cfg  *config.Configuration
}

func NewTenantService(repo tenant.Repository, cfg *config.Configuration) TenantService {
	return &tenantService{
		repo: repo,
		cfg:  cfg,
	}
}

func (s *tenantService) CreateTenant(ctx context.Context, req dto.CreateTenantRequest) (*dto.TenantResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	newTenant := req.ToTenant(ctx)

	if err := s.repo.Create(ctx, newTenant); err != nil {
		return nil, fmt.Errorf("failed to create tenant: %w", err)
	}

	return dto.NewTenantResponse(newTenant), nil
}

func (s *tenantService) GetTenantByID(ctx context.Context, id string) (*dto.TenantResponse, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve tenant: %w", err)
	}

	return dto.NewTenantResponse(t), nil
}

func (s *tenantService) AssignTenantToUser(ctx context.Context, req dto.AssignTenantRequest) error {
	if err := req.Validate(ctx); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}

	// Verify tenant exists
	_, err := s.GetTenantByID(ctx, req.TenantID)
	if err != nil {
		return fmt.Errorf("tenant not found: %w", err)
	}

	authProvider := auth.NewProvider(s.cfg)

	// Assign tenant to user using auth provider
	if err := authProvider.AssignUserToTenant(ctx, req.UserID, req.TenantID); err != nil {
		return fmt.Errorf("failed to assign tenant to user: %w", err)
	}

	return nil
}
