package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/taxconfig"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type TaxConfigService interface {
	Create(ctx context.Context, taxconfig *taxconfig.TaxConfig) (*taxconfig.TaxConfig, error)
	Get(ctx context.Context, id string) (*taxconfig.TaxConfig, error)
	Update(ctx context.Context, taxconfig *taxconfig.TaxConfig) (*taxconfig.TaxConfig, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.TaxConfigFilter) ([]*taxconfig.TaxConfig, error)
}

type taxConfigService struct {
	ServiceParams
}

func NewTaxConfigService(p ServiceParams) TaxConfigService {
	return &taxConfigService{
		ServiceParams: p,
	}
}

func (s *taxConfigService) Create(ctx context.Context, tc *taxconfig.TaxConfig) (*taxconfig.TaxConfig, error) {
	if tc == nil {
		return nil, ierr.NewError("tax config is required").
			WithHint("Tax config cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Validate required fields
	if tc.TaxRateID == "" {
		return nil, ierr.NewError("tax rate ID is required").
			WithHint("Tax rate ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	if tc.EntityType == "" {
		return nil, ierr.NewError("entity type is required").
			WithHint("Entity type cannot be empty").
			Mark(ierr.ErrValidation)
	}

	if tc.EntityID == "" {
		return nil, ierr.NewError("entity ID is required").
			WithHint("Entity ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not provided
	if tc.EnvironmentID == "" {
		tc.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	// Set base model fields
	tc.BaseModel = types.GetDefaultBaseModel(ctx)

	s.Logger.Infow("creating tax config",
		"tax_rate_id", tc.TaxRateID,
		"entity_type", tc.EntityType,
		"entity_id", tc.EntityID,
		"priority", tc.Priority,
		"auto_apply", tc.AutoApply)

	// Create tax config
	err := s.TaxConfigRepo.Create(ctx, tc)
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

	return tc, nil
}

func (s *taxConfigService) Get(ctx context.Context, id string) (*taxconfig.TaxConfig, error) {
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

	return tc, nil
}

func (s *taxConfigService) Update(ctx context.Context, tc *taxconfig.TaxConfig) (*taxconfig.TaxConfig, error) {
	if tc == nil {
		return nil, ierr.NewError("tax config is required").
			WithHint("Tax config cannot be nil").
			Mark(ierr.ErrValidation)
	}

	if tc.ID == "" {
		return nil, ierr.NewError("tax config ID is required").
			WithHint("Tax config ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Get existing tax config to ensure it exists
	existing, err := s.TaxConfigRepo.Get(ctx, tc.ID)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		return nil, ierr.NewError("tax config not found").
			WithHint(fmt.Sprintf("Tax config with ID %s does not exist", tc.ID)).
			WithReportableDetails(map[string]interface{}{
				"tax_config_id": tc.ID,
			}).
			Mark(ierr.ErrNotFound)
	}

	s.Logger.Infow("updating tax config",
		"tax_config_id", tc.ID,
		"tax_rate_id", tc.TaxRateID,
		"entity_type", tc.EntityType,
		"entity_id", tc.EntityID)

	// Update base model fields
	tc.UpdatedAt = time.Now().UTC()
	tc.UpdatedBy = types.GetUserID(ctx)

	// Update tax config
	err = s.TaxConfigRepo.Update(ctx, tc)
	if err != nil {
		s.Logger.Errorw("failed to update tax config",
			"error", err,
			"tax_config_id", tc.ID)
		return nil, err
	}

	s.Logger.Infow("tax config updated successfully",
		"tax_config_id", tc.ID,
		"tax_rate_id", tc.TaxRateID,
		"entity_type", tc.EntityType,
		"entity_id", tc.EntityID)

	return tc, nil
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

	if existing == nil {
		return ierr.NewError("tax config not found").
			WithHint(fmt.Sprintf("Tax config with ID %s does not exist", id)).
			WithReportableDetails(map[string]interface{}{
				"tax_config_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	s.Logger.Infow("deleting tax config",
		"tax_config_id", id,
		"tax_rate_id", existing.TaxRateID,
		"entity_type", existing.EntityType,
		"entity_id", existing.EntityID)

	// Delete tax config
	err = s.TaxConfigRepo.Delete(ctx, existing)
	if err != nil {
		s.Logger.Errorw("failed to delete tax config",
			"error", err,
			"tax_config_id", id)
		return err
	}

	s.Logger.Infow("tax config deleted successfully",
		"tax_config_id", id,
		"tax_rate_id", existing.TaxRateID,
		"entity_type", existing.EntityType,
		"entity_id", existing.EntityID)

	return nil
}

func (s *taxConfigService) List(ctx context.Context, filter *types.TaxConfigFilter) ([]*taxconfig.TaxConfig, error) {
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

	s.Logger.Debugw("successfully listed tax configs",
		"count", len(taxConfigs),
		"limit", filter.GetLimit(),
		"offset", filter.GetOffset())

	return taxConfigs, nil
}
