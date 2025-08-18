package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainPriceUnit "github.com/flexprice/flexprice/internal/domain/priceunit"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// PriceUnitService handles business logic for price units
type PriceUnitService struct {
	ServiceParams
}

// NewPriceUnitService creates a new instance of PriceUnitService
func NewPriceUnitService(params ServiceParams) *PriceUnitService {
	return &PriceUnitService{
		ServiceParams: params,
	}
}

func (s *PriceUnitService) Create(ctx context.Context, req *dto.CreatePriceUnitRequest) (*domainPriceUnit.PriceUnit, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	unit := req.ToPriceUnit(ctx)

	if err := s.PriceUnitRepo.Create(ctx, unit); err != nil {
		return nil, err
	}

	return unit, nil
}

// List returns a paginated list of pricing units
func (s *PriceUnitService) List(ctx context.Context, filter *domainPriceUnit.PriceUnitFilter) (*dto.ListPriceUnitsResponse, error) {
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
func (s *PriceUnitService) GetByID(ctx context.Context, id string) (*dto.PriceUnitResponse, error) {
	unit, err := s.PriceUnitRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return s.toResponse(unit), nil
}

func (s *PriceUnitService) GetByCode(ctx context.Context, code string) (*dto.PriceUnitResponse, error) {
	unit, err := s.PriceUnitRepo.GetByCode(ctx, strings.ToLower(code), string(types.StatusPublished))
	if err != nil {
		return nil, err
	}
	return s.toResponse(unit), nil
}

func (s *PriceUnitService) Update(ctx context.Context, id string, req *dto.UpdatePriceUnitRequest) (*dto.PriceUnitResponse, error) {
	// Get existing unit
	existingUnit, err := s.PriceUnitRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Track if any changes were made
	hasChanges := false
	changes := make(map[string]interface{})

	// Update fields if provided and different from current values
	if req.Name != "" {
		if req.Name != existingUnit.Name {
			existingUnit.Name = req.Name
			hasChanges = true
			changes["name"] = req.Name
		}
	}
	if req.Symbol != "" {
		if req.Symbol != existingUnit.Symbol {
			existingUnit.Symbol = req.Symbol
			hasChanges = true
			changes["symbol"] = req.Symbol
		}
	}

	// Check if any changes were actually made
	if !hasChanges {
		return nil, ierr.NewError("no changes detected").
			WithMessage("provided values are the same as current values").
			WithHint("Provide different values to update the price unit").
			WithReportableDetails(map[string]interface{}{
				"id":     id,
				"name":   existingUnit.Name,
				"symbol": existingUnit.Symbol,
			}).
			Mark(ierr.ErrValidation)
	}

	existingUnit.UpdatedAt = time.Now().UTC()

	if err := s.PriceUnitRepo.Update(ctx, existingUnit); err != nil {
		return nil, err
	}

	return s.toResponse(existingUnit), nil
}

func (s *PriceUnitService) Delete(ctx context.Context, id string) error {
	// Get the existing unit first
	existingUnit, err := s.PriceUnitRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	// Check the current status and handle accordingly
	switch existingUnit.Status {
	case types.StatusPublished:
		// Check if the unit is being used by any prices
		exists, err := s.checkPriceUnitInUse(ctx, id)
		if err != nil {
			return err
		}
		if exists {
			return ierr.NewError("price unit is in use").
				WithMessage("cannot archive unit that is in use").
				WithHint("This price unit is being used by one or more prices").
				Mark(ierr.ErrValidation)
		}

		// Archive the unit (set status to archived)
		existingUnit.Status = types.StatusArchived
		existingUnit.UpdatedAt = time.Now().UTC()

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
func (s *PriceUnitService) ConvertToBaseCurrency(ctx context.Context, code string, priceUnitAmount decimal.Decimal) (decimal.Decimal, error) {
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
	unit, err := s.PriceUnitRepo.GetByCode(ctx, strings.ToLower(code), string(types.StatusPublished))
	if err != nil {
		return decimal.Zero, err
	}

	return priceUnitAmount.Mul(unit.ConversionRate), nil
}

// ConvertToPriceUnit converts an amount from base currency to pricing unit
// amount in pricing unit = amount in fiat currency / conversion_rate
func (s *PriceUnitService) ConvertToPriceUnit(ctx context.Context, code string, fiatAmount decimal.Decimal) (decimal.Decimal, error) {
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
	unit, err := s.PriceUnitRepo.GetByCode(ctx, strings.ToLower(code), string(types.StatusPublished))
	if err != nil {
		return decimal.Zero, err
	}

	return fiatAmount.Div(unit.ConversionRate), nil
}

// checkPriceUnitInUse checks if a price unit is being used by any prices
func (s *PriceUnitService) checkPriceUnitInUse(ctx context.Context, priceUnitID string) (bool, error) {
	// Use the price repository to check if any prices use this price unit
	// Since PriceFilter doesn't have PriceUnitIDs, we'll use a different approach
	// We'll create a filter with a custom condition or use the repository directly

	// For now, let's use a simple approach: get all prices and check if any use this price unit
	// This is not the most efficient, but it works with the current repository interface
	filter := types.NewNoLimitPriceFilter()

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

// toResponse converts a domain PricingUnit to a dto.PriceUnitResponse
func (s *PriceUnitService) toResponse(unit *domainPriceUnit.PriceUnit) *dto.PriceUnitResponse {
	return &dto.PriceUnitResponse{
		PriceUnit: unit,
	}
}
