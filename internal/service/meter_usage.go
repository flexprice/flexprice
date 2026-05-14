package service

import (
	"context"
	"fmt"
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

// ---------------------------------------------------------------------------
// Service interface + constructor
// ---------------------------------------------------------------------------

// MeterUsageService handles read-side meter usage queries.
// This sits between the API handler and MeterUsageRepository,
// keeping the handler HTTP/DTO-only.
type MeterUsageService interface {
	GetUsage(ctx context.Context, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error)
	GetUsageMultiMeter(ctx context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error)
	GetDetailedAnalytics(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams) (*dto.GetUsageAnalyticsResponse, error)

	// GetSubscriptionMeterUsage is the centralized meter-usage query function.
	// Both analytics and billing paths call this per-subscription to get line-item-bounded usage.
	GetSubscriptionMeterUsage(ctx context.Context, req *GetSubscriptionMeterUsageRequest) (*SubscriptionMeterUsage, error)

	// ConvertToBillingCharges maps SubscriptionMeterUsage to billing charges.
	ConvertToBillingCharges(ctx context.Context, usage *SubscriptionMeterUsage) ([]*dto.SubscriptionUsageByMetersResponse, decimal.Decimal, error)
}

type meterUsageService struct {
	ServiceParams
	repo   events.MeterUsageRepository
	logger *logger.Logger
}

func NewMeterUsageService(params ServiceParams) MeterUsageService {
	return &meterUsageService{
		ServiceParams: params,
		repo:          params.MeterUsageRepo,
		logger:        params.Logger,
	}
}

// ---------------------------------------------------------------------------
// Centralized types
// ---------------------------------------------------------------------------

// LineItemMeterUsage holds raw usage data for one (line item, meter) pair.
// Created by GetSubscriptionMeterUsage; consumed by both the analytics merge
// and the billing charge-building paths.
type LineItemMeterUsage struct {
	LineItem    *subscription.SubscriptionLineItem
	MeterID     string
	Meter       *meter.Meter
	Price       *price.Price
	PeriodStart time.Time // effective start (bounded by line item dates)
	PeriodEnd   time.Time // effective end   (bounded by line item dates)

	// Scalar usage (aggregation-type-aware)
	Usage      decimal.Decimal
	EventCount uint64

	// Time-series points (nil when WindowSize is empty)
	Points []events.MeterUsageDetailedPoint

	// For bucketed meters (MAX/SUM with bucket_size); nil for standard meters
	BucketedResult *events.AggregationResult
}

// SubscriptionMeterUsage holds all meter usage for a single subscription,
// including the resolved lookup maps needed by downstream consumers.
type SubscriptionMeterUsage struct {
	Subscription        *subscription.Subscription
	ExternalCustomerIDs []string
	LineItemUsages      []*LineItemMeterUsage
	MeterMap            map[string]*meter.Meter
	PriceMap            map[string]*price.Price
	FeatureMap          map[string]*feature.Feature // feature_id → Feature
	MeterToFeature      map[string]*feature.Feature // meter_id  → Feature
}

// GetSubscriptionMeterUsageRequest configures the centralized query.
type GetSubscriptionMeterUsageRequest struct {
	SubscriptionID  string
	StartTime       time.Time
	EndTime         time.Time
	LifetimeUsage   bool
	WindowSize      types.WindowSize // non-empty → fetch time-series points
	BillingAnchor   *time.Time
	UseFinal        bool // true for invoice creation
	IncludeFeatures bool // true for analytics (resolve meter → feature)
}

// dateRangeGroup is the key used to batch standard-meter queries that share
// the same effective time range and aggregation type.
type dateRangeGroup struct {
	Start   time.Time
	End     time.Time
	AggType types.AggregationType
}

// lineItemWithMeter pairs a line item with its meter ID for grouping.
type lineItemWithMeter struct {
	Item    *subscription.SubscriptionLineItem
	MeterID string
}

// ---------------------------------------------------------------------------
// Simple passthrough methods
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// GetSubscriptionMeterUsage — centralized per-subscription usage query
// ---------------------------------------------------------------------------

// GetSubscriptionMeterUsage fetches meter usage for every usage-type line item
// in a subscription, respecting each line item's effective date range via
// GetPeriodStart / GetPeriodEnd. Standard meters are batched by (dateRange, aggType)
// to minimize ClickHouse round-trips; bucketed meters are queried individually.
func (s *meterUsageService) GetSubscriptionMeterUsage(
	ctx context.Context,
	req *GetSubscriptionMeterUsageRequest,
) (*SubscriptionMeterUsage, error) {
	if req == nil || req.SubscriptionID == "" {
		return nil, ierr.NewError("subscription_id is required").Mark(ierr.ErrValidation)
	}

	// 1. Get subscription
	sub, err := s.SubRepo.Get(ctx, req.SubscriptionID)
	if err != nil {
		return nil, err
	}

	// 2. Resolve external customer IDs for meter_usage queries
	externalCustomerIDs, err := s.resolveExternalCustomerIDs(ctx, sub)
	if err != nil {
		return nil, err
	}

	// 3. Resolve time range
	usageStartTime := req.StartTime
	if usageStartTime.IsZero() {
		usageStartTime = sub.CurrentPeriodStart
	}
	usageEndTime := req.EndTime
	if usageEndTime.IsZero() {
		usageEndTime = sub.CurrentPeriodEnd
	}
	if req.LifetimeUsage {
		usageStartTime = time.Time{}
		usageEndTime = time.Now().UTC()
	}

	// 4. Get line items for this subscription
	lineItemSubID := sub.ID
	if sub.SubscriptionType == types.SubscriptionTypeInherited &&
		sub.ParentSubscriptionID != nil && lo.FromPtr(sub.ParentSubscriptionID) != "" {
		lineItemSubID = lo.FromPtr(sub.ParentSubscriptionID)
	}

	lineItems, err := s.listLineItemsForUsageWindow(ctx, lineItemSubID, usageStartTime, req.LifetimeUsage)
	if err != nil {
		return nil, err
	}
	sub.LineItems = lineItems

	// 5. Collect usage line items and fetch prices with meter expansion
	priceIDs := make([]string, 0, len(lineItems))
	for _, item := range lineItems {
		if item.PriceType == types.PRICE_TYPE_USAGE && item.MeterID != "" {
			priceIDs = append(priceIDs, item.PriceID)
		}
	}

	result := &SubscriptionMeterUsage{
		Subscription:        sub,
		ExternalCustomerIDs: externalCustomerIDs,
		MeterMap:            make(map[string]*meter.Meter),
		PriceMap:            make(map[string]*price.Price),
		FeatureMap:          make(map[string]*feature.Feature),
		MeterToFeature:      make(map[string]*feature.Feature),
	}

	if len(priceIDs) == 0 {
		return result, nil
	}

	priceService := NewPriceService(s.ServiceParams)
	priceFilter := types.NewNoLimitPriceFilter()
	priceFilter.PriceIDs = priceIDs
	priceFilter.Expand = lo.ToPtr(string(types.ExpandMeters))
	priceFilter.AllowExpiredPrices = true
	pricesList, err := priceService.GetPrices(ctx, priceFilter)
	if err != nil {
		return nil, err
	}

	meterDisplayNames := make(map[string]string)
	for _, p := range pricesList.Items {
		result.PriceMap[p.ID] = p.Price
		if p.Meter != nil {
			m := p.Meter.ToMeter()
			result.MeterMap[p.Price.MeterID] = m
			meterDisplayNames[p.Price.MeterID] = p.Meter.Name
		}
	}

	// 6. (Optional) Resolve meter → feature mapping for analytics
	if req.IncludeFeatures {
		meterIDs := lo.Keys(result.MeterMap)
		if len(meterIDs) > 0 {
			featureFilter := types.NewNoLimitFeatureFilter()
			featureFilter.MeterIDs = meterIDs
			features, err := s.FeatureRepo.List(ctx, featureFilter)
			if err != nil {
				s.logger.Warnw("failed to fetch features for meter mapping", "error", err)
			} else {
				for _, f := range features {
					if f.MeterID != "" {
						result.MeterToFeature[f.MeterID] = f
						result.FeatureMap[f.ID] = f
					}
				}
			}
		}
	}

	// 7. Distinct meter optimization — skip meters with zero usage
	distinctMeterIDs, err := s.repo.GetDistinctMeterIDs(ctx, &events.MeterUsageQueryParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: externalCustomerIDs,
		StartTime:           usageStartTime,
		EndTime:             usageEndTime,
		UseFinal:            req.UseFinal,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get distinct meter_ids from meter_usage: %w", err)
	}
	activeMeterIDs := make(map[string]bool, len(distinctMeterIDs))
	for _, id := range distinctMeterIDs {
		activeMeterIDs[id] = true
	}

	// 8. Build meter_id → line items map (only meters with data)
	meterToLineItems := make(map[string][]*subscription.SubscriptionLineItem)
	meterAggType := make(map[string]types.AggregationType)
	for _, item := range lineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE || item.MeterID == "" {
			continue
		}
		if !activeMeterIDs[item.MeterID] {
			continue
		}
		meterToLineItems[item.MeterID] = append(meterToLineItems[item.MeterID], item)
		if m, ok := result.MeterMap[item.MeterID]; ok {
			meterAggType[item.MeterID] = m.Aggregation.Type
		}
	}

	// 9. Split bucketed vs standard meters
	bucketedMeterIDs := make(map[string]bool)
	for meterID, m := range result.MeterMap {
		if m.IsBucketedMaxMeter() || m.IsBucketedSumMeter() {
			bucketedMeterIDs[meterID] = true
		}
	}

	// 10. Query standard meters WITH per-line-item date ranges.
	//     Group by (effectiveStart, effectiveEnd, aggType) so line items that
	//     share the same period are batched into one ClickHouse call.
	standardGroups := make(map[dateRangeGroup][]*lineItemWithMeter)
	for meterID, items := range meterToLineItems {
		if bucketedMeterIDs[meterID] {
			continue
		}
		for _, item := range items {
			start := item.GetPeriodStart(usageStartTime)
			end := item.GetPeriodEnd(usageEndTime)
			key := dateRangeGroup{Start: start, End: end, AggType: meterAggType[meterID]}
			standardGroups[key] = append(standardGroups[key], &lineItemWithMeter{Item: item, MeterID: meterID})
		}
	}

	// lineItemResults maps line_item_id → usage result for standard meters
	lineItemResults := make(map[string]*events.MeterUsageAggregationResult)

	for group, lineItemsInGroup := range standardGroups {
		meterIDs := make([]string, 0, len(lineItemsInGroup))
		for _, liw := range lineItemsInGroup {
			meterIDs = append(meterIDs, liw.MeterID)
		}
		meterIDs = lo.Uniq(meterIDs)

		if req.WindowSize != "" {
			// Windowed query — use GetDetailedAnalytics for time-series points
			detailedParams := &events.MeterUsageDetailedAnalyticsParams{
				TenantID:            types.GetTenantID(ctx),
				EnvironmentID:       types.GetEnvironmentID(ctx),
				ExternalCustomerIDs: externalCustomerIDs,
				MeterIDs:            meterIDs,
				StartTime:           group.Start,
				EndTime:             group.End,
				AggregationTypes:    []types.AggregationType{group.AggType},
				WindowSize:          req.WindowSize,
				BillingAnchor:       req.BillingAnchor,
				UseFinal:            req.UseFinal,
			}
			if len(meterIDs) > 1 {
				detailedParams.GroupBy = []string{"meter_id"}
			}

			detailedResults, err := s.repo.GetDetailedAnalytics(ctx, detailedParams)
			if err != nil {
				return nil, fmt.Errorf("failed to query windowed meter usage for group %v: %w", group, err)
			}

			// Map results by meter_id
			resultByMeter := make(map[string]*events.MeterUsageDetailedResult, len(detailedResults))
			for _, dr := range detailedResults {
				resultByMeter[dr.MeterID] = dr
			}

			// Build LineItemMeterUsage for each line item
			for _, liw := range lineItemsInGroup {
				dr := resultByMeter[liw.MeterID]
				usage := &LineItemMeterUsage{
					LineItem:    liw.Item,
					MeterID:     liw.MeterID,
					Meter:       result.MeterMap[liw.MeterID],
					Price:       result.PriceMap[liw.Item.PriceID],
					PeriodStart: group.Start,
					PeriodEnd:   group.End,
				}
				if dr != nil {
					usage.Usage = getUsageValueFromDetailedResult(dr, group.AggType)
					usage.EventCount = dr.EventCount
					usage.Points = dr.Points
				}
				result.LineItemUsages = append(result.LineItemUsages, usage)
			}
		} else {
			// Scalar query — use GetUsageMultiMeter for batch efficiency
			queryParams := &events.MeterUsageQueryParams{
				TenantID:            types.GetTenantID(ctx),
				EnvironmentID:       types.GetEnvironmentID(ctx),
				ExternalCustomerIDs: externalCustomerIDs,
				MeterIDs:            meterIDs,
				StartTime:           group.Start,
				EndTime:             group.End,
				AggregationType:     group.AggType,
				UseFinal:            req.UseFinal,
			}

			results, err := s.repo.GetUsageMultiMeter(ctx, queryParams)
			if err != nil {
				return nil, fmt.Errorf("failed to query meter_usage for agg type %s: %w", group.AggType, err)
			}

			resultByMeter := make(map[string]*events.MeterUsageAggregationResult, len(results))
			for _, r := range results {
				resultByMeter[r.MeterID] = r
				lineItemResults[r.MeterID] = r // also store for zero-usage check
			}

			for _, liw := range lineItemsInGroup {
				r := resultByMeter[liw.MeterID]
				usage := &LineItemMeterUsage{
					LineItem:    liw.Item,
					MeterID:     liw.MeterID,
					Meter:       result.MeterMap[liw.MeterID],
					Price:       result.PriceMap[liw.Item.PriceID],
					PeriodStart: group.Start,
					PeriodEnd:   group.End,
				}
				if r != nil {
					usage.Usage = r.TotalValue
				}
				result.LineItemUsages = append(result.LineItemUsages, usage)
			}
		}
	}

	// 11. Query bucketed meters per line item (already uses GetPeriodStart/End)
	for meterID := range bucketedMeterIDs {
		m := result.MeterMap[meterID]
		if m == nil {
			continue
		}
		items := meterToLineItems[meterID]
		for _, item := range items {
			itemStart := item.GetPeriodStart(usageStartTime)
			itemEnd := item.GetPeriodEnd(usageEndTime)

			bucketedResult, err := s.queryBucketedMeterUsage(
				ctx, m, externalCustomerIDs,
				itemStart, itemEnd, req.BillingAnchor, req.UseFinal,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to query bucketed meter usage for meter %s: %w", meterID, err)
			}

			usage := &LineItemMeterUsage{
				LineItem:       item,
				MeterID:        meterID,
				Meter:          m,
				Price:          result.PriceMap[item.PriceID],
				PeriodStart:    itemStart,
				PeriodEnd:      itemEnd,
				BucketedResult: bucketedResult,
			}

			// Set scalar usage from bucketed result
			if bucketedResult != nil {
				usage.Usage = bucketedResult.Value
			}

			// Always populate points from bucketed results — cost calculation needs
			// per-bucket values for commitment windowing, true-up, etc.
			// Roll-up to request window happens downstream in mergeBucketPointsByWindow.
			if bucketedResult != nil && len(bucketedResult.Results) > 0 {
				points := make([]events.MeterUsageDetailedPoint, 0, len(bucketedResult.Results))
				for _, r := range bucketedResult.Results {
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
				usage.Points = points
			}

			result.LineItemUsages = append(result.LineItemUsages, usage)
		}
	}

	// 12. Zero-usage entries for line items that had no data
	processedLineItemIDs := make(map[string]bool, len(result.LineItemUsages))
	for _, lu := range result.LineItemUsages {
		if lu.LineItem != nil {
			processedLineItemIDs[lu.LineItem.ID] = true
		}
	}

	for _, item := range lineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE || item.MeterID == "" {
			continue
		}
		if processedLineItemIDs[item.ID] {
			continue
		}
		result.LineItemUsages = append(result.LineItemUsages, &LineItemMeterUsage{
			LineItem:    item,
			MeterID:     item.MeterID,
			Meter:       result.MeterMap[item.MeterID],
			Price:       result.PriceMap[item.PriceID],
			PeriodStart: item.GetPeriodStart(usageStartTime),
			PeriodEnd:   item.GetPeriodEnd(usageEndTime),
		})
	}

	return result, nil
}

// queryBucketedMeterUsage queries the meter_usage table for a single bucketed meter,
// returning a per-bucket AggregationResult.
func (s *meterUsageService) queryBucketedMeterUsage(
	ctx context.Context,
	m *meter.Meter,
	externalCustomerIDs []string,
	periodStart, periodEnd time.Time,
	billingAnchor *time.Time,
	useFinal bool,
) (*events.AggregationResult, error) {
	aggType := m.Aggregation.Type
	groupBy := m.Aggregation.GroupBy
	if m.IsBucketedSumMeter() {
		aggType = types.AggregationSum
		groupBy = ""
	}
	return s.repo.GetUsageForBucketedMeters(ctx, &events.MeterUsageQueryParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: externalCustomerIDs,
		MeterID:             m.ID,
		StartTime:           periodStart,
		EndTime:             periodEnd,
		AggregationType:     aggType,
		WindowSize:          m.Aggregation.BucketSize,
		BillingAnchor:       billingAnchor,
		GroupByProperty:     groupBy,
		UseFinal:            useFinal,
	})
}

// ---------------------------------------------------------------------------
// Analytics path — GetDetailedAnalytics
// ---------------------------------------------------------------------------

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

	// Resolve customer → subscriptions
	cust, subscriptions, err := s.resolveCustomerAndSubscriptions(ctx, params.ExternalCustomerID)
	if err != nil || len(subscriptions) == 0 {
		// No customer context — fallback to direct repo queries
		return s.getDetailedAnalyticsWithoutSubscriptionContext(ctx, params)
	}

	// Call GetSubscriptionMeterUsage per subscription
	var allUsages []*SubscriptionMeterUsage
	for _, sub := range subscriptions {
		billingAnchor := params.BillingAnchor
		if billingAnchor == nil {
			billingAnchor = &sub.BillingAnchor
		}
		usage, err := s.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
			SubscriptionID:  sub.ID,
			StartTime:       params.StartTime,
			EndTime:         params.EndTime,
			WindowSize:      params.WindowSize,
			BillingAnchor:   billingAnchor,
			UseFinal:        params.UseFinal,
			IncludeFeatures: true,
		})
		if err != nil {
			s.logger.Warnw("failed to get subscription meter usage, skipping",
				"error", err,
				"subscription_id", sub.ID,
			)
			continue
		}
		allUsages = append(allUsages, usage)
	}

	// Merge into AnalyticsData
	data := s.mergeSubscriptionUsagesToAnalyticsData(cust, subscriptions, allUsages, params)

	// Calculate costs inline (no dependency on featureUsageTrackingService)
	if len(data.Analytics) > 0 {
		if err := s.calculateCosts(ctx, data); err != nil {
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
	return s.toUsageAnalyticsResponseDTO(data, data.Meters, params), nil
}

// mergeSubscriptionUsagesToAnalyticsData converts N SubscriptionMeterUsage results
// into a single AnalyticsData suitable for the cost calculation pipeline.
func (s *meterUsageService) mergeSubscriptionUsagesToAnalyticsData(
	cust *customer.Customer,
	subscriptions []*subscription.Subscription,
	usages []*SubscriptionMeterUsage,
	params *events.MeterUsageDetailedAnalyticsParams,
) *AnalyticsData {
	data := &AnalyticsData{
		Customer:              cust,
		Subscriptions:         subscriptions,
		SubscriptionLineItems: make(map[string]*subscription.SubscriptionLineItem),
		SubscriptionsMap:      make(map[string]*subscription.Subscription),
		Features:              make(map[string]*feature.Feature),
		Meters:                make(map[string]*meter.Meter),
		Prices:                make(map[string]*price.Price),
		Plans:                 make(map[string]*plan.Plan),
		Analytics:             make([]*events.DetailedUsageAnalytic, 0),
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

	// Set currency from first subscription
	if len(subscriptions) > 0 {
		data.Currency = subscriptions[0].Currency
	}

	// Populate subscription maps
	for _, sub := range subscriptions {
		data.SubscriptionsMap[sub.ID] = sub
		for _, li := range sub.LineItems {
			data.SubscriptionLineItems[li.ID] = li
		}
	}

	// Merge all subscription usages
	for _, su := range usages {
		// Merge lookup maps
		for id, m := range su.MeterMap {
			data.Meters[id] = m
		}
		for id, p := range su.PriceMap {
			data.Prices[id] = p
		}
		for id, f := range su.FeatureMap {
			data.Features[id] = f
		}

		// Convert each LineItemMeterUsage → DetailedUsageAnalytic
		// Skip line items with zero usage — they add noise to analytics responses.
		// (Billing path uses ConvertToBillingCharges which keeps zero-usage for commitment minimums.)
		for _, lu := range su.LineItemUsages {
			if lu.Usage.IsZero() && lu.EventCount == 0 && len(lu.Points) == 0 && lu.BucketedResult == nil {
				continue
			}

			analytic := &events.DetailedUsageAnalytic{
				MeterID: lu.MeterID,
			}

			// Set usage values from the line item usage
			if lu.BucketedResult != nil && lu.Meter != nil {
				// Bucketed meter: set both MaxUsage and TotalUsage
				if lu.Meter.IsBucketedMaxMeter() {
					analytic.MaxUsage = lu.Usage
					analytic.TotalUsage = lu.Usage
				} else {
					analytic.TotalUsage = lu.Usage
				}
			} else {
				analytic.TotalUsage = lu.Usage
			}
			analytic.EventCount = lu.EventCount

			// Set meter metadata
			if lu.Meter != nil {
				analytic.EventName = lu.Meter.EventName
				analytic.AggregationType = lu.Meter.Aggregation.Type
			}

			// Set feature info
			if su.MeterToFeature != nil {
				if f, ok := su.MeterToFeature[lu.MeterID]; ok {
					analytic.FeatureID = f.ID
					analytic.FeatureName = f.Name
					analytic.Unit = f.UnitSingular
					analytic.UnitPlural = f.UnitPlural
				}
			}

			// Set subscription/pricing info from line item
			if lu.LineItem != nil {
				analytic.PriceID = lu.LineItem.PriceID
				analytic.SubLineItemID = lu.LineItem.ID
				analytic.SubscriptionID = lu.LineItem.SubscriptionID
			}

			// Convert time-series points
			if len(lu.Points) > 0 {
				analytic.Points = make([]events.UsageAnalyticPoint, 0, len(lu.Points))
				for _, p := range lu.Points {
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
	}

	return data
}

// getDetailedAnalyticsWithoutSubscriptionContext handles the fallback case when
// no customer/subscriptions are available (e.g. admin analytics across all customers).
// Uses direct repo queries without per-line-item date bounding.
func (s *meterUsageService) getDetailedAnalyticsWithoutSubscriptionContext(
	ctx context.Context,
	params *events.MeterUsageDetailedAnalyticsParams,
) (*dto.GetUsageAnalyticsResponse, error) {
	// Fetch meter configs
	meters, err := s.fetchMeters(ctx, params)
	if err != nil {
		return nil, err
	}

	meterMap := make(map[string]*meter.Meter, len(meters))
	var aggTypes []types.AggregationType
	for _, m := range meters {
		meterMap[m.ID] = m
		if m.Aggregation.Type != "" {
			aggTypes = append(aggTypes, m.Aggregation.Type)
		}
	}
	if len(params.AggregationTypes) == 0 {
		params.AggregationTypes = lo.Uniq(aggTypes)
	}

	// Split meters
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

	for _, meterID := range bucketedMaxMeterIDs {
		results, err := s.getBucketedMeterAnalytics(ctx, params, meterMap[meterID])
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
	}

	for _, meterID := range bucketedSumMeterIDs {
		results, err := s.getBucketedMeterAnalytics(ctx, params, meterMap[meterID])
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
	}

	if len(standardMeterIDs) > 0 || len(params.MeterIDs) == 0 {
		standardParams := *params
		if len(standardMeterIDs) > 0 {
			standardParams.MeterIDs = standardMeterIDs
		}
		if len(standardParams.MeterIDs) > 1 && !lo.Contains(standardParams.GroupBy, "meter_id") {
			standardParams.GroupBy = append([]string{"meter_id"}, standardParams.GroupBy...)
		}
		results, err := s.repo.GetDetailedAnalytics(ctx, &standardParams)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
	}

	// Build minimal AnalyticsData (no subscription context)
	data := &AnalyticsData{
		SubscriptionLineItems: make(map[string]*subscription.SubscriptionLineItem),
		SubscriptionsMap:      make(map[string]*subscription.Subscription),
		Features:              make(map[string]*feature.Feature),
		Meters:                meterMap,
		Prices:                make(map[string]*price.Price),
		Plans:                 make(map[string]*plan.Plan),
		Analytics:             make([]*events.DetailedUsageAnalytic, 0, len(allResults)),
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

	// Resolve features
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
					data.Features[f.ID] = f
				}
			}
		}
	}

	meterToFeature := make(map[string]*feature.Feature)
	for _, f := range data.Features {
		if f.MeterID != "" {
			meterToFeature[f.MeterID] = f
		}
	}

	// Convert results to analytics
	for _, r := range allResults {
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
		if m, ok := meterMap[r.MeterID]; ok {
			analytic.EventName = m.EventName
			analytic.AggregationType = m.Aggregation.Type
		}
		if f, ok := meterToFeature[r.MeterID]; ok {
			analytic.FeatureID = f.ID
			analytic.FeatureName = f.Name
			analytic.Unit = f.UnitSingular
			analytic.UnitPlural = f.UnitPlural
		}
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

	// Convert to response DTO (no cost calculation without subscription context)
	return s.toUsageAnalyticsResponseDTO(data, meterMap, params), nil
}

// ---------------------------------------------------------------------------
// Response DTO conversion
// ---------------------------------------------------------------------------

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

		if item.FeatureName == "" {
			if m, ok := meterMap[analytic.MeterID]; ok {
				item.FeatureName = m.Name
			}
		}

		if m, ok := meterMap[analytic.MeterID]; ok {
			if m.HasBucketSize() {
				item.WindowSize = m.Aggregation.BucketSize
			} else {
				item.WindowSize = params.WindowSize
			}
		}

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

	sort.Slice(response.Items, func(i, j int) bool {
		return response.Items[i].FeatureName < response.Items[j].FeatureName
	})

	return response
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// getCorrectMeterUsageValue returns the correct usage value based on the aggregation type.
func getCorrectMeterUsageValue(item *events.DetailedUsageAnalytic) decimal.Decimal {
	switch item.AggregationType {
	case types.AggregationCountUnique:
		return decimal.NewFromInt(int64(item.CountUniqueUsage))
	case types.AggregationMax:
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

// getUsageValueFromDetailedResult extracts the correct scalar usage from a
// MeterUsageDetailedResult based on aggregation type.
func getUsageValueFromDetailedResult(r *events.MeterUsageDetailedResult, aggType types.AggregationType) decimal.Decimal {
	switch aggType {
	case types.AggregationCountUnique:
		return decimal.NewFromInt(int64(r.CountUniqueUsage))
	case types.AggregationMax:
		if !r.TotalUsage.IsZero() {
			return r.TotalUsage
		}
		return r.MaxUsage
	case types.AggregationLatest:
		return r.LatestUsage
	default:
		return r.TotalUsage
	}
}

// fetchMeters fetches meter configurations for the requested meter IDs.
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

	if len(params.MeterIDs) == 0 {
		meterIDs := make([]string, len(meters))
		for i, m := range meters {
			meterIDs[i] = m.ID
		}
		params.MeterIDs = meterIDs
	}

	return meters, nil
}

// getBucketedMeterAnalytics handles analytics for a single bucketed meter (fallback path).
func (s *meterUsageService) getBucketedMeterAnalytics(
	ctx context.Context,
	params *events.MeterUsageDetailedAnalyticsParams,
	m *meter.Meter,
) ([]*events.MeterUsageDetailedResult, error) {
	bucketParams := &events.MeterUsageQueryParams{
		TenantID:           params.TenantID,
		EnvironmentID:      params.EnvironmentID,
		ExternalCustomerID: params.ExternalCustomerID,
		MeterID:            m.ID,
		StartTime:          params.StartTime,
		EndTime:            params.EndTime,
		AggregationType:    m.Aggregation.Type,
		WindowSize:         m.Aggregation.BucketSize,
		GroupByProperty:    m.Aggregation.GroupBy,
		UseFinal:           params.UseFinal,
		BillingAnchor:      params.BillingAnchor,
	}

	if len(params.ExternalCustomerIDs) > 0 {
		bucketParams.ExternalCustomerIDs = params.ExternalCustomerIDs
	}

	aggResult, err := s.repo.GetUsageForBucketedMeters(ctx, bucketParams)
	if err != nil {
		s.logger.Errorw("failed to get bucketed meter usage", "error", err, "meter_id", m.ID)
		return nil, err
	}

	result := &events.MeterUsageDetailedResult{
		MeterID:    m.ID,
		Properties: make(map[string]string),
	}

	if m.IsBucketedMaxMeter() {
		result.MaxUsage = aggResult.Value
		result.TotalUsage = aggResult.Value
	} else {
		result.TotalUsage = aggResult.Value
	}

	if params.WindowSize != "" && len(aggResult.Results) > 0 {
		points := make([]events.MeterUsageDetailedPoint, 0, len(aggResult.Results))
		for _, r := range aggResult.Results {
			p := events.MeterUsageDetailedPoint{WindowStart: r.WindowSize}
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

	eventCount, err := s.getEventCountForMeter(ctx, params, m.ID)
	if err != nil {
		s.logger.Warnw("failed to get event count for bucketed meter, defaulting to 0", "error", err, "meter_id", m.ID)
	} else {
		result.EventCount = eventCount
	}

	return []*events.MeterUsageDetailedResult{result}, nil
}

// getEventCountForMeter fetches the event count for a specific meter.
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

	return result.TotalValue.BigInt().Uint64(), nil
}

// resolveCustomerAndSubscriptions fetches the internal customer and their subscriptions.
func (s *meterUsageService) resolveCustomerAndSubscriptions(ctx context.Context, externalCustomerID string) (*customer.Customer, []*subscription.Subscription, error) {
	if externalCustomerID == "" {
		return nil, nil, nil
	}

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

// ConvertToBillingCharges maps SubscriptionMeterUsage to billing charges.
// Returns the charges and total cost (before commitment/overage adjustments).
func (s *meterUsageService) ConvertToBillingCharges(
	ctx context.Context,
	usage *SubscriptionMeterUsage,
) ([]*dto.SubscriptionUsageByMetersResponse, decimal.Decimal, error) {
	priceService := NewPriceService(s.ServiceParams)
	var charges []*dto.SubscriptionUsageByMetersResponse
	totalCost := decimal.Zero

	for _, lu := range usage.LineItemUsages {
		if lu.Price == nil {
			continue
		}

		var cost decimal.Decimal
		var quantity decimal.Decimal

		if lu.BucketedResult != nil && lu.Meter != nil {
			// Bucketed meter: use calculateBucketedMeterCost
			hasGroupBy := lu.Meter.IsBucketedMaxMeter() && lu.Meter.Aggregation.GroupBy != ""
			bucketedCost := calculateBucketedMeterCost(ctx, priceService, lu.Price, lu.BucketedResult, hasGroupBy)
			cost = bucketedCost.Amount
			quantity = bucketedCost.Quantity
		} else {
			// Standard meter: use CalculateCost
			quantity = lu.Usage
			cost = priceService.CalculateCost(ctx, lu.Price, quantity)
		}
		totalCost = totalCost.Add(cost)

		charge := &dto.SubscriptionUsageByMetersResponse{
			SubscriptionLineItemID: lu.LineItem.ID,
			Amount:                 cost.InexactFloat64(),
			Currency:               lu.Price.Currency,
			DisplayAmount:          fmt.Sprintf("%.2f %s", cost.InexactFloat64(), lu.Price.Currency),
			Quantity:               quantity.InexactFloat64(),
			FilterValues:           make(price.JSONBFilters),
			MeterID:                lu.MeterID,
			MeterDisplayName:       lu.Meter.Name,
			Price:                  lu.Price,
			BucketedUsageResult:    lu.BucketedResult,
		}

		if lu.Meter != nil {
			for _, filter := range lu.Meter.Filters {
				charge.FilterValues[filter.Key] = filter.Values
			}
		}

		charges = append(charges, charge)
	}

	return charges, totalCost, nil
}

// resolveExternalCustomerIDs returns the external customer IDs whose meter_usage
// rows belong to a subscription (owner + inherited children for parent subscriptions).
func (s *meterUsageService) resolveExternalCustomerIDs(ctx context.Context, sub *subscription.Subscription) ([]string, error) {
	internalIDs := []string{sub.CustomerID}
	if sub.SubscriptionType == types.SubscriptionTypeParent {
		childFilter := types.NewNoLimitSubscriptionFilter()
		childFilter.ParentSubscriptionIDs = []string{sub.ID}
		childFilter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeInherited}
		childFilter.SubscriptionStatus = []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
			types.SubscriptionStatusTrialing,
			types.SubscriptionStatusDraft,
			types.SubscriptionStatusPaused,
		}
		children, err := s.SubRepo.List(ctx, childFilter)
		if err != nil {
			return nil, err
		}
		for _, ch := range children {
			internalIDs = append(internalIDs, ch.CustomerID)
		}
	}
	internalIDs = lo.Uniq(internalIDs)

	custFilter := types.NewNoLimitCustomerFilter()
	custFilter.CustomerIDs = internalIDs
	customers, err := s.CustomerRepo.List(ctx, custFilter)
	if err != nil {
		return nil, err
	}
	externalIDs := make([]string, 0, len(customers))
	for _, cust := range customers {
		if cust.ExternalID != "" {
			externalIDs = append(externalIDs, cust.ExternalID)
		}
	}
	return lo.Uniq(externalIDs), nil
}

// listLineItemsForUsageWindow retrieves line items active within the usage window.
func (s *meterUsageService) listLineItemsForUsageWindow(ctx context.Context, subscriptionID string, usageStartTime time.Time, lifetime bool) ([]*subscription.SubscriptionLineItem, error) {
	filter := types.NewNoLimitSubscriptionLineItemFilter()
	filter.SubscriptionIDs = []string{subscriptionID}
	if lifetime {
		filter.ActiveFilter = false
		filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	} else {
		filter.ActiveFilter = true
		filter.CurrentPeriodStart = &usageStartTime
	}
	return s.SubscriptionLineItemRepo.List(ctx, filter)
}

// subscriptionStatusPriority returns a sort priority for subscription status.
// Lower value = higher priority (active preferred over cancelled).
func subscriptionStatusPriority(sub *subscription.Subscription) int {
	if sub == nil {
		return 99
	}
	switch sub.SubscriptionStatus {
	case types.SubscriptionStatusActive:
		return 0
	case types.SubscriptionStatusTrialing:
		return 1
	case types.SubscriptionStatusPaused:
		return 2
	case types.SubscriptionStatusCancelled:
		return 3
	default:
		return 10
	}
}

// ---------------------------------------------------------------------------
// Cost calculation (copied from feature_usage_tracking.go to remove dependency)
// ---------------------------------------------------------------------------

// calculateCosts calculates costs for all analytics items in the data.
func (s *meterUsageService) calculateCosts(ctx context.Context, data *AnalyticsData) error {
	priceService := NewPriceService(s.ServiceParams)

	for _, item := range data.Analytics {
		// Resolve meter: prefer via feature, fall back to direct MeterID lookup.
		var m *meter.Meter
		if f, ok := data.Features[item.FeatureID]; ok {
			m = data.Meters[f.MeterID]
		}
		if m == nil && item.MeterID != "" {
			m = data.Meters[item.MeterID]
		}
		if m == nil {
			continue
		}

		p, hasPricing := data.Prices[item.PriceID]
		if !hasPricing {
			continue
		}

		if m.IsBucketedMaxMeter() || m.IsBucketedSumMeter() {
			s.calculateBucketedCost(ctx, priceService, item, p, m, data)
		} else {
			s.calculateRegularCost(ctx, priceService, item, m, p, data)
		}
	}

	return nil
}

// bucketedCostParams encapsulates all context needed for bucketed cost calculation.
type meterUsageBucketedCostParams struct {
	ctx          context.Context
	priceService PriceService
	item         *events.DetailedUsageAnalytic
	price        *price.Price
	data         *AnalyticsData
	aggType      types.AggregationType
	bucketSize   types.WindowSize
}

// calculateBucketedCost calculates cost for bucketed max/sum meters.
func (s *meterUsageService) calculateBucketedCost(ctx context.Context, priceService PriceService, item *events.DetailedUsageAnalytic, p *price.Price, m *meter.Meter, data *AnalyticsData) {
	params := &meterUsageBucketedCostParams{ctx, priceService, item, p, data, m.Aggregation.Type, m.Aggregation.BucketSize}
	lineItem := data.SubscriptionLineItems[item.SubLineItemID]
	hasCommitment := lineItem != nil && lineItem.HasCommitment()
	isWindowed := hasCommitment && lineItem.CommitmentWindowed
	hasTrueUp := isWindowed && lineItem.CommitmentTrueUpEnabled

	var cost decimal.Decimal

	if len(item.Points) > 0 {
		cost = s.processPointsWithBuckets(params, lineItem, hasCommitment, isWindowed, hasTrueUp)
	} else {
		cost = s.processSingleBucket(params, lineItem, hasCommitment, isWindowed, hasTrueUp)
	}

	item.TotalCost = cost
	item.Currency = p.Currency
}

// processPointsWithBuckets handles the case where we have time-series points to process.
func (s *meterUsageService) processPointsWithBuckets(
	p *meterUsageBucketedCostParams,
	lineItem *subscription.SubscriptionLineItem,
	hasCommitment, isWindowed, hasTrueUp bool,
) decimal.Decimal {
	bucketedValues := s.extractBucketValues(p.item.Points, p.aggType)

	var cost decimal.Decimal
	switch {
	case !hasCommitment:
		cost = p.priceService.CalculateBucketedCost(p.ctx, p.price, bucketedValues)
	case isWindowed:
		cost = decimal.Zero // Will be summed from points after processing
	default:
		cost = s.applyLineItemCommitment(p.ctx, p.priceService, p.item, lineItem, p.price, bucketedValues, decimal.Zero)
	}

	s.calculatePointCosts(p, lineItem, isWindowed)

	if hasTrueUp && p.bucketSize != "" {
		cost = s.fillMissingWindowsAndRecalculate(p, lineItem)
	}

	p.item.Points = s.mergeBucketPointsByWindow(p.item.Points, p.aggType)

	if isWindowed && !hasTrueUp {
		cost = s.sumPointCosts(p.item.Points)
	}

	return cost
}

// processSingleBucket handles the case where there are no time-series points.
func (s *meterUsageService) processSingleBucket(
	p *meterUsageBucketedCostParams,
	lineItem *subscription.SubscriptionLineItem,
	hasCommitment, isWindowed, hasTrueUp bool,
) decimal.Decimal {
	totalUsage := s.getSingleBucketUsage(p.item, p.aggType)

	if totalUsage.IsPositive() {
		bucketedValues := []decimal.Decimal{totalUsage}
		baseCost := p.priceService.CalculateBucketedCost(p.ctx, p.price, bucketedValues)
		if hasCommitment {
			return s.applyLineItemCommitment(p.ctx, p.priceService, p.item, lineItem, p.price, bucketedValues, baseCost)
		}
		return baseCost
	}

	if !hasCommitment {
		return decimal.Zero
	}

	if hasTrueUp && p.bucketSize != "" {
		return s.fillZeroUsageWindows(p, lineItem)
	}

	return s.applyLineItemCommitment(p.ctx, p.priceService, p.item, lineItem, p.price, nil, decimal.Zero)
}

// extractBucketValues extracts usage values from points based on aggregation type.
func (s *meterUsageService) extractBucketValues(points []events.UsageAnalyticPoint, aggType types.AggregationType) []decimal.Decimal {
	values := make([]decimal.Decimal, len(points))
	for i, pt := range points {
		values[i] = s.getCorrectUsageValueForPoint(pt, aggType)
	}
	return values
}

// calculatePointCosts calculates cost for each individual point.
func (s *meterUsageService) calculatePointCosts(p *meterUsageBucketedCostParams, lineItem *subscription.SubscriptionLineItem, isWindowed bool) {
	if !isWindowed {
		for i := range p.item.Points {
			usage := s.getCorrectUsageValueForPoint(p.item.Points[i], p.aggType)
			p.item.Points[i].Cost = p.priceService.CalculateCost(p.ctx, p.price, usage)
		}
		return
	}

	commitmentCalc := newCommitmentCalculator(s.logger, p.priceService)
	for i := range p.item.Points {
		usage := s.getCorrectUsageValueForPoint(p.item.Points[i], p.aggType)
		pointCost, info, err := commitmentCalc.applyWindowCommitmentToLineItem(p.ctx, lineItem, []decimal.Decimal{usage}, p.price)
		if err != nil {
			s.logger.Warnw("failed to apply window commitment to point", "error", err, "point_index", i, "line_item_id", lineItem.ID)
			pointCost = p.priceService.CalculateCost(p.ctx, p.price, usage)
		}
		p.item.Points[i].Cost = pointCost
		if info != nil {
			p.item.Points[i].ComputedCommitmentUtilizedAmount = info.ComputedCommitmentUtilizedAmount
			p.item.Points[i].ComputedOverageAmount = info.ComputedOverageAmount
			p.item.Points[i].ComputedTrueUpAmount = info.ComputedTrueUpAmount
		}
	}
}

// fillMissingWindowsAndRecalculate fills gaps in bucket windows and recalculates total cost.
func (s *meterUsageService) fillMissingWindowsAndRecalculate(p *meterUsageBucketedCostParams, lineItem *subscription.SubscriptionLineItem) decimal.Decimal {
	billingAnchor := s.getBillingAnchorFromData(p.data, lineItem.SubscriptionID)
	periodStart := lineItem.GetPeriodStart(p.data.Params.StartTime)
	periodEnd := lineItem.GetPeriodEnd(p.data.Params.EndTime)
	expectedStarts := generateBucketStarts(periodStart, periodEnd, p.bucketSize, billingAnchor)

	pointsByBucket := make(map[time.Time]events.UsageAnalyticPoint, len(p.item.Points))
	for _, pt := range p.item.Points {
		pointsByBucket[pt.Timestamp] = pt
	}

	filled := make([]decimal.Decimal, 0, len(expectedStarts))
	filledPoints := make([]events.UsageAnalyticPoint, 0, len(expectedStarts))
	commitmentCalc := newCommitmentCalculator(s.logger, p.priceService)

	for _, t := range expectedStarts {
		if existing, ok := pointsByBucket[t]; ok {
			filled = append(filled, s.getCorrectUsageValueForPoint(existing, p.aggType))
			filledPoints = append(filledPoints, existing)
		} else {
			filled = append(filled, decimal.Zero)
			filledPoints = append(filledPoints, s.createFillPoint(p, lineItem, t, billingAnchor, commitmentCalc))
		}
	}

	p.item.Points = filledPoints
	if totalCost, _, err := commitmentCalc.applyWindowCommitmentToLineItem(p.ctx, lineItem, filled, p.price); err == nil {
		return totalCost
	}
	return decimal.Zero
}

// fillZeroUsageWindows creates fill points for all expected windows when there's no usage.
func (s *meterUsageService) fillZeroUsageWindows(p *meterUsageBucketedCostParams, lineItem *subscription.SubscriptionLineItem) decimal.Decimal {
	billingAnchor := s.getBillingAnchorFromData(p.data, lineItem.SubscriptionID)
	periodStart := lineItem.GetPeriodStart(p.data.Params.StartTime)
	periodEnd := lineItem.GetPeriodEnd(p.data.Params.EndTime)
	expectedStarts := generateBucketStarts(periodStart, periodEnd, p.bucketSize, billingAnchor)

	filled := make([]decimal.Decimal, len(expectedStarts))
	commitmentCalc := newCommitmentCalculator(s.logger, p.priceService)

	totalCost, info, err := commitmentCalc.applyWindowCommitmentToLineItem(p.ctx, lineItem, filled, p.price)
	if err != nil {
		return decimal.Zero
	}

	p.item.CommitmentInfo = info
	bucketPoints := make([]events.UsageAnalyticPoint, 0, len(expectedStarts))
	for _, t := range expectedStarts {
		bucketPoints = append(bucketPoints, s.createFillPoint(p, lineItem, t, billingAnchor, commitmentCalc))
	}
	p.item.Points = s.mergeBucketPointsByWindow(bucketPoints, p.aggType)

	return totalCost
}

// createFillPoint creates a zero-usage fill point for a missing bucket window.
func (s *meterUsageService) createFillPoint(
	p *meterUsageBucketedCostParams,
	lineItem *subscription.SubscriptionLineItem,
	timestamp time.Time,
	billingAnchor *time.Time,
	calc *commitmentCalculator,
) events.UsageAnalyticPoint {
	pointCost, info, _ := calc.applyWindowCommitmentToLineItem(p.ctx, lineItem, []decimal.Decimal{decimal.Zero}, p.price)
	windowStart := truncateToBucketStart(timestamp, p.data.Params.WindowSize, billingAnchor)

	pt := events.UsageAnalyticPoint{
		Timestamp:   timestamp,
		WindowStart: windowStart,
		Usage:       decimal.Zero,
		MaxUsage:    decimal.Zero,
		Cost:        pointCost,
		EventCount:  0,
	}
	if info != nil {
		pt.ComputedCommitmentUtilizedAmount = info.ComputedCommitmentUtilizedAmount
		pt.ComputedOverageAmount = info.ComputedOverageAmount
		pt.ComputedTrueUpAmount = info.ComputedTrueUpAmount
	}
	return pt
}

// getBillingAnchorFromData retrieves the billing anchor for a subscription from AnalyticsData.
func (s *meterUsageService) getBillingAnchorFromData(data *AnalyticsData, subscriptionID string) *time.Time {
	if sub := data.SubscriptionsMap[subscriptionID]; sub != nil {
		return &sub.BillingAnchor
	}
	return nil
}

// getSingleBucketUsage returns the usage value for single-bucket calculation.
func (s *meterUsageService) getSingleBucketUsage(item *events.DetailedUsageAnalytic, aggType types.AggregationType) decimal.Decimal {
	if aggType == types.AggregationMax {
		return item.MaxUsage
	}
	return s.getCorrectUsageValue(item, aggType)
}

// sumPointCosts sums the cost of all points.
func (s *meterUsageService) sumPointCosts(points []events.UsageAnalyticPoint) decimal.Decimal {
	total := decimal.Zero
	for _, pt := range points {
		total = total.Add(pt.Cost)
	}
	return total
}

// calculateRegularCost calculates cost for regular (non-bucketed) meters.
func (s *meterUsageService) calculateRegularCost(ctx context.Context, priceService PriceService, item *events.DetailedUsageAnalytic, m *meter.Meter, p *price.Price, data *AnalyticsData) {
	item.TotalUsage = s.getCorrectUsageValue(item, m.Aggregation.Type)

	cost := priceService.CalculateCost(ctx, p, item.TotalUsage)

	if item.SubLineItemID != "" {
		lineItem := data.SubscriptionLineItems[item.SubLineItemID]

		if lineItem != nil && lineItem.HasCommitment() {
			if lineItem.CommitmentWindowed {
				if len(item.Points) > 0 {
					bucketedValues := make([]decimal.Decimal, len(item.Points))
					for i, point := range item.Points {
						bucketedValues[i] = s.getCorrectUsageValueForPoint(point, m.Aggregation.Type)
					}
					cost = s.applyLineItemCommitment(ctx, priceService, item, lineItem, p, bucketedValues, decimal.Zero)
				} else {
					cost = s.applyLineItemCommitment(ctx, priceService, item, lineItem, p, nil, cost)
				}
			} else {
				cost = s.applyLineItemCommitment(ctx, priceService, item, lineItem, p, nil, cost)
			}
		}
	}

	item.TotalCost = cost
	item.Currency = p.Currency

	for i := range item.Points {
		pointUsage := s.getCorrectUsageValueForPoint(item.Points[i], m.Aggregation.Type)
		pointCost := priceService.CalculateCost(ctx, p, pointUsage)
		item.Points[i].Cost = pointCost
	}
}

// applyLineItemCommitment applies commitment logic to the calculated cost.
func (s *meterUsageService) applyLineItemCommitment(
	ctx context.Context,
	priceService PriceService,
	item *events.DetailedUsageAnalytic,
	lineItem *subscription.SubscriptionLineItem,
	p *price.Price,
	bucketedValues []decimal.Decimal,
	defaultCost decimal.Decimal,
) decimal.Decimal {
	commitmentCalc := newCommitmentCalculator(s.logger, priceService)
	var cost decimal.Decimal
	var commitmentInfo *types.CommitmentInfo
	var err error

	if lineItem.CommitmentWindowed {
		cost, commitmentInfo, err = commitmentCalc.applyWindowCommitmentToLineItem(
			ctx, lineItem, bucketedValues, p)
		if err == nil {
			item.CommitmentInfo = commitmentInfo
			return cost
		}
		s.logger.Warnw("failed to apply window commitment", "error", err, "line_item_id", lineItem.ID)
		if defaultCost.IsZero() && len(bucketedValues) > 0 {
			return priceService.CalculateBucketedCost(ctx, p, bucketedValues)
		}
		return defaultCost
	}

	// Non-window commitment
	rawCost := defaultCost
	if rawCost.IsZero() && len(bucketedValues) > 0 {
		rawCost = priceService.CalculateBucketedCost(ctx, p, bucketedValues)
	}

	cost, commitmentInfo, err = commitmentCalc.applyCommitmentToLineItem(
		ctx, lineItem, rawCost, p)

	if err == nil {
		item.CommitmentInfo = commitmentInfo
		return cost
	}

	s.logger.Warnw("failed to apply commitment", "error", err, "line_item_id", lineItem.ID)
	return rawCost
}

// mergeBucketPointsByWindow merges bucket-level points into request-window-level points.
func (s *meterUsageService) mergeBucketPointsByWindow(points []events.UsageAnalyticPoint, aggregationType types.AggregationType) []events.UsageAnalyticPoint {
	if len(points) == 0 {
		return points
	}

	if points[0].WindowStart.IsZero() {
		return points
	}

	windowGroups := make(map[time.Time][]events.UsageAnalyticPoint)
	for _, point := range points {
		windowGroups[point.WindowStart] = append(windowGroups[point.WindowStart], point)
	}

	mergedPoints := make([]events.UsageAnalyticPoint, 0, len(windowGroups))
	for windowStart, bucketPoints := range windowGroups {
		merged := events.UsageAnalyticPoint{
			Timestamp:                        windowStart,
			WindowStart:                      windowStart,
			Cost:                             decimal.Zero,
			EventCount:                       0,
			ComputedCommitmentUtilizedAmount: decimal.Zero,
			ComputedOverageAmount:            decimal.Zero,
			ComputedTrueUpAmount:             decimal.Zero,
		}

		for _, bucket := range bucketPoints {
			merged.Cost = merged.Cost.Add(bucket.Cost)
			merged.EventCount += bucket.EventCount
			merged.ComputedCommitmentUtilizedAmount = merged.ComputedCommitmentUtilizedAmount.Add(bucket.ComputedCommitmentUtilizedAmount)
			merged.ComputedOverageAmount = merged.ComputedOverageAmount.Add(bucket.ComputedOverageAmount)
			merged.ComputedTrueUpAmount = merged.ComputedTrueUpAmount.Add(bucket.ComputedTrueUpAmount)
		}

		if aggregationType == types.AggregationMax {
			maxUsage := decimal.Zero
			for _, bucket := range bucketPoints {
				if bucket.MaxUsage.GreaterThan(maxUsage) {
					maxUsage = bucket.MaxUsage
				}
			}
			merged.Usage = maxUsage
			merged.MaxUsage = maxUsage
		} else {
			sumUsage := decimal.Zero
			for _, bucket := range bucketPoints {
				sumUsage = sumUsage.Add(bucket.Usage)
			}
			merged.Usage = sumUsage
			merged.MaxUsage = sumUsage
		}

		// Find the chronologically latest bucket to get LatestUsage
		var latestBucket *events.UsageAnalyticPoint
		for i := range bucketPoints {
			if latestBucket == nil || bucketPoints[i].Timestamp.After(latestBucket.Timestamp) {
				latestBucket = &bucketPoints[i]
			}
		}
		if latestBucket != nil {
			merged.LatestUsage = latestBucket.LatestUsage
		}

		mergedPoints = append(mergedPoints, merged)
	}

	sort.Slice(mergedPoints, func(i, j int) bool {
		return mergedPoints[i].Timestamp.Before(mergedPoints[j].Timestamp)
	})

	return mergedPoints
}

// getCorrectUsageValue returns the correct usage value based on the meter's aggregation type.
func (s *meterUsageService) getCorrectUsageValue(item *events.DetailedUsageAnalytic, aggregationType types.AggregationType) decimal.Decimal {
	switch aggregationType {
	case types.AggregationCountUnique:
		return decimal.NewFromInt(int64(item.CountUniqueUsage))
	case types.AggregationMax:
		return item.MaxUsage
	case types.AggregationLatest:
		return item.LatestUsage
	case types.AggregationSum, types.AggregationSumWithMultiplier, types.AggregationAvg, types.AggregationWeightedSum:
		return item.TotalUsage
	default:
		return item.TotalUsage
	}
}

// getCorrectUsageValueForPoint returns the correct usage value for a time series point based on aggregation type.
func (s *meterUsageService) getCorrectUsageValueForPoint(point events.UsageAnalyticPoint, aggregationType types.AggregationType) decimal.Decimal {
	switch aggregationType {
	case types.AggregationCountUnique:
		return decimal.NewFromInt(int64(point.CountUniqueUsage))
	case types.AggregationMax:
		return point.MaxUsage
	case types.AggregationLatest:
		return point.LatestUsage
	case types.AggregationSum, types.AggregationSumWithMultiplier, types.AggregationAvg, types.AggregationWeightedSum:
		return point.Usage
	default:
		return point.Usage
	}
}

// ensure meterUsageService implements MeterUsageService
var _ MeterUsageService = (*meterUsageService)(nil)
