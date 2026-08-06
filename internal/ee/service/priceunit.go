package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
)

type PriceUnitService = interfaces.PriceUnitService

type priceUnitService struct {
	ServiceParams
}

func NewPriceUnitService(params ServiceParams) PriceUnitService {
	return &priceUnitService{
		ServiceParams: params,
	}
}

func (s *priceUnitService) CreatePriceUnit(ctx context.Context, req dto.CreatePriceUnitRequest) (*dto.CreatePriceUnitResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid price unit data provided").
			Mark(ierr.ErrValidation)
	}

	// Convert request to domain model
	priceUnit, err := req.ToPriceUnit(ctx)
	if err != nil {
		return nil, err
	}

	// Create price unit
	createdPriceUnit, err := s.PriceUnitRepo.Create(ctx, priceUnit)
	if err != nil {
		return nil, err
	}

	return &dto.CreatePriceUnitResponse{
		PriceUnit: createdPriceUnit,
	}, nil
}

func (s *priceUnitService) GetPriceUnit(ctx context.Context, id string) (*dto.PriceUnitResponse, error) {
	if id == "" {
		return nil, ierr.NewError("price unit id is required").
			WithHint("Please provide a valid price unit ID").
			Mark(ierr.ErrValidation)
	}

	priceUnit, err := s.PriceUnitRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return &dto.PriceUnitResponse{
		PriceUnit: priceUnit,
	}, nil
}

func (s *priceUnitService) GetPriceUnitByCode(ctx context.Context, code string) (*dto.PriceUnitResponse, error) {
	if code == "" {
		return nil, ierr.NewError("price unit code is required").
			WithHint("Please provide a valid price unit code").
			Mark(ierr.ErrValidation)
	}

	priceUnit, err := s.PriceUnitRepo.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}

	return &dto.PriceUnitResponse{
		PriceUnit: priceUnit,
	}, nil
}

func (s *priceUnitService) ListPriceUnits(ctx context.Context, filter *types.PriceUnitFilter) (*dto.ListPriceUnitsResponse, error) {
	// Validate filter
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	if filter.GetLimit() == 0 {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	// Get price units from repository
	priceUnits, err := s.PriceUnitRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Convert to response DTOs
	responses := make([]*dto.PriceUnitResponse, len(priceUnits))
	for i, priceUnit := range priceUnits {
		responses[i] = &dto.PriceUnitResponse{
			PriceUnit: priceUnit,
		}
	}

	// Create list response
	listResponse := &dto.ListPriceUnitsResponse{
		Items: responses,
		Pagination: types.PaginationResponse{
			Limit:  filter.GetLimit(),
			Offset: filter.GetOffset(),
			Total:  len(responses),
		},
	}

	return listResponse, nil
}

func (s *priceUnitService) UpdatePriceUnit(ctx context.Context, id string, req dto.UpdatePriceUnitRequest) (*dto.PriceUnitResponse, error) {
	if id == "" {
		return nil, ierr.NewError("price unit id is required").
			WithHint("Please provide a valid price unit ID").
			Mark(ierr.ErrValidation)
	}

	// Validate request
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid price unit update data provided").
			Mark(ierr.ErrValidation)
	}

	// Get existing price unit
	existingPriceUnit, err := s.PriceUnitRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields
	if req.Name != nil {
		existingPriceUnit.Name = *req.Name
	}
	if req.Metadata != nil {
		existingPriceUnit.Metadata = req.Metadata
	}

	// Update price unit
	err = s.PriceUnitRepo.Update(ctx, existingPriceUnit)
	if err != nil {
		return nil, err
	}

	return &dto.PriceUnitResponse{
		PriceUnit: existingPriceUnit,
	}, nil
}

func (s *priceUnitService) DeletePriceUnit(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("price unit id is required").
			WithHint("Please provide a valid price unit ID").
			Mark(ierr.ErrValidation)
	}

	// Get existing price unit
	priceUnit, err := s.PriceUnitRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Delete price unit
	return s.PriceUnitRepo.Delete(ctx, priceUnit)
}
