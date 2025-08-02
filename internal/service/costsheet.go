// Package service provides business logic implementations for the FlexPrice application.
package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"

	domainCostSheet "github.com/flexprice/flexprice/internal/domain/costsheet"
	// domainSubscription "github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
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
	GetInputCostForMargin(ctx context.Context, req *dto.GetCostBreakdownRequest) (*dto.CostBreakdownResponse, error)
	CalculateMargin(totalCost, totalRevenue decimal.Decimal) decimal.Decimal
	CalculateMarkup(totalCost, totalRevenue decimal.Decimal) decimal.Decimal
	CalculateROI(ctx context.Context, req *dto.CalculateROIRequest) (*dto.ROIResponse, error)

	// TODO: handler business logic -> service methods
	// GetServiceParams retrieves the service parameters
	GetServiceParams() ServiceParams
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
// 1. Getting subscription details and time period
// 2. Processing fixed costs from subscription line items
// 3. Processing usage-based costs by gathering meter data
// 4. Aggregating total cost and item-specific costs
func (s *costsheetService) GetInputCostForMargin(ctx context.Context, req *dto.GetCostBreakdownRequest) (*dto.CostBreakdownResponse, error) {
	// service instances
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.PriceUnitRepo, s.Logger)
	eventService := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger, s.Config)

	// Get subscription details first
	sub, err := subscriptionService.GetSubscription(ctx, req.SubscriptionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithMessage("failed to get subscription").
			WithHint("failed to get subscription details").
			Mark(ierr.ErrInternal)
	}

	// Determine time period - use request time if provided, otherwise use subscription period
	var startTime, endTime time.Time
	if req.StartTime != nil && req.EndTime != nil {
		startTime = *req.StartTime
		endTime = *req.EndTime
	} else {
		startTime = sub.Subscription.CurrentPeriodStart
		endTime = sub.Subscription.CurrentPeriodEnd
	}

	var totalCost decimal.Decimal
	itemCosts := make([]dto.CostBreakdownItem, 0)

	// First pass: Process fixed costs and prepare usage requests
	usageRequests := make([]*dto.GetUsageByMeterRequest, 0)

	for _, item := range sub.LineItems {
		if item.Status != types.StatusPublished {
			continue
		}

		// Get price details
		priceResp, err := priceService.GetPrice(ctx, item.PriceID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithMessage("failed to get price").
				WithHint("failed to get price").
				Mark(ierr.ErrInternal)
		}

		if priceResp.Price.Type == types.PRICE_TYPE_FIXED {
			// For fixed prices, use the price amount directly
			cost := priceResp.Price.Amount.Mul(item.Quantity)
			itemCosts = append(itemCosts, dto.CostBreakdownItem{
				Cost: cost,
			})
			totalCost = totalCost.Add(cost)
		} else if priceResp.Price.Type == types.PRICE_TYPE_USAGE {
			// For usage-based prices, validate both MeterID and PriceID before creating request
			if item.MeterID == "" || item.PriceID == "" {
				s.Logger.Warnw("skipping usage request due to missing meter or price ID",
					"line_item_id", item.ID,
					"meter_id", item.MeterID,
					"price_id", item.PriceID)
				continue
			}

			// Create usage request only if both IDs are present
			usageRequests = append(usageRequests, &dto.GetUsageByMeterRequest{
				MeterID:   item.MeterID,
				PriceID:   item.PriceID,
				StartTime: startTime,
				EndTime:   endTime,
			})
		}
	}

	// Second pass: Process usage-based costs
	if len(usageRequests) > 0 {
		// Fetch usage data for all meters in bulk
		usageData, err := eventService.BulkGetUsageByMeter(ctx, usageRequests)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("failed to get usage data").
				Mark(ierr.ErrInternal)
		}

		// Process each meter's usage
		for _, item := range sub.LineItems {
			if item.Status != types.StatusPublished {
				continue
			}

			// Skip if either MeterID or PriceID is empty
			if item.MeterID == "" || item.PriceID == "" {
				continue
			}

			usage := usageData[item.PriceID]
			if usage == nil {
				continue
			}

			// Get price details
			priceResp, err := priceService.GetPrice(ctx, item.PriceID)
			if err != nil {
				return nil, ierr.WithError(err).
					WithMessage("failed to get price").
					WithHint("failed to get price").
					Mark(ierr.ErrInternal)
			}

			// Calculate cost for this item using price and usage
			cost := priceService.CalculateCostSheetPrice(ctx, priceResp.Price, usage.Value)

			// Store item-specific cost details
			itemCosts = append(itemCosts, dto.CostBreakdownItem{
				MeterID: item.MeterID,
				Usage:   usage.Value,
				Cost:    cost,
			})

			// Add to total cost
			totalCost = totalCost.Add(cost)
		}
	}

	return &dto.CostBreakdownResponse{
		TotalCost: totalCost,
		Items:     itemCosts,
	}, nil
}

// CalculateMargin calculates the profit margin as a ratio.
// The formula used is: margin = (revenue - cost) / revenue
// Returns 0 if totalRevenue is zero to prevent division by zero errors.
func (s *costsheetService) CalculateMargin(totalCost, totalRevenue decimal.Decimal) decimal.Decimal {
	// if totalRevenue is zero, return 0
	if totalRevenue.IsZero() {
		return decimal.Zero
	}

	return totalRevenue.Sub(totalCost).Div(totalRevenue)

}

// CalculateMarkup calculates the markup as a ratio.
// The formula used is: markup = (revenue - cost) / cost
// Returns 0 if totalCost is zero to prevent division by zero errors.
func (s *costsheetService) CalculateMarkup(totalCost, totalRevenue decimal.Decimal) decimal.Decimal {

	// if totalRevenue is zero, return 0
	if totalCost.IsZero() {
		return decimal.Zero
	}

	return totalRevenue.Sub(totalCost).Div(totalCost)
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
		// Create a temporary costsheet to validate the new status
		bufferCostsheet := &domainCostSheet.Costsheet{
			ID:      costsheet.ID,
			MeterID: costsheet.MeterID,
			PriceID: costsheet.PriceID,
			BaseModel: types.BaseModel{
				Status: types.Status(req.Status),
			},
		}

		// Validate the status
		if err := bufferCostsheet.Validate(); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Invalid status provided").
				WithReportableDetails(map[string]interface{}{
					"status": req.Status,
					"valid_statuses": []types.Status{
						types.StatusPublished,
						types.StatusArchived,
						types.StatusDeleted,
					},
				}).
				Mark(ierr.ErrValidation)
		}

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

// DeleteCostSheet performs a soft delete by archiving a published cost sheet.
// If the cost sheet is not in published status, it returns an error.
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
	costSheetBreakdownReq := &dto.GetCostBreakdownRequest{
		SubscriptionID: req.SubscriptionID,
		StartTime:      &periodStart,
		EndTime:        &periodEnd,
	}

	// Get cost breakdown
	costBreakdown, err := s.GetInputCostForMargin(ctx, costSheetBreakdownReq)
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
	netMargin := s.CalculateMargin(totalCost, revenue)
	markup := s.CalculateMarkup(totalCost, revenue)

	response := &dto.ROIResponse{
		Cost:                totalCost,
		Revenue:             revenue,
		NetRevenue:          netRevenue,
		NetMargin:           netMargin,
		NetMarginPercentage: netMargin.Mul(decimal.NewFromInt(100)).Round(2),
		Markup:              markup,
		MarkupPercentage:    markup.Mul(decimal.NewFromInt(100)).Round(2),
		CostBreakdown:       costBreakdown.Items,
	}

	return response, nil
}

// GetServiceParams retrieves the service parameters
func (s *costsheetService) GetServiceParams() ServiceParams {
	return s.ServiceParams
}
