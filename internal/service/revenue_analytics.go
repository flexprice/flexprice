package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type RevenueAnalyticsService = interfaces.RevenueAnalyticsService

type revenueAnalyticsService struct {
	ServiceParams
	featureUsageTrackingService   FeatureUsageTrackingService
	costsheetUsageTrackingService CostSheetUsageTrackingService
}

func NewRevenueAnalyticsService(params ServiceParams, featureUsageTrackingService FeatureUsageTrackingService, costsheetUsageTrackingService CostSheetUsageTrackingService) RevenueAnalyticsService {
	return &revenueAnalyticsService{
		ServiceParams:                 params,
		featureUsageTrackingService:   featureUsageTrackingService,
		costsheetUsageTrackingService: costsheetUsageTrackingService,
	}
}

// GetDetailedCostAnalytics retrieves detailed cost analytics with derived metrics
func (s *revenueAnalyticsService) GetDetailedCostAnalytics(
	ctx context.Context,
	req *dto.GetCostAnalyticsRequest,
) (*dto.GetDetailedCostAnalyticsResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid cost analytics request").
			Mark(ierr.ErrValidation)
	}

	// 1. Fetch cost analytics. Behind a feature flag we now compute cost from
	// the meter_usage table instead of the legacy costsheet_usage table.
	var costAnalytics *dto.GetCostAnalyticsResponse
	var err error
	if s.Config.FeatureFlag.IsMeterUsageEnabledForAnalytics(types.GetTenantID(ctx)) {
		costAnalytics, err = s.costsheetUsageTrackingService.GetCostAnalyticsFromMeterUsage(ctx, req)
	} else {
		costAnalytics, err = s.costsheetUsageTrackingService.GetCostSheetUsageAnalytics(ctx, req)
	}
	if err != nil {
		s.Logger.Warnw("failed to fetch cost analytics", "error", err)
		costAnalytics = nil
	}

	// 2. Fetch revenue analytics from feature/meter usage tracking
	var revenueAnalytics *dto.GetUsageAnalyticsResponse
	revenueReq := &dto.GetUsageAnalyticsRequest{
		ExternalCustomerID: req.ExternalCustomerID,
		FeatureIDs:         req.FeatureIDs,
		StartTime:          req.StartTime,
		EndTime:            req.EndTime,
	}
	if s.Config.FeatureFlag.IsMeterUsageEnabledForAnalytics(types.GetTenantID(ctx)) {
		meterUsageService := NewMeterUsageService(s.ServiceParams)
		revenueAnalytics, err = meterUsageService.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerID:  revenueReq.ExternalCustomerID,
			ExternalCustomerIDs: revenueReq.ExternalCustomerIDs,
			FeatureIDs:          revenueReq.FeatureIDs,
			StartTime:           revenueReq.StartTime,
			EndTime:             revenueReq.EndTime,
			GroupBy:             revenueReq.GroupBy,
			PropertyFilters:     revenueReq.PropertyFilters,
			Sources:             revenueReq.Sources,
			WindowSize:          revenueReq.WindowSize,
			Expand:              revenueReq.Expand,
			IncludeChildren:     revenueReq.IncludeChildren,
		})
	} else {
		revenueAnalytics, err = s.featureUsageTrackingService.GetDetailedUsageAnalyticsV2(ctx, revenueReq)
	}
	if err != nil {
		s.Logger.Warnw("failed to fetch revenue analytics", "error", err)
		revenueAnalytics = nil
	}

	// 3. Build response with derived metrics
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

	// Calculate total revenue from revenue analytics
	if revenueAnalytics != nil {
		for _, item := range revenueAnalytics.Items {
			response.TotalRevenue = response.TotalRevenue.Add(item.TotalCost) // TotalCost in usage analytics represents revenue
		}
		if revenueAnalytics.Currency != "" && costAnalytics == nil {
			// Use revenue currency if cost analytics is not available
			response.Currency = revenueAnalytics.Currency
		}
	}

	// Calculate derived metrics if both cost and revenue are available
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

	// Return response even if cost or revenue analytics failed
	return response, nil
}
