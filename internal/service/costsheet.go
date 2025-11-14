// Package service provides business logic implementations for the FlexPrice application.
package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCostsheet "github.com/flexprice/flexprice/internal/domain/costsheet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CostsheetService defines the interface for managing costsheet operations.
// It provides functionality for CRUD operations and cost calculations.
type CostsheetService interface {
	// CRUD Operations
	CreateCostsheet(ctx context.Context, req dto.CreateCostsheetRequest) (*dto.CreateCostsheetResponse, error)
	GetCostsheet(ctx context.Context, id string) (*dto.CostsheetResponse, error)
	GetCostsheets(ctx context.Context, filter *domainCostsheet.Filter) (*dto.ListCostsheetResponse, error)
	UpdateCostsheet(ctx context.Context, id string, req dto.UpdateCostsheetRequest) (*dto.UpdateCostsheetResponse, error)
	DeleteCostsheet(ctx context.Context, id string) (*dto.DeleteCostsheetResponse, error)
	GetActiveCostsheetForTenant(ctx context.Context) (*dto.CostsheetResponse, error)

	// Calculation Operations (legacy methods for backward compatibility)
	GetInputCostForMargin(ctx context.Context, req *dto.GetCostBreakdownRequest) (*dto.CostBreakdownResponse, error)
	CalculateMargin(totalCost, totalRevenue decimal.Decimal) decimal.Decimal
	CalculateMarkup(totalCost, totalRevenue decimal.Decimal) decimal.Decimal
}

// costsheetService implements the CostsheetService interface.
// It coordinates between different components to manage costsheets and calculate costs.
type costsheetService struct {
	ServiceParams
}

// NewCostsheetService creates a new instance of the costsheet service with the required dependencies.
func NewCostsheetService(params ServiceParams) CostsheetService {
	return &costsheetService{
		ServiceParams: params,
	}
}

func (s *costsheetService) CreateCostsheet(ctx context.Context, req dto.CreateCostsheetRequest) (*dto.CreateCostsheetResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid costsheet data provided").
			Mark(ierr.ErrValidation)
	}

	costsheet := req.ToCostsheet(ctx)

	// Validate the domain model
	if err := costsheet.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid costsheet data").
			Mark(ierr.ErrValidation)
	}

	// Start a transaction to create costsheet
	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Check if a costsheet with the same name already exists in the same tenant and environment
		existing, err := s.CostSheetRepo.GetByName(ctx, costsheet.TenantID, costsheet.EnvironmentID, costsheet.Name)
		if err != nil && !ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Failed to check for existing costsheet").
				Mark(ierr.ErrDatabase)
		}
		if existing != nil {
			return ierr.NewError("costsheet with this name already exists").
				WithHint("A costsheet with this name already exists in the same tenant and environment").
				WithReportableDetails(map[string]any{
					"name":           costsheet.Name,
					"tenant_id":      costsheet.TenantID,
					"environment_id": costsheet.EnvironmentID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}

		// Create the costsheet
		if err := s.CostSheetRepo.Create(ctx, costsheet); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create costsheet").
				Mark(ierr.ErrDatabase)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	response := &dto.CreateCostsheetResponse{
		Costsheet: dto.ToCostsheetResponse(costsheet),
	}

	return response, nil
}

func (s *costsheetService) GetCostsheet(ctx context.Context, id string) (*dto.CostsheetResponse, error) {
	if id == "" {
		return nil, ierr.NewError("costsheet ID is required").
			WithHint("Please provide a valid costsheet ID").
			Mark(ierr.ErrValidation)
	}

	costsheet, err := s.CostSheetRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Fetch prices associated with this costsheet
	priceService := NewPriceService(s.ServiceParams)
	pricesResponse, err := priceService.GetPricesByCostsheetID(ctx, id)
	if err != nil {
		return nil, err
	}

	return dto.ToCostsheetResponseWithPrices(costsheet, pricesResponse.Items), nil
}

func (s *costsheetService) GetCostsheets(ctx context.Context, filter *domainCostsheet.Filter) (*dto.ListCostsheetResponse, error) {
	if filter == nil {
		filter = &domainCostsheet.Filter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	// Fetch costsheet records
	costsheets, err := s.CostSheetRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve costsheet records").
			Mark(ierr.ErrDatabase)
	}

	// Get count
	count, err := s.CostSheetRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Build response
	response := &dto.ListCostsheetResponse{
		Items: make([]*dto.CostsheetResponse, len(costsheets)),
		Pagination: &types.PaginationResponse{
			Total:  int(count),
			Limit:  filter.GetLimit(),
			Offset: filter.GetOffset(),
		},
	}

	if len(costsheets) == 0 {
		return response, nil
	}

	// Initialize response items without prices first
	for i, costsheet := range costsheets {
		response.Items[i] = dto.ToCostsheetResponse(costsheet)
	}

	// Check if prices should be expanded
	if filter.GetExpand().Has(types.ExpandPrices) && len(costsheets) > 0 {
		// Collect all costsheet IDs
		costsheetIDs := make([]string, len(costsheets))
		for i, cs := range costsheets {
			costsheetIDs[i] = cs.ID
		}

		// Fetch all prices for these costsheets in one go
		priceService := NewPriceService(s.ServiceParams)
		priceFilter := types.NewNoLimitPriceFilter().
			WithEntityIDs(costsheetIDs).
			WithStatus(types.StatusPublished).
			WithEntityType(types.PRICE_ENTITY_TYPE_COSTSHEET)

		// If meters should be expanded, propagate the expansion to prices
		if filter.GetExpand().Has(types.ExpandMeters) {
			priceFilter = priceFilter.WithExpand(string(types.ExpandMeters))
		}

		pricesResponse, err := priceService.GetPrices(ctx, priceFilter)
		if err != nil {
			s.Logger.Warnw("Failed to fetch prices for costsheet list", "error", err)
		} else {
			// Group prices by costsheet ID
			pricesByCostsheet := make(map[string][]*dto.PriceResponse)
			for _, price := range pricesResponse.Items {
				if price.EntityID != "" {
					pricesByCostsheet[price.EntityID] = append(pricesByCostsheet[price.EntityID], price)
				}
			}

			// Build response with expanded fields
			for i, costsheet := range costsheets {
				// Add prices if available
				if prices, ok := pricesByCostsheet[costsheet.ID]; ok {
					response.Items[i].Prices = prices
				}
			}
		}
	}

	return response, nil
}

func (s *costsheetService) UpdateCostsheet(ctx context.Context, id string, req dto.UpdateCostsheetRequest) (*dto.UpdateCostsheetResponse, error) {
	if id == "" {
		return nil, ierr.NewError("costsheet ID is required").
			WithHint("Costsheet ID is required").
			Mark(ierr.ErrValidation)
	}

	// Validate request
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid update data provided").
			Mark(ierr.ErrValidation)
	}

	// Get the existing costsheet
	costsheet, err := s.CostSheetRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Start a transaction to update costsheet
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Update the costsheet
		req.UpdateCostsheet(costsheet, ctx)

		// Save the updated costsheet
		if err := s.CostSheetRepo.Update(ctx, costsheet); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to update costsheet").
				Mark(ierr.ErrDatabase)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	response := &dto.UpdateCostsheetResponse{
		Costsheet: dto.ToCostsheetResponse(costsheet),
	}

	return response, nil
}

func (s *costsheetService) DeleteCostsheet(ctx context.Context, id string) (*dto.DeleteCostsheetResponse, error) {
	if id == "" {
		return nil, ierr.NewError("costsheet ID is required").
			WithHint("Costsheet ID is required").
			Mark(ierr.ErrValidation)
	}

	// Start a transaction to delete costsheet
	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Soft delete the costsheet
		if err := s.CostSheetRepo.Delete(ctx, id); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to delete costsheet").
				Mark(ierr.ErrDatabase)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	response := &dto.DeleteCostsheetResponse{
		Message: "Costsheet deleted successfully",
		ID:      id,
	}

	return response, nil
}

func (s *costsheetService) GetActiveCostsheetForTenant(ctx context.Context) (*dto.CostsheetResponse, error) {
	// Create filter with no limit to get all costsheets
	filter := &domainCostsheet.Filter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	}

	filter.QueryFilter.Expand = lo.ToPtr(string(types.ExpandPrices))
	// List costsheets (automatically filtered by tenant and environment from context)
	response, err := s.GetCostsheets(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve active costsheet").
			Mark(ierr.ErrDatabase)
	}

	if len(response.Items) == 0 {
		return nil, ierr.NewError("no active costsheet found").
			WithHint("No active costsheet found for this tenant").
			Mark(ierr.ErrNotFound)
	}

	// Return the first (most recent) costsheet
	return response.Items[0], nil
}

// Legacy calculation methods for backward compatibility
func (s *costsheetService) GetInputCostForMargin(ctx context.Context, req *dto.GetCostBreakdownRequest) (*dto.CostBreakdownResponse, error) {
	// Implementation for backward compatibility
	// This would need to be implemented based on the original costsheet logic
	return nil, ierr.NewError("method not implemented").
		WithHint("This method needs to be implemented for the new costsheet structure").
		Mark(ierr.ErrInvalidOperation)
}

func (s *costsheetService) CalculateMargin(totalCost, totalRevenue decimal.Decimal) decimal.Decimal {
	if totalRevenue.IsZero() {
		return decimal.Zero
	}
	return totalRevenue.Sub(totalCost).Div(totalRevenue).Mul(decimal.NewFromInt(100))
}

func (s *costsheetService) CalculateMarkup(totalCost, totalRevenue decimal.Decimal) decimal.Decimal {
	if totalCost.IsZero() {
		return decimal.Zero
	}
	return totalRevenue.Sub(totalCost).Div(totalCost).Mul(decimal.NewFromInt(100))
}
