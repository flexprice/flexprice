package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type TaxConfigService interface {
	Create(ctx context.Context, taxconfig *dto.TaxConfigCreateRequest) (*dto.TaxConfigResponse, error)
	Get(ctx context.Context, id string) (*dto.TaxConfigResponse, error)
	Update(ctx context.Context, id string, taxconfig *dto.TaxConfigUpdateRequest) (*dto.TaxConfigResponse, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.TaxConfigFilter) (*dto.ListTaxConfigsResponse, error)
}

type taxConfigService struct {
	ServiceParams
}

func NewTaxConfigService(p ServiceParams) TaxConfigService {
	return &taxConfigService{
		ServiceParams: p,
	}
}

func (s *taxConfigService) Create(ctx context.Context, req *dto.TaxConfigCreateRequest) (*dto.TaxConfigResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	taxRate, err := s.TaxRateRepo.Get(ctx, req.TaxRateID)
	if err != nil {
		return nil, err
	}

	// Convert request to domain model
	tc := req.ToTaxConfig(ctx, lo.FromPtr(taxRate))

	s.Logger.Infow("creating tax config",
		"tax_rate_id", tc.TaxRateID,
		"entity_type", tc.EntityType,
		"entity_id", tc.EntityID,
		"priority", tc.Priority,
		"auto_apply", tc.AutoApply)

	// Create tax config
	err = s.TaxConfigRepo.Create(ctx, tc)
	if err != nil {
		s.Logger.Errorw("failed to create tax config",
			"error", err,
			"tax_rate_id", tc.TaxRateID,
			"entity_type", tc.EntityType,
			"entity_id", tc.EntityID)
		return nil, err
	}

	s.Logger.Infow("tax config created successfully",
		"tax_config_id", tc.ID,
		"tax_rate_id", tc.TaxRateID,
		"entity_type", tc.EntityType,
		"entity_id", tc.EntityID)

	return dto.ToTaxConfigResponse(tc), nil
}

func (s *taxConfigService) Get(ctx context.Context, id string) (*dto.TaxConfigResponse, error) {
	if id == "" {
		return nil, ierr.NewError("tax config ID is required").
			WithHint("Tax config ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	s.Logger.Debugw("getting tax config", "tax_config_id", id)

	tc, err := s.TaxConfigRepo.Get(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to get tax config",
			"error", err,
			"tax_config_id", id)
		return nil, err
	}

	if tc == nil {
		return nil, ierr.NewError("tax config not found").
			WithHint(fmt.Sprintf("Tax config with ID %s does not exist", id)).
			WithReportableDetails(map[string]interface{}{
				"tax_config_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	return dto.ToTaxConfigResponse(tc), nil
}

func (s *taxConfigService) Update(ctx context.Context, id string, req *dto.TaxConfigUpdateRequest) (*dto.TaxConfigResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	if id == "" {
		return nil, ierr.NewError("tax config ID is required").
			WithHint("Tax config ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Get existing tax config to ensure it exists
	existing, err := s.TaxConfigRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		return nil, ierr.NewError("tax config not found").
			WithHint(fmt.Sprintf("Tax config with ID %s does not exist", id)).
			WithReportableDetails(map[string]interface{}{
				"tax_config_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Update fields if provided
	if req.Priority >= 0 {
		existing.Priority = req.Priority
	}
	existing.AutoApply = req.AutoApply

	if req.Metadata != nil {
		existing.Metadata = req.Metadata
	}

	s.Logger.Infow("updating tax config",
		"tax_config_id", id,
		"tax_rate_id", existing.TaxRateID,
		"entity_type", existing.EntityType,
		"entity_id", existing.EntityID)

	// Update tax config
	err = s.TaxConfigRepo.Update(ctx, existing)
	if err != nil {
		s.Logger.Errorw("failed to update tax config",
			"error", err,
			"tax_config_id", id)
		return nil, err
	}

	s.Logger.Infow("tax config updated successfully",
		"tax_config_id", id,
		"tax_rate_id", existing.TaxRateID,
		"entity_type", existing.EntityType,
		"entity_id", existing.EntityID)

	return dto.ToTaxConfigResponse(existing), nil
}

func (s *taxConfigService) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("tax config ID is required").
			WithHint("Tax config ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Get existing tax config to ensure it exists
	existing, err := s.TaxConfigRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Delete tax config
	err = s.TaxConfigRepo.Delete(ctx, existing)
	if err != nil {
		s.Logger.Errorw("failed to delete tax config",
			"error", err,
			"tax_config_id", id)
		return err
	}

	return nil
}

func (s *taxConfigService) List(ctx context.Context, filter *types.TaxConfigFilter) (*dto.ListTaxConfigsResponse, error) {
	if filter == nil {
		filter = types.NewTaxConfigFilter()
	}

	// Validate filter
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	s.Logger.Debugw("listing tax configs",
		"limit", filter.GetLimit(),
		"offset", filter.GetOffset(),
		"entity_type", filter.EntityType,
		"entity_id", filter.EntityID,
		"tax_rate_ids_count", len(filter.TaxRateIDs))

	// List tax configs
	taxConfigs, err := s.TaxConfigRepo.List(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to list tax configs",
			"error", err,
			"limit", filter.GetLimit(),
			"offset", filter.GetOffset())
		return nil, err
	}

	// Get total count for pagination
	total, err := s.TaxConfigRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	response := &dto.ListTaxConfigsResponse{
		Items: make([]*dto.TaxConfigResponse, len(taxConfigs)),
	}

	for i, tc := range taxConfigs {
		response.Items[i] = dto.ToTaxConfigResponse(tc)
	}

	response.Pagination = types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset())

	s.Logger.Debugw("successfully listed tax configs",
		"count", len(taxConfigs),
		"total", total,
		"limit", filter.GetLimit(),
		"offset", filter.GetOffset())

	return response, nil
}
