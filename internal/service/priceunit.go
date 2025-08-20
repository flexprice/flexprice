package service

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainPriceUnit "github.com/flexprice/flexprice/internal/domain/priceunit"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type PriceUnitService interface {
	Create(ctx context.Context, req *dto.CreatePriceUnitRequest) (*dto.PriceUnitResponse, error)
	List(ctx context.Context, filter *domainPriceUnit.PriceUnitFilter) (*dto.ListPriceUnitsResponse, error)
	GetByID(ctx context.Context, id string) (*dto.PriceUnitResponse, error)
	GetByCode(ctx context.Context, code string) (*dto.PriceUnitResponse, error)
	Update(ctx context.Context, id string, req *dto.UpdatePriceUnitRequest) (*dto.PriceUnitResponse, error)
	Delete(ctx context.Context, id string) error
	ConvertToBaseCurrency(ctx context.Context, code string, priceUnitAmount decimal.Decimal) (decimal.Decimal, error)
	ConvertToPriceUnit(ctx context.Context, code string, fiatAmount decimal.Decimal) (decimal.Decimal, error)
}

type priceUnitService struct {
	ServiceParams
}

// NewPriceUnitService creates a new instance of PriceUnitService
func NewPriceUnitService(params ServiceParams) PriceUnitService {
	return &priceUnitService{
		ServiceParams: params,
	}
}

func (s *priceUnitService) Create(ctx context.Context, req *dto.CreatePriceUnitRequest) (*dto.PriceUnitResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	unit := req.ToPriceUnit(ctx)

	if err := s.PriceUnitRepo.Create(ctx, unit); err != nil {
		return nil, err
	}

	return s.toResponse(unit), nil
}

// List returns a paginated list of pricing units
func (s *priceUnitService) List(ctx context.Context, filter *domainPriceUnit.PriceUnitFilter) (*dto.ListPriceUnitsResponse, error) {
	// Validate the filter
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	// Get total count for pagination
	totalCount, err := s.PriceUnitRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Get paginated results
	units, err := s.PriceUnitRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Convert to response
	response := &dto.ListPriceUnitsResponse{
		Items: make([]*dto.PriceUnitResponse, len(units)),
		Pagination: types.PaginationResponse{
			Total:  totalCount,
			Limit:  filter.GetLimit(),
			Offset: filter.GetOffset(),
		},
	}

	for i, unit := range units {
		response.Items[i] = s.toResponse(unit)
	}

	return response, nil
}

// GetByID retrieves a pricing unit by ID
func (s *priceUnitService) GetByID(ctx context.Context, id string) (*dto.PriceUnitResponse, error) {
	if id == "" {
		return nil, ierr.NewError("id is required").
			WithMessage("missing id parameter").
			WithHint("Price unit ID is required").
			Mark(ierr.ErrValidation)
	}

	unit, err := s.PriceUnitRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return s.toResponse(unit), nil
}

func (s *priceUnitService) GetByCode(ctx context.Context, code string) (*dto.PriceUnitResponse, error) {
	if code == "" {
		return nil, ierr.NewError("code is required").
			WithMessage("missing code parameter").
			WithHint("Price unit code is required").
			Mark(ierr.ErrValidation)
	}

	unit, err := s.PriceUnitRepo.GetByCode(ctx, strings.ToLower(code))
	if err != nil {
		return nil, err
	}
	return s.toResponse(unit), nil
}

func (s *priceUnitService) Update(ctx context.Context, id string, req *dto.UpdatePriceUnitRequest) (*dto.PriceUnitResponse, error) {
	if id == "" {
		return nil, ierr.NewError("id is required").
			WithMessage("missing id parameter").
			WithHint("Price unit ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get existing unit
	existingUnit, err := s.PriceUnitRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Check if unit is in a state that allows updates
	if existingUnit.Status != types.StatusPublished {
		return nil, ierr.NewError("cannot update price unit").
			WithMessage("price unit is not in published status").
			WithHint("Only published price units can be updated").
			WithReportableDetails(map[string]interface{}{
				"id":     id,
				"status": existingUnit.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	// Update fields if provided and different from current values
	hasChanges := false
	if req.Name != "" && req.Name != existingUnit.Name {
		existingUnit.Name = req.Name
		hasChanges = true
	}
	if req.Symbol != "" && req.Symbol != existingUnit.Symbol {
		existingUnit.Symbol = req.Symbol
		hasChanges = true
	}

	// Only update if there are actual changes
	if !hasChanges {
		return s.toResponse(existingUnit), nil
	}

	if err := s.PriceUnitRepo.Update(ctx, existingUnit); err != nil {
		return nil, err
	}

	return s.toResponse(existingUnit), nil
}

func (s *priceUnitService) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("id is required").
			WithMessage("missing id parameter").
			WithHint("Price unit ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get the existing unit first
	existingUnit, err := s.PriceUnitRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Check the current status and handle accordingly
	switch existingUnit.Status {
	case types.StatusPublished:
		// Check if the unit is being used by any prices
		inUse, err := s.checkPriceUnitInUse(ctx, id)
		if err != nil {
			return err
		}
		if inUse {
			return ierr.NewError("price unit is in use").
				WithMessage("cannot archive unit that is in use").
				WithHint("This price unit is being used by one or more prices").
				Mark(ierr.ErrValidation)
		}

		// Archive the unit (set status to archived)
		existingUnit.Status = types.StatusArchived
		return s.PriceUnitRepo.Update(ctx, existingUnit)

	case types.StatusArchived:
		return ierr.NewError("price unit is already archived").
			WithMessage("cannot archive unit that is already archived").
			WithHint("The price unit is already in archived status").
			WithReportableDetails(map[string]interface{}{
				"id":     id,
				"status": existingUnit.Status,
			}).
			Mark(ierr.ErrValidation)

	case types.StatusDeleted:
		return ierr.NewError("price unit has been hard deleted").
			WithMessage("cannot archive unit that has been hard deleted").
			WithHint("The price unit has been permanently deleted and cannot be modified").
			WithReportableDetails(map[string]interface{}{
				"id":     id,
				"status": existingUnit.Status,
			}).
			Mark(ierr.ErrValidation)

	default:
		return ierr.NewError("invalid price unit status").
			WithMessage("price unit has an invalid status").
			WithHint("Price unit status must be one of: published, archived, deleted").
			WithReportableDetails(map[string]interface{}{
				"id":     id,
				"status": existingUnit.Status,
			}).
			Mark(ierr.ErrValidation)
	}
}

// ConvertToBaseCurrency converts an amount from pricing unit to base currency
// amount in fiat currency = amount in pricing unit * conversion_rate
func (s *priceUnitService) ConvertToBaseCurrency(ctx context.Context, code string, priceUnitAmount decimal.Decimal) (decimal.Decimal, error) {
	if code == "" {
		return decimal.Zero, ierr.NewError("code is required").
			WithMessage("missing code parameter").
			WithHint("Price unit code is required").
			Mark(ierr.ErrValidation)
	}

	if priceUnitAmount.IsZero() {
		return decimal.Zero, nil
	}

	if priceUnitAmount.IsNegative() {
		return decimal.Zero, ierr.NewError("amount must be positive").
			WithMessage("negative amount provided for conversion").
			WithHint("Amount must be greater than zero").
			WithReportableDetails(map[string]interface{}{
				"amount": priceUnitAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get the price unit to get the conversion rate
	unit, err := s.PriceUnitRepo.GetByCode(ctx, strings.ToLower(code))
	if err != nil {
		return decimal.Zero, err
	}

	return priceUnitAmount.Mul(unit.ConversionRate), nil
}

// ConvertToPriceUnit converts an amount from base currency to pricing unit
// amount in pricing unit = amount in fiat currency / conversion_rate
func (s *priceUnitService) ConvertToPriceUnit(ctx context.Context, code string, fiatAmount decimal.Decimal) (decimal.Decimal, error) {
	if code == "" {
		return decimal.Zero, ierr.NewError("code is required").
			WithMessage("missing code parameter").
			WithHint("Price unit code is required").
			Mark(ierr.ErrValidation)
	}

	if fiatAmount.IsZero() {
		return decimal.Zero, nil
	}

	if fiatAmount.IsNegative() {
		return decimal.Zero, ierr.NewError("amount must be positive").
			WithMessage("negative amount provided for conversion").
			WithHint("Amount must be greater than zero").
			WithReportableDetails(map[string]interface{}{
				"amount": fiatAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get the price unit to get the conversion rate
	unit, err := s.PriceUnitRepo.GetByCode(ctx, strings.ToLower(code))
	if err != nil {
		return decimal.Zero, err
	}

	return fiatAmount.Div(unit.ConversionRate), nil
}

// checkPriceUnitInUse checks if a price unit is being used by any prices
// This is a simplified implementation - in a production environment, you might want
// to add a direct query method to the price repository for better performance
func (s *priceUnitService) checkPriceUnitInUse(ctx context.Context, priceUnitID string) (bool, error) {
	// Create a filter to check for prices using this price unit
	// For now, we'll use a simple approach with a reasonable limit
	filter := types.NewNoLimitPriceFilter()
	filter.PriceUnitIDs = []string{priceUnitID}
	filter.Limit = lo.ToPtr(1)

	prices, err := s.PriceRepo.List(ctx, filter)
	if err != nil {
		return false, ierr.WithError(err).
			WithHint("Failed to check if price unit is in use").
			Mark(ierr.ErrDatabase)
	}

	// Check if any price uses this price unit
	for _, price := range prices {
		if price.PriceUnitID == priceUnitID {
			return true, nil
		}
	}

	return false, nil
}

// toResponse converts a domain PriceUnit to a dto.PriceUnitResponse
func (s *priceUnitService) toResponse(unit *domainPriceUnit.PriceUnit) *dto.PriceUnitResponse {
	return &dto.PriceUnitResponse{
		PriceUnit: unit,
	}
}
