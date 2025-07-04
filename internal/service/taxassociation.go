package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type TaxConfigService interface {
	Create(ctx context.Context, taxconfig *dto.CreateTaxAssociationRequest) (*dto.TaxConfigResponse, error)
	Get(ctx context.Context, id string) (*dto.TaxConfigResponse, error)
	Update(ctx context.Context, id string, taxconfig *dto.TaxConfigUpdateRequest) (*dto.TaxConfigResponse, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.TaxConfigFilter) (*dto.ListTaxConfigsResponse, error)

	// LinkTaxRatesToEntity links tax rates to any entity type
	LinkTaxRatesToEntity(ctx context.Context, entityType types.TaxrateEntityType, entityID string, taxRateLinks []*dto.CreateEntityTaxAssociation) (*dto.TaxLinkingResponse, error)
}

type taxConfigService struct {
	ServiceParams
}

func NewTaxConfigService(p ServiceParams) TaxConfigService {
	return &taxConfigService{
		ServiceParams: p,
	}
}

func (s *taxConfigService) Create(ctx context.Context, req *dto.CreateTaxAssociationRequest) (*dto.TaxConfigResponse, error) {
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

// LinkTaxRatesToEntity links tax rates to any entity in a single transaction
func (s *taxConfigService) LinkTaxRatesToEntity(ctx context.Context, entityType types.TaxrateEntityType, entityID string, taxRateLinks []*dto.CreateEntityTaxAssociation) (*dto.TaxLinkingResponse, error) {
	// Early return for empty input
	if len(taxRateLinks) == 0 {
		return &dto.TaxLinkingResponse{
			EntityID:       entityID,
			EntityType:     entityType,
			LinkedTaxRates: []*dto.LinkedTaxRateInfo{},
		}, nil
	}

	// Pre-validate all tax rate links before starting transaction
	for _, taxRateLink := range taxRateLinks {
		if err := taxRateLink.Validate(); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Invalid tax rate configuration").
				Mark(ierr.ErrValidation)
		}
	}

	var linkedTaxRates []*dto.LinkedTaxRateInfo
	taxService := NewTaxService(s.ServiceParams)

	// Execute all operations within a single transaction
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		linkedTaxRates = make([]*dto.LinkedTaxRateInfo, 0, len(taxRateLinks))

		for _, taxRateLink := range taxRateLinks {
			var taxRateToUse *taxrate.TaxRate
			var wasCreated bool

			// Step 1: Resolve or create tax rate
			if taxRateLink.TaxRateID != nil {
				// Use existing tax rate
				taxRateID := *taxRateLink.TaxRateID
				existingTaxRate, err := s.TaxRateRepo.Get(txCtx, taxRateID)
				if err != nil {
					return ierr.WithError(err).
						WithHint("Tax rate not found").
						WithReportableDetails(map[string]interface{}{
							"tax_rate_id": taxRateID,
						}).
						Mark(ierr.ErrNotFound)
				}
				taxRateToUse = existingTaxRate
				wasCreated = false
			} else {
				taxRateResponse, err := taxService.CreateTaxRate(txCtx, taxRateLink.CreateTaxRateRequest)
				if err != nil {
					return ierr.WithError(err).
						WithHint("Failed to create tax rate").
						WithReportableDetails(map[string]interface{}{
							"tax_rate_id": taxRateLink.CreateTaxRateRequest.Code,
							"entity_type": entityType,
							"entity_id":   entityID,
						}).
						Mark(ierr.ErrDatabase)
				}
				taxRateToUse = taxRateResponse.TaxRate
				wasCreated = true
			}

			// Step 2: Create tax config
			taxConfigReq := &dto.CreateTaxAssociationRequest{
				TaxRateID:  taxRateToUse.ID,
				EntityType: string(entityType),
				EntityID:   entityID,
				Priority:   taxRateLink.Priority,
				AutoApply:  taxRateLink.AutoApply,
			}

			// Step 3: using internal service to create tax config
			taxConfig, err := s.Create(txCtx, taxConfigReq)
			if err != nil {
				return err
			}

			// Step 4: Add to results
			linkedTaxRates = append(linkedTaxRates, &dto.LinkedTaxRateInfo{
				TaxRateID:   taxRateToUse.ID,
				TaxConfigID: taxConfig.ID,
				WasCreated:  wasCreated,
				Priority:    taxConfig.Priority,
				AutoApply:   taxConfig.AutoApply,
			})
		}

		return nil
	})

	if err != nil {
		s.Logger.Errorw("failed to link tax rates to entity",
			"error", err,
			"entity_type", entityType,
			"entity_id", entityID,
			"links_count", len(taxRateLinks))
		return nil, err
	}

	s.Logger.Infow("successfully linked tax rates to entity",
		"entity_type", entityType,
		"entity_id", entityID,
		"links_count", len(linkedTaxRates))

	return &dto.TaxLinkingResponse{
		EntityID:       entityID,
		EntityType:     entityType,
		LinkedTaxRates: linkedTaxRates,
	}, nil
}
