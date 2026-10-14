package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type TenantService interface {
	CreateTenant(ctx context.Context, req dto.CreateTenantRequest) (*dto.TenantResponse, error)
	GetTenantByID(ctx context.Context, id string) (*dto.TenantResponse, error)
	GetTenantInternalStatus(ctx context.Context, id string) (types.TenantInternalStatus, error)
	AssignTenantToUser(ctx context.Context, req dto.AssignTenantRequest) error
	GetAllTenants(ctx context.Context) ([]*dto.TenantResponse, error)
	UpdateTenant(ctx context.Context, id string, req dto.UpdateTenantRequest) (*dto.TenantResponse, error)
}

type tenantService struct {
	ServiceParams
}

func NewTenantService(
	params ServiceParams,
) TenantService {
	return &tenantService{
		ServiceParams: params,
	}
}

func (s *tenantService) CreateTenant(ctx context.Context, req dto.CreateTenantRequest) (*dto.TenantResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	newTenant := req.ToTenant(ctx)

	if err := s.TenantRepo.Create(ctx, newTenant); err != nil {
		return nil, err
	}

	if s.SelfBilling != nil {
		s.SelfBilling.OnTenantCreated(ctx, newTenant.ID, newTenant.Name, newTenant.BillingDetails.Email)
	}

	return dto.NewTenantResponse(newTenant), nil
}

func (s *tenantService) GetTenantByID(ctx context.Context, id string) (*dto.TenantResponse, error) {
	t, err := s.TenantRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return dto.NewTenantResponse(t), nil
}

func (s *tenantService) GetTenantInternalStatus(ctx context.Context, id string) (types.TenantInternalStatus, error) {
	t, err := s.TenantRepo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}

	return t.InternalStatus, nil
}

func (s *tenantService) AssignTenantToUser(ctx context.Context, req dto.AssignTenantRequest) error {
	if err := req.Validate(ctx); err != nil {
		return err
	}

	// Verify tenant exists
	_, err := s.GetTenantByID(ctx, req.TenantID)
	if err != nil {
		return err
	}

	authProvider := auth.NewProvider(s.Config)

	// Assign tenant to user using auth provider
	if err := authProvider.AssignUserToTenant(ctx, req.UserID, req.TenantID); err != nil {
		return err
	}

	return nil
}

func (s *tenantService) GetAllTenants(ctx context.Context) ([]*dto.TenantResponse, error) {
	tenants, err := s.TenantRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	tenantResponses := make([]*dto.TenantResponse, 0, len(tenants))
	for _, t := range tenants {
		tenantResponses = append(tenantResponses, dto.NewTenantResponse(t))
	}

	return tenantResponses, nil
}

func (s *tenantService) UpdateTenant(ctx context.Context, id string, req dto.UpdateTenantRequest) (*dto.TenantResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the existing tenant
	existingTenant, err := s.TenantRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	existingTenant.BillingDetails = req.BillingDetails.MergeInto(existingTenant.BillingDetails)

	// Update the name only if it is explicitly provided (not nil), not empty, and different from existing
	if req.Name != nil && *req.Name != "" && *req.Name != existingTenant.Name {
		existingTenant.Name = *req.Name
	}

	if req.Metadata != nil {
		existingTenant.Metadata = lo.FromPtr(req.Metadata)
	}

	// Update the timestamp
	existingTenant.UpdatedAt = time.Now()

	// Save the updated tenant
	if err := s.TenantRepo.Update(ctx, existingTenant); err != nil {
		return nil, err
	}

	return dto.NewTenantResponse(existingTenant), nil
}
