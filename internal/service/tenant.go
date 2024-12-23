package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/tenant"
)

type TenantService interface {
	CreateTenant(ctx context.Context, req dto.CreateTenantRequest) (*dto.TenantResponse, error)
	GetTenantByID(ctx context.Context, id string) (*dto.TenantResponse, error)
}

type tenantService struct {
	repo tenant.Repository
}

func NewTenantService(repo tenant.Repository) TenantService {
	return &tenantService{repo: repo}
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
