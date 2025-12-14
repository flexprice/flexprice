package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/shopspring/decimal"
)

type RevenueAnalyticsService = interfaces.RevenueAnalyticsService

type revenueAnalyticsService struct {
	ServiceParams
	featureUsageTrackingService   FeatureUsageTrackingService
	costsheetService              CostsheetService
	costsheetUsageTrackingService CostSheetUsageTrackingService
}

func NewRevenueAnalyticsService(params ServiceParams, featureUsageTrackingService FeatureUsageTrackingService, costsheetService CostsheetService, costsheetUsageTrackingService CostSheetUsageTrackingService) RevenueAnalyticsService {
	return &revenueAnalyticsService{
		ServiceParams:                 params,
		featureUsageTrackingService:   featureUsageTrackingService,
		costsheetService:              costsheetService,
		costsheetUsageTrackingService: costsheetUsageTrackingService,
	}
}

// getCostAnalytics retrieves cost analytics using the costsheet usage tracking service
func (s *revenueAnalyticsService) getCostAnalytics(
	ctx context.Context,
	req *dto.GetCostAnalyticsRequest,
) (*dto.GetCostAnalyticsResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid cost analytics request").
			Mark(ierr.ErrValidation)
	}

	// Call the costsheet usage tracking service to get analytics
	// The service will fetch the active costsheet internally
	return s.costsheetUsageTrackingService.GetCostSheetUsageAnalytics(ctx, req)
}

// GetDetailedCostAnalytics retrieves detailed cost analytics with derived metrics
func (s *revenueAnalyticsService) GetDetailedCostAnalytics(
	ctx context.Context,
	req *dto.GetCostAnalyticsRequest,
) (*dto.GetDetailedCostAnalyticsResponse, error) {
	// 1. Fetch cost analytics (gracefully handle errors)
	var costAnalytics *dto.GetCostAnalyticsResponse
	costsheet, err := s.costsheetService.GetActiveCostsheetForTenant(ctx)
	if err != nil {
		s.Logger.Warnw("failed to fetch active costsheet", "error", err)
		costAnalytics = nil
	} else if len(costsheet.Prices) == 0 {
		s.Logger.Warnw("no prices found for costsheet", "costsheet_id", costsheet.ID)
		costAnalytics = nil
	} else {
		costAnalytics, err = s.getCostAnalytics(ctx, req)
		if err != nil {
			s.Logger.Warnw("failed to fetch cost analytics", "error", err)
			costAnalytics = nil
		}
	}

	// 2. Fetch revenue analytics
	var revenueAnalytics *dto.GetUsageAnalyticsResponse
	revenueReq := s.buildRevenueRequest(req)
	if revenueReq != nil {
		revenueAnalytics, err = s.featureUsageTrackingService.GetDetailedUsageAnalyticsV2(ctx, revenueReq)
		if err != nil {
			s.Logger.Warnw("failed to fetch revenue analytics", "error", err)
			revenueAnalytics = nil
		}
	}

	// 3. Build response - prioritize revenue if cost fails
	response := &dto.GetDetailedCostAnalyticsResponse{
		CostAnalytics: []dto.CostAnalyticItem{},
		TotalCost:     decimal.Zero,
		TotalRevenue:  decimal.Zero,
		Margin:        decimal.Zero,
		MarginPercent: decimal.Zero,
		ROI:           decimal.Zero,
		ROIPercent:    decimal.Zero,
		Currency:      "USD", // Default currency
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
	}

	// Populate cost analytics if available
	if costAnalytics != nil {
		response.CostAnalytics = costAnalytics.CostAnalytics
		response.TotalCost = costAnalytics.TotalCost
		if costAnalytics.Currency != "" {
			response.Currency = costAnalytics.Currency
		}
		response.StartTime = costAnalytics.StartTime
		response.EndTime = costAnalytics.EndTime
	}

	// Calculate revenue
	response.TotalRevenue = s.calculateTotalRevenue(revenueAnalytics)
	if revenueAnalytics != nil && revenueAnalytics.Currency != "" {
		// Use revenue currency if cost currency is default
		if response.Currency == "USD" && costAnalytics == nil {
			response.Currency = revenueAnalytics.Currency
		}
	}

	// Only calculate derived metrics if both cost and revenue are available
	if costAnalytics != nil && revenueAnalytics != nil {
		response.Margin = response.TotalRevenue.Sub(response.TotalCost)

		if !response.TotalRevenue.IsZero() {
			response.MarginPercent = response.Margin.Div(response.TotalRevenue).Mul(decimal.NewFromInt(100))
		}

		if !response.TotalCost.IsZero() {
			response.ROI = response.Margin.Div(response.TotalCost)
			response.ROIPercent = response.ROI.Mul(decimal.NewFromInt(100))
		}
	}

	// Return response even if cost analytics failed, as long as we have revenue or at least attempted both
	return response, nil
}

// buildRevenueRequest builds a revenue analytics request from combined request
func (s *revenueAnalyticsService) buildRevenueRequest(req *dto.GetCostAnalyticsRequest) *dto.GetUsageAnalyticsRequest {
	return &dto.GetUsageAnalyticsRequest{
		ExternalCustomerID: req.ExternalCustomerID,
		FeatureIDs:         req.FeatureIDs,
		StartTime:          req.StartTime,
		EndTime:            req.EndTime,
	}
}

// calculateTotalRevenue calculates total revenue from usage analytics response
func (s *revenueAnalyticsService) calculateTotalRevenue(revenueAnalytics *dto.GetUsageAnalyticsResponse) decimal.Decimal {
	if revenueAnalytics == nil {
		return decimal.Zero
	}

	totalRevenue := decimal.Zero
	for _, item := range revenueAnalytics.Items {
		totalRevenue = totalRevenue.Add(item.TotalCost) // TotalCost in usage analytics represents revenue
	}
	return totalRevenue
}
