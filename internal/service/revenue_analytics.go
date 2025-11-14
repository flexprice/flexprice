package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type RevenueAnalyticsService = interfaces.RevenueAnalyticsService

type revenueAnalyticsService struct {
	ServiceParams
	featureUsageTrackingService FeatureUsageTrackingService
	costsheetService            CostsheetService
}

func NewRevenueAnalyticsService(params ServiceParams, featureUsageTrackingService FeatureUsageTrackingService, costsheetService CostsheetService) RevenueAnalyticsService {
	return &revenueAnalyticsService{
		ServiceParams:               params,
		featureUsageTrackingService: featureUsageTrackingService,
		costsheetService:            costsheetService,
	}
}

// GetCostAnalytics retrieves cost analytics following the GetUsageBySubscription pattern
func (s *revenueAnalyticsService) getCostAnalytics(
	ctx context.Context,
	costsheetID string,
	req *dto.GetCostAnalyticsRequest,
) (*dto.GetCostAnalyticsResponse, error) {
	// 1. Validate request
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid cost analytics request").
			Mark(ierr.ErrValidation)
	}

	// 2. Fetch costsheet and associated prices (like subscription line items)
	prices, err := s.fetchCostsheetPrices(ctx, costsheetID)
	if err != nil {
		return nil, err
	}

	if len(prices) == 0 {
		s.Logger.Warnw("no prices found for costsheet", "costsheet_id", costsheetID)
		return s.buildEmptyResponse(costsheetID, req), nil
	}

	priceMap := make(map[string]*price.Price)
	for _, price := range prices {
		priceMap[price.ID] = price
	}

	meters, err := s.fetchCostsheetMeters(ctx, prices)
	if err != nil {
		return nil, err
	}

	if len(meters) == 0 {
		s.Logger.Warnw("no meters found for costsheet", "costsheet_id", costsheetID)
		return s.buildEmptyResponse(costsheetID, req), nil
	}

	// 4. Build meter usage requests for each customer-price combination
	meterUsageRequests := s.buildMeterUsageRequests(prices, req)

	if len(meterUsageRequests) == 0 {
		s.Logger.Warnw("no meter usage requests generated")
		return s.buildEmptyResponse(costsheetID, req), nil
	}

	// 5. Use existing BulkGetUsageByMeter (same as subscription billing)
	eventService := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger, s.Config)
	usageMap, err := eventService.BulkGetUsageByMeter(ctx, meterUsageRequests)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get usage data").
			Mark(ierr.ErrInternal)
	}

	// 6. Calculate costs using price service (same logic as subscription)
	costAnalytics := s.calculateCostsFromUsage(ctx, usageMap, prices, meters, req)

	// 7. Build response with prefetched data
	return s.buildResponse(costsheetID, req, costAnalytics, meters, priceMap), nil
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
		costAnalytics, err = s.getCostAnalytics(ctx, costsheet.ID, req)
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

// fetchCostsheetPrices fetches prices associated with a costsheet
func (s *revenueAnalyticsService) fetchCostsheetPrices(ctx context.Context, costsheetID string) ([]*price.Price, error) {
	if costsheetID == "" {
		// If no costsheet specified, we could fetch all prices, but for now return empty
		return []*price.Price{}, nil
	}

	priceService := NewPriceService(s.ServiceParams)
	priceFilter := types.NewNoLimitPriceFilter()
	priceFilter.EntityIDs = []string{costsheetID}
	priceFilter.EntityType = lo.ToPtr(types.PRICE_ENTITY_TYPE_COSTSHEET)
	priceFilter.Status = lo.ToPtr(types.StatusPublished)
	priceFilter.Expand = lo.ToPtr(string(types.ExpandMeters))

	pricesResponse, err := priceService.GetPrices(ctx, priceFilter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch costsheet prices").
			Mark(ierr.ErrDatabase)
	}

	// Convert to domain models
	prices := make([]*price.Price, len(pricesResponse.Items))
	for i, priceResp := range pricesResponse.Items {
		prices[i] = priceResp.Price
	}

	return prices, nil
}

// fetchCostsheetPrices fetches prices associated with a costsheet
func (s *revenueAnalyticsService) fetchCostsheetMeters(ctx context.Context, prices []*price.Price) (map[string]*meter.Meter, error) {
	meterIDs := make([]string, 0)
	meterIDSet := make(map[string]bool)

	for _, price := range prices {
		if price.MeterID != "" && !meterIDSet[price.MeterID] {
			meterIDs = append(meterIDs, price.MeterID)
			meterIDSet[price.MeterID] = true
		}
	}

	if len(meterIDs) == 0 {
		return make(map[string]*meter.Meter), nil
	}

	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.MeterIDs = meterIDs
	meterList, err := s.MeterRepo.List(ctx, meterFilter)
	if err != nil {
		return nil, err
	}

	// Convert to map for quick lookup
	meters := make(map[string]*meter.Meter, len(meterList))
	for _, m := range meterList {
		meters[m.ID] = m
	}

	return meters, nil
}

// buildMeterUsageRequests creates usage requests for each customer-price combination
func (s *revenueAnalyticsService) buildMeterUsageRequests(
	prices []*price.Price,
	req *dto.GetCostAnalyticsRequest,
) []*dto.GetUsageByMeterRequest {
	var meterUsageRequests []*dto.GetUsageByMeterRequest

	// For each customer and each price, create a usage request

	for _, price := range prices {
		// Skip if meter ID is not set
		if price.MeterID == "" {
			continue
		}

		usageRequest := &dto.GetUsageByMeterRequest{
			MeterID:            price.MeterID,
			PriceID:            price.ID,
			ExternalCustomerID: req.ExternalCustomerID,
			StartTime:          req.StartTime,
			EndTime:            req.EndTime,
			Filters:            make(map[string][]string),
		}

		meterUsageRequests = append(meterUsageRequests, usageRequest)
	}

	return meterUsageRequests
}

// calculateCostsFromUsage processes usage results and calculates costs
func (s *revenueAnalyticsService) calculateCostsFromUsage(
	ctx context.Context,
	usageMap map[string]*events.AggregationResult,
	prices []*price.Price,
	meters map[string]*meter.Meter,
	req *dto.GetCostAnalyticsRequest,
) []dto.CostAnalyticItem {
	priceService := NewPriceService(s.ServiceParams)
	var costAnalytics []dto.CostAnalyticItem

	// Build price map for quick lookup
	priceMap := make(map[string]*price.Price)
	for _, p := range prices {
		priceMap[p.ID] = p
	}

	// Process each usage result
	for priceID, usage := range usageMap {
		price, exists := priceMap[priceID]
		if !exists {
			continue
		}

		// Calculate cost (same logic as GetUsageBySubscription)
		var cost decimal.Decimal
		var quantity decimal.Decimal

		// Handle bucketed max meters
		if usage.MeterID != "" {
			meter, exists := meters[usage.MeterID]
			if !exists {
				continue
			}
			if meter.IsBucketedMaxMeter() {
				// For bucketed max, use array of values
				bucketedValues := make([]decimal.Decimal, len(usage.Results))
				for i, result := range usage.Results {
					bucketedValues[i] = result.Value
				}
				cost = priceService.CalculateBucketedCost(ctx, price, bucketedValues)

				// Calculate quantity as sum of all bucket maxes
				quantity = decimal.Zero
				for _, bucketValue := range bucketedValues {
					quantity = quantity.Add(bucketValue)
				}
			} else {
				// For all other cases, use single value
				quantity = usage.Value
				cost = priceService.CalculateCost(ctx, price, quantity)
			}
		} else {
			quantity = usage.Value
			cost = priceService.CalculateCost(ctx, price, quantity)
		}

		costAnalytic := dto.CostAnalyticItem{
			MeterID:            usage.MeterID,
			MeterName:          "", // Will be enriched later
			Source:             "", // Can be extracted from usage if needed
			CustomerID:         req.ExternalCustomerID,
			ExternalCustomerID: req.ExternalCustomerID,
			TotalCost:          cost,
			TotalQuantity:      quantity,
			TotalEvents:        int64(len(usage.Results)),
			Currency:           price.Currency,
			PriceID:            price.ID,
			CostsheetID:        price.EntityID, // Assuming price is linked to costsheet
		}

		costAnalytics = append(costAnalytics, costAnalytic)
	}

	return costAnalytics
}

// buildResponse builds the final cost analytics response
func (s *revenueAnalyticsService) buildResponse(
	costsheetID string,
	req *dto.GetCostAnalyticsRequest,
	costAnalytics []dto.CostAnalyticItem,
	meters map[string]*meter.Meter,
	prices map[string]*price.Price,
) *dto.GetCostAnalyticsResponse {
	// Apply expansion if requested
	if len(req.Expand) > 0 {
		costAnalytics = s.expandCostAnalyticsSimple(req, costAnalytics, meters, prices)
	}

	response := &dto.GetCostAnalyticsResponse{
		CostsheetID:   costsheetID,
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
		CostAnalytics: costAnalytics,
		TotalCost:     decimal.Zero,
		TotalQuantity: decimal.Zero,
		TotalEvents:   0,
	}

	// Set customer info if single customer
	if req.ExternalCustomerID != "" {
		response.ExternalCustomerID = req.ExternalCustomerID
	}

	// Calculate totals
	var currency string
	for _, item := range costAnalytics {
		response.TotalCost = response.TotalCost.Add(item.TotalCost)
		response.TotalQuantity = response.TotalQuantity.Add(item.TotalQuantity)
		response.TotalEvents += item.TotalEvents
		if currency == "" {
			currency = item.Currency
		}
	}
	response.Currency = currency

	return response
}

// buildEmptyResponse builds an empty response for cases with no data
func (s *revenueAnalyticsService) buildEmptyResponse(costsheetID string, req *dto.GetCostAnalyticsRequest) *dto.GetCostAnalyticsResponse {
	return &dto.GetCostAnalyticsResponse{
		CostsheetID:        costsheetID,
		ExternalCustomerID: req.ExternalCustomerID,
		StartTime:          req.StartTime,
		EndTime:            req.EndTime,
		Currency:           "USD", // Default currency
		TotalCost:          decimal.Zero,
		TotalQuantity:      decimal.Zero,
		TotalEvents:        0,
		CostAnalytics:      []dto.CostAnalyticItem{},
	}
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

// expandCostAnalyticsSimple expands meter and price data for cost analytics items using batch fetching and domain types
func (s *revenueAnalyticsService) expandCostAnalyticsSimple(
	req *dto.GetCostAnalyticsRequest,
	costAnalytics []dto.CostAnalyticItem,
	meters map[string]*meter.Meter,
	prices map[string]*price.Price,
) []dto.CostAnalyticItem {

	// Expand the cost analytics items - use domain types directly
	expandedItems := make([]dto.CostAnalyticItem, len(costAnalytics))
	for i, item := range costAnalytics {
		expandedItems[i] = item

		// Expand meter data
		if lo.Contains(req.Expand, "meter") && meters != nil && item.MeterID != "" {
			if m, exists := meters[item.MeterID]; exists {
				expandedItems[i].Meter = m
			}
		}

		// Expand price data
		if lo.Contains(req.Expand, "price") && prices != nil && item.PriceID != "" {
			if p, exists := prices[item.PriceID]; exists {
				expandedItems[i].Price = p
			}
		}
	}

	return expandedItems
}
