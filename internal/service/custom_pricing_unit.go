package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/custompricingunit"
	"github.com/flexprice/flexprice/ent/price"
	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/google/uuid"
)

// CustomPricingUnitService handles business logic for custom pricing units
type CustomPricingUnitService struct {
	client *ent.Client
}

// NewCustomPricingUnitService creates a new instance of CustomPricingUnitService
func NewCustomPricingUnitService(client *ent.Client) *CustomPricingUnitService {
	return &CustomPricingUnitService{
		client: client,
	}
}

// Create creates a new custom pricing unit
func (s *CustomPricingUnitService) Create(ctx context.Context, req *dto.CreateCustomPricingUnitRequest) (*dto.CustomPricingUnitResponse, error) {
	// Check if code already exists
	exists, err := s.client.CustomPricingUnit.Query().
		Where(custompricingunit.CodeEQ(req.Code)).
		Exist(ctx)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ierr.WithError(err).
			WithMessage("code already exists").
			WithHint("Code already exists").
			Mark(ierr.ErrValidation)
	}

	// Validate conversion rate is provided (since it's required)
	if req.ConversionRate == nil {
		return nil, ierr.NewError("conversion rate is required").
			WithHint("Conversion rate is required").
			Mark(ierr.ErrValidation)
	}

	// Create the custom pricing unit
	unit, err := s.client.CustomPricingUnit.Create().
		SetID(uuid.New().String()).
		SetName(req.Name).
		SetCode(req.Code).
		SetSymbol(req.Symbol).
		SetBaseCurrency(req.BaseCurrency).
		SetConversionRate(*req.ConversionRate).
		SetPrecision(req.Precision).
		Save(ctx)
	if err != nil {
		return nil, err
	}

	return s.toResponse(unit), nil
}

// List returns a paginated list of custom pricing units
func (s *CustomPricingUnitService) List(ctx context.Context, filter *dto.CustomPricingUnitFilter) (*dto.ListCustomPricingUnitsResponse, error) {
	query := s.client.CustomPricingUnit.Query()

	// Apply filters
	if filter.Status != "" {
		query = query.Where(custompricingunit.StatusEQ(string(filter.Status)))
	}

	// Get total count
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, err
	}

	// Get paginated results
	units, err := query.All(ctx)
	if err != nil {
		return nil, err
	}

	// Convert to response
	response := &dto.ListCustomPricingUnitsResponse{
		Data:       make([]dto.CustomPricingUnitResponse, len(units)),
		TotalCount: total,
		HasMore:    false, // TODO: Implement proper pagination
	}

	for i, unit := range units {
		response.Data[i] = *s.toResponse(unit)
	}

	return response, nil
}

// Get retrieves a custom pricing unit by ID
func (s *CustomPricingUnitService) Get(ctx context.Context, id string) (*dto.CustomPricingUnitResponse, error) {
	unit, err := s.client.CustomPricingUnit.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithMessage("custom pricing unit not found").
				WithHint("Custom pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		return nil, err
	}

	return s.toResponse(unit), nil
}

// Update updates a custom pricing unit
func (s *CustomPricingUnitService) Update(ctx context.Context, id string, req *dto.UpdateCustomPricingUnitRequest) (*dto.CustomPricingUnitResponse, error) {
	update := s.client.CustomPricingUnit.UpdateOneID(id)

	if req.Name != "" {
		update = update.SetName(req.Name)
	}
	if req.Symbol != "" {
		update = update.SetSymbol(req.Symbol)
	}
	if req.Precision >= 0 {
		update = update.SetPrecision(req.Precision)
	}
	if req.Status != "" {
		update = update.SetStatus(string(req.Status))
	}

	unit, err := update.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithMessage("custom pricing unit not found").
				WithHint("Custom pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		return nil, err
	}

	return s.toResponse(unit), nil
}

// Delete deletes a custom pricing unit
func (s *CustomPricingUnitService) Delete(ctx context.Context, id string) error {
	// Check if the unit is being used by any prices
	exists, err := s.client.Price.Query().
		Where(price.CustomPricingUnitIDEQ(id)).
		Exist(ctx)
	if err != nil {
		return err
	}
	if exists {
		return ierr.WithError(err).
			WithMessage("custom pricing unit is in use by one or more prices").
			WithHint("Custom pricing unit is in use by one or more prices").
			Mark(ierr.ErrValidation)
	}

	err = s.client.CustomPricingUnit.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithMessage("custom pricing unit not found").
				WithHint("Custom pricing unit not found").
				Mark(ierr.ErrNotFound)
		}
		return err
	}

	return nil
}

// toResponse converts an ent.CustomPricingUnit to a dto.CustomPricingUnitResponse
func (s *CustomPricingUnitService) toResponse(unit *ent.CustomPricingUnit) *dto.CustomPricingUnitResponse {
	return &dto.CustomPricingUnitResponse{
		ID:             unit.ID,
		Name:           unit.Name,
		Code:           unit.Code,
		Symbol:         unit.Symbol,
		BaseCurrency:   unit.BaseCurrency,
		ConversionRate: unit.ConversionRate,
		Precision:      unit.Precision,
		Status:         types.Status(unit.Status),
		CreatedAt:      unit.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      unit.UpdatedAt.Format(time.RFC3339),
	}
}
