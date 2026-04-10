package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// MeterUsageService handles read-side meter usage queries.
// This sits between the API handler and MeterUsageRepository,
// keeping the handler HTTP/DTO-only.
type MeterUsageService interface {
	GetUsage(ctx context.Context, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error)
	GetUsageMultiMeter(ctx context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error)
	GetDetailedAnalytics(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams) (*dto.GetUsageAnalyticsResponse, error)
}

type meterUsageService struct {
	ServiceParams
	repo                        events.MeterUsageRepository
	featureUsageTrackingService FeatureUsageTrackingService
	logger                      *logger.Logger
}

func NewMeterUsageService(params ServiceParams, featureUsageTrackingService FeatureUsageTrackingService) MeterUsageService {
	return &meterUsageService{
		ServiceParams:               params,
		repo:                        params.MeterUsageRepo,
		featureUsageTrackingService: featureUsageTrackingService,
		logger:                      params.Logger,
	}
}

func (s *meterUsageService) GetUsage(ctx context.Context, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}
	return s.repo.GetUsage(ctx, params)
}

func (s *meterUsageService) GetUsageMultiMeter(ctx context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error) {
	if params == nil || len(params.MeterIDs) == 0 {
		return nil, ierr.NewError("params with meter_ids are required").Mark(ierr.ErrValidation)
	}
	return s.repo.GetUsageMultiMeter(ctx, params)
}

func (s *meterUsageService) GetDetailedAnalytics(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams) (*dto.GetUsageAnalyticsResponse, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	// Set defaults
	if params.EndTime.IsZero() {
		params.EndTime = time.Now().UTC()
	}
	if params.StartTime.IsZero() {
		params.StartTime = params.EndTime.Add(-6 * time.Hour)
	}
	// Fetch meter configs to identify bucketed meters
	meters, err := s.fetchMeters(ctx, params)
	if err != nil {
		return nil, err
	}

	// Build meter map and collect aggregation types
	meterMap := make(map[string]*meter.Meter, len(meters))
	var aggTypes []types.AggregationType
	for _, m := range meters {
		meterMap[m.ID] = m
		if m.Aggregation.Type != "" {
			aggTypes = append(aggTypes, m.Aggregation.Type)
		}
	}

	// Set aggregation types from meters if not explicitly provided
	if len(params.AggregationTypes) == 0 {
		params.AggregationTypes = lo.Uniq(aggTypes)
	}

	// Split meters into bucketed and standard
	var bucketedMaxMeterIDs, bucketedSumMeterIDs, standardMeterIDs []string
	for _, m := range meters {
		switch {
		case m.IsBucketedMaxMeter():
			bucketedMaxMeterIDs = append(bucketedMaxMeterIDs, m.ID)
		case m.IsBucketedSumMeter():
			bucketedSumMeterIDs = append(bucketedSumMeterIDs, m.ID)
		default:
			standardMeterIDs = append(standardMeterIDs, m.ID)
		}
	}

	var allResults []*events.MeterUsageDetailedResult

	// Process bucketed MAX meters
	for _, meterID := range bucketedMaxMeterIDs {
		m := meterMap[meterID]
		results, err := s.getBucketedMeterAnalytics(ctx, params, m)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
	}

	// Process bucketed SUM meters
	for _, meterID := range bucketedSumMeterIDs {
		m := meterMap[meterID]
		results, err := s.getBucketedMeterAnalytics(ctx, params, m)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
	}

	// Process standard meters via the detailed analytics query
	if len(standardMeterIDs) > 0 || len(params.MeterIDs) == 0 {
		standardParams := *params
		if len(standardMeterIDs) > 0 {
			standardParams.MeterIDs = standardMeterIDs
		}
		// Ensure meter_id is in group_by when querying multiple meters so results don't collapse
		if len(standardParams.MeterIDs) > 1 && !lo.Contains(standardParams.GroupBy, "meter_id") {
			standardParams.GroupBy = append([]string{"meter_id"}, standardParams.GroupBy...)
		}
		results, err := s.repo.GetDetailedAnalytics(ctx, &standardParams)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
	}

	// Build analytics data with subscription/pricing context and calculate costs
	return s.buildAnalyticsResponse(ctx, allResults, meterMap, params)
}

// buildAnalyticsResponse converts MeterUsageDetailedResult items to GetUsageAnalyticsResponse,
// enriching with meter metadata and calculating costs via the feature usage cost pipeline.
func (s *meterUsageService) buildAnalyticsResponse(
	ctx context.Context,
	results []*events.MeterUsageDetailedResult,
	meterMap map[string]*meter.Meter,
	params *events.MeterUsageDetailedAnalyticsParams,
) (*dto.GetUsageAnalyticsResponse, error) {
	// Build AnalyticsData with subscription/pricing context for cost calculation
	data, err := s.buildAnalyticsData(ctx, results, meterMap, params)
	if err != nil {
		return nil, err
	}

	// Calculate costs using the feature usage pipeline (handles all aggregation types, bucketed, commitments)
	if len(data.Analytics) > 0 && s.featureUsageTrackingService != nil {
		if err := s.featureUsageTrackingService.CalculateCostsForAnalytics(ctx, data); err != nil {
			s.logger.Warnw("failed to calculate costs for meter usage analytics, costs will be zero",
				"error", err,
			)
		}
	}

	// Set currency on all analytics items
	if data.Currency != "" {
		for _, item := range data.Analytics {
			item.Currency = data.Currency
		}
	}

	// Convert to response DTO
	return s.toUsageAnalyticsResponseDTO(data, meterMap, params), nil
}

// buildAnalyticsData constructs the AnalyticsData structure required by the cost calculation pipeline.
// It resolves customer → subscriptions → line items → prices, and converts meter usage results
// to DetailedUsageAnalytic objects.
func (s *meterUsageService) buildAnalyticsData(
	ctx context.Context,
	results []*events.MeterUsageDetailedResult,
	meterMap map[string]*meter.Meter,
	params *events.MeterUsageDetailedAnalyticsParams,
) (*AnalyticsData, error) {
	data := &AnalyticsData{
		SubscriptionLineItems: make(map[string]*subscription.SubscriptionLineItem),
		SubscriptionsMap:      make(map[string]*subscription.Subscription),
		Features:              make(map[string]*feature.Feature),
		Meters:                meterMap,
		Prices:                make(map[string]*price.Price),
		Plans:                 make(map[string]*plan.Plan),
		Analytics:             make([]*events.DetailedUsageAnalytic, 0, len(results)),
		Params: &events.UsageAnalyticsParams{
			TenantID:        params.TenantID,
			EnvironmentID:   params.EnvironmentID,
			StartTime:       params.StartTime,
			EndTime:         params.EndTime,
			GroupBy:         params.GroupBy,
			WindowSize:      params.WindowSize,
			PropertyFilters: params.PropertyFilters,
		},
	}

	// Resolve customer and subscriptions for cost calculation
	customer, subscriptions, err := s.resolveCustomerAndSubscriptions(ctx, params.ExternalCustomerID)
	if err != nil {
		// Continue without cost data rather than failing
		s.logger.Warnw("failed to resolve customer/subscriptions for cost calculation",
			"error", err,
			"external_customer_id", params.ExternalCustomerID,
		)
	} else {
		data.Customer = customer
		data.Subscriptions = subscriptions

		// Validate currency
		if len(subscriptions) > 0 {
			data.Currency = subscriptions[0].Currency
		}

		// Build subscription and line item maps
		for _, sub := range subscriptions {
			data.SubscriptionsMap[sub.ID] = sub
			for _, li := range sub.LineItems {
				data.SubscriptionLineItems[li.ID] = li
			}
		}
	}

	// Build meter_id → (line_item, price) mapping from subscriptions
	meterToLineItem := make(map[string]*subscription.SubscriptionLineItem)
	meterToPrice := make(map[string]*price.Price)
	if len(data.SubscriptionLineItems) > 0 {
		// Collect all price IDs
		priceIDs := make(map[string]bool)
		for _, li := range data.SubscriptionLineItems {
			if li.MeterID != "" && li.PriceID != "" {
				priceIDs[li.PriceID] = true
			}
		}

		// Fetch prices in batch
		if len(priceIDs) > 0 {
			priceFilter := types.NewNoLimitPriceFilter()
			priceFilter.PriceIDs = lo.Keys(priceIDs)
			prices, err := s.PriceRepo.List(ctx, priceFilter)
			if err != nil {
				s.logger.Warnw("failed to fetch prices for cost calculation", "error", err)
			} else {
				for _, p := range prices {
					data.Prices[p.ID] = p
				}
			}
		}

		// Map meter_id → first matching line item and price
		for _, li := range data.SubscriptionLineItems {
			if li.MeterID == "" || li.PriceID == "" {
				continue
			}
			if _, exists := meterToLineItem[li.MeterID]; exists {
				continue
			}
			meterToLineItem[li.MeterID] = li
			if p, ok := data.Prices[li.PriceID]; ok {
				meterToPrice[li.MeterID] = p
			}
		}
	}

	// Resolve meter_id → feature_id mapping
	meterToFeature := make(map[string]*feature.Feature)
	meterIDs := lo.Keys(meterMap)
	if len(meterIDs) > 0 {
		featureFilter := types.NewNoLimitFeatureFilter()
		featureFilter.MeterIDs = meterIDs
		features, err := s.FeatureRepo.List(ctx, featureFilter)
		if err != nil {
			s.logger.Warnw("failed to fetch features for meter mapping", "error", err)
		} else {
			for _, f := range features {
				if f.MeterID != "" {
					meterToFeature[f.MeterID] = f
					data.Features[f.ID] = f
				}
			}
		}
	}

	// Convert MeterUsageDetailedResult → DetailedUsageAnalytic
	for _, r := range results {
		analytic := &events.DetailedUsageAnalytic{
			MeterID:          r.MeterID,
			TotalUsage:       r.TotalUsage,
			MaxUsage:         r.MaxUsage,
			LatestUsage:      r.LatestUsage,
			CountUniqueUsage: r.CountUniqueUsage,
			EventCount:       r.EventCount,
			Source:           r.Source,
			Sources:          r.Sources,
			Properties:       r.Properties,
		}

		// Set meter metadata
		if m, ok := meterMap[r.MeterID]; ok {
			analytic.EventName = m.EventName
			analytic.AggregationType = m.Aggregation.Type
		}

		// Set feature info
		if f, ok := meterToFeature[r.MeterID]; ok {
			analytic.FeatureID = f.ID
			analytic.FeatureName = f.Name
			analytic.Unit = f.UnitSingular
			analytic.UnitPlural = f.UnitPlural
		}

		// Set subscription/pricing info
		if li, ok := meterToLineItem[r.MeterID]; ok {
			analytic.PriceID = li.PriceID
			analytic.SubLineItemID = li.ID
			analytic.SubscriptionID = li.SubscriptionID
		}

		// Convert points
		if len(r.Points) > 0 {
			analytic.Points = make([]events.UsageAnalyticPoint, 0, len(r.Points))
			for _, p := range r.Points {
				analytic.Points = append(analytic.Points, events.UsageAnalyticPoint{
					Timestamp:        p.WindowStart,
					WindowStart:      p.WindowStart,
					Usage:            p.TotalUsage,
					MaxUsage:         p.MaxUsage,
					LatestUsage:      p.LatestUsage,
					CountUniqueUsage: p.CountUniqueUsage,
					EventCount:       p.EventCount,
				})
			}
		}

		data.Analytics = append(data.Analytics, analytic)
	}

	return data, nil
}

// resolveCustomerAndSubscriptions fetches the internal customer and their active subscriptions.
func (s *meterUsageService) resolveCustomerAndSubscriptions(ctx context.Context, externalCustomerID string) (*customer.Customer, []*subscription.Subscription, error) {
	cust, err := s.CustomerRepo.GetByLookupKey(ctx, externalCustomerID)
	if err != nil {
		return nil, nil, err
	}

	subService := NewSubscriptionService(s.ServiceParams)
	filter := types.NewSubscriptionFilter()
	filter.CustomerID = cust.ID
	filter.WithLineItems = true
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusPaused,
		types.SubscriptionStatusCancelled,
	}

	subsList, err := subService.ListSubscriptions(ctx, filter)
	if err != nil {
		return cust, nil, err
	}

	subscriptions := make([]*subscription.Subscription, len(subsList.Items))
	for i, subResp := range subsList.Items {
		subscriptions[i] = subResp.Subscription
	}

	return cust, subscriptions, nil
}

// toUsageAnalyticsResponseDTO converts the enriched analytics data to the response DTO.
func (s *meterUsageService) toUsageAnalyticsResponseDTO(
	data *AnalyticsData,
	meterMap map[string]*meter.Meter,
	params *events.MeterUsageDetailedAnalyticsParams,
) *dto.GetUsageAnalyticsResponse {
	response := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.Zero,
		Currency:  data.Currency,
		Items:     make([]dto.UsageAnalyticItem, 0, len(data.Analytics)),
	}

	for _, analytic := range data.Analytics {
		// Use correct usage value based on aggregation type
		totalUsage := getCorrectMeterUsageValue(analytic)

		item := dto.UsageAnalyticItem{
			FeatureID:       analytic.FeatureID,
			PriceID:         analytic.PriceID,
			MeterID:         analytic.MeterID,
			SubLineItemID:   analytic.SubLineItemID,
			SubscriptionID:  analytic.SubscriptionID,
			FeatureName:     analytic.FeatureName,
			EventName:       analytic.EventName,
			Source:          analytic.Source,
			Sources:         analytic.Sources,
			Unit:            analytic.Unit,
			UnitPlural:      analytic.UnitPlural,
			AggregationType: analytic.AggregationType,
			TotalUsage:      totalUsage,
			TotalCost:       analytic.TotalCost,
			Currency:        analytic.Currency,
			EventCount:      analytic.EventCount,
			Properties:      analytic.Properties,
			CommitmentInfo:  analytic.CommitmentInfo,
			Points:          make([]dto.UsageAnalyticPoint, 0, len(analytic.Points)),
		}

		// If feature has no name, use meter name
		if item.FeatureName == "" {
			if m, ok := meterMap[analytic.MeterID]; ok {
				item.FeatureName = m.Name
			}
		}

		// Set window size
		if m, ok := meterMap[analytic.MeterID]; ok {
			if m.HasBucketSize() {
				item.WindowSize = m.Aggregation.BucketSize
			} else {
				item.WindowSize = params.WindowSize
			}
		}

		// Map time-series points
		for _, point := range analytic.Points {
			item.Points = append(item.Points, dto.UsageAnalyticPoint{
				Timestamp:                        point.Timestamp,
				Usage:                            point.Usage,
				Cost:                             point.Cost,
				EventCount:                       point.EventCount,
				ComputedCommitmentUtilizedAmount: point.ComputedCommitmentUtilizedAmount,
				ComputedOverageAmount:            point.ComputedOverageAmount,
				ComputedTrueUpAmount:             point.ComputedTrueUpAmount,
			})
		}

		response.Items = append(response.Items, item)
		response.TotalCost = response.TotalCost.Add(analytic.TotalCost)
	}

	// Sort by feature name (same as feature usage)
	sort.Slice(response.Items, func(i, j int) bool {
		return response.Items[i].FeatureName < response.Items[j].FeatureName
	})

	return response
}

// getCorrectMeterUsageValue returns the correct usage value based on the aggregation type.
// Mirrors the logic in featureUsageTrackingService.getCorrectUsageValue.
func getCorrectMeterUsageValue(item *events.DetailedUsageAnalytic) decimal.Decimal {
	switch item.AggregationType {
	case types.AggregationCountUnique:
		return decimal.NewFromInt(int64(item.CountUniqueUsage))
	case types.AggregationMax:
		// For bucketed MAX, TotalUsage already contains sum of bucket maxes
		if !item.TotalUsage.IsZero() {
			return item.TotalUsage
		}
		return item.MaxUsage
	case types.AggregationLatest:
		return item.LatestUsage
	default:
		return item.TotalUsage
	}
}

// fetchMeters fetches meter configurations for the requested meter IDs.
// If no meter IDs are specified, fetches all meters for the tenant.
func (s *meterUsageService) fetchMeters(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams) ([]*meter.Meter, error) {
	filter := types.NewNoLimitMeterFilter()
	if len(params.MeterIDs) > 0 {
		filter.MeterIDs = params.MeterIDs
	}

	meters, err := s.MeterRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch meters for detailed analytics").
			Mark(ierr.ErrDatabase)
	}

	// If no specific meter IDs were requested, populate params.MeterIDs from fetched meters
	if len(params.MeterIDs) == 0 {
		meterIDs := make([]string, len(meters))
		for i, m := range meters {
			meterIDs[i] = m.ID
		}
		params.MeterIDs = meterIDs
	}

	return meters, nil
}

// getBucketedMeterAnalytics handles analytics for a single bucketed meter (MAX or SUM with bucket_size).
// It uses GetUsageForBucketedMeters for the total, then fetches time-series points if windowed.
func (s *meterUsageService) getBucketedMeterAnalytics(
	ctx context.Context,
	params *events.MeterUsageDetailedAnalyticsParams,
	m *meter.Meter,
) ([]*events.MeterUsageDetailedResult, error) {
	// Build query params for the bucketed query
	bucketParams := &events.MeterUsageQueryParams{
		TenantID:           params.TenantID,
		EnvironmentID:      params.EnvironmentID,
		ExternalCustomerID: params.ExternalCustomerID,
		MeterID:            m.ID,
		StartTime:          params.StartTime,
		EndTime:            params.EndTime,
		AggregationType:    m.Aggregation.Type,
		WindowSize:         m.Aggregation.BucketSize, // use meter's bucket size for aggregation
		GroupByProperty:    m.Aggregation.GroupBy,
		UseFinal:           params.UseFinal,
		BillingAnchor:      params.BillingAnchor,
	}

	if len(params.ExternalCustomerIDs) > 0 {
		bucketParams.ExternalCustomerIDs = params.ExternalCustomerIDs
	}

	aggResult, err := s.repo.GetUsageForBucketedMeters(ctx, bucketParams)
	if err != nil {
		s.logger.Errorw("failed to get bucketed meter usage",
			"error", err,
			"meter_id", m.ID,
		)
		return nil, err
	}

	result := &events.MeterUsageDetailedResult{
		MeterID:    m.ID,
		Properties: make(map[string]string),
	}

	// Map the aggregation result based on meter type
	if m.IsBucketedMaxMeter() {
		result.MaxUsage = aggResult.Value
		result.TotalUsage = aggResult.Value // total = SUM(MAX per bucket)
	} else {
		result.TotalUsage = aggResult.Value
	}

	// Convert bucketed points to detailed points
	if params.WindowSize != "" && len(aggResult.Results) > 0 {
		points := make([]events.MeterUsageDetailedPoint, 0, len(aggResult.Results))
		for _, r := range aggResult.Results {
			p := events.MeterUsageDetailedPoint{
				WindowStart: r.WindowSize,
			}
			if m.IsBucketedMaxMeter() {
				p.MaxUsage = r.Value
				p.TotalUsage = r.Value
			} else {
				p.TotalUsage = r.Value
			}
			points = append(points, p)
		}
		result.Points = points
	}

	// Count events separately for bucketed meters (the bucketed query doesn't return event counts)
	eventCount, err := s.getEventCountForMeter(ctx, params, m.ID)
	if err != nil {
		s.logger.Warnw("failed to get event count for bucketed meter, defaulting to 0",
			"error", err,
			"meter_id", m.ID,
		)
	} else {
		result.EventCount = eventCount
	}

	return []*events.MeterUsageDetailedResult{result}, nil
}

// getEventCountForMeter fetches the event count for a specific meter using a simple scalar query.
func (s *meterUsageService) getEventCountForMeter(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams, meterID string) (uint64, error) {
	countParams := &events.MeterUsageQueryParams{
		TenantID:           params.TenantID,
		EnvironmentID:      params.EnvironmentID,
		ExternalCustomerID: params.ExternalCustomerID,
		MeterID:            meterID,
		StartTime:          params.StartTime,
		EndTime:            params.EndTime,
		AggregationType:    types.AggregationCount,
		UseFinal:           params.UseFinal,
	}

	if len(params.ExternalCustomerIDs) > 0 {
		countParams.ExternalCustomerIDs = params.ExternalCustomerIDs
	}

	result, err := s.repo.GetUsage(ctx, countParams)
	if err != nil {
		return 0, err
	}

	// For COUNT aggregation, TotalValue holds the count as a decimal
	return result.TotalValue.BigInt().Uint64(), nil
}

// ensure meterUsageService implements MeterUsageService
var _ MeterUsageService = (*meterUsageService)(nil)
