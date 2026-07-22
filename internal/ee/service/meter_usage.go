package service

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	ca "github.com/flexprice/flexprice/internal/domain/coupon_association"
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

	// GetDetailedUsageAnalytics adapts GetDetailedAnalytics to the dto-based
	// request shape used by callers that need to be signal-agnostic (e.g. the
	// usage-analytics CSV exporter). It resolves tenant/environment from ctx.
	GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error)

	// GetSubscriptionMeterUsage is the centralized meter-usage query function.
	// Both analytics and billing paths call this per-subscription to get line-item-bounded usage.
	GetSubscriptionMeterUsage(ctx context.Context, req *GetSubscriptionMeterUsageRequest) (*SubscriptionMeterUsage, error)

	// ConvertToBillingCharges maps SubscriptionMeterUsage to billing charges.
	ConvertToBillingCharges(ctx context.Context, usage *SubscriptionMeterUsage) ([]*dto.SubscriptionUsageByMetersResponse, decimal.Decimal, error)

	// DebugEvent powers GET /events/:id — reports processing status and
	// per-lookup diagnostics for a single event under the meter-usage pipeline.
	DebugEvent(ctx context.Context, eventID string) (*dto.GetEventByIDResponse, error)

	// GetHuggingFaceBillingData resolves per-event cost (nano-USD) for the
	// requested event IDs. Powers POST /events/huggingface-billing.
	GetHuggingFaceBillingData(ctx context.Context, req *dto.GetHuggingFaceBillingDataRequest) (*dto.GetHuggingFaceBillingDataResponse, error)
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
	// CollectSources when true fetches distinct source values for bucketed meters
	// via a secondary query (used when expand:"source" is requested by analytics callers).
	CollectSources bool

	// IncludeChildren, when true on a Parent subscription, extends the query
	// scope to every inherited child customer's external_id. False (default)
	// restricts the query to the subscription owner's external_id only.
	IncludeChildren bool

	// ForceApplyCommitment, when true, keeps commitment / true-up cost active
	// even on fanned-out analytics AND routes commitment line items through
	// the source-fanning query path so the CSV export can produce per-source
	// rows for them. Internal-only — the export pipeline flips this on and
	// accepts that per-source rows will each fire the line item's commitment
	// (multi-counts the true-up amount across rows). Default (false) is what
	// every user-facing caller uses.
	ForceApplyCommitment bool
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

// GetDetailedUsageAnalytics adapts GetDetailedAnalytics to the dto-based
// request shape used by callers that need to be signal-agnostic (e.g. the
// usage-analytics CSV exporter).
func (s *meterUsageService) GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
	return s.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:             types.GetTenantID(ctx),
		EnvironmentID:        types.GetEnvironmentID(ctx),
		ExternalCustomerID:   req.ExternalCustomerID,
		ExternalCustomerIDs:  req.ExternalCustomerIDs,
		FeatureIDs:           req.FeatureIDs,
		StartTime:            req.StartTime,
		EndTime:              req.EndTime,
		GroupBy:              req.GroupBy,
		PropertyFilters:      req.PropertyFilters,
		Sources:              req.Sources,
		WindowSize:           req.WindowSize,
		Expand:               req.Expand,
		IncludeChildren:      req.IncludeChildren,
		ForceApplyCommitment: req.ForceApplyCommitment,
	})
}

// AnalyticsData holds all data required for analytics processing.
type AnalyticsData struct {
	Customer              *customer.Customer
	Subscriptions         []*subscription.Subscription
	SubscriptionLineItems map[string]*subscription.SubscriptionLineItem
	SubscriptionsMap      map[string]*subscription.Subscription
	Analytics             []*events.DetailedUsageAnalytic
	Features              map[string]*feature.Feature
	Meters                map[string]*meter.Meter
	Prices                map[string]*price.Price
	PriceResponses        map[string]*dto.PriceResponse
	Plans                 map[string]*plan.Plan
	Addons                map[string]*addon.Addon
	Groups                map[string]*group.Group
	Currency              string
	Params                *events.UsageAnalyticsParams
}

// ---------------------------------------------------------------------------
// Simple passthrough methods
// ---------------------------------------------------------------------------

// activeSubscriptionMeterIDs returns the set of meter_ids that belong to any
// currently-active (active or trialing) subscription for the given customer.
//
// The meter ingestion pipeline no longer validates that a meter belongs to an
// active subscription before writing, so meter_usage now contains rows for
// "stale" meters — meters that match the customer's event_name but aren't on
// any of their current subscription line items. Reads must filter those out.
//
// Returns an empty set (not an error) if the customer doesn't exist; callers
// should treat that as "no usage to return".
func (s *meterUsageService) activeSubscriptionMeterIDs(ctx context.Context, externalCustomerID string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	if externalCustomerID == "" {
		return out, nil
	}
	cust, err := s.CustomerRepo.GetByLookupKey(ctx, externalCustomerID)
	if err != nil {
		if ierr.IsNotFound(err) {
			return out, nil
		}
		return nil, err
	}
	subs, err := s.SubRepo.ListByCustomerID(ctx, cust.ID)
	if err != nil {
		return nil, err
	}
	for _, sub := range subs {
		for _, li := range sub.LineItems {
			if li.PriceType == types.PRICE_TYPE_USAGE && li.MeterID != "" {
				out[li.MeterID] = struct{}{}
			}
		}
	}
	return out, nil
}

func (s *meterUsageService) GetUsage(ctx context.Context, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	activeMeters, err := s.activeSubscriptionMeterIDs(ctx, params.ExternalCustomerID)
	if err != nil {
		return nil, err
	}
	if _, ok := activeMeters[params.MeterID]; !ok {
		// meter_id is not on any active subscription line item for this
		// customer — return zeroed result rather than leaking stale rows.
		return &events.MeterUsageAggregationResult{
			MeterID:         params.MeterID,
			AggregationType: params.AggregationType,
		}, nil
	}

	return s.repo.GetUsage(ctx, params)
}

func (s *meterUsageService) GetUsageMultiMeter(ctx context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error) {
	if params == nil || len(params.MeterIDs) == 0 {
		return nil, ierr.NewError("params with meter_ids are required").Mark(ierr.ErrValidation)
	}

	activeMeters, err := s.activeSubscriptionMeterIDs(ctx, params.ExternalCustomerID)
	if err != nil {
		return nil, err
	}
	filtered := params.MeterIDs[:0:0]
	for _, id := range params.MeterIDs {
		if _, ok := activeMeters[id]; ok {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		// None of the requested meters are on the customer's active
		// subscriptions — nothing to fetch.
		return nil, nil
	}

	// Don't mutate caller's params slice — make a shallow copy.
	q := *params
	q.MeterIDs = filtered
	return s.repo.GetUsageMultiMeter(ctx, &q)
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

	// 2. Resolve external customer IDs for meter_usage queries.
	// Parent subscriptions fan out to inherited children only when the caller
	// asks for it via req.IncludeChildren (billing path passes true; analytics
	// passes params.IncludeChildren).
	externalCustomerIDs, err := s.resolveExternalCustomerIDs(ctx, sub, req.IncludeChildren)
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
				if g != "" && g != "meter_id" && g != "feature_id" {
					hasExtraGroupBy = true
					break
				}
			}

			var commitmentLIs, nonCommitmentLIs []*lineItemWithMeter
			if hasExtraGroupBy {
				for _, liw := range lineItemsInGroup {
					// ForceApplyCommitment (export path) folds commitment LIs
					// into the fan-out path so the CSV gets per-source rows for
					// them too. Trade-off: commitment fires per fanned row and
					// multi-counts the true-up across sources — accepted at the
					// flag's call site.
					if !req.ForceApplyCommitment && liw.Item != nil && liw.Item.HasAnyCommitment() {
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

	useDetailed := hasUserBucketedGroupBy(req.GroupBy)

	for meterID := range bucketedMeterIDs {
		m := result.MeterMap[meterID]
		if m == nil {
			continue
		}
		items := meterToLineItems[meterID]
		for _, item := range items {
			itemStart := item.GetPeriodStart(usageStartTime)
			itemEnd := item.GetPeriodEnd(usageEndTime)

			if useDetailed {
				// Analytics fan-out: one LineItemMeterUsage per (source, properties)
				// combo. Repo does the per-combo aggregation in SQL; nothing to
				// re-group in Go.
				detailedResults, err := s.queryBucketedMeterAnalyticsDetailed(
					ctx, m, externalCustomerIDs,
					itemStart, itemEnd, req.BillingAnchor, req.UseFinal,
					req.PropertyFilters, req.Sources, req.GroupBy,
				)
				if err != nil {
					return nil, fmt.Errorf("failed to query bucketed meter analytics for meter %s: %w", meterID, err)
				}
				if skipBucketedZeros && len(detailedResults) == 0 {
					continue
				}
				for _, dr := range detailedResults {
					if dr == nil {
						continue
					}
					result.LineItemUsages = append(result.LineItemUsages, &LineItemMeterUsage{
						LineItem:        item,
						MeterID:         meterID,
						Meter:           m,
						Price:           result.PriceMap[item.PriceID],
						PeriodStart:     itemStart,
						PeriodEnd:       itemEnd,
						Usage:           dr.TotalUsage,
						EventCount:      dr.EventCount,
						Points:          dr.Points,
						AnalyticsResult: dr,
						// BucketedResult intentionally nil: this LIMU is one
						// (source, properties) combo, not the line-item total.
						// Per-combo commitment math would over-charge (commitment
						// fires once per combo instead of once per line item) —
						// calculateCosts gates this via skipCommitment when
						// hasUserBucketedGroupBy(data.Params.GroupBy) is true.
					})
				}
				continue
			}

			bucketedResult, err := s.queryBucketedMeterUsage(
				ctx, m, externalCustomerIDs,
				itemStart, itemEnd, req.BillingAnchor, req.UseFinal,
				req.PropertyFilters, req.Sources, req.CollectSources,
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

			// Set scalar usage and event count from bucketed result
			if bucketedResult != nil {
				usage.Usage = bucketedResult.Value
				usage.EventCount = bucketedResult.EventCount
			}

			// Always populate points from bucketed results — cost calculation needs
			// per-bucket values for commitment windowing, true-up, etc.
			// Roll-up to request window happens downstream in mergeBucketPointsByWindow.
			if bucketedResult != nil && len(bucketedResult.Results) > 0 {
				points := make([]events.MeterUsageDetailedPoint, 0, len(bucketedResult.Results))
				for _, r := range bucketedResult.Results {
					p := events.MeterUsageDetailedPoint{
						WindowStart: r.WindowSize,
						EventCount:  r.EventCount,
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
			if g != "" && g != "meter_id" && g != "feature_id" {
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
// For analytics requests with user GroupBy on source/properties, use
// queryBucketedMeterAnalyticsDetailed instead — this method returns a single
// line-item total, not per-combo rows.
func (s *meterUsageService) queryBucketedMeterUsage(
	ctx context.Context,
	m *meter.Meter,
	externalCustomerIDs []string,
	periodStart, periodEnd time.Time,
	billingAnchor *time.Time,
	useFinal bool,
	propertyFilters map[string][]string,
	sources []string,
	collectSources bool,
) (*events.AggregationResult, error) {
	aggType := m.Aggregation.Type
	meterGroupBy := m.Aggregation.GroupBy
	if m.IsBucketedSumMeter() {
		aggType = types.AggregationSum
		meterGroupBy = ""
	}
	// Translate meter-level Aggregation.GroupBy (a single property name) to the
	// unified GroupBy []string convention used by BuildBucketedQuery.
	var paramsGroupBy []string
	if meterGroupBy != "" {
		paramsGroupBy = []string{"properties." + meterGroupBy}
	}

	queryParams := &events.MeterUsageQueryParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: externalCustomerIDs,
		MeterID:             m.ID,
		StartTime:           periodStart,
		EndTime:             periodEnd,
		AggregationType:     aggType,
		WindowSize:          m.Aggregation.BucketSize,
		BillingAnchor:       billingAnchor,
		GroupBy:             paramsGroupBy,
		UseFinal:            useFinal,
		PropertyFilters:     propertyFilters,
		Sources:             sources,
	}

	result, err := s.repo.GetUsageForBucketedMeters(ctx, queryParams)
	if err != nil {
		return nil, err
	}

	if collectSources {
		sourcesResult, sourcesErr := s.repo.GetSourcesForBucketedMeter(ctx, queryParams)
		if sourcesErr != nil {
			s.logger.Error(ctx, "failed to collect sources for bucketed meter, sources omitted from response", "error", sourcesErr, "meter_id", m.ID)
		} else {
			result.Sources = sourcesResult
		}
	}

	return result, nil
}

// hasUserBucketedGroupBy reports whether the request asks for fan-out on a
// bucketed-meter analytics result. Mirrors the dim filter in
// clickhouse.bucketedGroupByDims so the dispatch decision lives next to the
// call site, and so calculateCosts can use the same predicate to gate
// commitment math (commitment applied per combo would over-charge).
func hasUserBucketedGroupBy(groupBy []string) bool {
	for _, g := range groupBy {
		if g == "source" || strings.HasPrefix(g, "properties.") {
			return true
		}
	}
	return false
}

// analyticsGroupByPropertyName matches safe property names for "properties.X"
// dims. Same shape as clickhouse.validMeterUsageGroupByPattern so the service
// layer's validation matches what the SQL builder will actually accept.
var analyticsGroupByPropertyName = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)

// validateAnalyticsGroupBy rejects entries the SQL builder would silently drop.
// Accepted forms:
//   - "meter_id"          (no-op at SQL level; implicit at the meter)
//   - "source"
//   - "properties.<name>" where name matches a safe identifier pattern
//
// Anything else (empty string, "properties." with no name, unknown tokens) is
// a user error and surfaces as a 400 with a clear hint, rather than being
// dropped on the floor inside clickhouse.bucketedGroupByDims where it would
// show up as a successful response with surprising shape.
func validateAnalyticsGroupBy(groupBy []string) error {
	for _, g := range groupBy {
		switch {
		case g == "meter_id" || g == "source":
			// ok
		case strings.HasPrefix(g, "properties."):
			name := strings.TrimPrefix(g, "properties.")
			if name == "" || !analyticsGroupByPropertyName.MatchString(name) {
				return ierr.NewError("invalid group_by entry").
					WithHintf("group_by entry %q has an invalid property name (allowed: alphanumerics, '_', '.')", g).
					WithReportableDetails(map[string]interface{}{
						"group_by_entry": g,
					}).
					Mark(ierr.ErrValidation)
			}
		default:
			return ierr.NewError("invalid group_by entry").
				WithHintf("group_by entry %q is not recognized (allowed: 'meter_id', 'source', 'properties.<name>')", g).
				WithReportableDetails(map[string]interface{}{
					"group_by_entry": g,
				}).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// queryBucketedMeterAnalyticsDetailed is the analytics-side bucketed query: one
// MeterUsageDetailedResult per (source, properties) combo, with per-combo
// Points pre-fetched by the repo. Bypassed entirely for billing — billing
// callers use queryBucketedMeterUsage which returns a single line-item total.
func (s *meterUsageService) queryBucketedMeterAnalyticsDetailed(
	ctx context.Context,
	m *meter.Meter,
	externalCustomerIDs []string,
	periodStart, periodEnd time.Time,
	billingAnchor *time.Time,
	useFinal bool,
	propertyFilters map[string][]string,
	sources []string,
	groupBy []string,
) ([]*events.MeterUsageDetailedResult, error) {
	aggType := m.Aggregation.Type
	if m.IsBucketedSumMeter() {
		aggType = types.AggregationSum
	}
	return s.repo.GetUsageForBucketedMetersDetailed(ctx, &events.MeterUsageQueryParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: externalCustomerIDs,
		MeterID:             m.ID,
		StartTime:           periodStart,
		EndTime:             periodEnd,
		AggregationType:     aggType,
		WindowSize:          m.Aggregation.BucketSize,
		BillingAnchor:       billingAnchor,
		// Intentionally do NOT forward m.Aggregation.GroupBy — the analytics
		// fan-out shape is owned by the request's GroupBy. The meter-level
		// GroupBy is a billing concept handled by GetUsageForBucketedMeters.
		GroupBy:         groupBy,
		UseFinal:        useFinal,
		PropertyFilters: propertyFilters,
		Sources:         sources,
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

	// Bucket the time-series in the customer's local timezone so analytics align
	// with their billing periods. Auto-derived from the primary customer record;
	// never supplied by the request. A lookup miss or empty timezone leaves it
	// empty, which the query builder treats as UTC (today's behaviour) — a tz
	// lookup must never fail the analytics query.
	if params.ExternalCustomerID != "" {
		if cust, err := s.CustomerRepo.GetByLookupKey(ctx, params.ExternalCustomerID); err != nil {
			s.logger.Debug(ctx, "could not resolve customer timezone for detailed analytics; defaulting to UTC",
				"external_customer_id", params.ExternalCustomerID, "error", err)
		} else if cust != nil {
			params.Timezone = cust.Timezone
		}
	}

	// feature_id ≡ meter_id (1:1). Rewrite at entry so the SQL builder accepts
	// it; the converter restores FeatureID via the meter→feature lookup.
	if len(params.GroupBy) > 0 {
		rewritten := make([]string, 0, len(params.GroupBy))
		for _, g := range params.GroupBy {
			if g == "feature_id" {
				g = "meter_id"
			}
			rewritten = append(rewritten, g)
		}
		params.GroupBy = lo.Uniq(rewritten)
		// Surface invalid group_by as 400 — the SQL builder would otherwise drop it silently.
		if err := validateAnalyticsGroupBy(params.GroupBy); err != nil {
			return nil, err
		}
	}

	// Resolve FeatureIDs → MeterIDs. Fail closed so an unresolvable feature
	// list doesn't silently broaden to "all meters".
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

	cust, subscriptions, err := s.resolveCustomerAndSubscriptions(ctx, params.ExternalCustomerID)
	if err != nil || len(subscriptions) == 0 {
		// No customer context — admin-style query across raw meter_usage.
		return s.getDetailedAnalyticsWithoutSubscriptionContext(ctx, params)
	}

	// Filter to meters on a subscription line item overlapping the query window.
	// Ingestion doesn't validate "meter on active sub", so meter_usage can carry
	// stale rows from shared event_name fan-out. Uses the same CancelledAt and
	// GetPeriodStart/End clamps as the GSMU loop below so the bounds agree.
	activeMeters := make(map[string]struct{})
	for _, sub := range subscriptions {
		subEnd := params.EndTime
		if sub.SubscriptionStatus == types.SubscriptionStatusCancelled && sub.CancelledAt != nil {
			if sub.CancelledAt.Before(subEnd) {
				subEnd = *sub.CancelledAt
			}
		}
		if !subEnd.After(params.StartTime) {
			continue // sub's effective window ended before query window starts
		}
		for _, li := range sub.LineItems {
			if li.PriceType != types.PRICE_TYPE_USAGE || li.MeterID == "" {
				continue
			}
			liStart := li.GetPeriodStart(params.StartTime)
			liEnd := li.GetPeriodEnd(subEnd)
			if liStart.Before(liEnd) {
				activeMeters[li.MeterID] = struct{}{}
			}
		}
	}
	if len(params.MeterIDs) > 0 {
		filtered := make([]string, 0, len(params.MeterIDs))
		for _, id := range params.MeterIDs {
			if _, ok := activeMeters[id]; ok {
				filtered = append(filtered, id)
			}
		}
		params.MeterIDs = filtered
	} else {
		// No caller filter → restrict to the active set.
		params.MeterIDs = make([]string, 0, len(activeMeters))
		for id := range activeMeters {
			params.MeterIDs = append(params.MeterIDs, id)
		}
	}
	if len(params.MeterIDs) == 0 {
		return &dto.GetUsageAnalyticsResponse{
			TotalCost: decimal.Zero,
			Items:     []dto.UsageAnalyticItem{},
		}, nil
	}

	// Call GetSubscriptionMeterUsage per subscription
	var allUsages []*SubscriptionMeterUsage
	for _, sub := range subscriptions {
		// Skip parent subs that resolveCustomerAndSubscriptions appended for a
		// child caller. Only those subs have sub.CustomerID != cust.ID —
		// every caller-owned sub (including the caller's inherited child sub)
		// was fetched via filter.CustomerID = cust.ID and therefore matches.
		// The appended parent sub is present only so enrichment can see its
		// line items; the caller's inherited sub has already queried those
		// same line items scoped to the caller's external_id, so running the
		// parent sub through GetSubscriptionMeterUsage here would leak the
		// parent customer's raw usage into the child's response.
		if sub.CustomerID != cust.ID {
			continue
		}

		billingAnchor := params.BillingAnchor
		if billingAnchor == nil {
			billingAnchor = &sub.BillingAnchor
		}

		// Clamp by CancelledAt: meter_usage has no per-event sub linkage, so
		// without this a cancelled sub's line items would steal post-cancel
		// events from whichever sub is now active for the same meter.
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
			SubscriptionID:       sub.ID,
			StartTime:            params.StartTime,
			EndTime:              subEndTime,
			WindowSize:           params.WindowSize,
			BillingAnchor:        billingAnchor,
			UseFinal:             params.UseFinal,
			IncludeFeatures:      true,
			MeterIDs:             params.MeterIDs,
			GroupBy:              params.GroupBy,
			PropertyFilters:      params.PropertyFilters,
			Sources:              params.Sources,
			CollectSources:       lo.Contains(params.Expand, "source"),
			IncludeChildren:      params.IncludeChildren,
			ForceApplyCommitment: params.ForceApplyCommitment,
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

	// Calculate costs inline
	err = s.calculateCosts(ctx, data)
	if err != nil {
		s.logger.Error(ctx, "failed to calculate costs for meter usage analytics, costs will be zero", "error", err)
	}

	// Set currency on all analytics items
	if data.Currency != "" {
		for _, item := range data.Analytics {
			item.Currency = data.Currency
		}
	}

	// Load percentage-coupon associations (applied inline while building the response DTO).
	lineItemCoupons, subscriptionCoupons := loadAnalyticsCoupons(ctx, s.ServiceParams, data, params.StartTime, params.EndTime)

	// Enrich with Groups + parent prices (always-on) and expand-gated Plans/Addons
	s.enrichAnalyticsDataForResponse(ctx, data, params)

	// Convert to response DTO
	return s.toUsageAnalyticsResponseDTO(ctx, data, data.Meters, params, lineItemCoupons, subscriptionCoupons), nil
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
			TenantID:             params.TenantID,
			EnvironmentID:        params.EnvironmentID,
			ExternalCustomerID:   params.ExternalCustomerID,
			StartTime:            params.StartTime,
			EndTime:              params.EndTime,
			GroupBy:              params.GroupBy,
			WindowSize:           params.WindowSize,
			PropertyFilters:      params.PropertyFilters,
			Sources:              params.Sources,
			AggregationTypes:     params.AggregationTypes,
			BillingAnchor:        params.BillingAnchor,
			ForceApplyCommitment: params.ForceApplyCommitment,
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

		// Convert each LineItemMeterUsage → DetailedUsageAnalytic(s).
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
			} else if lu.BucketedResult != nil && len(lu.BucketedResult.Sources) > 0 {
				analytic.Sources = lu.BucketedResult.Sources
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
			TenantID:             params.TenantID,
			EnvironmentID:        params.EnvironmentID,
			ExternalCustomerID:   params.ExternalCustomerID,
			StartTime:            params.StartTime,
			EndTime:              params.EndTime,
			GroupBy:              params.GroupBy,
			WindowSize:           params.WindowSize,
			PropertyFilters:      params.PropertyFilters,
			Sources:              params.Sources,
			AggregationTypes:     params.AggregationTypes,
			BillingAnchor:        params.BillingAnchor,
			ForceApplyCommitment: params.ForceApplyCommitment,
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
	return s.toUsageAnalyticsResponseDTO(ctx, data, meterMap, params, nil, nil), nil
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
	lineItemCoupons map[string][]*analyticsCoupon,
	subscriptionCoupons map[string][]*analyticsCoupon,
) *dto.GetUsageAnalyticsResponse {
	response := &dto.GetUsageAnalyticsResponse{
		Subtotal:      decimal.Zero,
		TotalDiscount: decimal.Zero,
		TotalCost:     decimal.Zero,
		Currency:      data.Currency,
		Items:         make([]dto.UsageAnalyticItem, 0, len(data.Analytics)),
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
			Subtotal:        analytic.TotalCost,
			TotalCost:       analytic.TotalCost,
			TotalDiscount:   decimal.Zero,
			Currency:        analytic.Currency,
			EventCount:      analytic.EventCount,
			Properties:      analytic.Properties,
			CommitmentInfo:  analytic.CommitmentInfo,
			Points:          make([]dto.UsageAnalyticPoint, 0, len(analytic.Points)),
		}

		// Apply percentage-coupon discounts inline (folded from the former separate
		// discount pass). Default is no discount (Subtotal == TotalCost); when coupons
		// apply to this item, ApplyAnalyticsDiscounts recomputes TotalDiscount/TotalCost
		// (and per-point Discount/Cost below).
		lineCoupons := lineItemCoupons[analytic.SubLineItemID]
		subCoupons := subscriptionCoupons[analytic.SubscriptionID]
		var discountOut *discountOutput
		if len(lineCoupons) > 0 || len(subCoupons) > 0 {
			discountOut = ApplyAnalyticsDiscounts(&discountInput{
				Currency:    analytic.Currency,
				SubTotal:    analytic.TotalCost,
				LineCoupons: lineCoupons,
				SubCoupons:  subCoupons,
				RangeStart:  params.StartTime,
				RangeEnd:    params.EndTime,
				Points:      analytic.Points,
			})

			if discountOut != nil {
				item.TotalDiscount = discountOut.TotalDiscount
				item.TotalCost = discountOut.SubTotal
				analytic.Points = discountOut.PointDiscounts
			}

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

		// Window size reflects the granularity Points were computed at. Bucketed
		// meters cannot be subdivided below their bucket size, so points are at
		// max(request window, bucket size); non-bucketed meters use the request.
		if m, ok := meterMap[analytic.MeterID]; ok {
			if m.HasBucketSize() {
				item.WindowSize = params.WindowSize.Max(m.Aggregation.BucketSize)
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
					Subtotal:                         point.Cost,
					Discount:                         point.Discount,
					Cost:                             point.Cost.Sub(point.Discount),
					EventCount:                       point.EventCount,
					ComputedCommitmentUtilizedAmount: point.ComputedCommitmentUtilizedAmount,
					ComputedOverageAmount:            point.ComputedOverageAmount,
					ComputedTrueUpAmount:             point.ComputedTrueUpAmount,
				}
				// Per-point bucket identity: every bucket the rolled-up window
				// overlaps (informational hint only — see dto.UsageAnalyticPoint).
				if lineItemForBucket != nil {
					windowMin := effectivePointWindowMinutes(params.WindowSize, meterMap[analytic.MeterID])
					ids, priceIDs := bucketIDsForPointWindow(
						lineItemForBucket.CommitmentTimeBuckets, point.Timestamp, windowMin)
					for i := range ids {
						dtoPoint.Buckets = append(dtoPoint.Buckets, dto.PointBucket{BucketID: ids[i], PriceID: priceIDs[i]})
					}
				}
				item.Points = append(item.Points, dtoPoint)
			}

			// Bucket-level summaries. Always built from the bucket-grain points
			// (analytic.BucketPoints) — one per meter window, each fully inside a
			// single bucket — so attribution is exact regardless of the request
			// window grain. Never from item.Points, whose rolled-up windows can
			// straddle bucket boundaries.
			if lineItemForBucket != nil {
				priceService := NewPriceService(s.ServiceParams)
				item.BucketSummaries = buildBucketSummaries(ctx, priceService, analytic.BucketPoints, lineItemForBucket, data)
			}
		}

		response.Items = append(response.Items, item)
		response.Subtotal = response.Subtotal.Add(analytic.TotalCost)
		response.TotalDiscount = response.TotalDiscount.Add(item.TotalDiscount)
	}

	// Derive TotalCost (final) from fully-summed Subtotal and TotalDiscount so the
	// result is consistent even when the discount pass short-circuits (zero discount).
	response.TotalCost = response.Subtotal.Sub(response.TotalDiscount)

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
	// Mirror the subscription path: when the request has source/properties.*
	// dims, fan out via the detailed query so each combo becomes its own
	// MeterUsageDetailedResult with Source/Properties populated and per-combo
	// Points. The basic GetUsageForBucketedMeters can't surface those — it
	// returns a single AggregationResult and the user's request GroupBy would
	// be silently dropped.
	if hasUserBucketedGroupBy(params.GroupBy) {
		bucketParams := &events.MeterUsageQueryParams{
			TenantID:           params.TenantID,
			EnvironmentID:      params.EnvironmentID,
			ExternalCustomerID: params.ExternalCustomerID,
			MeterID:            m.ID,
			StartTime:          params.StartTime,
			EndTime:            params.EndTime,
			AggregationType:    m.Aggregation.Type,
			WindowSize:         m.Aggregation.BucketSize,
			// Intentionally use only params.GroupBy here. Meter-level
			// Aggregation.GroupBy is a billing concept (per-KRN pricing); the
			// analytics fan-out shape is owned by the request. Matches the
			// subscription path's queryBucketedMeterAnalyticsDetailed contract.
			GroupBy:         params.GroupBy,
			UseFinal:        params.UseFinal,
			BillingAnchor:   params.BillingAnchor,
			PropertyFilters: params.PropertyFilters,
			Sources:         params.Sources,
		}
		if len(params.ExternalCustomerIDs) > 0 {
			bucketParams.ExternalCustomerIDs = params.ExternalCustomerIDs
		}
		return s.repo.GetUsageForBucketedMetersDetailed(ctx, bucketParams)
	}

	// No user fan-out: use the basic path with meter-level Aggregation.GroupBy
	// (the billing / KRN-style convention).
	var bucketParamsGroupBy []string
	if m.Aggregation.GroupBy != "" {
		bucketParamsGroupBy = []string{"properties." + m.Aggregation.GroupBy}
	}
	bucketParams := &events.MeterUsageQueryParams{
		TenantID:           params.TenantID,
		EnvironmentID:      params.EnvironmentID,
		ExternalCustomerID: params.ExternalCustomerID,
		MeterID:            m.ID,
		StartTime:          params.StartTime,
		EndTime:            params.EndTime,
		AggregationType:    m.Aggregation.Type,
		WindowSize:         m.Aggregation.BucketSize,
		GroupBy:            bucketParamsGroupBy,
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
	result.EventCount = aggResult.EventCount

	if params.WindowSize != "" && len(aggResult.Results) > 0 {
		points := make([]events.MeterUsageDetailedPoint, 0, len(aggResult.Results))
		for _, r := range aggResult.Results {
			p := events.MeterUsageDetailedPoint{WindowStart: r.WindowSize, EventCount: r.EventCount}
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

	return []*events.MeterUsageDetailedResult{result}, nil
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
			Price:                  lu.Price,
			BucketedUsageResult:    lu.BucketedResult,
		}

		if lu.Meter != nil {
			charge.MeterDisplayName = lu.Meter.Name
			for _, filter := range lu.Meter.Filters {
				charge.FilterValues[filter.Key] = filter.Values
			}
		}

		charges = append(charges, charge)
	}

	return charges, totalCost, nil
}

// resolveExternalCustomerIDs returns the external customer IDs whose meter_usage
// rows belong to a subscription. For Parent subscriptions the inherited-child
// customers are folded in only when includeChildren is true; otherwise the
// query stays scoped to the owning customer.
func (s *meterUsageService) resolveExternalCustomerIDs(ctx context.Context, sub *subscription.Subscription, includeChildren bool) ([]string, error) {
	internalIDs := []string{sub.CustomerID}
	if includeChildren && sub.SubscriptionType == types.SubscriptionTypeParent {
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

// loadAnalyticsCoupons loads the applicable percentage-coupon associations for ANY analytics
// endpoint and projects them into the applicator's line-item/subscription-keyed maps. Callers
// thread the returned maps into toUsageAnalyticsResponseDTO, which applies discounts inline
// while building the DTO. Short-circuits (returning nil, nil) when there are no costed
// analytics/subscriptions, no coupon repo, or no applicable coupons.
func loadAnalyticsCoupons(ctx context.Context, sp ServiceParams, data *AnalyticsData, start, end time.Time) (lineItemCoupons, subscriptionCoupons map[string][]*analyticsCoupon) {
	if len(data.Analytics) == 0 || len(data.Subscriptions) == 0 {
		return nil, nil
	}

	filter := func(c *coupon.Coupon, _ *ca.CouponAssociation) bool {
		return c.Type == types.CouponTypePercentage &&
			(c.Cadence == types.CouponCadenceForever || c.Cadence == types.CouponCadenceRepeated) &&
			c.IsValid()
	}
	sel, err := selectSubscriptionCoupons(ctx, sp, data.Subscriptions, start, end, filter)
	if err != nil {
		sp.Logger.Info(ctx, "failed to load coupons for analytics discounts, skipping", "error", err)
		return nil, nil
	}
	lineItemCoupons, subscriptionCoupons = projectAnalyticsCoupons(sel)
	if len(lineItemCoupons) == 0 && len(subscriptionCoupons) == 0 {
		return nil, nil
	}
	return lineItemCoupons, subscriptionCoupons
}

// calculateCosts calculates costs for all analytics items in the data.
func (s *meterUsageService) calculateCosts(ctx context.Context, data *AnalyticsData) error {
	if len(data.Analytics) == 0 {
		return nil
	}

	priceService := NewPriceService(s.ServiceParams)

	// Analytics filters (property_filters, sources) restrict the SQL result set
	// to a subset of the customer's actual usage. Commitment / overage / true-up
	// are billing concepts tied to the FULL usage in the period — applying them
	// to a filtered subset surfaces misleading values (e.g. a filter that yields
	// zero matching events would otherwise show the full commitment as unutilized
	// and report it as cost). When any analytics-only filter is active, skip
	// commitment application and report the raw filtered cost.
	//
	// User group_by also triggers skip: each fanned-out (source, properties)
	// combo analytic carries its own per-combo Usage and Points, and applying
	// commitment math per combo would over-charge — the line item's commitment
	// would fire once per combo instead of once across the whole line item.
	//
	// ForceApplyCommitment (internal-only, set by the CSV export path)
	// overrides the group_by skip so bucketed commitment line items keep their
	// true-up / overage cost even when the export requests group_by=source.
	// Filter-based skip is NOT overridden — a filter subset is genuinely
	// partial data and commitment math on it would still be misleading.
	skipCommitment := len(data.Params.PropertyFilters) > 0 ||
		len(data.Params.Sources) > 0 ||
		(hasUserBucketedGroupBy(data.Params.GroupBy) && !data.Params.ForceApplyCommitment)

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
// synthetic zero-usage fill point. Fill only where commitment math can charge an
// empty window — anywhere else the window bills $0 and a fill is pure noise:
//
//   - Inside a bucket, the bucket's own commitment governs the window (it
//     overrides the line item), so an empty in-bucket window is charged only when
//     that bucket has true-up.
//   - Outside every bucket, only the line item's own commitment can charge an
//     empty window, and only when it has true-up. A bucket-level-only true-up
//     (no top-level commitment) therefore fills its bucket windows but NOT the
//     out-of-bucket remainder — which previously produced 1440 all-zero
//     points/day for a MINUTE meter.
func shouldFillWindow(lineItem *subscription.SubscriptionLineItem, t time.Time) bool {
	if idx, ok := lineItem.CommitmentTimeBuckets.BucketIndexAt([]time.Time{t}, 0); ok {
		return lineItem.CommitmentTimeBuckets[idx].TrueUpEnabled
	}
	return lineItem.CommitmentTrueUpEnabled && lineItem.HasCommitment()
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

	s.captureBucketPoints(p.item, lineItem)
	p.item.Points = s.mergeBucketPointsByWindow(p.item.Points, p.aggType, p.data.Params.WindowSize, p.data.Params.BillingAnchor)

	return cost
}

// effectivePointWindowMinutes returns the span (in minutes) a displayed point
// actually covers: the request window, or the meter's bucket size when that is
// coarser. mergeBucketPointsByWindow cannot subdivide below the meter bucket, so a
// request window finer than the bucket leaves points at meter-bucket grain — the
// overlap check must use that coarser span, not the (smaller) request window.
func effectivePointWindowMinutes(requestWindow types.WindowSize, m *meter.Meter) int {
	minutes := requestWindow.ToMinutes()
	if m != nil && m.HasBucketSize() {
		if bm := m.Aggregation.BucketSize.ToMinutes(); bm > minutes {
			minutes = bm
		}
	}
	return minutes
}

// captureBucketPoints snapshots the bucket-grain points (with their per-window
// BucketID) before mergeBucketPointsByWindow rolls them up to the requested window.
// Only meaningful for line items with commitment time buckets — that's where the
// per-bucket summaries are built. The snapshot is the same backing slice; merge
// returns a new slice and does not mutate it.
func (s *meterUsageService) captureBucketPoints(item *events.DetailedUsageAnalytic, lineItem *subscription.SubscriptionLineItem) {
	if lineItem != nil && lineItem.HasCommitmentTimeBuckets() {
		item.BucketPoints = item.Points
	}
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
		s.captureBucketPoints(p.item, lineItem)
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
		points[i].BucketID = perWindow[i].bucketID
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

// PriceMatch pairs a resolved price with its meter, used by usage-tracking
// paths that need to match an event to a price+meter tuple.
type PriceMatch struct {
	Price *price.Price
	Meter *meter.Meter
}

// buildBucketSummaries produces one BucketSummary per CommitmentTimeBucket on
// the line item. It sums usage per-bucket from the supplied per-point series
// and rolls up the pre-stamped commitment fields (utilized / overage / true-up).
func buildBucketSummaries(
	ctx context.Context,
	priceService PriceService,
	points []events.UsageAnalyticPoint,
	lineItem *subscription.SubscriptionLineItem,
	data *AnalyticsData,
) []dto.BucketSummary {
	buckets := lineItem.CommitmentTimeBuckets
	summaries := make([]dto.BucketSummary, 0, len(buckets))
	for _, b := range buckets {
		r := rollupBucketPoints(ctx, priceService, points, b.ID, data.Prices[b.PriceID])
		summaries = append(summaries, dto.BucketSummary{
			BucketID:               b.ID,
			Start:                  b.Start,
			End:                    b.End,
			SubscriptionLineItemID: lineItem.ID,
			PriceID:                b.PriceID,
			CommitmentType:         string(b.CommitmentType),
			CommitmentValue:        b.CommitmentValue,
			TotalUsage:             r.usage,
			BaseCharge:             r.base,
			ComputedUtilized:       r.utilized,
			ComputedOverage:        r.overage,
			ComputedTrueUp:         r.trueUp,
		})
	}
	return summaries
}

type bucketPointRollup struct {
	usage, base, utilized, overage, trueUp decimal.Decimal
}

func rollupBucketPoints(
	ctx context.Context,
	priceService PriceService,
	points []events.UsageAnalyticPoint,
	bucketID string,
	p *price.Price,
) bucketPointRollup {
	var r bucketPointRollup
	for _, pt := range points {
		if pt.BucketID != bucketID {
			continue
		}
		r.usage = r.usage.Add(pt.Usage)
		if p != nil {
			r.base = r.base.Add(priceService.CalculateCost(ctx, p, pt.Usage))
		}
		r.utilized = r.utilized.Add(pt.ComputedCommitmentUtilizedAmount)
		r.overage = r.overage.Add(pt.ComputedOverageAmount)
		r.trueUp = r.trueUp.Add(pt.ComputedTrueUpAmount)
	}
	return r
}

// ensure meterUsageService implements MeterUsageService
var _ MeterUsageService = (*meterUsageService)(nil)
