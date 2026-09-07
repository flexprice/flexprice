package service

import (
	"context"
	"math"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/expression"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CostSheetUsageTrackingService handles cost sheet usage tracking operations for metered events
type CostSheetUsageTrackingService interface {
	// Get Analytics computed from meter usage (replaces the costsheet_usage read path)
	GetCostAnalyticsFromMeterUsage(ctx context.Context, req *dto.GetCostAnalyticsRequest) (*dto.GetCostAnalyticsResponse, error)
}

type costsheetUsageTrackingService struct {
	ServiceParams
	eventRepo           events.Repository
	costUsageRepo       events.CostSheetUsageRepository
	expressionEvaluator expression.Evaluator
}

// NewCostSheetUsageTrackingService creates a new cost sheet usage tracking service
func NewCostSheetUsageTrackingService(
	params ServiceParams,
	eventRepo events.Repository,
	costUsageRepo events.CostSheetUsageRepository,
) CostSheetUsageTrackingService {
	ev := &costsheetUsageTrackingService{
		ServiceParams:       params,
		eventRepo:           eventRepo,
		costUsageRepo:       costUsageRepo,
		expressionEvaluator: expression.NewCELEvaluator(),
	}
	return ev
}

// GetCostAnalyticsFromMeterUsage computes cost analytics by combining the
// active costsheet's prices with raw quantities pulled from the meter_usage
// pipeline. It replaces the previous read path against the costsheet_usage
// ClickHouse table (which was populated by a Kafka consumer that has now been
// disabled).
//
// Behavior:
//   - Resolves the active costsheet for the tenant (cached, 5m TTL).
//   - Collects the meters referenced by the costsheet's usage-based prices.
//   - Calls MeterUsageService.GetDetailedAnalytics scoped to those meters to
//     get aggregated quantities per (meter, source, properties).
//   - Multiplies each quantity by its costsheet price using PriceService.
func (s *costsheetUsageTrackingService) GetCostAnalyticsFromMeterUsage(
	ctx context.Context,
	req *dto.GetCostAnalyticsRequest,
) (*dto.GetCostAnalyticsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// STEP1: Resolve active costsheet (cached).
	costSheet, err := s.getActiveCostsheetWithCache(ctx)
	if err != nil {
		return nil, err
	}
	if costSheet == nil {
		return nil, ierr.NewError("no active costsheet found").
			WithHint("Please activate a costsheet for this tenant").
			Mark(ierr.ErrNotFound)
	}

	// STEP2: Optional customer lookup.
	var cust *customer.Customer
	if req.ExternalCustomerID != "" {
		cust, err = s.CustomerRepo.GetByLookupKey(ctx, req.ExternalCustomerID)
		if err != nil {
			return nil, err
		}
		if cust == nil {
			return nil, ierr.NewError("customer not found").
				WithHint("No customer found with the provided external_customer_id").
				WithReportableDetails(map[string]interface{}{
					"external_customer_id": req.ExternalCustomerID,
				}).
				Mark(ierr.ErrNotFound)
		}
	}

	// STEP3: Walk the costsheet's prices to get meter IDs and a meter->price
	// map. Costsheet prices are scoped via PRICE_ENTITY_TYPE_COSTSHEET, which
	// is distinct from subscription prices — i.e. these are the cost rates,
	// not the customer-facing rates.
	meterToPrice := make(map[string]*price.Price)
	meterIDs := make([]string, 0)
	for _, p := range costSheet.Prices {
		if p == nil || p.Price == nil || !p.IsUsage() {
			continue
		}
		if p.MeterID == "" {
			continue
		}
		if _, seen := meterToPrice[p.MeterID]; seen {
			continue
		}
		meterToPrice[p.MeterID] = p.Price
		meterIDs = append(meterIDs, p.MeterID)
	}

	if len(meterIDs) == 0 {
		// No usage-based pricing in the costsheet — return an empty response.
		return s.emptyCostAnalyticsResponse(req, costSheet, cust), nil
	}

	// STEP4: Resolve meter metadata in bulk so we know how to interpret the
	// usage value (regular vs bucketed) when calling PriceService.
	meters, err := s.MeterRepo.ListByIDs(ctx, meterIDs)
	if err != nil {
		return nil, err
	}
	meterMap := make(map[string]*meter.Meter, len(meters))
	for _, m := range meters {
		meterMap[m.ID] = m
	}

	// STEP5: Query raw meter usage (no subscription context). This goes
	// through MeterUsageService so we get analytics-shaped results that we can
	// then re-cost using costsheet prices.
	meterUsageService := NewMeterUsageService(s.ServiceParams)
	analyticsParams := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:        types.GetTenantID(ctx),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		MeterIDs:        meterIDs,
		FeatureIDs:      req.FeatureIDs,
		StartTime:       req.StartTime,
		EndTime:         req.EndTime,
		IncludeChildren: req.IncludeChildren,
		// Group by meter_id so we get one row per meter (matches how the old
		// costsheet_usage path keyed its results).
		GroupBy:         []string{"meter_id"},
		PropertyFilters: req.PropertyFilters,
	}
	if cust != nil {
		analyticsParams.ExternalCustomerID = cust.ExternalID
	}

	usageResp, err := meterUsageService.GetDetailedAnalytics(ctx, analyticsParams)
	if err != nil {
		return nil, err
	}

	// STEP6: Re-cost each item using the costsheet price for its meter.
	priceService := NewPriceService(s.ServiceParams)
	response := &dto.GetCostAnalyticsResponse{
		CostsheetID:   costSheet.ID,
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
		TotalCost:     decimal.Zero,
		TotalQuantity: decimal.Zero,
		TotalEvents:   0,
		CostAnalytics: make([]dto.CostAnalyticItem, 0, len(usageResp.Items)),
	}
	if cust != nil {
		response.CustomerID = cust.ID
		response.ExternalCustomerID = cust.ExternalID
	}

	for _, item := range usageResp.Items {
		csPrice, ok := meterToPrice[item.MeterID]
		if !ok {
			// Meter is present in usage results but not in the costsheet —
			// skip; nothing to cost it against.
			continue
		}
		m := meterMap[item.MeterID]

		itemEventCount, err := safeInt64FromEventCount(item.EventCount)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Event count exceeds supported range").
				Mark(ierr.ErrValidation)
		}

		// Compute the cost using PriceService. Bucketed meters need
		// CalculateBucketedCost (single-bucket fallback when there are no
		// time-series points); standard meters use CalculateCost on the
		// total usage.
		var totalCost decimal.Decimal
		if m != nil && (priceDomain.IsBucketedMax(nil, m) || priceDomain.IsBucketedSum(nil, m)) {
			if len(item.Points) > 0 {
				bucketed := make([]decimal.Decimal, len(item.Points))
				for i, pt := range item.Points {
					bucketed[i] = pt.Usage
				}
				totalCost = priceService.CalculateBucketedCost(ctx, csPrice, bucketed)
			} else if item.TotalUsage.IsPositive() {
				totalCost = priceService.CalculateBucketedCost(ctx, csPrice, []decimal.Decimal{item.TotalUsage})
			}
		} else {
			totalCost = priceService.CalculateCost(ctx, csPrice, item.TotalUsage)
		}

		costItem := dto.CostAnalyticItem{
			MeterID:       item.MeterID,
			MeterName:     item.EventName,
			Source:        item.Source,
			Properties:    item.Properties,
			TotalCost:     totalCost,
			TotalQuantity: item.TotalUsage,
			TotalEvents:   itemEventCount, // #nosec G115 -- bounded above before cast, see safeInt64FromEventCount
			Currency:      csPrice.Currency,
			PriceID:       csPrice.ID,
			CostsheetID:   costSheet.ID,
		}
		if cust != nil {
			costItem.CustomerID = cust.ID
			costItem.ExternalCustomerID = cust.ExternalID
		}
		if len(item.Points) > 0 {
			costItem.CostByPeriod = make([]dto.CostPoint, 0, len(item.Points))
			for _, pt := range item.Points {
				pointEventCount, err := safeInt64FromEventCount(pt.EventCount)
				if err != nil {
					return nil, ierr.WithError(err).
						WithHint("Event count exceeds supported range").
						Mark(ierr.ErrValidation)
				}
				pointCost := priceService.CalculateCost(ctx, csPrice, pt.Usage)
				costItem.CostByPeriod = append(costItem.CostByPeriod, dto.CostPoint{
					Timestamp:  pt.Timestamp,
					Cost:       pointCost,
					Quantity:   pt.Usage,
					EventCount: pointEventCount, // #nosec G115 -- bounded above before cast, see safeInt64FromEventCount
				})
			}
		}

		response.CostAnalytics = append(response.CostAnalytics, costItem)
		response.TotalCost = response.TotalCost.Add(totalCost)
		response.TotalQuantity = response.TotalQuantity.Add(item.TotalUsage)
		response.TotalEvents += itemEventCount // #nosec G115 -- bounded above before cast, see safeInt64FromEventCount
		if response.Currency == "" && csPrice.Currency != "" {
			response.Currency = csPrice.Currency
		}
	}

	return response, nil
}

// safeInt64FromEventCount converts a ClickHouse COUNT(DISTINCT ...) result to
// int64, rejecting values above math.MaxInt64 instead of silently wrapping.
func safeInt64FromEventCount(ec uint64) (int64, error) {
	if ec > math.MaxInt64 {
		return 0, ierr.NewError("event count exceeds int64 range").
			WithHint("Event count exceeds int64 range").
			WithReportableDetails(map[string]any{"event_count": ec}).
			Mark(ierr.ErrValidation)
	}
	return int64(ec), nil
}

// emptyCostAnalyticsResponse returns a zero-valued cost response — used when
// the active costsheet has no usage-based prices, so there's nothing to cost.
func (s *costsheetUsageTrackingService) emptyCostAnalyticsResponse(
	req *dto.GetCostAnalyticsRequest,
	costSheet *dto.CostsheetResponse,
	cust *customer.Customer,
) *dto.GetCostAnalyticsResponse {
	resp := &dto.GetCostAnalyticsResponse{
		CostsheetID:   costSheet.ID,
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
		TotalCost:     decimal.Zero,
		TotalQuantity: decimal.Zero,
		TotalEvents:   0,
		CostAnalytics: []dto.CostAnalyticItem{},
	}
	if cust != nil {
		resp.CustomerID = cust.ID
		resp.ExternalCustomerID = cust.ExternalID
	}
	return resp
}

// getActiveCostsheetWithCache fetches the active costsheet for the tenant with caching
// It first checks the cache, and if not found, fetches from the service and caches the result
func (s *costsheetUsageTrackingService) getActiveCostsheetWithCache(ctx context.Context) (*dto.CostsheetResponse, error) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(ctx, cache.PrefixCostsheet)

	// Try to get from cache first
	cacheClient := cache.GetInMemoryCache()
	var costSheet *dto.CostsheetResponse
	if cached, found := cacheClient.ForceCacheGet(ctx, cacheKey); found {
		if cachedCostSheet, ok := cached.(*dto.CostsheetResponse); ok {
			s.Logger.Debug(ctx, "costsheet cache hit", "tenant_id", tenantID, "environment_id", environmentID)
			costSheet = cachedCostSheet
		}
	}

	// If not in cache, fetch from service
	if costSheet == nil {
		costSheetService := NewCostsheetService(s.ServiceParams)
		var err error
		costSheet, err = costSheetService.GetActiveCostsheetForTenant(ctx)
		if err != nil {
			return nil, err
		}

		// Cache the result for 5 minutes
		cacheClient.ForceCacheSet(ctx, cacheKey, costSheet, 5*time.Minute)
		s.Logger.Debug(ctx, "costsheet cached", "tenant_id", tenantID, "environment_id", environmentID)
	}

	return costSheet, nil
}
