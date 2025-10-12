package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
)

type EntityIntegrationMappingService = interfaces.EntityIntegrationMappingService
type entityIntegrationMappingService struct {
	ServiceParams
}

func NewEntityIntegrationMappingService(params ServiceParams) EntityIntegrationMappingService {
	return &entityIntegrationMappingService{
		ServiceParams: params,
	}
}

func (s *entityIntegrationMappingService) CreateEntityIntegrationMapping(ctx context.Context, req dto.CreateEntityIntegrationMappingRequest) (*dto.EntityIntegrationMappingResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	mapping := req.ToEntityIntegrationMapping(ctx)

	// Validate the mapping
	if err := entityintegrationmapping.Validate(mapping); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid entity integration mapping data").
			Mark(ierr.ErrValidation)
	}

	if err := s.EntityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		// No need to wrap the error as the repository already returns properly formatted errors
		return nil, err
	}

	return &dto.EntityIntegrationMappingResponse{
		ID:               mapping.ID,
		EntityID:         mapping.EntityID,
		EntityType:       mapping.EntityType,
		ProviderType:     mapping.ProviderType,
		ProviderEntityID: mapping.ProviderEntityID,
		EnvironmentID:    mapping.EnvironmentID,
		TenantID:         mapping.TenantID,
		Status:           mapping.Status,
		CreatedAt:        mapping.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:        mapping.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		CreatedBy:        mapping.CreatedBy,
		UpdatedBy:        mapping.UpdatedBy,
	}, nil
}

func (s *entityIntegrationMappingService) GetEntityIntegrationMapping(ctx context.Context, id string) (*dto.EntityIntegrationMappingResponse, error) {
	if id == "" {
		return nil, ierr.NewError("entity integration mapping ID is required").
			WithHint("Entity integration mapping ID is required").
			Mark(ierr.ErrValidation)
	}

	mapping, err := s.EntityIntegrationMappingRepo.Get(ctx, id)
	if err != nil {
		// No need to wrap the error as the repository already returns properly formatted errors
		return nil, err
	}

	return &dto.EntityIntegrationMappingResponse{
		ID:               mapping.ID,
		EntityID:         mapping.EntityID,
		EntityType:       mapping.EntityType,
		ProviderType:     mapping.ProviderType,
		ProviderEntityID: mapping.ProviderEntityID,
		EnvironmentID:    mapping.EnvironmentID,
		TenantID:         mapping.TenantID,
		Status:           mapping.Status,
		CreatedAt:        mapping.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:        mapping.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		CreatedBy:        mapping.CreatedBy,
		UpdatedBy:        mapping.UpdatedBy,
	}, nil
}

func (s *entityIntegrationMappingService) GetEntityIntegrationMappings(ctx context.Context, filter *types.EntityIntegrationMappingFilter) (*dto.ListEntityIntegrationMappingsResponse, error) {
	if filter == nil {
		filter = &types.EntityIntegrationMappingFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	mappings, err := s.EntityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		// No need to wrap the error as the repository already returns properly formatted errors
		return nil, err
	}

	total, err := s.EntityIntegrationMappingRepo.Count(ctx, filter)
	if err != nil {
		// No need to wrap the error as the repository already returns properly formatted errors
		return nil, err
	}

	response := make([]*dto.EntityIntegrationMappingResponse, 0, len(mappings))
	for _, m := range mappings {
		response = append(response, &dto.EntityIntegrationMappingResponse{
			ID:               m.ID,
			EntityID:         m.EntityID,
			EntityType:       m.EntityType,
			ProviderType:     m.ProviderType,
			ProviderEntityID: m.ProviderEntityID,
			EnvironmentID:    m.EnvironmentID,
			TenantID:         m.TenantID,
			Status:           m.Status,
			CreatedAt:        m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt:        m.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			CreatedBy:        m.CreatedBy,
			UpdatedBy:        m.UpdatedBy,
		})
	}

	return &dto.ListEntityIntegrationMappingsResponse{
		Items:      response,
		Pagination: types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

func (s *entityIntegrationMappingService) UpdateEntityIntegrationMapping(ctx context.Context, id string, req dto.UpdateEntityIntegrationMappingRequest) (*dto.EntityIntegrationMappingResponse, error) {
	if id == "" {
		return nil, ierr.NewError("entity integration mapping ID is required").
			WithHint("Entity integration mapping ID is required").
			Mark(ierr.ErrValidation)
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get existing mapping
	mapping, err := s.EntityIntegrationMappingRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields if provided
	if req.ProviderEntityID != nil {
		mapping.ProviderEntityID = *req.ProviderEntityID
	}

	if req.Metadata != nil {
		mapping.Metadata = req.Metadata
	}

	// Update timestamps
	mapping.UpdatedAt = time.Now().UTC()
	mapping.UpdatedBy = types.GetUserID(ctx)

	// Validate the updated mapping
	if err := entityintegrationmapping.Validate(mapping); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid entity integration mapping data").
			Mark(ierr.ErrValidation)
	}

	if err := s.EntityIntegrationMappingRepo.Update(ctx, mapping); err != nil {
		return nil, err
	}

	return &dto.EntityIntegrationMappingResponse{
		ID:               mapping.ID,
		EntityID:         mapping.EntityID,
		EntityType:       mapping.EntityType,
		ProviderType:     mapping.ProviderType,
		ProviderEntityID: mapping.ProviderEntityID,
		EnvironmentID:    mapping.EnvironmentID,
		TenantID:         mapping.TenantID,
		Status:           mapping.Status,
		CreatedAt:        mapping.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:        mapping.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		CreatedBy:        mapping.CreatedBy,
		UpdatedBy:        mapping.UpdatedBy,
	}, nil
}

func (s *entityIntegrationMappingService) DeleteEntityIntegrationMapping(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("entity integration mapping ID is required").
			WithHint("Entity integration mapping ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get existing mapping
	mapping, err := s.EntityIntegrationMappingRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	if err := s.EntityIntegrationMappingRepo.Delete(ctx, mapping); err != nil {
		// No need to wrap the error as the repository already returns properly formatted errors
		return err
	}

	return nil
}
