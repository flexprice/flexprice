package service

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/group"
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

	// AnalyticsResult is set by GetSubscriptionMeterUsage when isAnalyticsQuery is true.
	// It carries the raw per-group row (Source, Properties, etc.) for analytics display.
	AnalyticsResult *events.MeterUsageDetailedResult

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

	// Analytics-only filters. These are forwarded to the windowed
	// GetDetailedAnalytics call inside GetSubscriptionMeterUsage. They have no
	// effect on the scalar GetUsageMultiMeter path used by billing.
	MeterIDs        []string            // when non-empty, only these meters are queried
	GroupBy         []string            // appended after "meter_id"; "meter_id" is always present
	PropertyFilters map[string][]string // e.g. {"model": ["gpt-4"]}
	Sources         []string            // event source filter
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

	// 5. Collect usage line items and fetch prices with meter expansion.
	// Per-bucket prices are referenced only from CommitmentTimeBuckets, so collect
	// them too — otherwise bucket summaries would under-report charges as zero.
	priceIDs := make([]string, 0, len(lineItems))
	for _, item := range lineItems {
		if item.PriceType == types.PRICE_TYPE_USAGE && item.MeterID != "" {
			priceIDs = append(priceIDs, item.PriceID)
		}
		for _, b := range item.CommitmentTimeBuckets {
			if b.PriceID != "" {
				priceIDs = append(priceIDs, b.PriceID)
			}
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
				s.logger.Info(context.Background(), "failed to fetch features for meter mapping", "error", err)
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

	// Filter meterToLineItems to the caller-requested subset (pushes filtering to ClickHouse).
	// meterIDSet is kept alive so the zero-usage fallback loop (step 12) also respects it.
	var meterIDSet map[string]struct{}
	if len(req.MeterIDs) > 0 {
		meterIDSet = lo.SliceToMap(req.MeterIDs, func(id string) (string, struct{}) { return id, struct{}{} })
		for meterID := range meterToLineItems {
			if _, ok := meterIDSet[meterID]; !ok {
				delete(meterToLineItems, meterID)
			}
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
			key := dateRangeGroup{
				Start:   start,
				End:     end,
				AggType: meterAggType[meterID],
			}
			standardGroups[key] = append(standardGroups[key], &lineItemWithMeter{Item: item, MeterID: meterID})
		}
	}

	for group, lineItemsInGroup := range standardGroups {
		meterIDs := make([]string, 0, len(lineItemsInGroup))
		for _, liw := range lineItemsInGroup {
			meterIDs = append(meterIDs, liw.MeterID)
		}
		meterIDs = lo.Uniq(meterIDs)

		// Use GetDetailedAnalytics when a window size, property/source filters, or extra
		// group-by dimensions are requested (analytics path). Fall back to the faster
		// GetUsageMultiMeter only for plain scalar billing queries.
		isAnalyticsQuery := req.WindowSize != "" ||
			len(req.PropertyFilters) > 0 ||
			len(req.Sources) > 0 ||
			len(req.GroupBy) > 0

		if isAnalyticsQuery {
			// Decide whether user-supplied group_by adds dimensions beyond meter_id.
			// If yes, split the batch: commitment line items query with meter_id only
			// (clean aggregate for applyLineItemCommitment), non-commitment items query
			// with the full group_by (per-group breakdown in the response).
			hasExtraGroupBy := false
			for _, g := range req.GroupBy {
				if g != "" && g != "meter_id" {
					hasExtraGroupBy = true
					break
				}
			}

			var commitmentLIs, nonCommitmentLIs []*lineItemWithMeter
			if hasExtraGroupBy {
				for _, liw := range lineItemsInGroup {
					if liw.Item != nil && liw.Item.HasAnyCommitment() {
						commitmentLIs = append(commitmentLIs, liw)
					} else {
						nonCommitmentLIs = append(nonCommitmentLIs, liw)
					}
				}
			} else {
				// No extra group_by → one dr per meter for everyone, single query.
				nonCommitmentLIs = lineItemsInGroup
			}

			if err := s.queryAndAppendAnalyticsEntries(ctx, req, group, commitmentLIs, false, externalCustomerIDs, result); err != nil {
				return nil, err
			}
			if err := s.queryAndAppendAnalyticsEntries(ctx, req, group, nonCommitmentLIs, hasExtraGroupBy, externalCustomerIDs, result); err != nil {
				return nil, err
			}
		} else {
			// Scalar billing query — use GetUsageMultiMeter for batch efficiency.
			// Analytics-only filters (PropertyFilters, Sources, GroupBy) are never
			// set by billing callers, so nothing is lost here.
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
					usage.EventCount = r.EventCount
				}
				result.LineItemUsages = append(result.LineItemUsages, usage)
			}
		}
	}

	// 11. Query bucketed meters per line item (already uses GetPeriodStart/End)
	//
	// When analytics filters (PropertyFilters / Sources) are active, suppress
	// bucketed line-item entries that have no matching events — same rationale
	// as the gates in queryAndAppendAnalyticsEntries and the step-12 loop:
	// surfacing them would misrepresent the filtered slice (and pin commitment
	// cost for committed items). Without filters, empty bucketed line items
	// continue to be surfaced as zero-usage rows (preserves the contract that
	// committed line items can have their commitment fire on no usage).
	skipBucketedZeros := len(req.PropertyFilters) > 0 || len(req.Sources) > 0

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
				req.PropertyFilters, req.Sources,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to query bucketed meter usage for meter %s: %w", meterID, err)
			}

			if skipBucketedZeros && (bucketedResult == nil || len(bucketedResult.Results) == 0) {
				continue
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

	// 12. Zero-usage entries for line items that had no data.
	// Skip when analytics filters (PropertyFilters / Sources) are active: those
	// filters restrict the SQL result by design, so "line item has no rows" means
	// the filter excluded them — not that there was zero usage. Surfacing a
	// zero-usage row for every filtered-out line item would misrepresent the
	// filtered slice and (for committed line items) pin commitment cost regardless
	// of the filter. Mirrors the skipSyntheticZeros gate in
	// featureUsageTrackingService.fetchAnalyticsData.
	skipSyntheticZeros := len(req.PropertyFilters) > 0 || len(req.Sources) > 0

	if !skipSyntheticZeros {
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
			if meterIDSet != nil {
				if _, ok := meterIDSet[item.MeterID]; !ok {
					continue
				}
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
	}

	return result, nil
}

// queryAndAppendAnalyticsEntries runs one GetDetailedAnalytics call for the
// given line items and appends LineItemMeterUsage entries to result.
//
// useUserGroupBy=false: GroupBy is just ["meter_id"] → one dr per meter →
// one entry per (line item, meter). Used for commitment line items so the
// aggregated value feeds applyLineItemCommitment cleanly.
//
// useUserGroupBy=true: GroupBy includes user-supplied dimensions → N drs per
// meter → N entries per (line item, meter). Used for non-commitment items so
// the response carries per-group Source/Properties.
func (s *meterUsageService) queryAndAppendAnalyticsEntries(
	ctx context.Context,
	req *GetSubscriptionMeterUsageRequest,
	group dateRangeGroup,
	lis []*lineItemWithMeter,
	useUserGroupBy bool,
	externalCustomerIDs []string,
	result *SubscriptionMeterUsage,
) error {
	if len(lis) == 0 {
		return nil
	}

	meterIDs := lo.Uniq(lo.Map(lis, func(liw *lineItemWithMeter, _ int) string { return liw.MeterID }))

	// meter_id must always be in GroupBy or the repo drops it from SELECT and
	// result.MeterID comes back as "".
	groupBy := []string{"meter_id"}
	if useUserGroupBy {
		for _, g := range req.GroupBy {
			if g != "" && g != "meter_id" {
				groupBy = append(groupBy, g)
			}
		}
	}

	detailedResults, err := s.repo.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
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
		PropertyFilters:     req.PropertyFilters,
		Sources:             req.Sources,
		GroupBy:             groupBy,
	})
	if err != nil {
		return fmt.Errorf("failed to query meter usage analytics for group %v: %w", group, err)
	}

	resultsByMeter := make(map[string][]*events.MeterUsageDetailedResult, len(detailedResults))
	for _, dr := range detailedResults {
		resultsByMeter[dr.MeterID] = append(resultsByMeter[dr.MeterID], dr)
	}

	// When analytics filters are active, suppress the zero-usage entry for line
	// items whose analytics query returned no rows — that's the filter excluding
	// them, not zero usage. Surfacing a zero-usage row would misrepresent the
	// filtered slice. Mirrors the step-12 skipSyntheticZeros gate.
	skipSyntheticZeros := len(req.PropertyFilters) > 0 || len(req.Sources) > 0

	for _, liw := range lis {
		drs := resultsByMeter[liw.MeterID]
		if len(drs) == 0 {
			if skipSyntheticZeros {
				continue
			}
			// No data — zero-usage entry; step 12 commitment check uses LineItem.HasAnyCommitment().
			result.LineItemUsages = append(result.LineItemUsages, &LineItemMeterUsage{
				LineItem:    liw.Item,
				MeterID:     liw.MeterID,
				Meter:       result.MeterMap[liw.MeterID],
				Price:       result.PriceMap[liw.Item.PriceID],
				PeriodStart: group.Start,
				PeriodEnd:   group.End,
			})
			continue
		}

		for _, dr := range drs {
			if dr == nil {
				continue
			}
			result.LineItemUsages = append(result.LineItemUsages, &LineItemMeterUsage{
				LineItem:        liw.Item,
				MeterID:         liw.MeterID,
				Meter:           result.MeterMap[liw.MeterID],
				Price:           result.PriceMap[liw.Item.PriceID],
				PeriodStart:     group.Start,
				PeriodEnd:       group.End,
				Usage:           dr.TotalUsage,
				EventCount:      dr.EventCount,
				Points:          dr.Points,
				AnalyticsResult: dr,
			})
		}
	}

	return nil
}

// queryBucketedMeterUsage queries the meter_usage table for a single bucketed meter,
// returning a per-bucket AggregationResult. propertyFilters and sources are forwarded
// from analytics callers; pass nil for billing callers that don't use them.
func (s *meterUsageService) queryBucketedMeterUsage(
	ctx context.Context,
	m *meter.Meter,
	externalCustomerIDs []string,
	periodStart, periodEnd time.Time,
	billingAnchor *time.Time,
	useFinal bool,
	propertyFilters map[string][]string,
	sources []string,
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
		PropertyFilters:     propertyFilters,
		Sources:             sources,
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

	// feature_id is a feature_usage-table column; meter_usage doesn't carry it.
	// Since feature.meter_id is 1:1, group_by=[feature_id] is semantically
	// equivalent to group_by=[meter_id] — rewrite at the entry point so the
	// downstream query builder (which only knows meter_id/source/properties.*)
	// accepts it, and the converter populates FeatureID from the meter→feature
	// lookup. Dedupe so [feature_id, meter_id] doesn't become [meter_id, meter_id].
	if len(params.GroupBy) > 0 {
		rewritten := make([]string, 0, len(params.GroupBy))
		for _, g := range params.GroupBy {
			if g == "feature_id" {
				g = "meter_id"
			}
			rewritten = append(rewritten, g)
		}
		params.GroupBy = lo.Uniq(rewritten)
	}

	// Resolve FeatureIDs → MeterIDs (only when MeterIDs not already specified).
	// Fail closed: a feature-scoped request must not silently broaden to all meters.
	if len(params.FeatureIDs) > 0 && len(params.MeterIDs) == 0 {
		features, err := s.FeatureRepo.ListByIDs(ctx, params.FeatureIDs)
		if err != nil {
			return nil, ierr.NewError("failed to resolve feature IDs to meter IDs").Mark(ierr.ErrDatabase)
		}
		for _, f := range features {
			if f.MeterID != "" {
				params.MeterIDs = append(params.MeterIDs, f.MeterID)
			}
		}
		params.MeterIDs = lo.Uniq(params.MeterIDs)
		if len(params.MeterIDs) == 0 {
			return &dto.GetUsageAnalyticsResponse{
				TotalCost: decimal.Zero,
				Items:     []dto.UsageAnalyticItem{},
			}, nil
		}
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

		// Clamp the query window by CancelledAt for cancelled subs. meter_usage
		// has no per-event subscription linkage — the per-line-item date bound
		// (lineItem.GetPeriodEnd) falls back to the request EndTime when the
		// line item has no EndDate, which is the common case after a cancel.
		// Without this clamp, a cancelled sub's line items would re-attribute
		// post-cancellation events from whichever sub is now active for the
		// same meter. Skip entirely when the clamped window has no overlap.
		subEndTime := params.EndTime
		if sub.SubscriptionStatus == types.SubscriptionStatusCancelled && sub.CancelledAt != nil {
			if sub.CancelledAt.Before(subEndTime) {
				subEndTime = *sub.CancelledAt
			}
		}
		if !subEndTime.After(params.StartTime) {
			continue
		}

		usage, err := s.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
			SubscriptionID:  sub.ID,
			StartTime:       params.StartTime,
			EndTime:         subEndTime,
			WindowSize:      params.WindowSize,
			BillingAnchor:   billingAnchor,
			UseFinal:        params.UseFinal,
			IncludeFeatures: true,
			MeterIDs:        params.MeterIDs,
			GroupBy:         params.GroupBy,
			PropertyFilters: params.PropertyFilters,
			Sources:         params.Sources,
		})
		if err != nil {
			s.logger.Info(ctx, "failed to get subscription meter usage, skipping",
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
			s.logger.Info(ctx, "failed to calculate costs for meter usage analytics, costs will be zero",
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

	// Enrich with Groups + parent prices (always-on) and expand-gated Plans/Addons
	s.enrichAnalyticsDataForResponse(ctx, data, params)

	// Convert to response DTO
	return s.toUsageAnalyticsResponseDTO(ctx, data, data.Meters, params), nil
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
		Addons:                make(map[string]*addon.Addon),
		Groups:                make(map[string]*group.Group),
		Analytics:             make([]*events.DetailedUsageAnalytic, 0),
		Params: &events.UsageAnalyticsParams{
			TenantID:           params.TenantID,
			EnvironmentID:      params.EnvironmentID,
			ExternalCustomerID: params.ExternalCustomerID,
			StartTime:          params.StartTime,
			EndTime:            params.EndTime,
			GroupBy:            params.GroupBy,
			WindowSize:         params.WindowSize,
			PropertyFilters:    params.PropertyFilters,
			Sources:            params.Sources,
			AggregationTypes:   params.AggregationTypes,
			BillingAnchor:      params.BillingAnchor,
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
		maps.Copy(data.Meters, su.MeterMap)
		maps.Copy(data.Prices, su.PriceMap)
		maps.Copy(data.Features, su.FeatureMap)

		// Convert each LineItemMeterUsage → DetailedUsageAnalytic.
		// Skip line items with zero usage to avoid noise, EXCEPT when the line item
		// has a commitment — those need to flow through calculateCosts so the
		// committed minimum (and true-up, if windowed) is applied even with no usage.
		for _, lu := range su.LineItemUsages {
			if lu.Usage.IsZero() && lu.EventCount == 0 && len(lu.Points) == 0 && lu.BucketedResult == nil {
				if lu.LineItem == nil || !lu.LineItem.HasAnyCommitment() {
					continue
				}
			}

			analytic := &events.DetailedUsageAnalytic{
				MeterID: lu.MeterID,
			}
			if lu.AnalyticsResult != nil {
				analytic.Source = lu.AnalyticsResult.Source
				analytic.Sources = lu.AnalyticsResult.Sources
				analytic.Properties = lu.AnalyticsResult.Properties
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

			// Convert time-series points. p.TotalUsage is the primary aggregation
			// value (see buildMeterUsageAggregationColumns), so it works for every
			// meter type without per-aggregation routing.
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

// enrichAnalyticsDataForResponse fetches downstream objects the response
// builder needs:
//   - Groups (unconditional, scoped to features that have a GroupID) — item.Group is always-on.
//   - Parent prices for SUBSCRIPTION-typed override prices — needed so PlanID/AddOnID
//     can be derived from the parent's EntityType/EntityID.
//   - Plans / Addons (only when expand=plan / expand=addon) — required only when the
//     response needs the embedded object; PlanID/AddOnID derivation uses price.EntityID alone.
//
// Errors are logged and swallowed — analytics should still return without enrichment.
func (s *meterUsageService) enrichAnalyticsDataForResponse(
	ctx context.Context,
	data *AnalyticsData,
	params *events.MeterUsageDetailedAnalyticsParams,
) {
	if data == nil {
		return
	}
	if data.Plans == nil {
		data.Plans = make(map[string]*plan.Plan)
	}
	if data.Addons == nil {
		data.Addons = make(map[string]*addon.Addon)
	}
	if data.Groups == nil {
		data.Groups = make(map[string]*group.Group)
	}

	// Groups — always populate item.Group, but only fetch when features need them.
	groupIDs := make(map[string]struct{})
	for _, f := range data.Features {
		if f != nil && f.GroupID != "" {
			groupIDs[f.GroupID] = struct{}{}
		}
	}
	for gid := range groupIDs {
		if _, ok := data.Groups[gid]; ok {
			continue
		}
		g, err := s.GroupRepo.Get(ctx, gid)
		if err != nil {
			s.logger.Info(context.Background(), "failed to fetch group for meter usage analytics", "group_id", gid, "error", err)
			continue
		}
		data.Groups[gid] = g
	}
	for _, f := range data.Features {
		if f != nil && f.GroupID != "" {
			f.Group = data.Groups[f.GroupID]
		}
	}

	// Parent prices — override (subscription-scoped) prices carry the plan/addon
	// link only on the parent row. Fetch any missing parents into data.Prices so
	// the response builder can resolve PlanID/AddOnID via one extra lookup.
	parentIDs := make([]string, 0)
	seen := make(map[string]struct{})
	for _, p := range data.Prices {
		if p == nil || p.EntityType != types.PRICE_ENTITY_TYPE_SUBSCRIPTION || p.ParentPriceID == "" {
			continue
		}
		if _, ok := data.Prices[p.ParentPriceID]; ok {
			continue
		}
		if _, ok := seen[p.ParentPriceID]; ok {
			continue
		}
		seen[p.ParentPriceID] = struct{}{}
		parentIDs = append(parentIDs, p.ParentPriceID)
	}
	if len(parentIDs) > 0 {
		priceService := NewPriceService(s.ServiceParams)
		parentFilter := types.NewNoLimitPriceFilter()
		parentFilter.PriceIDs = parentIDs
		parentFilter.AllowExpiredPrices = true
		parentList, err := priceService.GetPrices(ctx, parentFilter)
		if err != nil {
			s.logger.Info(context.Background(), "failed to fetch parent prices for meter usage analytics", "error", err)
		} else {
			for _, p := range parentList.Items {
				data.Prices[p.ID] = p.Price
			}
		}
	}

	expand := lo.SliceToMap(params.Expand, func(e string) (string, struct{}) { return e, struct{}{} })

	// Plans — only needed when caller asked to expand them. PlanID is already
	// derivable from price.EntityID without fetching the Plan object.
	if _, want := expand["plan"]; want {
		planFilter := types.NewNoLimitPlanFilter()
		plans, err := s.PlanRepo.List(ctx, planFilter)
		if err != nil {
			s.logger.Info(context.Background(), "failed to fetch plans for meter usage analytics", "error", err)
		} else {
			for _, p := range plans {
				data.Plans[p.ID] = p
			}
		}
	}

	// Addons — same gating as Plans.
	if _, want := expand["addon"]; want {
		addonFilter := types.NewNoLimitAddonFilter()
		addons, err := s.AddonRepo.List(ctx, addonFilter)
		if err != nil {
			s.logger.Info(context.Background(), "failed to fetch addons for meter usage analytics", "error", err)
		} else {
			for _, a := range addons {
				data.Addons[a.ID] = a
			}
		}
	}
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
	var bucketedMeterIDs, standardMeterIDs []string
	for _, m := range meters {
		switch {
		case m.IsBucketedMaxMeter(), m.IsBucketedSumMeter():
			bucketedMeterIDs = append(bucketedMeterIDs, m.ID)
		default:
			standardMeterIDs = append(standardMeterIDs, m.ID)
		}
	}

	allResults := make([]*events.MeterUsageDetailedResult, 0, len(bucketedMeterIDs)+len(standardMeterIDs))
	for _, meterID := range bucketedMeterIDs {
		results, err := s.getBucketedMeterAnalytics(ctx, params, meterMap[meterID])
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
	}

	// Standard (non-bucketed) meters: one repo call per aggregation type.
	// Mirrors the subscription path, which already splits by AggType via
	// dateRangeGroup (see queryAndAppendAnalyticsEntries). N is small in
	// practice — most requests touch 1-2 aggregation types — and keeping the
	// two paths consistent is worth more than collapsing this loop.
	// Without splitting, buildMeterUsageAggregationColumns would emit a
	// single primary total_usage expression chosen by priority order, which
	// is wrong for every non-winning meter in a mixed-type request.
	var standardTargets []string
	if len(standardMeterIDs) > 0 {
		standardTargets = standardMeterIDs
	} else if len(params.MeterIDs) == 0 {
		// Catch-all: enumerate every standard meter we already fetched.
		for _, m := range meters {
			if !m.IsBucketedMaxMeter() && !m.IsBucketedSumMeter() {
				standardTargets = append(standardTargets, m.ID)
			}
		}
	}

	if len(standardTargets) > 0 {
		byAggType := make(map[types.AggregationType][]string)
		for _, mid := range standardTargets {
			m := meterMap[mid]
			if m == nil {
				continue
			}
			byAggType[m.Aggregation.Type] = append(byAggType[m.Aggregation.Type], mid)
		}

		for aggType, meterIDs := range byAggType {
			subParams := *params
			subParams.MeterIDs = meterIDs
			subParams.AggregationTypes = []types.AggregationType{aggType}
			// Always group by meter_id so the repo populates result.MeterID even
			// when the subquery has a single meter — the converter keys analytics
			// by MeterID downstream.
			if !lo.Contains(subParams.GroupBy, "meter_id") {
				subParams.GroupBy = append([]string{"meter_id"}, subParams.GroupBy...)
			}
			results, err := s.repo.GetDetailedAnalytics(ctx, &subParams)
			if err != nil {
				return nil, err
			}
			allResults = append(allResults, results...)
		}
	}

	// Build minimal AnalyticsData (no subscription context)
	data := &AnalyticsData{
		SubscriptionLineItems: make(map[string]*subscription.SubscriptionLineItem),
		SubscriptionsMap:      make(map[string]*subscription.Subscription),
		Features:              make(map[string]*feature.Feature),
		Meters:                meterMap,
		Prices:                make(map[string]*price.Price),
		Plans:                 make(map[string]*plan.Plan),
		Addons:                make(map[string]*addon.Addon),
		Groups:                make(map[string]*group.Group),
		Analytics:             make([]*events.DetailedUsageAnalytic, 0, len(allResults)),
		Params: &events.UsageAnalyticsParams{
			TenantID:           params.TenantID,
			EnvironmentID:      params.EnvironmentID,
			ExternalCustomerID: params.ExternalCustomerID,
			StartTime:          params.StartTime,
			EndTime:            params.EndTime,
			GroupBy:            params.GroupBy,
			WindowSize:         params.WindowSize,
			PropertyFilters:    params.PropertyFilters,
			Sources:            params.Sources,
			AggregationTypes:   params.AggregationTypes,
			BillingAnchor:      params.BillingAnchor,
		},
	}

	// Resolve features
	meterIDs := lo.Keys(meterMap)
	if len(meterIDs) > 0 {
		featureFilter := types.NewNoLimitFeatureFilter()
		featureFilter.MeterIDs = meterIDs
		features, err := s.FeatureRepo.List(ctx, featureFilter)
		if err != nil {
			s.logger.Info(context.Background(), "failed to fetch features for meter mapping", "error", err)
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

	// Convert results to analytics. r.TotalUsage / p.TotalUsage hold the primary
	// aggregation value courtesy of buildMeterUsageAggregationColumns, so no
	// per-aggregation routing is needed here.
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

	// Enrich with Groups + parent prices (always-on) and expand-gated Plans/Addons
	s.enrichAnalyticsDataForResponse(ctx, data, params)

	// Convert to response DTO (no cost calculation without subscription context)
	return s.toUsageAnalyticsResponseDTO(ctx, data, meterMap, params), nil
}

// ---------------------------------------------------------------------------
// Response DTO conversion
// ---------------------------------------------------------------------------

// toUsageAnalyticsResponseDTO converts the enriched analytics data to the response DTO.
//
// Parity with feature-usage's builder (feature_usage_tracking.go ~line 2938):
//   - Always populates FeatureID/Name, Unit, AggregationType, TotalUsage, EventCount,
//     Properties, CommitmentInfo, Points, WindowSize.
//   - Populates TotalUsageDisplay+ReportingUnit from feature.ReportingUnit.
//   - Populates Group from feature.GroupID via data.Groups.
//   - Derives PlanID/AddOnID from price.EntityType (and parent price for subscription overrides).
//   - Honors params.Expand to attach Price/Meter/Feature/SubscriptionLineItem/Plan/Addon
//     objects, and treats Sources as expand-driven (expand=source).
func (s *meterUsageService) toUsageAnalyticsResponseDTO(
	ctx context.Context,
	data *AnalyticsData,
	meterMap map[string]*meter.Meter,
	params *events.MeterUsageDetailedAnalyticsParams,
) *dto.GetUsageAnalyticsResponse {
	response := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.Zero,
		Currency:  data.Currency,
		Items:     make([]dto.UsageAnalyticItem, 0, len(data.Analytics)),
	}

	expandMap := make(map[string]bool, len(params.Expand))
	for _, e := range params.Expand {
		expandMap[e] = true
	}

	for _, analytic := range data.Analytics {
		item := dto.UsageAnalyticItem{
			FeatureID:       analytic.FeatureID,
			PriceID:         analytic.PriceID,
			MeterID:         analytic.MeterID,
			SubLineItemID:   analytic.SubLineItemID,
			SubscriptionID:  analytic.SubscriptionID,
			FeatureName:     analytic.FeatureName,
			EventName:       analytic.EventName,
			Source:          analytic.Source,
			Unit:            analytic.Unit,
			UnitPlural:      analytic.UnitPlural,
			AggregationType: analytic.AggregationType,
			TotalUsage:      analytic.TotalUsage,
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

		// Reporting unit conversion when the feature exposes one.
		if f, ok := data.Features[analytic.FeatureID]; ok && f != nil && f.ReportingUnit != nil {
			if reportingUsage, err := f.ToReportingValue(analytic.TotalUsage); err == nil {
				item.TotalUsageDisplay = reportingUsage.String()
				item.ReportingUnit = f.ReportingUnit
			}
		}

		// Group attached to the feature (already backfilled by enrichAnalyticsDataForResponse).
		if f, ok := data.Features[analytic.FeatureID]; ok && f != nil && f.GroupID != "" {
			item.Group = data.Groups[f.GroupID]
		}

		// Sources is expand-driven (mirrors feature-usage). Use the array from
		// the analytic, which the repo populates via groupUniqArray(source).
		if expandMap["source"] {
			item.Sources = analytic.Sources
		}

		// Derive PlanID/AddOnID from price.EntityType. For SUBSCRIPTION-typed
		// override prices, walk to the parent (enrichAnalyticsDataForResponse
		// adds the parent rows to data.Prices).
		if p, ok := data.Prices[analytic.PriceID]; ok && p != nil {
			switch p.EntityType {
			case types.PRICE_ENTITY_TYPE_PLAN:
				item.PlanID = p.EntityID
			case types.PRICE_ENTITY_TYPE_ADDON:
				item.AddOnID = p.EntityID
			case types.PRICE_ENTITY_TYPE_SUBSCRIPTION:
				if p.ParentPriceID != "" {
					if parent, ok := data.Prices[p.ParentPriceID]; ok && parent != nil {
						switch parent.EntityType {
						case types.PRICE_ENTITY_TYPE_PLAN:
							item.PlanID = parent.EntityID
						case types.PRICE_ENTITY_TYPE_ADDON:
							item.AddOnID = parent.EntityID
						}
					}
				}
			}
			if expandMap["price"] {
				item.Price = &dto.PriceResponse{Price: p}
			}
		}

		// Window size: bucketed meters expose their bucket size; others use the request.
		if m, ok := meterMap[analytic.MeterID]; ok {
			if m.HasBucketSize() {
				item.WindowSize = m.Aggregation.BucketSize
			} else {
				item.WindowSize = params.WindowSize
			}
			if expandMap["meter"] {
				item.Meter = m
			}
		} else {
			item.WindowSize = params.WindowSize
		}

		if expandMap["feature"] && analytic.FeatureID != "" {
			if f, ok := data.Features[analytic.FeatureID]; ok {
				item.Feature = f
			}
		}

		if expandMap["subscription_line_item"] && analytic.SubLineItemID != "" {
			if li, ok := data.SubscriptionLineItems[analytic.SubLineItemID]; ok {
				item.SubscriptionLineItem = li
			}
		}

		if expandMap["plan"] && item.PlanID != "" {
			if pl, ok := data.Plans[item.PlanID]; ok {
				item.Plan = pl
			}
		}

		if expandMap["addon"] && item.AddOnID != "" {
			if ad, ok := data.Addons[item.AddOnID]; ok {
				item.Addon = ad
			}
		}

		// Points are computed internally for bucketed cost calc regardless of request;
		// only expose them when the caller asked for a window_size (mirrors feature_usage).
		if params.WindowSize != "" {
			// Resolve the line item for bucket attribution (nil when no buckets).
			var lineItemForBucket *subscription.SubscriptionLineItem
			if params.BreakdownBucket && analytic.SubLineItemID != "" {
				lineItemForBucket = data.SubscriptionLineItems[analytic.SubLineItemID]
				if lineItemForBucket != nil && !lineItemForBucket.HasCommitmentTimeBuckets() {
					lineItemForBucket = nil
				}
			}

			for _, point := range analytic.Points {
				dtoPoint := dto.UsageAnalyticPoint{
					Timestamp:                        point.Timestamp,
					Usage:                            point.Usage,
					Cost:                             point.Cost,
					EventCount:                       point.EventCount,
					ComputedCommitmentUtilizedAmount: point.ComputedCommitmentUtilizedAmount,
					ComputedOverageAmount:            point.ComputedOverageAmount,
					ComputedTrueUpAmount:             point.ComputedTrueUpAmount,
				}
				// Per-point bucket identity (only when the point's whole window
				// fits inside a single bucket).
				if lineItemForBucket != nil {
					if id, priceID, ok := bucketIDForPointWindow(lineItemForBucket.CommitmentTimeBuckets, point.Timestamp, params.WindowSize); ok {
						dtoPoint.BucketID = id
						dtoPoint.PriceID = priceID
					}
				}
				item.Points = append(item.Points, dtoPoint)
			}

			// Bucket-level summaries.
			if lineItemForBucket != nil {
				priceService := NewPriceService(s.ServiceParams)
				item.BucketSummaries = buildBucketSummaries(ctx, priceService, item.Points, lineItemForBucket, data)
			}
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
		PropertyFilters:    params.PropertyFilters,
		Sources:            params.Sources,
	}

	if len(params.ExternalCustomerIDs) > 0 {
		bucketParams.ExternalCustomerIDs = params.ExternalCustomerIDs
	}

	aggResult, err := s.repo.GetUsageForBucketedMeters(ctx, bucketParams)
	if err != nil {
		s.logger.Error(ctx, "failed to get bucketed meter usage", "error", err, "meter_id", m.ID)
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
		s.logger.Info(context.Background(), "failed to get event count for bucketed meter, defaulting to 0", "error", err, "meter_id", m.ID)
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
		PropertyFilters:    params.PropertyFilters,
		Sources:            params.Sources,
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

	// when any child sub is SubscriptionTypeInherited, additionally load its parent
	// so parent-scoped line items / commitments are visible to analytics.
	parentSubIDs := make([]string, 0)
	for _, subResp := range subsList.Items {
		if subResp.Subscription.SubscriptionType == types.SubscriptionTypeInherited &&
			subResp.Subscription.ParentSubscriptionID != nil {
			parentSubIDs = append(parentSubIDs, lo.FromPtr(subResp.Subscription.ParentSubscriptionID))
		}
	}
	if len(parentSubIDs) > 0 {
		parentFilter := types.NewNoLimitSubscriptionFilter()
		parentFilter.WithLineItems = true
		parentFilter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeParent}
		parentFilter.SubscriptionIDs = parentSubIDs
		parentFilter.SubscriptionStatus = []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
			types.SubscriptionStatusTrialing,
			types.SubscriptionStatusPaused,
			types.SubscriptionStatusCancelled,
		}
		parentsList, err := subService.ListSubscriptions(ctx, parentFilter)
		if err != nil {
			return cust, nil, err
		}
		subsList.Items = append(subsList.Items, parentsList.Items...)
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

	// Analytics filters (property_filters, sources) restrict the SQL result set
	// to a subset of the customer's actual usage. Commitment / overage / true-up
	// are billing concepts tied to the FULL usage in the period — applying them
	// to a filtered subset surfaces misleading values (e.g. a filter that yields
	// zero matching events would otherwise show the full commitment as unutilized
	// and report it as cost). When any analytics-only filter is active, skip
	// commitment application and report the raw filtered cost.
	skipCommitment := len(data.Params.PropertyFilters) > 0 || len(data.Params.Sources) > 0

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
			s.calculateBucketedCost(ctx, priceService, item, p, m, data, skipCommitment)
		} else {
			s.calculateRegularCost(ctx, priceService, item, m, p, data, skipCommitment)
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

// shouldFillWindow reports whether an EMPTY window starting at t needs a
// synthetic zero-usage fill point. Filling only matters where commitment math
// can produce a charge for an empty window: any window when the line item
// carries its own (top-level) commitment, or windows inside a commitment time
// bucket otherwise. Empty windows outside both bill $0 regardless, so filling
// them would only add noise points.
func shouldFillWindow(lineItem *subscription.SubscriptionLineItem, t time.Time) bool {
	if lineItem.HasTrueUpEnabled() {
		return true
	}
	_, ok := lineItem.CommitmentTimeBuckets.BucketIndexAt([]time.Time{t}, 0)
	return ok
}

// calculateBucketedCost calculates cost for bucketed max/sum meters.
// skipCommitment forces hasCommitment=false when set; used for analytics queries
// with property/source filters where applying commitment over a filtered subset
// of events would surface misleading commitment/overage/true-up amounts.
func (s *meterUsageService) calculateBucketedCost(ctx context.Context, priceService PriceService, item *events.DetailedUsageAnalytic, p *price.Price, m *meter.Meter, data *AnalyticsData, skipCommitment bool) {
	params := &meterUsageBucketedCostParams{ctx, priceService, item, p, data, m.Aggregation.Type, m.Aggregation.BucketSize}
	lineItem := data.SubscriptionLineItems[item.SubLineItemID]
	hasCommitment := !skipCommitment && lineItem != nil && lineItem.HasAnyCommitment()
	isWindowed := hasCommitment && lineItem.CommitmentWindowed
	// needsWindowedFill gates the per-window fill paths (fillMissingWindowsAndRecalculate,
	// fillZeroUsageWindows). True-up requires filling missing windows so commitment can be
	// charged for them too — but only meaningful when the commitment is itself windowed.
	// Bucket-level TrueUpEnabled counts too: a true-up bucket needs its empty windows
	// filled even when the line item's top-level true-up flag is off.
	// Non-windowed commitments with TrueUpEnabled still get true-up applied at the
	// aggregate level inside applyCommitmentToLineItem; no per-window fill needed.
	needsWindowedFill := isWindowed && lineItem.HasTrueUpEnabled()

	var cost decimal.Decimal

	if len(item.Points) > 0 {
		cost = s.processPointsWithBuckets(params, lineItem, hasCommitment, isWindowed, needsWindowedFill)
	} else {
		cost = s.processSingleBucket(params, lineItem, hasCommitment, isWindowed, needsWindowedFill)
	}

	item.TotalCost = cost
	item.Currency = p.Currency
}

// processPointsWithBuckets handles the case where we have time-series points to
// process. Windowed commitment runs in ONE pass over the window grid (filled
// with empty windows first when true-up needs them); non-windowed paths stamp
// plain per-point costs.
func (s *meterUsageService) processPointsWithBuckets(
	p *meterUsageBucketedCostParams,
	lineItem *subscription.SubscriptionLineItem,
	hasCommitment, isWindowed, needsWindowedFill bool,
) decimal.Decimal {
	var cost decimal.Decimal
	switch {
	case isWindowed && needsWindowedFill && p.bucketSize != "":
		cost = s.fillWindowsAndApplyCommitment(p, lineItem)
	case isWindowed:
		cost = s.applyWindowCommitmentToPoints(p, lineItem, p.item.Points)
	case !hasCommitment:
		cost = p.priceService.CalculateBucketedCost(p.ctx, p.price, s.extractBucketValues(p.item.Points, p.aggType))
		s.stampPointCosts(p.ctx, p.priceService, p.item, p.price)
	default:
		// Non-windowed commitment applies to the period aggregate.
		bucketedValues := s.extractBucketValues(p.item.Points, p.aggType)
		cost = s.applyLineItemCommitment(p.ctx, p.priceService, p.item, lineItem, p.price, bucketedValues, nil, decimal.Zero)
		s.stampPointCosts(p.ctx, p.priceService, p.item, p.price)
	}

	p.item.Points = s.mergeBucketPointsByWindow(p.item.Points, p.aggType, p.data.Params.WindowSize, p.data.Params.BillingAnchor)

	return cost
}

// processSingleBucket handles the case where there are no time-series points.
// needsWindowedFill gates the window-fill path used for windowed true-up when
// there's no usage at all.
func (s *meterUsageService) processSingleBucket(
	p *meterUsageBucketedCostParams,
	lineItem *subscription.SubscriptionLineItem,
	hasCommitment, isWindowed, needsWindowedFill bool,
) decimal.Decimal {
	// p.item.TotalUsage is the primary aggregation value (see
	// buildMeterUsageAggregationColumns) — bucketed-max meters also write it from
	// bucketedResult.Value at construction, so reading it works uniformly.
	totalUsage := p.item.TotalUsage

	if totalUsage.IsPositive() {
		bucketedValues := []decimal.Decimal{totalUsage}
		baseCost := p.priceService.CalculateBucketedCost(p.ctx, p.price, bucketedValues)
		if hasCommitment {
			// Single-bucket path: the value is a period-wide aggregate with no per-window
			// timestamps. We cannot determine which UTC hours the events occurred in, so
			// any proxy timestamp would be arbitrary. Pass nil to skip TimeBucket filtering
			// — commitment applies normally (24/7) in this degenerate case.
			return s.applyLineItemCommitment(p.ctx, p.priceService, p.item, lineItem, p.price, bucketedValues, nil, baseCost)
		}
		return baseCost
	}

	if !hasCommitment {
		return decimal.Zero
	}

	if needsWindowedFill && p.bucketSize != "" {
		cost := s.fillWindowsAndApplyCommitment(p, lineItem)
		p.item.Points = s.mergeBucketPointsByWindow(p.item.Points, p.aggType, p.data.Params.WindowSize, p.data.Params.BillingAnchor)
		return cost
	}

	return s.applyLineItemCommitment(p.ctx, p.priceService, p.item, lineItem, p.price, nil, nil, decimal.Zero)
}

// extractBucketValues extracts usage values from points based on aggregation type.
func (s *meterUsageService) extractBucketValues(points []events.UsageAnalyticPoint, aggType types.AggregationType) []decimal.Decimal {
	values := make([]decimal.Decimal, len(points))
	for i, pt := range points {
		values[i] = pt.Usage
	}
	return values
}

// stampPointCosts sets each point's cost at the plain price (no commitment math).
func (s *meterUsageService) stampPointCosts(ctx context.Context, priceService PriceService, item *events.DetailedUsageAnalytic, p *price.Price) {
	for i := range item.Points {
		item.Points[i].Cost = priceService.CalculateCost(ctx, p, item.Points[i].Usage)
	}
}

// applyWindowCommitmentToPoints applies windowed commitment over the given
// points in ONE pass: each point is stamped with its window's charge and
// commitment breakdown, item.CommitmentInfo is set, and the total charge is
// returned. Bucket prices are fetched once per bucket for the whole pass. On
// calculation failure it logs, stamps plain costs and returns the uncommitted
// bucketed cost.
func (s *meterUsageService) applyWindowCommitmentToPoints(
	p *meterUsageBucketedCostParams,
	lineItem *subscription.SubscriptionLineItem,
	points []events.UsageAnalyticPoint,
) decimal.Decimal {
	values := make([]decimal.Decimal, len(points))
	starts := make([]time.Time, len(points))
	for i := range points {
		values[i] = points[i].Usage
		starts[i] = points[i].Timestamp
	}

	calc := newCommitmentCalculator(s.logger, p.priceService)
	total, perWindow, info, err := calc.applyWindowCommitmentPerBucket(p.ctx, lineItem, values, starts, p.price)
	if err != nil {
		s.logger.Info(p.ctx, "failed to apply window commitment", "error", err, "line_item_id", lineItem.ID)
		p.item.Points = points
		s.stampPointCosts(p.ctx, p.priceService, p.item, p.price)
		return p.priceService.CalculateBucketedCost(p.ctx, p.price, values)
	}

	for i := range points {
		points[i].Cost = perWindow[i].charge
		points[i].ComputedCommitmentUtilizedAmount = perWindow[i].utilized
		points[i].ComputedOverageAmount = perWindow[i].overage
		points[i].ComputedTrueUpAmount = perWindow[i].trueUp
	}
	p.item.Points = points
	p.item.CommitmentInfo = info
	return total
}

// fillWindowsAndApplyCommitment builds the expected window grid for the line
// item period — real points plus zero-usage fills for windows where commitment
// can charge for emptiness (line-item commitment, or inside a commitment time
// bucket) — and applies windowed commitment over it in one pass.
func (s *meterUsageService) fillWindowsAndApplyCommitment(
	p *meterUsageBucketedCostParams,
	lineItem *subscription.SubscriptionLineItem,
) decimal.Decimal {
	billingAnchor := s.getBillingAnchorFromData(p.data, lineItem.SubscriptionID)
	periodStart := lineItem.GetPeriodStart(p.data.Params.StartTime)
	periodEnd := lineItem.GetPeriodEnd(p.data.Params.EndTime)
	expectedStarts := generateBucketStarts(periodStart, periodEnd, p.bucketSize, billingAnchor)

	existing := make(map[time.Time]events.UsageAnalyticPoint, len(p.item.Points))
	for _, pt := range p.item.Points {
		existing[pt.Timestamp] = pt
	}

	points := make([]events.UsageAnalyticPoint, 0, len(expectedStarts))
	for _, t := range expectedStarts {
		if pt, ok := existing[t]; ok {
			// Real usage is always billed, in- or out-of-bucket.
			points = append(points, pt)
			continue
		}
		if !shouldFillWindow(lineItem, t) {
			continue
		}
		points = append(points, events.UsageAnalyticPoint{
			Timestamp:   t,
			WindowStart: truncateToBucketStart(t, p.data.Params.WindowSize, billingAnchor),
			Usage:       decimal.Zero,
			MaxUsage:    decimal.Zero,
		})
	}

	return s.applyWindowCommitmentToPoints(p, lineItem, points)
}

// getBillingAnchorFromData retrieves the billing anchor for a subscription from AnalyticsData.
func (s *meterUsageService) getBillingAnchorFromData(data *AnalyticsData, subscriptionID string) *time.Time {
	if sub := data.SubscriptionsMap[subscriptionID]; sub != nil {
		return &sub.BillingAnchor
	}
	return nil
}

// calculateRegularCost calculates cost for regular (non-bucketed) meters.
// skipCommitment forces the commitment branch to be bypassed; used for analytics
// queries with property/source filters where applying commitment over a filtered
// subset of events would surface misleading commitment/overage/true-up amounts.
func (s *meterUsageService) calculateRegularCost(ctx context.Context, priceService PriceService, item *events.DetailedUsageAnalytic, m *meter.Meter, p *price.Price, data *AnalyticsData, skipCommitment bool) {
	cost := priceService.CalculateCost(ctx, p, item.TotalUsage)

	if !skipCommitment && item.SubLineItemID != "" {
		lineItem := data.SubscriptionLineItems[item.SubLineItemID]

		// Windowed commitment (and time buckets, which require it) can only
		// exist on bucketed meters: meter validation allows bucket_size only
		// with MAX/SUM aggregation, and windowed commitment requires a meter
		// with bucket_size — those meters route to calculateBucketedCost. So a
		// regular meter can carry only a non-windowed aggregate commitment; a
		// windowed flag here is invalid data and is ignored (billed plain).
		if lineItem != nil && !lineItem.CommitmentWindowed && lineItem.HasCommitment() {
			cost = s.applyLineItemCommitment(ctx, priceService, item, lineItem, p, nil, nil, cost)
		}
	}

	item.TotalCost = cost
	item.Currency = p.Currency

	// Points here are a display series (request window_size); the commitment
	// applies to the period aggregate, so per-point costs are plain price.
	s.stampPointCosts(ctx, priceService, item, p)
}

// applyLineItemCommitment applies the line item's commitment — windowed (per
// window, with per-bucket pricing) or aggregate — to the calculated cost and
// records the commitment info on the analytic item. windowStarts (optional) is
// paired 1:1 with windowValues and enables per-bucket pricing on
// lineItem.CommitmentTimeBuckets in the windowed path. On calculation failure it
// logs and falls back to the uncommitted cost.
func (s *meterUsageService) applyLineItemCommitment(
	ctx context.Context,
	priceService PriceService,
	item *events.DetailedUsageAnalytic,
	lineItem *subscription.SubscriptionLineItem,
	p *price.Price,
	windowValues []decimal.Decimal,
	windowStarts []time.Time,
	defaultCost decimal.Decimal,
) decimal.Decimal {
	// Uncommitted fallback: the caller-provided cost, or the bucketed cost of
	// the window values when no cost was provided.
	rawCost := defaultCost
	if rawCost.IsZero() && len(windowValues) > 0 {
		rawCost = priceService.CalculateBucketedCost(ctx, p, windowValues)
	}

	calc := newCommitmentCalculator(s.logger, priceService)
	var cost decimal.Decimal
	var info *types.CommitmentInfo
	var err error
	if lineItem.CommitmentWindowed {
		cost, info, err = calc.applyWindowCommitmentToLineItem(ctx, lineItem, windowValues, windowStarts, p)
	} else {
		cost, info, err = calc.applyCommitmentToLineItem(ctx, lineItem, rawCost, p)
	}
	if err != nil {
		s.logger.Info(ctx, "failed to apply commitment", "error", err, "line_item_id", lineItem.ID, "windowed", lineItem.CommitmentWindowed)
		return rawCost
	}
	item.CommitmentInfo = info
	return cost
}

// mergeBucketPointsByWindow merges bucket-level points into request-window-level points.
// Each input point's WindowStart is the bucket start (at the meter's bucket_size). To emit
// points at the requested window_size, WindowStart is truncated to that window before grouping
// — so when request window > bucket size (e.g. DAY request, MINUTE bucket), many bucket points
// collapse into one response point per request window. When request window <= bucket size, the
// truncation is a no-op and points stay at bucket grain (we cannot subdivide a bucket).
// requestWindowSize == "" disables roll-up entirely.
func (s *meterUsageService) mergeBucketPointsByWindow(points []events.UsageAnalyticPoint, aggregationType types.AggregationType, requestWindowSize types.WindowSize, billingAnchor *time.Time) []events.UsageAnalyticPoint {
	if len(points) == 0 {
		return points
	}

	if points[0].WindowStart.IsZero() {
		return points
	}

	windowGroups := make(map[time.Time][]events.UsageAnalyticPoint)
	for _, point := range points {
		key := point.WindowStart
		if requestWindowSize != "" {
			key = truncateToBucketStart(point.WindowStart, requestWindowSize, billingAnchor)
		}
		windowGroups[key] = append(windowGroups[key], point)
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

// ensure meterUsageService implements MeterUsageService
var _ MeterUsageService = (*meterUsageService)(nil)
