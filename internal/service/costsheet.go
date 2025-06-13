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
	// GetInputCostForMargin calculates input costs for margin calculation by:
	// 1. Retrieving all published cost sheet items
	// 2. Gathering usage data for each meter
	// 3. Calculating costs based on pricing and usage
	// Returns the total cost and individual item costs for the specified time period
	GetInputCostForMargin(ctx context.Context, req *dto.CreateCostsheetRequest) (*dto.CostBreakdownResponse, error)

	// CalculateMargin calculates the profit margin as a decimal value using the formula:
	// margin = (revenue - cost) / cost
	// Returns zero if total cost is zero to avoid division by zero
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
