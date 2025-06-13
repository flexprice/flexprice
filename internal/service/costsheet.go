// Package service provides business logic implementations for the FlexPrice application.
package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"

	domainCostSheet "github.com/flexprice/flexprice/internal/domain/costsheet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// CostSheetService defines the interface for managing cost sheet operations.
// It provides functionality for calculating input costs and margins based on
// cost sheet items, usage data, and pricing information.
type CostSheetService interface {
	// CRUD Operations
	CreateCostsheet(ctx context.Context, req *dto.CreateCostsheetRequest) (*dto.CostsheetResponse, error)
	GetCostsheet(ctx context.Context, id string) (*dto.CostsheetResponse, error)
	ListCostsheets(ctx context.Context, filter *domainCostSheet.Filter) (*dto.ListCostsheetsResponse, error)
	UpdateCostsheet(ctx context.Context, req *dto.UpdateCostsheetRequest) (*dto.CostsheetResponse, error)
	DeleteCostsheet(ctx context.Context, id string) error

	// Calculation Operations
	GetInputCostForMargin(ctx context.Context, req *dto.CreateCostsheetRequest) (*dto.CostBreakdownResponse, error)
	CalculateMargin(totalCost, totalRevenue decimal.Decimal) decimal.Decimal
}

// costSheetService implements the CostSheetService interface.
// It coordinates between different components to calculate costs and margins.
type costSheetService struct {
	ServiceParams // Common service parameters

	//TODO:Create service instance for costsheet in functions
	// eventService EventService // Service for handling usage events
	// priceService PriceService // Service for price calculations
}

// NewCostSheetService creates a new instance of the cost sheet service with the required dependencies.
func NewCostSheetService(params ServiceParams, eventService EventService, priceService PriceService) CostSheetService {
	return &costSheetService{
		ServiceParams: params, // common service parameters// repository for cost sheet data access
		// eventService:  eventService,
		// priceService:  priceService,
	}
}

// GetInputCostForMargin implements the core business logic for calculating input costs.
// The process involves:
// 1. Retrieving published cost sheet items for the tenant and environment
// 2. Gathering usage data for all relevant meters
// 3. Calculating individual costs using pricing information
// 4. Aggregating total cost and item-specific costs
func (s *costSheetService) GetInputCostForMargin(ctx context.Context, req *dto.CreateCostsheetRequest) (*dto.CostBreakdownResponse, error) {
	tenantID, envID := domainCostSheet.GetTenantAndEnvFromContext(ctx)

	//Create service instance for costsheet in functions
	eventService := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger, s.Config)
	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)

	// Construct filter to get only published cost sheet items
	filter := &domainCostSheet.Filter{
		TenantID:      tenantID,
		EnvironmentID: envID,
		Filters: []*types.FilterCondition{
			{
				Field:    lo.ToPtr("status"),
				Operator: lo.ToPtr(types.EQUAL),
				DataType: lo.ToPtr(types.DataTypeString),
				Value: &types.Value{
					String: lo.ToPtr("published"),
				},
			},
		},
	}

	// Retrieve all published cost sheet items
	items, err := s.CostSheetRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithMessage("failed to get cost sheet items").
			WithHint("failed to get cost sheet items").
			Mark(ierr.ErrInternal)
	}

	// Create usage requests for each meter to gather usage data
	usageRequests := make([]*dto.GetUsageByMeterRequest, len(items))
	for i, item := range items {
		usageRequests[i] = &dto.GetUsageByMeterRequest{
			MeterID:   item.MeterID,
			StartTime: time.Now().AddDate(0, 0, -1),
			EndTime:   time.Now(),
		}
	}

	// Fetch usage data for all meters in bulk
	// usageData, err := s.eventService.BulkGetUsageByMeter(ctx, usageRequests)
	usageData, err := eventService.BulkGetUsageByMeter(ctx, usageRequests)

	if err != nil {
		return nil, ierr.WithError(err).
			WithMessage("failed to get usage data").
			WithHint("failed to get usage data").
			Mark(ierr.ErrInternal)
	}

	// Calculate individual and total costs based on usage and pricing
	var totalCost decimal.Decimal
	itemCosts := make([]dto.CostBreakdownItem, len(items))

	for i, item := range items {
		// Get pricing information for the item
		// priceResp, err := s.priceService.GetPrice(ctx, item.PriceID)
		priceResp, err := priceService.GetPrice(ctx, item.PriceID)

		if err != nil {
			return nil, ierr.WithError(err).
				WithMessage("failed to get price").
				WithHint("failed to get price").
				Mark(ierr.ErrInternal)
		}

		// Get usage data for the specific meter
		usage := usageData[item.MeterID]
		if usage == nil {
			continue // Skip if no usage data available
		}

		// Calculate cost for this item using price and usage
		// cost := s.priceService.CalculateCostSheetPrice(ctx, priceResp.Price, usage.Value)
		cost := priceService.CalculateCostSheetPrice(ctx, priceResp.Price, usage.Value)

		// Store item-specific cost details
		itemCosts[i] = dto.CostBreakdownItem{
			MeterID: item.MeterID,
			Usage:   usage.Value,
			Cost:    cost,
		}

		// Add to total cost
		totalCost = totalCost.Add(cost)
	}

	return &dto.CostBreakdownResponse{
		TotalCost: totalCost,
		Items:     itemCosts,
	}, nil
}

// CalculateMargin calculates the profit margin as a ratio.
// The formula used is: margin = (revenue - cost) / cost
// Returns 0 if totalCost is zero to prevent division by zero errors.
func (s *costSheetService) CalculateMargin(totalCost, totalRevenue decimal.Decimal) decimal.Decimal {
	// if totalCost is zero, return 0
	if totalCost.IsZero() {
		return decimal.Zero
	}

	return totalRevenue.Sub(totalCost).Div(totalCost)

	// Example: 50% Margin
	// totalCost = 100
	// totalRevenue = 150
	// margin = (150 - 100) / 100 = 0.5 (50% margin)

}

// Case 1: Zero Cost
// totalCost = 0
// totalRevenue = 100
// margin = 0  // Can't divide by zero!

// Case 2: Zero Revenue
// totalCost = 100
// totalRevenue = 0
// margin = 0   // No revenue = no margin

// Case 3: Negative Cost
// totalCost = -100
// totalRevenue = 100
// margin = 0   // Invalid business case

// Case 4: Negative Revenue
// totalCost = 100
// totalRevenue = -100
// margin = 0   // Invalid business case

// CreateCostsheet creates a new cost sheet record
func (s *costSheetService) CreateCostsheet(ctx context.Context, req *dto.CreateCostsheetRequest) (*dto.CostsheetResponse, error) {
	// Create domain model
	costsheet := domainCostSheet.New(ctx, req.MeterID, req.PriceID)

	// Validate the model
	if err := costsheet.Validate(); err != nil {
		return nil, err
	}

	// Save to repository
	if err := s.CostSheetRepo.Create(ctx, costsheet); err != nil {
		return nil, err
	}

	// Return response
	return &dto.CostsheetResponse{
		ID:        costsheet.ID,
		MeterID:   costsheet.MeterID,
		PriceID:   costsheet.PriceID,
		Status:    costsheet.Status,
		CreatedAt: costsheet.CreatedAt,
		UpdatedAt: costsheet.UpdatedAt,
	}, nil
}

// GetCostsheet retrieves a cost sheet by ID
func (s *costSheetService) GetCostsheet(ctx context.Context, id string) (*dto.CostsheetResponse, error) {
	// Get from repository
	costsheet, err := s.CostSheetRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Return response
	return &dto.CostsheetResponse{
		ID:        costsheet.ID,
		MeterID:   costsheet.MeterID,
		PriceID:   costsheet.PriceID,
		Status:    costsheet.Status,
		CreatedAt: costsheet.CreatedAt,
		UpdatedAt: costsheet.UpdatedAt,
	}, nil
}

// ListCostsheets retrieves a list of cost sheets based on filter criteria
func (s *costSheetService) ListCostsheets(ctx context.Context, filter *domainCostSheet.Filter) (*dto.ListCostsheetsResponse, error) {
	// Get from repository
	items, err := s.CostSheetRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Convert to response
	response := &dto.ListCostsheetsResponse{
		Items: make([]dto.CostsheetResponse, len(items)),
		Total: len(items),
	}

	for i, item := range items {
		response.Items[i] = dto.CostsheetResponse{
			ID:        item.ID,
			MeterID:   item.MeterID,
			PriceID:   item.PriceID,
			Status:    item.Status,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		}
	}

	return response, nil
}

// UpdateCostsheet updates an existing cost sheet
func (s *costSheetService) UpdateCostsheet(ctx context.Context, req *dto.UpdateCostsheetRequest) (*dto.CostsheetResponse, error) {
	// Get existing cost sheet
	costsheet, err := s.CostSheetRepo.Get(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	// Update fields
	if req.Status != "" {
		costsheet.Status = types.Status(req.Status)
	}

	// Save changes
	if err := s.CostSheetRepo.Update(ctx, costsheet); err != nil {
		return nil, err
	}

	// Return response
	return &dto.CostsheetResponse{
		ID:        costsheet.ID,
		MeterID:   costsheet.MeterID,
		PriceID:   costsheet.PriceID,
		Status:    costsheet.Status,
		CreatedAt: costsheet.CreatedAt,
		UpdatedAt: costsheet.UpdatedAt,
	}, nil
}

// DeleteCostsheet deletes a cost sheet by ID
func (s *costSheetService) DeleteCostsheet(ctx context.Context, id string) error {
	return s.CostSheetRepo.Delete(ctx, id)
}
