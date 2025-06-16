// Package service provides business logic implementations for the FlexPrice application.
package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"

	domainCostSheet "github.com/flexprice/flexprice/internal/domain/costsheet"
	domainSubscription "github.com/flexprice/flexprice/internal/domain/subscription"
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
	CreateCostSheet(ctx context.Context, req *dto.CreateCostSheetRequest) (*dto.CostSheetResponse, error)
	GetCostSheet(ctx context.Context, id string) (*dto.CostSheetResponse, error)
	ListCostSheets(ctx context.Context, filter *domainCostSheet.Filter) (*dto.ListCostSheetsResponse, error)
	UpdateCostSheet(ctx context.Context, req *dto.UpdateCostSheetRequest) (*dto.CostSheetResponse, error)
	DeleteCostSheet(ctx context.Context, id string) error

	// Calculation Operations
	GetInputCostForMargin(ctx context.Context, req *dto.CreateCostSheetRequest) (*dto.CostBreakdownResponse, error)
	CalculateMargin(totalCost, totalRevenue decimal.Decimal) decimal.Decimal
	CalculateROI(ctx context.Context, req *dto.CalculateROIRequest) (*dto.ROIResponse, error)

	// Helper Operations
	GetSubscriptionDetails(ctx context.Context, subscriptionID string) (*domainSubscription.Subscription, error)
}

// costSheetService implements the CostSheetService interface.
// It coordinates between different components to calculate costs and margins.
type costsheetService struct {
	ServiceParams
}

// NewCostSheetService creates a new instance of the cost sheet service with the required dependencies.
func NewCostSheetService(params ServiceParams) CostSheetService {
	return &costsheetService{
		ServiceParams: params,
	}
}

// GetInputCostForMargin implements the core business logic for calculating input costs.
// The process involves:
// 1. Retrieving published cost sheet items for the tenant and environment
// 2. Gathering usage data for all relevant meters
// 3. Calculating individual costs using pricing information
// 4. Aggregating total cost and item-specific costs
func (s *costsheetService) GetInputCostForMargin(ctx context.Context, req *dto.CreateCostSheetRequest) (*dto.CostBreakdownResponse, error) {
	tenantID, envID := domainCostSheet.GetTenantAndEnvFromContext(ctx)

	// Get time range from context
	startTime, ok := ctx.Value("start_time").(time.Time)
	if !ok {
		startTime = time.Now().AddDate(0, 0, -1) // Default to last 24 hours
	}
	endTime, ok := ctx.Value("end_time").(time.Time)
	if !ok {
		endTime = time.Now()
	}

	//Create service instance for costsheet in functions
	eventService := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger, s.Config)
	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)

	// Construct filter to get only published cost sheet items
	filter := &domainCostSheet.Filter{
		QueryFilter:   types.NewDefaultQueryFilter(),
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
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	// Fetch usage data for all meters in bulk
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
func (s *costsheetService) CalculateMargin(totalCost, totalRevenue decimal.Decimal) decimal.Decimal {
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

// API Services

// CreateCostsheet creates a new cost sheet record
func (s *costsheetService) CreateCostSheet(ctx context.Context, req *dto.CreateCostSheetRequest) (*dto.CostSheetResponse, error) {
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
	return &dto.CostSheetResponse{
		ID:        costsheet.ID,
		MeterID:   costsheet.MeterID,
		PriceID:   costsheet.PriceID,
		Status:    costsheet.Status,
		CreatedAt: costsheet.CreatedAt,
		UpdatedAt: costsheet.UpdatedAt,
	}, nil
}

// GetCostsheet retrieves a cost sheet by ID
func (s *costsheetService) GetCostSheet(ctx context.Context, id string) (*dto.CostSheetResponse, error) {
	// Get from repository
	costsheet, err := s.CostSheetRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Return response
	return &dto.CostSheetResponse{
		ID:        costsheet.ID,
		MeterID:   costsheet.MeterID,
		PriceID:   costsheet.PriceID,
		Status:    costsheet.Status,
		CreatedAt: costsheet.CreatedAt,
		UpdatedAt: costsheet.UpdatedAt,
	}, nil
}

// ListCostsheets retrieves a list of cost sheets based on filter criteria
func (s *costsheetService) ListCostSheets(ctx context.Context, filter *domainCostSheet.Filter) (*dto.ListCostSheetsResponse, error) {
	// Get from repository
	items, err := s.CostSheetRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Convert to response
	response := &dto.ListCostSheetsResponse{
		Items: make([]dto.CostSheetResponse, len(items)),
		Total: len(items),
	}

	for i, item := range items {
		response.Items[i] = dto.CostSheetResponse{
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
func (s *costsheetService) UpdateCostSheet(ctx context.Context, req *dto.UpdateCostSheetRequest) (*dto.CostSheetResponse, error) {
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
	return &dto.CostSheetResponse{
		ID:        costsheet.ID,
		MeterID:   costsheet.MeterID,
		PriceID:   costsheet.PriceID,
		Status:    costsheet.Status,
		CreatedAt: costsheet.CreatedAt,
		UpdatedAt: costsheet.UpdatedAt,
	}, nil
}

// DeleteCostsheet deletes a cost sheet by ID
func (s *costsheetService) DeleteCostSheet(ctx context.Context, id string) error {
	return s.CostSheetRepo.Delete(ctx, id)
}

// CalculateROI calculates the ROI for a cost sheet
func (s *costsheetService) CalculateROI(ctx context.Context, req *dto.CalculateROIRequest) (*dto.ROIResponse, error) {
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	billingService := NewBillingService(s.ServiceParams)

	// Get subscription details first to determine time period
	sub, err := subscriptionService.GetSubscription(ctx, req.SubscriptionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithMessage("failed to get subscription").
			WithHint("failed to get subscription details").
			Mark(ierr.ErrInternal)
	}

	// Use subscription period if time range not provided
	periodStart := sub.Subscription.CurrentPeriodStart
	periodEnd := sub.Subscription.CurrentPeriodEnd

	if req.PeriodStart != nil && req.PeriodEnd != nil {
		periodStart = *req.PeriodStart
		periodEnd = *req.PeriodEnd
	}

	// Create a cost sheet request from ROI request
	costSheetReq := &dto.CreateCostSheetRequest{
		MeterID: req.MeterID,
		PriceID: req.PriceID,
	}

	// Add time range to context for GetInputCostForMargin
	ctx = context.WithValue(ctx, "start_time", periodStart)
	ctx = context.WithValue(ctx, "end_time", periodEnd)

	// Get cost breakdown
	costBreakdown, err := s.GetInputCostForMargin(ctx, costSheetReq)
	if err != nil {
		return nil, ierr.WithError(err).
			WithMessage("failed to get cost breakdown").
			WithHint("failed to calculate costs").
			Mark(ierr.ErrInternal)
	}

	// Get usage data for the subscription
	usageReq := &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: req.SubscriptionID,
		StartTime:      periodStart,
		EndTime:        periodEnd,
	}

	usage, err := subscriptionService.GetUsageBySubscription(ctx, usageReq)
	if err != nil {
		return nil, ierr.WithError(err).
			WithMessage("failed to get usage data").
			WithHint("failed to get subscription usage").
			Mark(ierr.ErrInternal)
	}

	// Calculate total revenue using billing service
	billingResult, err := billingService.CalculateAllCharges(ctx, sub.Subscription, usage, periodStart, periodEnd)
	if err != nil {
		return nil, ierr.WithError(err).
			WithMessage("failed to calculate charges").
			WithHint("failed to calculate total revenue").
			Mark(ierr.ErrInternal)
	}

	// Calculate metrics
	revenue := billingResult.TotalAmount
	totalCost := costBreakdown.TotalCost
	netRevenue := revenue.Sub(totalCost)

	response := &dto.ROIResponse{
		Revenue: revenue,
		Costs: struct {
			Total decimal.Decimal         `json:"total"`
			Items []dto.CostBreakdownItem `json:"items"`
		}{
			Total: totalCost,
			Items: costBreakdown.Items,
		},
		NetRevenue: netRevenue,
		ROI:        s.CalculateMargin(totalCost, revenue),
		Margin:     netRevenue.Div(revenue), // (Revenue - Cost) / Revenue
	}

	return response, nil
}

// GetSubscriptionDetails retrieves subscription details
func (s *costsheetService) GetSubscriptionDetails(ctx context.Context, subscriptionID string) (*domainSubscription.Subscription, error) {
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	sub, err := subscriptionService.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	return sub.Subscription, nil
}
