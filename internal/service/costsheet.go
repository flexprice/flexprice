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
	CalculateMarkup(totalCost, totalRevenue decimal.Decimal) decimal.Decimal
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
	startTime, startOk := ctx.Value("start_time").(time.Time)
	endTime, endOk := ctx.Value("end_time").(time.Time)

	// If time range not provided in context and subscription ID is available, use subscription period
	if (!startOk || !endOk) && req.SubscriptionID != "" {
		subscriptionService := NewSubscriptionService(s.ServiceParams)
		sub, err := subscriptionService.GetSubscription(ctx, req.SubscriptionID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithMessage("failed to get subscription").
				WithHint("failed to get subscription details").
				Mark(ierr.ErrInternal)
		}

		if !startOk {
			startTime = sub.Subscription.CurrentPeriodStart
		}
		if !endOk {
			endTime = sub.Subscription.CurrentPeriodEnd
		}
	} else if !startOk || !endOk {
		// Fallback to last 24 hours if no subscription ID and no time range in context
		if !startOk {
			startTime = time.Now().AddDate(0, 0, -1)
		}
		if !endOk {
			endTime = time.Now()
		}
	}

	//Create service instance for costsheet in functions
	eventService := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger, s.Config)
	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)
	subscriptionService := NewSubscriptionService(s.ServiceParams)

	var totalCost decimal.Decimal
	itemCosts := make([]dto.CostBreakdownItem, 0)

	// If we have a subscription ID, get all line items to process both fixed and usage-based costs
	if req.SubscriptionID != "" {
		sub, err := subscriptionService.GetSubscription(ctx, req.SubscriptionID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithMessage("failed to get subscription").
				WithHint("failed to get subscription details").
				Mark(ierr.ErrInternal)
		}

		// Create a map to track unique meters for usage-based pricing
		uniqueMeters := make(map[string]struct{})

		// First pass: Process fixed costs and collect meters for usage-based costs
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
			} else if priceResp.Price.Type == types.PRICE_TYPE_USAGE && item.MeterID != "" {
				// For usage-based prices, collect the meter ID for later processing
				uniqueMeters[item.MeterID] = struct{}{}
			}
		}

		// Second pass: Process usage-based costs
		if len(uniqueMeters) > 0 {
			// Create usage requests for unique meters
			usageRequests := make([]*dto.GetUsageByMeterRequest, 0, len(uniqueMeters))
			for meterID := range uniqueMeters {
				usageRequests = append(usageRequests, &dto.GetUsageByMeterRequest{
					MeterID:   meterID,
					StartTime: startTime,
					EndTime:   endTime,
				})
			}

			// Fetch usage data for all meters in bulk
			usageData, err := eventService.BulkGetUsageByMeter(ctx, usageRequests)
			if err != nil {
				return nil, ierr.WithError(err).
					WithMessage("failed to get usage data").
					WithHint("failed to get usage data").
					Mark(ierr.ErrInternal)
			}

			// Process each meter's usage
			for _, item := range sub.LineItems {
				if item.Status != types.StatusPublished || item.MeterID == "" {
					continue
				}

				usage := usageData[item.MeterID]
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
	} else {
		// If no subscription ID, fall back to original cost sheet behavior
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

		items, err := s.CostSheetRepo.List(ctx, filter)
		if err != nil {
			return nil, ierr.WithError(err).
				WithMessage("failed to get cost sheet items").
				WithHint("failed to get cost sheet items").
				Mark(ierr.ErrInternal)
		}

		// Create usage requests for meters
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

		// Process each cost sheet item
		for _, item := range items {
			usage := usageData[item.MeterID]
			if usage == nil {
				continue
			}

			priceResp, err := priceService.GetPrice(ctx, item.PriceID)
			if err != nil {
				return nil, ierr.WithError(err).
					WithMessage("failed to get price").
					WithHint("failed to get price").
					Mark(ierr.ErrInternal)
			}

			cost := priceService.CalculateCostSheetPrice(ctx, priceResp.Price, usage.Value)

			itemCosts = append(itemCosts, dto.CostBreakdownItem{
				MeterID: item.MeterID,
				Usage:   usage.Value,
				Cost:    cost,
			})

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

	// Example: 1% Margin
	// totalCost = 1.00
	// totalRevenue = 1.01
	// margin = (1.01 - 1.00) / 1.01 = 0.0099 (0.99% margin)

}

func (s *costsheetService) CalculateMarkup(totalCost, totalRevenue decimal.Decimal) decimal.Decimal {

	// if totalRevenue is zero, return 0
	if totalRevenue.IsZero() {
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
		MeterID:        req.MeterID,
		PriceID:        req.PriceID,
		SubscriptionID: req.SubscriptionID,
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

// GetSubscriptionDetails retrieves subscription details
func (s *costsheetService) GetSubscriptionDetails(ctx context.Context, subscriptionID string) (*domainSubscription.Subscription, error) {
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	sub, err := subscriptionService.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	return sub.Subscription, nil
}
