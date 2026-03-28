package service

import (
	"context"
	"fmt"
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

func (s *entityIntegrationMappingService) LinkIntegrationMapping(ctx context.Context, req dto.LinkIntegrationMappingRequest) (*dto.LinkIntegrationMappingResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	mapping, err := s.upsertEntityMapping(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := s.applyEntitySideEffects(ctx, req); err != nil {
		return nil, err
	}

	return &dto.LinkIntegrationMappingResponse{
		Mapping: &dto.EntityIntegrationMappingResponse{
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
		},
	}, nil
}

func (s *entityIntegrationMappingService) upsertEntityMapping(ctx context.Context, req dto.LinkIntegrationMappingRequest) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	filter := &types.EntityIntegrationMappingFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityID:    req.EntityID,
		EntityType:  req.EntityType,
		ProviderTypes: []string{
			req.ProviderType,
		},
	}
	existing, err := s.EntityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	metadata := req.Metadata
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	metadata["linked_via"] = "integrations_mapping_link_api"
	metadata["linked_at"] = time.Now().UTC().Format(time.RFC3339)

	if len(existing) > 0 {
		mapping := existing[0]
		mapping.ProviderEntityID = req.ProviderEntityID
		mapping.Metadata = metadata
		mapping.UpdatedAt = time.Now().UTC()
		mapping.UpdatedBy = types.GetUserID(ctx)
		if err := s.EntityIntegrationMappingRepo.Update(ctx, mapping); err != nil {
			return nil, err
		}
		return mapping, nil
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         req.EntityID,
		EntityType:       req.EntityType,
		ProviderType:     req.ProviderType,
		ProviderEntityID: req.ProviderEntityID,
		Metadata:         metadata,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	if err := entityintegrationmapping.Validate(mapping); err != nil {
		return nil, err
	}
	if err := s.EntityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		return nil, err
	}
	return mapping, nil
}

func (s *entityIntegrationMappingService) applyEntitySideEffects(ctx context.Context, req dto.LinkIntegrationMappingRequest) error {
	switch req.EntityType {
	case types.IntegrationEntityTypeCustomer:
		return s.applyCustomerLinkSideEffects(ctx, req)
	default:
		return ierr.NewError("unsupported entity type for link side effects").
			WithHint(fmt.Sprintf("Entity type %s is not supported yet", req.EntityType)).
			Mark(ierr.ErrValidation)
	}
}

func (s *entityIntegrationMappingService) applyCustomerLinkSideEffects(ctx context.Context, req dto.LinkIntegrationMappingRequest) error {
	switch types.SecretProvider(req.ProviderType) {
	case types.SecretProviderRazorpay:
		return s.applyRazorpayCustomerLinkSideEffects(ctx, req)
	default:
		return ierr.NewError("unsupported provider for customer link").
			WithHint(fmt.Sprintf("Provider %s is not supported yet for customer links", req.ProviderType)).
			Mark(ierr.ErrValidation)
	}
}

func (s *entityIntegrationMappingService) applyRazorpayCustomerLinkSideEffects(ctx context.Context, req dto.LinkIntegrationMappingRequest) error {
	cust, err := s.CustomerRepo.Get(ctx, req.EntityID)
	if err != nil {
		return err
	}
	if cust.Metadata == nil {
		cust.Metadata = map[string]string{}
	}
	cust.Metadata["razorpay_customer_id"] = req.ProviderEntityID
	if err := s.CustomerRepo.Update(ctx, cust); err != nil {
		return err
	}

	razorpayIntegration, err := s.IntegrationFactory.GetRazorpayIntegration(ctx)
	if err != nil {
		s.Logger.WarnwCtx(ctx, "razorpay integration unavailable for customer notes update", "error", err, "customer_id", req.EntityID, "provider_entity_id", req.ProviderEntityID)
		return nil
	}
	if err := razorpayIntegration.CustomerSvc.UpdateRazorpayCustomerNotes(ctx, req.ProviderEntityID, map[string]interface{}{
		"flexprice_customer_id": req.EntityID,
		"environment_id":        types.GetEnvironmentID(ctx),
	}); err != nil {
		s.Logger.WarnwCtx(ctx, "failed to update razorpay customer notes", "error", err, "customer_id", req.EntityID, "provider_entity_id", req.ProviderEntityID)
		return nil
	}
	return nil
}
