package service

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	ca "github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/expression"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// defaultPropertyValuesLimit is the number of property values returned when the request sets no limit.
const defaultPropertyValuesLimit = 50

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

	// GetUnpricedMeterAnalytics returns one customer's raw usage on params.MeterIDs (both required),
	// whether or not those meters are on a subscription. Usage only; callers apply prices.
	GetUnpricedMeterAnalytics(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams) (*dto.GetUsageAnalyticsResponse, error)

	// GetDetailedUsageAnalytics adapts GetDetailedAnalytics to the dto-based
	// request shape used by callers that need to be signal-agnostic (e.g. the
	// usage-analytics CSV exporter). It resolves tenant/environment from ctx.
	GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error)

	// GetSubscriptionMeterUsage is the centralized meter-usage query function.
	// Both analytics and billing paths call this per-subscription to get line-item-bounded usage.
	GetSubscriptionMeterUsage(ctx context.Context, req *GetSubscriptionMeterUsageRequest) (*SubscriptionMeterUsage, error)

	// GetSubscriptionMeterUsageWithSub is the data-fed variant: the caller
	// supplies the subscription and no DB fetch happens for it.
	GetSubscriptionMeterUsageWithSub(ctx context.Context, sub *subscription.Subscription, req *GetSubscriptionMeterUsageRequest) (*SubscriptionMeterUsage, error)

	// ConvertToBillingCharges maps SubscriptionMeterUsage to billing charges.
	ConvertToBillingCharges(ctx context.Context, usage *SubscriptionMeterUsage) ([]*dto.SubscriptionUsageByMetersResponse, decimal.Decimal, error)

	// GetUsageTotal returns a meter's aggregated usage total (FINAL consistency;
	// supports a single range or multiple disjoint TimeRanges in one query).
	GetUsageTotal(ctx context.Context, req *dto.UsageTotalRequest) (decimal.Decimal, error)

	// GetCheckpointedUsage returns raw SUM usage before the cutoff using a checkpoint and its tail.
	GetCheckpointedUsage(ctx context.Context, req CheckpointedUsageRequest) (decimal.Decimal, error)

	// SaveUsageCheckpoint recomputes a cutoff from the preceding checkpoint, without double-counting retries.
	SaveUsageCheckpoint(ctx context.Context, req CheckpointedUsageRequest, invoiceID *string) (*meterusagecheckpoint.MeterUsageCheckpoint, error)

	// SaveUsageCheckpointsForSubscription records running SUM balances for the subscription's usage meters.
	SaveUsageCheckpointsForSubscription(ctx context.Context, sub *subscription.Subscription, until time.Time, invoiceID *string) error

	// GetMeterWindowCost prices the meter's usage in [from, to) through the
	// billing path, so a mid-window price change is priced per line-item segment.
	GetMeterWindowCost(ctx context.Context, sub *subscription.Subscription, meterID string, from, to time.Time) (decimal.Decimal, error)

	// GetSubscriptionWindowSpend prices every usage line on the subscription over [from, to),
	// returning the total and the amount per subscription line item.
	GetSubscriptionWindowSpend(ctx context.Context, sub *subscription.Subscription, from, to time.Time) (decimal.Decimal, map[string]decimal.Decimal, error)

	// ListUsageLineItems returns the line items that price the subscription's usage from `from`, or
	// every published line when lifetime; an inherited subscription uses its parent's.
	ListUsageLineItems(ctx context.Context, sub *subscription.Subscription, from time.Time, lifetime bool) ([]*subscription.SubscriptionLineItem, error)

	// DebugEvent powers GET /events/:id — reports processing status and
	// per-lookup diagnostics for a single event under the meter-usage pipeline.
	DebugEvent(ctx context.Context, externalCustomerID, eventID string, window events.TimeRange) (*dto.GetEventByIDResponse, error)

	// GetGroupablePropertyKeys returns the property keys in the customer's usage, most seen first, without aggregation keys.
	GetGroupablePropertyKeys(ctx context.Context, params *events.PropertyLookupParams) ([]string, error)

	// GetPropertyValues returns the key's values in the customer's usage, most seen first.
	GetPropertyValues(ctx context.Context, params *events.PropertyLookupParams) ([]string, error)
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

func (s *meterUsageService) GetUsageTotal(ctx context.Context, req *dto.UsageTotalRequest) (decimal.Decimal, error) {
	if s.repo == nil {
		return decimal.Zero, ierr.NewError("meter usage repository is not configured").Mark(ierr.ErrSystem)
	}
	result, err := s.repo.GetUsage(ctx, req.ToParams())
	if err != nil {
		return decimal.Zero, err
	}
	return result.TotalValue, nil
}

func (s *meterUsageService) GetMeterWindowCost(ctx context.Context, sub *subscription.Subscription, meterID string, from, to time.Time) (decimal.Decimal, error) {
	charges, err := s.windowCharges(ctx, sub, []string{meterID}, from, to)
	if err != nil {
		return decimal.Zero, err
	}
	total := decimal.Zero
	for _, c := range charges {
		if c != nil && c.MeterID == meterID {
			total = total.Add(c.AmountDecimal())
		}
	}
	return total, nil
}

func (s *meterUsageService) GetSubscriptionWindowSpend(ctx context.Context, sub *subscription.Subscription, from, to time.Time) (decimal.Decimal, map[string]decimal.Decimal, error) {
	perLine := make(map[string]decimal.Decimal)
	charges, err := s.windowCharges(ctx, sub, nil, from, to)
	if err != nil {
		return decimal.Zero, nil, err
	}
	total := decimal.Zero
	for _, c := range charges {
		if c == nil {
			continue
		}
		amount := c.AmountDecimal()
		total = total.Add(amount)
		perLine[c.SubscriptionLineItemID] = perLine[c.SubscriptionLineItemID].Add(amount)
	}
	return total, perLine, nil
}

// windowCharges prices the subscription's usage over [from, to) at final consistency, limited to
// meterIDs when given.
func (s *meterUsageService) windowCharges(ctx context.Context, sub *subscription.Subscription, meterIDs []string, from, to time.Time) ([]*dto.SubscriptionUsageByMetersResponse, error) {
	if !to.After(from) {
		return nil, nil
	}
	// The usage query replaces LineItems with the window's lines; a copy keeps the caller's.
	window := *sub
	subUsage, err := s.GetSubscriptionMeterUsageWithSub(ctx, &window, &GetSubscriptionMeterUsageRequest{
		SubscriptionID:  sub.ID,
		StartTime:       from,
		EndTime:         to,
		MeterIDs:        meterIDs,
		UseFinal:        true,
		IncludeChildren: true,
	})
	if err != nil {
		return nil, err
	}
	charges, _, err := s.ConvertToBillingCharges(ctx, subUsage)
	return charges, err
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

	// LineItems are the active line items at StartTime; empty loads them.
	LineItems []*subscription.SubscriptionLineItem
}

// usageQueryGroup batches usage queries with the same period and aggregation settings.
type usageQueryGroup struct {
	Start             time.Time
	End               time.Time
	AggType           types.AggregationType
	BillingBucketSize types.WindowSize
	BillingGroupBy    string
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

	// LineTotals holds the selected cost per line item, before allocation to groups and points.
	LineTotals map[string]decimal.Decimal
	// LineBilledPeriods is what each billing period bills the lines v2 commitments cover: utilized, overage and true-up.
	LineBilledPeriods map[string][]billedPeriod
}

// windowAnchor returns the anchor the subscription's usage windows were cut with: the request's, else its billing anchor.
func (d *AnalyticsData) windowAnchor(subscriptionID string) *time.Time {
	if d.Params.BillingAnchor != nil {
		return d.Params.BillingAnchor
	}

	if sub := d.SubscriptionsMap[subscriptionID]; sub != nil {
		return &sub.BillingAnchor
	}

	return nil
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

	sub, err := s.SubRepo.Get(ctx, req.SubscriptionID)
	if err != nil {
		return nil, err
	}
	return s.GetSubscriptionMeterUsageWithSub(ctx, sub, req)
}

// GetSubscriptionMeterUsageWithSub is the data-fed variant of
// GetSubscriptionMeterUsage: the caller supplies the subscription so no extra
// DB fetch happens for it. Line items are (re)loaded scoped to the usage window
// and assigned onto sub.LineItems.
func (s *meterUsageService) GetSubscriptionMeterUsageWithSub(
	ctx context.Context,
	sub *subscription.Subscription,
	req *GetSubscriptionMeterUsageRequest,
) (*SubscriptionMeterUsage, error) {
	if req == nil || sub == nil {
		return nil, ierr.NewError("subscription is required").Mark(ierr.ErrValidation)
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
		usageEndTime = clock.Now()
	}

	// 4. Get line items for this subscription
	lineItems := req.LineItems
	if len(lineItems) == 0 || req.LifetimeUsage {
		lineItems, err = s.ListUsageLineItems(ctx, sub, usageStartTime, req.LifetimeUsage)
		if err != nil {
			return nil, err
		}
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

	// 7. Distinct meter optimization — skip meters with zero usage.
	// No FINAL: duplicate rows share their meter_id, so deduping can't change the set.
	distinctMeterIDs, err := s.repo.GetDistinctMeterIDs(ctx, &events.MeterUsageQueryParams{
		TenantID:            types.GetTenantID(ctx),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		ExternalCustomerIDs: externalCustomerIDs,
		StartTime:           usageStartTime,
		EndTime:             usageEndTime,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get distinct meter_ids from meter_usage: %w", err)
	}
	activeMeterIDs := make(map[string]bool, len(distinctMeterIDs))
	for _, id := range distinctMeterIDs {
		activeMeterIDs[id] = true
	}

	isAnalyticsQuery := req.WindowSize != "" || len(req.PropertyFilters) > 0 || len(req.Sources) > 0 || len(req.GroupBy) > 0

	// 8. Include running SUM meters even when the requested window has no events.
	meterToLineItems := make(map[string][]*subscription.SubscriptionLineItem)
	meterAggType := make(map[string]types.AggregationType)
	for _, item := range lineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE || item.MeterID == "" {
			continue
		}
		if !activeMeterIDs[item.MeterID] && (isAnalyticsQuery || !keepsRunningAggregate(result.MeterMap[item.MeterID])) {
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

	// 9. Split bucketed vs standard work. The decision is per line item, not per
	// meter: bucketing can come from the line item's price, so two line items on
	// the same meter may disagree. Legacy meter-level bucketing still resolves
	// through the same helper, so nothing changes for existing configurations.
	isBucketedItem := func(item *subscription.SubscriptionLineItem, m *meter.Meter) bool {
		return price.IsBucketedMax(result.PriceMap[item.PriceID], m) ||
			price.IsBucketedSum(result.PriceMap[item.PriceID], m)
	}

	// 10. Query standard meters WITH per-line-item date ranges.
	//     Group by (effectiveStart, effectiveEnd, aggType) so line items that
	//     share the same period are batched into one ClickHouse call.
	standardGroups := make(map[usageQueryGroup][]*lineItemWithMeter)
	for meterID, items := range meterToLineItems {
		m := result.MeterMap[meterID]
		for _, item := range items {
			if isBucketedItem(item, m) {
				continue
			}
			start := item.GetPeriodStart(usageStartTime)
			end := item.GetPeriodEnd(usageEndTime)
			key := usageQueryGroup{
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
		if isAnalyticsQuery {
			// Decide whether user-supplied group_by adds dimensions beyond meter_id.
			// If yes, split the batch: commitment line items query with meter_id only
			// (clean aggregate for applyLineItemCommitment), non-commitment items query
			// with the full group_by (per-group breakdown in the response).
			splitsLines := hasExtraGroupBy(req.GroupBy)
			splitCommitments := req.ForceApplyCommitment || len(req.PropertyFilters) > 0 || len(req.Sources) > 0

			var commitmentLIs, nonCommitmentLIs []*lineItemWithMeter
			if splitsLines {
				for _, liw := range lineItemsInGroup {
					// ForceApplyCommitment (export path) folds commitment LIs
					// into the fan-out path so the CSV gets per-source rows for
					// them too. Trade-off: commitment fires per fanned row and
					// multi-counts the true-up across sources — accepted at the
					// flag's call site.
					if !splitCommitments && liw.Item != nil && liw.Item.HasAnyCommitment() {
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
			if err := s.queryAndAppendAnalyticsEntries(ctx, req, group, nonCommitmentLIs, splitsLines, externalCustomerIDs, result); err != nil {
				return nil, err
			}
		} else {
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

			// Running meters report the total since the first event; the window query keeps their event count.
			for _, meterID := range meterIDs {
				if !keepsRunningAggregate(result.MeterMap[meterID]) {
					continue
				}
				total, err := s.GetCheckpointedUsage(ctx, CheckpointedUsageRequest{
					CustomerID: sub.CustomerID, ExternalCustomerIDs: externalCustomerIDs,
					MeterID: meterID, AggregationType: types.AggregationSum, Until: group.End,
					IgnoreCheckpoints: !usesCheckpoints(sub),
				})
				if err != nil {
					return nil, err
				}
				r, ok := resultByMeter[meterID]
				if !ok {
					r = &events.MeterUsageAggregationResult{MeterID: meterID}
					resultByMeter[meterID] = r
				}
				r.TotalValue = total
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

	for meterID, items := range meterToLineItems {
		m := result.MeterMap[meterID]
		if m == nil {
			continue
		}
		for _, item := range items {
			if !isBucketedItem(item, m) {
				continue
			}
			p := result.PriceMap[item.PriceID]
			itemStart := item.GetPeriodStart(usageStartTime)
			itemEnd := item.GetPeriodEnd(usageEndTime)

			if useDetailed {
				// Analytics fan-out: one LineItemMeterUsage per (source, properties)
				// combo. Repo does the per-combo aggregation in SQL; nothing to
				// re-group in Go.
				detailedResults, err := s.queryBucketedMeterAnalyticsDetailed(
					ctx, m, p, externalCustomerIDs,
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
				ctx, m, p, externalCustomerIDs,
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
					if price.IsBucketedMax(result.PriceMap[item.PriceID], m) {
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
	group usageQueryGroup,
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
	p *price.Price,
	externalCustomerIDs []string,
	periodStart, periodEnd time.Time,
	billingAnchor *time.Time,
	useFinal bool,
	propertyFilters map[string][]string,
	sources []string,
	collectSources bool,
) (*events.AggregationResult, error) {
	aggType := m.Aggregation.Type
	meterGroupBy := price.BucketedGroupBy(p, m)
	if price.IsBucketedSum(p, m) {
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
		WindowSize:          price.ResolveBucketSize(p, m),
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

// hasExtraGroupBy reports whether group_by asks for any dimension beyond the meter, splitting a line into combos.
func hasExtraGroupBy(groupBy []string) bool {
	for _, g := range groupBy {
		if g != "" && g != "meter_id" && g != "feature_id" {
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
//   - "meter_id"              (no-op at SQL level; implicit at the meter)
//   - "source"
//   - "external_customer_id"  (real, indexed column — groups usage per customer)
//   - "properties.<name>" where name matches a safe identifier pattern
//
// Anything else (empty string, "properties." with no name, unknown tokens) is
// a user error and surfaces as a 400 with a clear hint, rather than being
// dropped on the floor inside clickhouse.bucketedGroupByDims where it would
// show up as a successful response with surprising shape.
func validateAnalyticsGroupBy(groupBy []string) error {
	for _, g := range groupBy {
		switch {
		case g == "meter_id" || g == "source" || g == "external_customer_id":
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
				WithHintf("group_by entry %q is not recognized (allowed: 'meter_id', 'source', 'external_customer_id', 'properties.<name>')", g).
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
	p *price.Price,
	externalCustomerIDs []string,
	periodStart, periodEnd time.Time,
	billingAnchor *time.Time,
	useFinal bool,
	propertyFilters map[string][]string,
	sources []string,
	groupBy []string,
) ([]*events.MeterUsageDetailedResult, error) {
	aggType := m.Aggregation.Type
	if price.IsBucketedSum(p, m) {
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
		WindowSize:          price.ResolveBucketSize(p, m),
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

	// One instant serves the whole request, so subscriptions priced across midnight are projected alike.
	now := clock.Now()

	// Set defaults
	if params.EndTime.IsZero() {
		params.EndTime = now
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
		meterIDs, err := featureMeterIDs(ctx, s.FeatureRepo, params.FeatureIDs)
		if err != nil {
			return nil, ierr.NewError("failed to resolve feature IDs to meter IDs").Mark(ierr.ErrDatabase)
		}
		params.MeterIDs = meterIDs
		if len(params.MeterIDs) == 0 {
			return emptyAnalyticsResponse(params), nil
		}
	}

	cust, subscriptions, err := s.resolveCustomerAndSubscriptions(ctx, params.ExternalCustomerID)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}
	if len(subscriptions) == 0 {
		// An unknown customer or one without subscriptions has no price to bill against, so report zero instead of
		// fanning the query out over every meter in the environment.
		return emptyAnalyticsResponse(params), nil
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
		return emptyAnalyticsResponse(params), nil
	}

	// Call GetSubscriptionMeterUsage per subscription
	var allUsages []*SubscriptionMeterUsage
	lineTotals := map[string]decimal.Decimal{}
	lineBilledPeriods := map[string][]billedPeriod{}
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
			return nil, err
		}

		totals, billed, err := s.calculateLineItemTotals(ctx, usage, params, billingAnchor, subEndTime, now)
		if err != nil {
			return nil, err
		}

		maps.Copy(lineTotals, totals)
		maps.Copy(lineBilledPeriods, billed)
		allUsages = append(allUsages, usage)
	}

	// Merge into AnalyticsData
	data := s.mergeSubscriptionUsagesToAnalyticsData(cust, subscriptions, allUsages, params, lineTotals, lineBilledPeriods)

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
	response := s.toUsageAnalyticsResponseDTO(ctx, data, data.Meters, params, lineItemCoupons, subscriptionCoupons)
	if lo.Contains(params.Expand, string(types.ExpandInstalments)) {
		instalments, err := s.invoicedInstalments(ctx, cust, subscriptions, params)
		if err != nil {
			return nil, err
		}

		setInstalments(response, instalments)
	}

	return response, nil
}

// emptyAnalyticsResponse returns a response without usage; with no meter queried, it lists no instalments.
func emptyAnalyticsResponse(params *events.MeterUsageDetailedAnalyticsParams) *dto.GetUsageAnalyticsResponse {
	response := &dto.GetUsageAnalyticsResponse{
		TotalCost: decimal.Zero,
		Items:     []dto.UsageAnalyticItem{},
	}
	if lo.Contains(params.Expand, string(types.ExpandInstalments)) {
		setInstalments(response, nil)
	}

	return response
}

// invoicedInstalments returns the instalments invoiced in the request's range for the commitments on the customer's
// subscriptions that cover a queried meter. Property and source filters apply no commitments, so they list none.
func (s *meterUsageService) invoicedInstalments(
	ctx context.Context,
	cust *customer.Customer,
	subscriptions []*subscription.Subscription,
	params *events.MeterUsageDetailedAnalyticsParams,
) ([]*billingterm.Instalment, error) {
	if len(params.PropertyFilters) > 0 || len(params.Sources) > 0 {
		return nil, nil
	}

	commitments := newCommitmentService(s.ServiceParams)
	var ownerIDs []string
	for _, sub := range subscriptions {
		// A parent subscription appended for a child caller bills the parent, not this customer.
		if sub.CustomerID != cust.ID {
			continue
		}

		ids, err := commitments.CoveringMeters(ctx, sub.ID, params.MeterIDs)
		if err != nil {
			return nil, err
		}

		ownerIDs = append(ownerIDs, ids...)
	}

	invoiced := types.Period{Start: params.StartTime, End: params.EndTime}
	return s.BillingInstalmentRepo.ListInvoiced(ctx, types.BillingTermOwnerCommitment, ownerIDs, invoiced)
}

// setInstalments lists the instalments on the response and sets its total with them.
func setInstalments(response *dto.GetUsageAnalyticsResponse, instalments []*billingterm.Instalment) {
	total := response.TotalCost
	response.Instalments = make([]dto.UsageAnalyticInstalment, 0, len(instalments))
	for _, inst := range instalments {
		total = total.Add(inst.Amount)
		response.Instalments = append(response.Instalments, dto.UsageAnalyticInstalment{
			ID:                 inst.ID,
			CommitmentID:       inst.OwnerID,
			SubscriptionID:     lo.FromPtr(inst.SubscriptionID),
			InvoiceID:          lo.FromPtr(inst.InvoiceID),
			InvoiceLabel:       inst.InvoiceLabel,
			Amount:             inst.Amount,
			Currency:           inst.Currency,
			InvoicedAt:         inst.InvoicedAt,
			ServicePeriodStart: inst.ServicePeriodStart,
			ServicePeriodEnd:   inst.ServicePeriodEnd,
		})
	}

	response.TotalCostWithInstalments = &total
}

// mergeSubscriptionUsagesToAnalyticsData converts N SubscriptionMeterUsage results
// into a single AnalyticsData suitable for the cost calculation pipeline.
func (s *meterUsageService) mergeSubscriptionUsagesToAnalyticsData(
	cust *customer.Customer,
	subscriptions []*subscription.Subscription,
	usages []*SubscriptionMeterUsage,
	params *events.MeterUsageDetailedAnalyticsParams,
	lineTotals map[string]decimal.Decimal,
	lineBilledPeriods map[string][]billedPeriod,
) *AnalyticsData {
	data := &AnalyticsData{
		LineTotals:            lineTotals,
		LineBilledPeriods:     lineBilledPeriods,
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
				if lu.LineItem == nil || (!lu.LineItem.HasAnyCommitment() && lineBilledPeriods[lu.LineItem.ID] == nil) {
					continue
				}
			}

			analytic := &events.DetailedUsageAnalytic{
				MeterID: lu.MeterID,
			}
			if lu.AnalyticsResult != nil {
				analytic.Source = lu.AnalyticsResult.Source
				analytic.Sources = lu.AnalyticsResult.Sources
				analytic.ExternalCustomerID = lu.AnalyticsResult.ExternalCustomerID
				analytic.Properties = lu.AnalyticsResult.Properties
			} else if lu.BucketedResult != nil && len(lu.BucketedResult.Sources) > 0 {
				analytic.Sources = lu.BucketedResult.Sources
			}

			// Set usage values from the line item usage
			if lu.BucketedResult != nil && lu.Meter != nil {
				// Bucketed meter: set both MaxUsage and TotalUsage
				if price.IsBucketedMax(lu.Price, lu.Meter) {
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

func (s *meterUsageService) GetUnpricedMeterAnalytics(
	ctx context.Context,
	params *events.MeterUsageDetailedAnalyticsParams,
) (*dto.GetUsageAnalyticsResponse, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}
	if len(params.MeterIDs) == 0 {
		return nil, ierr.NewError("meter_ids are required").
			WithHint("Unpriced meter analytics must be scoped to an explicit meter set").
			Mark(ierr.ErrValidation)
	}

	if params.ExternalCustomerID == "" {
		// Without a customer this would scan every customer's usage.
		return nil, ierr.NewError("external_customer_id is required").
			WithHint("Unpriced meter analytics must be scoped to a customer").
			Mark(ierr.ErrValidation)
	}

	if !params.IncludeChildren {
		// Only the timezone is needed; skip loading subscriptions and line items.
		cust, err := s.CustomerRepo.GetByLookupKey(ctx, params.ExternalCustomerID)
		if err != nil {
			return nil, err
		}
		params.Timezone = cust.Timezone
		return s.getDetailedAnalyticsWithoutSubscriptionContext(ctx, params)
	}

	cust, subs, err := s.resolveCustomerAndSubscriptions(ctx, params.ExternalCustomerID)
	if err != nil {
		return nil, err
	}
	params.Timezone = cust.Timezone

	externalIDs := []string{params.ExternalCustomerID}
	for _, sub := range subs {
		// Parent subs loaded for an inherited child belong to another customer;
		// expanding them would pull in sibling usage.
		if sub.CustomerID != cust.ID {
			continue
		}
		ids, err := s.resolveExternalCustomerIDs(ctx, sub, true)
		if err != nil {
			return nil, err
		}
		externalIDs = append(externalIDs, ids...)
	}
	params.ExternalCustomerIDs = lo.Uniq(externalIDs)
	params.ExternalCustomerID = ""

	return s.getDetailedAnalyticsWithoutSubscriptionContext(ctx, params)
}

// getDetailedAnalyticsWithoutSubscriptionContext queries meter_usage directly for
// params.MeterIDs, with no subscription bounding and no pricing.
func (s *meterUsageService) getDetailedAnalyticsWithoutSubscriptionContext(
	ctx context.Context,
	params *events.MeterUsageDetailedAnalyticsParams,
) (*dto.GetUsageAnalyticsResponse, error) {
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

	// Split meters. No subscription was named, so there is no price to resolve
	// against — nil yields the meter's legacy bucket size, which is the
	// grandfathered answer for this surface.
	var bucketedMeterIDs, standardMeterIDs []string
	for _, m := range meters {
		switch {
		case price.IsBucketedMax(nil, m), price.IsBucketedSum(nil, m):
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
	// usageQueryGroup (see queryAndAppendAnalyticsEntries). N is small in
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
			if !price.IsBucketedMax(nil, m) && !price.IsBucketedSum(nil, m) {
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
			MeterID:            r.MeterID,
			TotalUsage:         r.TotalUsage,
			MaxUsage:           r.MaxUsage,
			LatestUsage:        r.LatestUsage,
			CountUniqueUsage:   r.CountUniqueUsage,
			EventCount:         r.EventCount,
			Source:             r.Source,
			Sources:            r.Sources,
			ExternalCustomerID: r.ExternalCustomerID,
			Properties:         r.Properties,
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
			FeatureID:          analytic.FeatureID,
			PriceID:            analytic.PriceID,
			MeterID:            analytic.MeterID,
			SubLineItemID:      analytic.SubLineItemID,
			SubscriptionID:     analytic.SubscriptionID,
			FeatureName:        analytic.FeatureName,
			EventName:          analytic.EventName,
			Source:             analytic.Source,
			ExternalCustomerID: analytic.ExternalCustomerID,
			Unit:               analytic.Unit,
			UnitPlural:         analytic.UnitPlural,
			AggregationType:    analytic.AggregationType,
			TotalUsage:         analytic.TotalUsage,
			Subtotal:           analytic.TotalCost,
			TotalCost:          analytic.TotalCost,
			TotalDiscount:      decimal.Zero,
			Currency:           analytic.Currency,
			EventCount:         analytic.EventCount,
			Properties:         analytic.Properties,
			CommitmentInfo:     analytic.CommitmentInfo,
			Points:             make([]dto.UsageAnalyticPoint, 0, len(analytic.Points)),
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
			if bs := price.ResolveBucketSize(data.Prices[analytic.PriceID], m); bs != "" {
				item.WindowSize = params.WindowSize.Max(bs)
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

					ComputedCommitmentUtilizedQuantity: point.ComputedCommitmentUtilizedQuantity,
					ComputedOverageQuantity:            point.ComputedOverageQuantity,
					ComputedTrueUpQuantity:             point.ComputedTrueUpQuantity,
				}
				// Per-point bucket identity: every bucket the rolled-up window
				// overlaps (informational hint only — see dto.UsageAnalyticPoint).
				if lineItemForBucket != nil {
					windowMin := effectivePointWindowMinutes(params.WindowSize, data.Prices[analytic.PriceID], meterMap[analytic.MeterID])
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
	// Known ID set → per-id cache path. Empty set → fall back to full tenant list.
	if len(params.MeterIDs) > 0 {
		meters, err := s.MeterRepo.ListByIDs(ctx, params.MeterIDs)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to fetch meters for detailed analytics").
				Mark(ierr.ErrDatabase)
		}
		return meters, nil
	}

	meters, err := s.MeterRepo.List(ctx, types.NewNoLimitMeterFilter())
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch meters for detailed analytics").
			Mark(ierr.ErrDatabase)
	}

	meterIDs := make([]string, len(meters))
	for i, m := range meters {
		meterIDs[i] = m.ID
	}
	params.MeterIDs = meterIDs
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
			WindowSize:         price.ResolveBucketSize(nil, m),
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
		WindowSize:         price.ResolveBucketSize(nil, m),
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

	// Admin analytics path — no price in scope (see getBucketedMeterAnalytics),
	// so nil resolves to the meter's legacy bucket size.
	if price.IsBucketedMax(nil, m) {
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
			if price.IsBucketedMax(nil, m) {
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
			hasGroupBy := price.BucketedGroupBy(lu.Price, lu.Meter) != ""
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
			Currency:               lu.Price.Currency,
			DisplayAmount:          price.GetDisplayAmountWithPrecision(cost, lu.Price.Currency),
			FilterValues:           make(price.JSONBFilters),
			MeterID:                lu.MeterID,
			Price:                  lu.Price,
			BucketedUsageResult:    lu.BucketedResult,
		}
		charge.SetAmountWithCurrencyPrecision(cost, lu.Price.Currency)
		charge.SetQuantityDecimal(quantity)

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
//
// It never returns an empty slice: callers feed the result straight into
// MeterUsageQueryParams.ExternalCustomerIDs, and BuildWhereClause drops the
// external_customer_id predicate when that slice is empty. An empty return
// would therefore widen every downstream meter_usage query to the entire
// tenant instead of narrowing it. Unresolvable customers are an error.
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
	externalIDs = lo.Uniq(externalIDs)

	// Fail closed rather than silently querying the whole tenant. CustomerRepo.List
	// matches published customers only — CustomerQueryOptions.ApplyStatusFilter turns
	// an unset status into status = 'published' — so a customer soft-deleted while its
	// subscription stayed active resolves to zero rows and lands here.
	if len(externalIDs) == 0 {
		return nil, ierr.NewError("no resolvable customer for subscription").
			WithHint("The subscription's customer could not be resolved. It may have been deleted while the subscription remained active.").
			WithReportableDetails(map[string]any{
				"subscription_id":        sub.ID,
				"customer_id":            sub.CustomerID,
				"looked_up_customer_ids": internalIDs,
				"include_children":       includeChildren,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	return externalIDs, nil
}

func (s *meterUsageService) ListUsageLineItems(ctx context.Context, sub *subscription.Subscription, from time.Time, lifetime bool) ([]*subscription.SubscriptionLineItem, error) {
	// An inherited subscription bills on its parent's lines.
	ownerID := sub.ID
	if sub.SubscriptionType == types.SubscriptionTypeInherited && lo.FromPtr(sub.ParentSubscriptionID) != "" {
		ownerID = lo.FromPtr(sub.ParentSubscriptionID)
	}
	filter := types.NewNoLimitSubscriptionLineItemFilter()
	filter.SubscriptionIDs = []string{ownerID}
	if lifetime {
		filter.ActiveFilter = false
		filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	} else {
		filter.ActiveFilter = true
		filter.CurrentPeriodStart = &from
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

	// Filtered usage excludes commitments; grouped usage gets its line-level allocation below.
	// CSV exports may retain legacy per-group commitments only when no usage filters apply.
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

		if price.IsBucketedMax(p, m) || price.IsBucketedSum(p, m) {
			s.calculateBucketedCost(ctx, priceService, item, p, m, data, skipCommitment)
		} else {
			s.calculateRegularCost(ctx, priceService, item, m, p, data, skipCommitment)
		}
	}

	data.Analytics = allocateLineItemCosts(data)
	return nil
}

// allocateLineItemCosts amortizes each line's selected cost across groups and points in proportion to usage. A line v2
// commitments priced keeps each billing period's charges on that period's points.
func allocateLineItemCosts(data *AnalyticsData) []*events.DetailedUsageAnalytic {
	allocated := slices.Clone(data.Analytics)
	groupsByLine := make(map[string][]*events.DetailedUsageAnalytic)
	for i, original := range data.Analytics {
		if _, hasTotal := data.LineTotals[original.SubLineItemID]; !hasTotal {
			continue
		}
		group := *original
		group.Points = slices.Clone(original.Points)
		group.BucketPoints = slices.Clone(original.BucketPoints)
		allocated[i] = &group
		groupsByLine[group.SubLineItemID] = append(groupsByLine[group.SubLineItemID], &group)
	}

	for lineID, groups := range groupsByLine {
		if billed := data.LineBilledPeriods[lineID]; len(billed) > 0 {
			allocateBilledPeriods(data, groups, billed)
			continue
		}

		groupUsage := lo.Map(groups, func(group *events.DetailedUsageAnalytic, _ int) decimal.Decimal { return group.TotalUsage })
		// Group cost = selected line cost × group usage / total grouped usage.
		groupCosts := roundShares(allocateUsageCost(data.LineTotals[lineID], groupUsage, groups[0].AggregationType), groups[0].Currency)
		for i, group := range groups {
			// Keep existing correction points when a zero-net group's charge is unchanged.
			if group.TotalUsage.IsZero() && group.TotalCost.Equal(groupCosts[i]) {
				continue
			}
			group.TotalCost = groupCosts[i]
			for _, points := range [][]events.UsageAnalyticPoint{group.Points, group.BucketPoints} {
				pointUsage := make([]decimal.Decimal, len(points))
				for j, point := range points {
					pointUsage[j] = point.Billed()
				}

				for j, cost := range roundShares(allocateUsageCost(group.TotalCost, pointUsage, group.AggregationType), group.Currency) {
					points[j].Cost = cost
				}
			}
		}
	}
	return allocated
}

// allocateBilledPeriods shares what each billing period billed a line among its groups, and each group's share among its
// points in that period, by the usage billing priced.
func allocateBilledPeriods(data *AnalyticsData, groups []*events.DetailedUsageAnalytic, periods []billedPeriod) {
	var flags types.CommitmentInfo
	for _, bp := range periods {
		flags.Type = lo.CoalesceOrEmpty(flags.Type, bp.billed.Type)
		flags.IsWindowed = flags.IsWindowed || bp.billed.IsWindowed
		flags.TrueUpEnabled = flags.TrueUpEnabled || bp.billed.TrueUpEnabled
	}

	// A bucketed price keeps its points at the bucket when that is coarser than the request window.
	bucket := price.ResolveBucketSize(data.Prices[groups[0].PriceID], data.Meters[groups[0].MeterID])
	grain := data.Params.WindowSize
	if grain != "" {
		grain = grain.Max(bucket)
	}
	windows := newPointWindows(grain, data.windowAnchor(groups[0].SubscriptionID))

	// Bucket points belong to v1 commitment time buckets, which no v2 commitment covers, so only points are reset.
	rangeUsage := make([]decimal.Decimal, len(groups))
	for gi, group := range groups {
		info := flags
		group.CommitmentInfo, group.TotalCost, rangeUsage[gi] = &info, decimal.Zero, group.TotalUsage
		for j := range group.Points {
			point := &group.Points[j]
			point.Cost, point.ComputedCommitmentUtilizedAmount = decimal.Zero, decimal.Zero
			point.ComputedOverageAmount, point.ComputedTrueUpAmount = decimal.Zero, decimal.Zero
			point.ComputedCommitmentUtilizedQuantity, point.ComputedOverageQuantity = decimal.Zero, decimal.Zero
			point.ComputedTrueUpQuantity = decimal.Zero
		}
	}

	// Billed usage adds up across windows for sums, counts and any bucketed price. Other aggregations' points don't, so a
	// group using the line in a period weighs its range usage.
	additive := bucket != "" || lo.Contains(
		[]types.AggregationType{types.AggregationSum, types.AggregationCount, types.AggregationSumWithMultiplier},
		groups[0].AggregationType,
	)
	for i, bp := range periods {
		// A point counts toward the period holding the middle of its window, so windows cut on other edges or in another
		// timezone still land on the period they mostly cover. The first and last periods take the points beyond them.
		bounds := bp.period
		if i == 0 {
			bounds.Start = time.Time{}
		}
		if i == len(periods)-1 {
			bounds.End = time.Date(9999, time.January, 1, 0, 0, 0, 0, time.UTC)
		}

		var fill time.Time
		if grain != "" {
			fill = windows.fillWindow(bp.period)
		}

		pointsIn := make([][]int, len(groups))
		weights := make([]decimal.Decimal, len(groups))
		for gi, group := range groups {
			pointsIn[gi] = windows.periodPoints(group.Points, bounds, fill)
			for _, j := range pointsIn[gi] {
				if additive {
					weights[gi] = weights[gi].Add(group.Points[j].Billed())
				} else if group.Points[j].Usage.IsPositive() {
					weights[gi] = group.TotalUsage
				}
			}
		}
		if !lo.SomeBy(weights, decimal.Decimal.IsPositive) {
			weights = rangeUsage
		}

		for gi, share := range splitBilled(bp.billed, weights, groups[0].AggregationType, groups[0].Currency) {
			group := groups[gi]
			info := group.CommitmentInfo
			info.ComputedCommitmentUtilizedAmount = info.ComputedCommitmentUtilizedAmount.Add(share.ComputedCommitmentUtilizedAmount)
			info.ComputedOverageAmount = info.ComputedOverageAmount.Add(share.ComputedOverageAmount)
			info.ComputedTrueUpAmount = info.ComputedTrueUpAmount.Add(share.ComputedTrueUpAmount)
			info.ComputedCommitmentUtilizedQuantity = info.ComputedCommitmentUtilizedQuantity.Add(share.ComputedCommitmentUtilizedQuantity)
			info.ComputedOverageQuantity = info.ComputedOverageQuantity.Add(share.ComputedOverageQuantity)
			info.ComputedTrueUpQuantity = info.ComputedTrueUpQuantity.Add(share.ComputedTrueUpQuantity)
			// Utilized plus true-up: what the commitment itself bills; overage is billed outside it.
			info.Amount = info.ComputedCommitmentUtilizedAmount.Add(info.ComputedTrueUpAmount)
			total := share.ComputedCommitmentUtilizedAmount.Add(share.ComputedOverageAmount).Add(share.ComputedTrueUpAmount)
			group.TotalCost = group.TotalCost.Add(total)

			// A billing-terms commitment covers usage at $0, so a share can carry quantities without an amount.
			carries := !total.IsZero() ||
				!share.ComputedCommitmentUtilizedQuantity.IsZero() ||
				!share.ComputedOverageQuantity.IsZero() ||
				!share.ComputedTrueUpQuantity.IsZero()
			at := pointsIn[gi]
			if len(at) == 0 && !fill.IsZero() && carries {
				group.Points = append(group.Points, events.UsageAnalyticPoint{Timestamp: fill, WindowStart: fill})
				at = []int{len(group.Points) - 1}
			}

			usage := make([]decimal.Decimal, len(at))
			for k, j := range at {
				usage[k] = group.Points[j].Billed()
			}

			for k, part := range splitBilled(&share, usage, group.AggregationType, group.Currency) {
				point := &group.Points[at[k]]
				point.ComputedCommitmentUtilizedAmount = point.ComputedCommitmentUtilizedAmount.Add(part.ComputedCommitmentUtilizedAmount)
				point.ComputedOverageAmount = point.ComputedOverageAmount.Add(part.ComputedOverageAmount)
				point.ComputedTrueUpAmount = point.ComputedTrueUpAmount.Add(part.ComputedTrueUpAmount)
				point.ComputedCommitmentUtilizedQuantity = point.ComputedCommitmentUtilizedQuantity.Add(part.ComputedCommitmentUtilizedQuantity)
				point.ComputedOverageQuantity = point.ComputedOverageQuantity.Add(part.ComputedOverageQuantity)
				point.ComputedTrueUpQuantity = point.ComputedTrueUpQuantity.Add(part.ComputedTrueUpQuantity)
				point.Cost = point.Cost.Add(part.ComputedCommitmentUtilizedAmount).
					Add(part.ComputedOverageAmount).
					Add(part.ComputedTrueUpAmount)
			}
		}
	}

	for _, group := range groups {
		slices.SortStableFunc(group.Points, func(a, b events.UsageAnalyticPoint) int {
			return a.Timestamp.Compare(b.Timestamp)
		})
	}
}

// pointWindows cuts time into the windows analytics points cover, in UTC from the anchor's UTC day as the usage
// queries and bucket roll-ups cut them.
type pointWindows struct {
	grain  types.WindowSize
	anchor *time.Time
}

// newPointWindows returns the windows of grain, months starting on the anchor's day.
func newPointWindows(grain types.WindowSize, anchor *time.Time) pointWindows {
	return pointWindows{grain: grain, anchor: anchor}
}

// startOf returns the start of the window holding t.
func (w pointWindows) startOf(t time.Time) time.Time {
	return truncateToBucketStart(t.UTC(), w.grain, w.anchor)
}

// endOf returns the end of the window starting at start; months run to the next anchor day, not a fixed 30 days.
func (w pointWindows) endOf(start time.Time) time.Time {
	start = start.UTC()
	if w.grain != types.WindowSizeMonth {
		return start.Add(time.Duration(w.grain.ToMinutes()) * time.Minute)
	}

	shift := 0
	if w.anchor != nil {
		shift = w.anchor.Day() - 1
	}

	month := start.AddDate(0, 0, -shift)
	return time.Date(month.Year(), month.Month()+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, shift)
}

// middleOf returns the middle of the window starting at start.
func (w pointWindows) middleOf(start time.Time) time.Time {
	return start.Add(w.endOf(start).Sub(start) / 2)
}

// fillWindow returns the start of the window that carries the charges of a period without points of its own: the
// first window whose middle the period holds, else, for a period shorter than a window, the window holding its middle.
func (w pointWindows) fillWindow(period types.Period) time.Time {
	start := w.startOf(period.Start)
	if w.middleOf(start).Before(period.Start) {
		start = w.endOf(start)
	}

	if !w.middleOf(start).Before(period.End) {
		return w.startOf(period.Start.Add(period.End.Sub(period.Start) / 2))
	}

	return start
}

// periodPoints returns the indexes of the points whose windows have their middle in bounds or, with none, of the point
// whose window holds the middle of the fill window.
func (w pointWindows) periodPoints(points []events.UsageAnalyticPoint, bounds types.Period, fill time.Time) []int {
	fillMiddle := w.middleOf(fill)
	var in []int
	holding := -1
	for j, point := range points {
		end := w.endOf(point.Timestamp)
		middle := point.Timestamp.Add(end.Sub(point.Timestamp) / 2)
		if !middle.Before(bounds.Start) && middle.Before(bounds.End) {
			in = append(in, j)
		}
		if !fill.IsZero() && !fillMiddle.Before(point.Timestamp) && fillMiddle.Before(end) {
			holding = j
		}
	}

	if len(in) == 0 && holding >= 0 {
		in = append(in, holding)
	}

	return in
}

// splitBilled shares billed amounts, rounded to currency, and their quantities by weight. Utilized and overage follow
// usage, keeping signed SUM corrections; a true-up is no usage, so it follows positive weights only.
func splitBilled(
	billed *types.CommitmentInfo,
	weights []decimal.Decimal,
	aggregation types.AggregationType,
	currency string,
) []types.CommitmentInfo {
	utilized := roundShares(allocateUsageCost(billed.ComputedCommitmentUtilizedAmount, weights, aggregation), currency)
	overage := roundShares(allocateUsageCost(billed.ComputedOverageAmount, weights, aggregation), currency)
	trueUp := roundShares(allocateCostByWeight(billed.ComputedTrueUpAmount, weights), currency)
	utilizedQuantity := allocateUsageCost(billed.ComputedCommitmentUtilizedQuantity, weights, aggregation)
	overageQuantity := allocateUsageCost(billed.ComputedOverageQuantity, weights, aggregation)
	trueUpQuantity := allocateCostByWeight(billed.ComputedTrueUpQuantity, weights)
	shares := make([]types.CommitmentInfo, len(weights))
	for i := range shares {
		shares[i] = types.CommitmentInfo{
			ComputedCommitmentUtilizedAmount:   utilized[i],
			ComputedOverageAmount:              overage[i],
			ComputedTrueUpAmount:               trueUp[i],
			ComputedCommitmentUtilizedQuantity: utilizedQuantity[i],
			ComputedOverageQuantity:            overageQuantity[i],
			ComputedTrueUpQuantity:             trueUpQuantity[i],
		}
	}

	return shares
}

// roundShares rounds shares to the currency on their running total, so each moves by less than one unit and together
// they keep their rounded sum.
func roundShares(shares []decimal.Decimal, currency string) []decimal.Decimal {
	running, given := decimal.Zero, decimal.Zero
	for i, share := range shares {
		running = running.Add(share)
		upTo := types.RoundToCurrencyPrecision(running, currency)
		shares[i], given = upTo.Sub(given), upTo
	}

	return shares
}

// allocateCostByWeight uses positive weights, or equal shares when none are positive.
func allocateCostByWeight(total decimal.Decimal, weights []decimal.Decimal) []decimal.Decimal {
	if !lo.SomeBy(weights, decimal.Decimal.IsPositive) {
		weights = lo.Map(weights, func(decimal.Decimal, int) decimal.Decimal { return decimal.NewFromInt(1) })
	}

	return spreadCost(weights, total)
}

// allocateUsageCost preserves signed SUM corrections and assigns the rounding residual to the last nonzero weight.
func allocateUsageCost(total decimal.Decimal, weights []decimal.Decimal, aggregation types.AggregationType) []decimal.Decimal {
	if aggregation != types.AggregationSum || !lo.SomeBy(weights, decimal.Decimal.IsNegative) {
		return allocateCostByWeight(total, weights)
	}

	totalUsage := decimal.Zero
	lastNonzero := -1
	for i, weight := range weights {
		totalUsage = totalUsage.Add(weight)
		if !weight.IsZero() {
			lastNonzero = i
		}
	}
	if totalUsage.IsZero() {
		return allocateCostByWeight(total, weights)
	}

	shares := make([]decimal.Decimal, len(weights))
	remaining := total
	for i, weight := range weights {
		shares[i] = total.Mul(weight).Div(totalUsage)
		remaining = remaining.Sub(shares[i])
	}
	shares[lastNonzero] = shares[lastNonzero].Add(remaining)
	return shares
}

// calculateLineItemTotals prices each line as of now before its cost is allocated to groups and display points.
// Filtered queries receive a share of the unfiltered usage charge and exclude commitments.
func (s *meterUsageService) calculateLineItemTotals(
	ctx context.Context,
	usage *SubscriptionMeterUsage,
	params *events.MeterUsageDetailedAnalyticsParams,
	billingAnchor *time.Time,
	end, now time.Time,
) (map[string]decimal.Decimal, map[string][]billedPeriod, error) {
	hasUsageFilters := len(params.PropertyFilters) > 0 || len(params.Sources) > 0
	selectedCharges, err := s.collectLineItemCharges(ctx, usage, params, billingAnchor, end)
	if err != nil {
		return nil, nil, err
	}

	var billedByLine map[string][]billedPeriod
	if !hasUsageFilters {
		billedByLine, err = s.calculateLineItemCommitments(ctx, usage, selectedCharges, params, billingAnchor, end, now)
		if err != nil {
			return nil, nil, err
		}
	}

	costByLine := make(map[string]decimal.Decimal, len(selectedCharges))
	for lineID, periods := range billedByLine {
		for _, bp := range periods {
			costByLine[lineID] = costByLine[lineID].Add(bp.billed.ComputedCommitmentUtilizedAmount).
				Add(bp.billed.ComputedOverageAmount).
				Add(bp.billed.ComputedTrueUpAmount)
		}
	}
	if !hasUsageFilters && !hasExtraGroupBy(params.GroupBy) {
		return costByLine, billedByLine, nil
	}

	fullCharges := selectedCharges
	if hasUsageFilters {
		selectedLines := lo.Filter(usage.Subscription.LineItems, func(item *subscription.SubscriptionLineItem, _ int) bool {
			return selectedCharges[item.ID] != nil
		})
		unfilteredParams := *params
		unfilteredParams.PropertyFilters = nil
		unfilteredParams.Sources = nil
		fullCharges, err = s.fetchLineItemCharges(ctx, usage, selectedLines, &unfilteredParams, billingAnchor, end)
		if err != nil {
			return nil, nil, err
		}
	}

	preserveLegacyCommitments := !hasUsageFilters && (!hasUserBucketedGroupBy(params.GroupBy) || params.ForceApplyCommitment)
	priceService := NewPriceService(s.ServiceParams)
	for _, item := range usage.Subscription.LineItems {
		selectedCharge := selectedCharges[item.ID]
		if selectedCharge == nil || billedByLine[item.ID] != nil || (preserveLegacyCommitments && item.HasAnyCommitment()) {
			continue
		}

		fullCharge := fullCharges[item.ID]
		fullQuantity := fullCharge.QuantityDecimal()
		// A net-zero line has no blended rate; retain its direct group and bucket costs.
		if fullQuantity.IsZero() {
			continue
		}

		lineCost := chargeCost(ctx, priceService, fullCharge, usage.MeterMap[item.MeterID])

		if hasUsageFilters {
			// Amortize at the full line's rate: filtered cost = full cost × selected quantity / full quantity.
			lineCost = lineCost.Mul(selectedCharge.QuantityDecimal()).Div(fullQuantity)
		}
		costByLine[item.ID] = lineCost
	}

	return costByLine, billedByLine, nil
}

// collectLineItemCharges reuses selected usage, querying only totals that cannot be recovered from its groups.
func (s *meterUsageService) collectLineItemCharges(
	ctx context.Context,
	usage *SubscriptionMeterUsage,
	params *events.MeterUsageDetailedAnalyticsParams,
	billingAnchor *time.Time,
	end time.Time,
) (map[string]*dto.SubscriptionUsageByMetersResponse, error) {
	groupsByLine := lo.GroupBy(
		lo.Filter(usage.LineItemUsages, func(group *LineItemMeterUsage, _ int) bool {
			return group.LineItem != nil && group.Price != nil && group.Meter != nil
		}),
		func(group *LineItemMeterUsage) string { return group.LineItem.ID },
	)

	chargesByLine := make(map[string]*dto.SubscriptionUsageByMetersResponse, len(groupsByLine))
	var linesToFetch []*subscription.SubscriptionLineItem
	for lineID, groups := range groupsByLine {
		linePrice := groups[0].Price
		lineMeter := groups[0].Meter
		charge := &dto.SubscriptionUsageByMetersResponse{
			SubscriptionLineItemID: lineID,
			MeterID:                lineMeter.ID,
			Price:                  linePrice,
			Currency:               linePrice.Currency,
		}

		switch {
		case price.IsBucketedMax(linePrice, lineMeter) || price.IsBucketedSum(linePrice, lineMeter):
			if groups[0].BucketedResult != nil {
				charge.BucketedUsageResult = groups[0].BucketedResult
				charge.SetQuantityDecimal(charge.BucketedUsageResult.Value)
			} else if lo.SomeBy(groups, func(group *LineItemMeterUsage) bool { return group.EventCount > 0 || !group.Usage.IsZero() }) {
				linesToFetch = append(linesToFetch, groups[0].LineItem)
				continue
			} else {
				charge.BucketedUsageResult = &events.AggregationResult{}
			}
		case len(groups) > 1 && !lineMeter.Aggregation.Type.Additive():
			linesToFetch = append(linesToFetch, groups[0].LineItem)
			continue
		default:
			quantity := decimal.Zero
			for _, group := range groups {
				quantity = quantity.Add(group.Usage)
			}
			charge.SetQuantityDecimal(quantity)
		}
		chargesByLine[lineID] = charge
	}

	fetchedCharges, err := s.fetchLineItemCharges(ctx, usage, linesToFetch, params, billingAnchor, end)
	if err != nil {
		return nil, err
	}
	maps.Copy(chargesByLine, fetchedCharges)
	return chargesByLine, nil
}

// fetchLineItemCharges batches reads with matching periods and billing aggregation settings.
// Separate analytics reads are eventually consistent while events are being ingested.
func (s *meterUsageService) fetchLineItemCharges(
	ctx context.Context,
	usage *SubscriptionMeterUsage,
	lineItems []*subscription.SubscriptionLineItem,
	params *events.MeterUsageDetailedAnalyticsParams,
	billingAnchor *time.Time,
	end time.Time,
) (map[string]*dto.SubscriptionUsageByMetersResponse, error) {
	queryBatches := make(map[usageQueryGroup][]*subscription.SubscriptionLineItem)
	for _, item := range lineItems {
		lineMeter := usage.MeterMap[item.MeterID]
		linePrice := usage.PriceMap[item.PriceID]
		batchKey := usageQueryGroup{
			Start:   item.GetPeriodStart(params.StartTime),
			End:     item.GetPeriodEnd(end),
			AggType: lineMeter.Aggregation.Type,
		}
		if price.IsBucketedMax(linePrice, lineMeter) || price.IsBucketedSum(linePrice, lineMeter) {
			batchKey.BillingBucketSize = price.ResolveBucketSize(linePrice, lineMeter)
			batchKey.BillingGroupBy = price.BucketedGroupBy(linePrice, lineMeter)
		}
		queryBatches[batchKey] = append(queryBatches[batchKey], item)
	}

	chargesByLine := make(map[string]*dto.SubscriptionUsageByMetersResponse, len(lineItems))
	for batchKey, batch := range queryBatches {
		meterIDs := lo.Uniq(lo.Map(batch, func(item *subscription.SubscriptionLineItem, _ int) string { return item.MeterID }))
		query := &events.MeterUsageQueryParams{
			TenantID:            types.GetTenantID(ctx),
			EnvironmentID:       types.GetEnvironmentID(ctx),
			ExternalCustomerIDs: usage.ExternalCustomerIDs,
			MeterIDs:            meterIDs,
			StartTime:           batchKey.Start,
			EndTime:             batchKey.End,
			AggregationType:     batchKey.AggType,
			WindowSize:          batchKey.BillingBucketSize,
			BillingAnchor:       billingAnchor,
			UseFinal:            params.UseFinal,
			PropertyFilters:     params.PropertyFilters,
			Sources:             params.Sources,
		}
		if batchKey.BillingGroupBy != "" {
			query.GroupBy = []string{"properties." + batchKey.BillingGroupBy}
		}
		results, err := s.repo.GetUsageMultiMeter(ctx, query)
		if err != nil {
			return nil, err
		}

		usageByMeter := lo.KeyBy(results, func(result *events.MeterUsageAggregationResult) string { return result.MeterID })
		for _, item := range batch {
			meterUsage := usageByMeter[item.MeterID]
			if meterUsage == nil {
				meterUsage = &events.MeterUsageAggregationResult{}
			}
			linePrice := usage.PriceMap[item.PriceID]
			charge := &dto.SubscriptionUsageByMetersResponse{
				SubscriptionLineItemID: item.ID,
				MeterID:                item.MeterID,
				Price:                  linePrice,
				Currency:               linePrice.Currency,
			}
			charge.SetQuantityDecimal(meterUsage.TotalValue)
			if batchKey.BillingBucketSize != "" {
				charge.BucketedUsageResult = &events.AggregationResult{
					Type:       batchKey.AggType,
					MeterID:    item.MeterID,
					Value:      meterUsage.TotalValue,
					EventCount: meterUsage.EventCount,
					Results: lo.Map(meterUsage.Points, func(point events.MeterUsageResult, _ int) events.UsageResult {
						return events.UsageResult{
							WindowSize: point.WindowStart,
							Value:      point.Value,
							EventCount: point.EventCount,
						}
					}),
				}
			}
			chargesByLine[item.ID] = charge
		}
	}
	return chargesByLine, nil
}

// maxCommitmentPeriods bounds the billing periods of a range a v2 commitment overlaps, each priced on its own.
const maxCommitmentPeriods = 366

// billedPeriod is what one billing period bills a line v2 commitments cover in the range.
type billedPeriod struct {
	period types.Period
	billed *types.CommitmentInfo
}

// calculateLineItemCommitments returns what each billing period of [params.StartTime, end) bills, as of now, the queried
// lines v2 commitments cover, at the line's own price where none does.
func (s *meterUsageService) calculateLineItemCommitments(
	ctx context.Context,
	usage *SubscriptionMeterUsage,
	selectedCharges map[string]*dto.SubscriptionUsageByMetersResponse,
	params *events.MeterUsageDetailedAnalyticsParams,
	billingAnchor *time.Time,
	end, now time.Time,
) (map[string][]billedPeriod, error) {
	// Only a range some v2 commitment overlaps is priced period by period, so a long range without one stays valid.
	svc := newCommitmentService(s.ServiceParams)
	enabled, err := svc.IsEnabled(ctx)
	if err != nil || !enabled {
		return nil, err
	}

	// The open period's loader prices through the period end, so the lookup reaches it too.
	sub := usage.Subscription
	lookupEnd := projectedPeriodEnd(sub, params.StartTime, end, now)
	overlapping, err := svc.ListOverlapping(ctx, sub.ID, params.StartTime, lookupEnd)
	if err != nil || len(overlapping) == 0 {
		return nil, err
	}

	periods, err := billingPeriodsIn(sub, types.Period{Start: params.StartTime, End: end}, now)
	if err != nil {
		return nil, err
	}

	breakdownsByPeriod := make([]map[string]*types.CommitmentInfo, len(periods))
	covered := map[string]bool{}
	for i, period := range periods {
		breakdowns, err := s.periodCommitments(ctx, usage, selectedCharges, params, billingAnchor, period, end, now)
		if err != nil {
			return nil, err
		}

		breakdownsByPeriod[i] = breakdowns
		for lineID := range breakdowns {
			covered[lineID] = true
		}
	}

	// A covered line's periods without a commitment bill at its own price, as those periods' invoices do.
	priceService := NewPriceService(s.ServiceParams)
	for i, period := range periods {
		var uncovered []*subscription.SubscriptionLineItem
		for _, item := range usage.Subscription.LineItems {
			runs := item.GetPeriodStart(period.Start).Before(item.GetPeriodEnd(period.End))
			if covered[item.ID] && breakdownsByPeriod[i][item.ID] == nil && runs {
				uncovered = append(uncovered, item)
			}
		}
		if len(uncovered) == 0 {
			continue
		}

		periodParams := *params
		periodParams.StartTime = period.Start
		charges, err := s.fetchLineItemCharges(ctx, usage, uncovered, &periodParams, billingAnchor, period.End)
		if err != nil {
			return nil, err
		}

		if err := s.applyRunningTotals(ctx, usage, charges, period.End); err != nil {
			return nil, err
		}

		if breakdownsByPeriod[i] == nil {
			breakdownsByPeriod[i] = map[string]*types.CommitmentInfo{}
		}
		for lineID, charge := range charges {
			cost := chargeCost(ctx, priceService, charge, usage.MeterMap[charge.MeterID])
			breakdownsByPeriod[i][lineID] = &types.CommitmentInfo{
				ComputedOverageAmount:   types.RoundToCurrencyPrecision(cost, charge.Currency),
				ComputedOverageQuantity: charge.QuantityDecimal(),
			}
		}
	}

	byLine := map[string][]billedPeriod{}
	for i, period := range periods {
		for lineID, breakdown := range breakdownsByPeriod[i] {
			byLine[lineID] = append(byLine[lineID], billedPeriod{period: period, billed: breakdown})
		}
	}

	// Periods starting after now bill nothing; an empty period for them keeps their points off the open period's charges.
	if last := len(periods) - 1; last >= 0 && periods[last].End.Before(end) {
		unbilled := billedPeriod{period: types.Period{Start: periods[last].End, End: end}, billed: &types.CommitmentInfo{}}
		for lineID := range byLine {
			byLine[lineID] = append(byLine[lineID], unbilled)
		}
	}

	return byLine, nil
}

// applyRunningTotals sets each charge of a running SUM meter to the meter's total at its line's end in the period, as
// invoices bill it; a window read holds only the period's changes.
func (s *meterUsageService) applyRunningTotals(
	ctx context.Context,
	usage *SubscriptionMeterUsage,
	charges map[string]*dto.SubscriptionUsageByMetersResponse,
	end time.Time,
) error {
	for _, item := range usage.Subscription.LineItems {
		charge := charges[item.ID]
		if charge == nil || charge.BucketedUsageResult != nil || !keepsRunningAggregate(usage.MeterMap[item.MeterID]) {
			continue
		}

		total, err := s.GetCheckpointedUsage(ctx, CheckpointedUsageRequest{
			CustomerID:          usage.Subscription.CustomerID,
			ExternalCustomerIDs: usage.ExternalCustomerIDs,
			MeterID:             item.MeterID,
			AggregationType:     types.AggregationSum,
			Until:               item.GetPeriodEnd(end),
			IgnoreCheckpoints:   !usesCheckpoints(usage.Subscription),
		})
		if err != nil {
			return err
		}

		charge.SetQuantityDecimal(total)
	}

	return nil
}

// billingPeriodsIn cuts span at the subscription's billing period edges, from its start up to now: nothing bills
// before the one or after the other.
func billingPeriodsIn(sub *subscription.Subscription, span types.Period, now time.Time) ([]types.Period, error) {
	var periods []types.Period
	for at := maxTime(span.Start, sub.StartDate); at.Before(span.End) && !at.After(now); {
		period, err := periodAt(sub, at)
		if err != nil {
			return nil, err
		}

		if !period.End.After(at) {
			break
		}

		if len(periods) == maxCommitmentPeriods {
			return nil, ierr.NewErrorf("the range spans more than %d billing periods with commitments", maxCommitmentPeriods).
				WithHint("Narrow start_time and end_time to fewer billing periods").
				WithReportableDetails(map[string]any{"subscription_id": sub.ID, "max_periods": maxCommitmentPeriods}).
				Mark(ierr.ErrValidation)
		}

		periods = append(periods, types.Period{Start: at, End: minTime(period.End, span.End)})
		at = period.End
	}

	return periods, nil
}

// periodCommitments prices one billing period's part of the range through the billing commitment loader: what its
// commitments bill each queried line they cover.
func (s *meterUsageService) periodCommitments(
	ctx context.Context,
	usage *SubscriptionMeterUsage,
	selectedCharges map[string]*dto.SubscriptionUsageByMetersResponse,
	params *events.MeterUsageDetailedAnalyticsParams,
	billingAnchor *time.Time,
	period types.Period,
	end, now time.Time,
) (map[string]*types.CommitmentInfo, error) {
	// The loader finds the commitments from the subscription's lines, so their usage is added once it knows which.
	commitmentCharges := map[string][]*dto.SubscriptionUsageByMetersResponse{}

	// A range reaching today prices the open period through its end, as its coming invoice bills it.
	loader, err := newBillingService(s.ServiceParams).newProjectedCommitmentLoader(
		ctx, usage.Subscription, period.Start, period.End, now, commitmentCharges, usage.MeterMap, types.UsageSourceAnalytics,
	)
	if err != nil || len(loader.commitments) == 0 {
		return nil, err
	}

	queried := make(map[string]bool, len(selectedCharges))
	for lineID := range selectedCharges {
		queried[lineID] = true
	}

	if len(loader.CommitmentsCovering(queried)) == 0 {
		return nil, nil
	}

	// The lines read include those a meter filter left out. A period holding the whole range reuses the queried usage,
	// except a running meter's, which bills its total.
	affecting := loader.LinesAffecting(queried)
	wholeRange := period.Start.Equal(params.StartTime) && period.End.Equal(end)
	var linesToRead []*subscription.SubscriptionLineItem
	for _, item := range usage.Subscription.LineItems {
		if !affecting[item.ID] || !item.GetPeriodStart(period.Start).Before(item.GetPeriodEnd(period.End)) {
			continue
		}

		if wholeRange && queried[item.ID] && !keepsRunningAggregate(usage.MeterMap[item.MeterID]) {
			commitmentCharges[item.ID] = []*dto.SubscriptionUsageByMetersResponse{selectedCharges[item.ID]}
		} else if usage.PriceMap[item.PriceID] != nil && usage.MeterMap[item.MeterID] != nil {
			linesToRead = append(linesToRead, item)
		}
	}

	periodParams := *params
	periodParams.StartTime = period.Start
	readCharges, err := s.fetchLineItemCharges(ctx, usage, linesToRead, &periodParams, billingAnchor, period.End)
	if err != nil {
		return nil, err
	}

	if err := s.applyRunningTotals(ctx, usage, readCharges, period.End); err != nil {
		return nil, err
	}
	for lineID, charge := range readCharges {
		commitmentCharges[lineID] = []*dto.SubscriptionUsageByMetersResponse{charge}
	}

	lines, _, err := loader.BillingLines(ctx)
	if err != nil {
		return nil, err
	}

	breakdownByLine := map[string]*types.CommitmentInfo{}
	for _, sc := range loader.commitments {
		for _, item := range loader.scopeByCommitment[sc.ID].lineItems {
			lineBreakdown := breakdownByLine[item.ID]
			if lineBreakdown == nil {
				lineBreakdown = &types.CommitmentInfo{
					Type:       sc.Type,
					IsWindowed: sc.Measurement == types.CommitmentMeasurementWindow,
				}
				breakdownByLine[item.ID] = lineBreakdown
			}

			// A billing-terms commitment takes no true-up: its instalments bill its value.
			takesTrueUp := sc.Binding && lo.FromPtr(sc.CollectionMethod) != types.CommitmentCollectionBillingTerms
			lineBreakdown.TrueUpEnabled = lineBreakdown.TrueUpEnabled || takesTrueUp
		}
	}

	utilizedByCommitment := map[string]map[string]decimal.Decimal{}
	var trueUps []dto.CreateInvoiceLineItemRequest
	for _, line := range lines {
		kind := types.CommitmentLineKind(line.Metadata[types.MetadataKeyCommitmentLineKind])
		if kind == types.CommitmentLineTrueUp {
			trueUps = append(trueUps, line)
			continue
		}

		lineID := lo.FromPtr(line.SubscriptionLineItemID)
		lineBreakdown := breakdownByLine[lineID]
		if lineBreakdown == nil {
			continue
		}

		// Overage and plain usage both bill outside any commitment's capacity.
		if kind == types.CommitmentLineOverage || kind == types.CommitmentLinePlain {
			lineBreakdown.ComputedOverageAmount = lineBreakdown.ComputedOverageAmount.Add(line.Amount)
			lineBreakdown.ComputedOverageQuantity = lineBreakdown.ComputedOverageQuantity.Add(line.Quantity)
			continue
		}

		commitmentID := lo.FromPtr(line.CommitmentID)
		if utilizedByCommitment[commitmentID] == nil {
			utilizedByCommitment[commitmentID] = map[string]decimal.Decimal{}
		}

		utilizedByCommitment[commitmentID][lineID] = utilizedByCommitment[commitmentID][lineID].Add(line.Amount)
		lineBreakdown.ComputedCommitmentUtilizedAmount = lineBreakdown.ComputedCommitmentUtilizedAmount.Add(line.Amount)
		// Usage a billing-terms commitment covers bills nothing, but its quantity still counts as utilized.
		lineBreakdown.ComputedCommitmentUtilizedQuantity = lineBreakdown.ComputedCommitmentUtilizedQuantity.Add(line.Quantity)
	}

	// A shared true-up is split across the commitment's whole scope by utilized value.
	for _, line := range trueUps {
		commitmentID := lo.FromPtr(line.CommitmentID)
		scope := loader.scopeByCommitment[commitmentID].lineItems
		utilized := make([]decimal.Decimal, len(scope))
		for i, item := range scope {
			utilized[i] = utilizedByCommitment[commitmentID][item.ID]
		}

		// An amount commitment's true-up row bills one unit of money, not unused usage, so only a quantity one counts.
		unused := decimal.Zero
		if lo.ContainsBy(loader.commitments, func(sc *stackedCommitment) bool {
			return sc.ID == commitmentID && sc.Type == types.COMMITMENT_TYPE_QUANTITY
		}) {
			unused = line.Quantity
		}

		amounts := roundShares(allocateCostByWeight(line.Amount, utilized), usage.Subscription.Currency)
		quantities := allocateCostByWeight(unused, utilized)
		for i, item := range scope {
			lineBreakdown := breakdownByLine[item.ID]
			lineBreakdown.ComputedTrueUpAmount = lineBreakdown.ComputedTrueUpAmount.Add(amounts[i])
			lineBreakdown.ComputedTrueUpQuantity = lineBreakdown.ComputedTrueUpQuantity.Add(quantities[i])
		}
	}

	for lineID := range breakdownByLine {
		if !queried[lineID] {
			delete(breakdownByLine, lineID)
		}
	}

	return breakdownByLine, nil
}

// chargeCost prices a line's usage the way billing prices it: bucketed usage bucket by bucket, otherwise in one go.
func chargeCost(ctx context.Context, prices PriceService, charge *dto.SubscriptionUsageByMetersResponse, m *meter.Meter) decimal.Decimal {
	if charge.BucketedUsageResult == nil {
		return prices.CalculateCost(ctx, charge.Price, charge.QuantityDecimal())
	}

	hasBillingGroups := price.BucketedGroupBy(charge.Price, m) != ""
	return calculateBucketedMeterCost(ctx, prices, charge.Price, charge.BucketedUsageResult, hasBillingGroups).Amount
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
	params := &meterUsageBucketedCostParams{ctx, priceService, item, p, data, m.Aggregation.Type, price.ResolveBucketSize(p, m)}
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
	anchor := p.data.windowAnchor(p.item.SubscriptionID)
	p.item.Points = s.mergeBucketPointsByWindow(p.item.Points, p.aggType, p.data.Params.WindowSize, anchor)

	return cost
}

// effectivePointWindowMinutes returns the span (in minutes) a displayed point
// actually covers: the request window, or the meter's bucket size when that is
// coarser. mergeBucketPointsByWindow cannot subdivide below the meter bucket, so a
// request window finer than the bucket leaves points at meter-bucket grain — the
// overlap check must use that coarser span, not the (smaller) request window.
func effectivePointWindowMinutes(requestWindow types.WindowSize, p *price.Price, m *meter.Meter) int {
	minutes := requestWindow.ToMinutes()
	if bs := price.ResolveBucketSize(p, m); bs != "" {
		if bm := bs.ToMinutes(); bm > minutes {
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
		anchor := p.data.windowAnchor(p.item.SubscriptionID)
		p.item.Points = s.mergeBucketPointsByWindow(p.item.Points, p.aggType, p.data.Params.WindowSize, anchor)
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

	// Linear charges retain corrections even when positive and negative usage cancel out.
	if m.Aggregation.Type == types.AggregationSum && p.BillingModel == types.BILLING_MODEL_FLAT_FEE && item.TotalUsage.IsZero() && cost.IsZero() {
		s.stampPointCosts(ctx, priceService, item, p)
		return
	}

	// Display windows share the aggregate charge; pricing each point separately would restart tiers and packages.
	weights := lo.Map(item.Points, func(point events.UsageAnalyticPoint, _ int) decimal.Decimal { return point.Usage })
	for i, share := range allocateUsageCost(item.TotalCost, weights, m.Aggregation.Type) {
		item.Points[i].Cost = share
	}
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
			merged.BilledUsage = merged.BilledUsage.Add(bucket.Usage)
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

// GetGroupablePropertyKeys drops the meters' aggregation keys, which users can't group by.
func (s *meterUsageService) GetGroupablePropertyKeys(ctx context.Context, params *events.PropertyLookupParams) ([]string, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	meterIDs, err := resolvePropertyLookupMeters(ctx, s.ServiceParams, params)
	if err != nil {
		return nil, err
	}
	params.MeterIDs = meterIDs
	if len(params.MeterIDs) == 0 {
		return []string{}, nil
	}

	propertyKeys, err := s.repo.GetPropertyKeys(ctx, params)
	if err != nil {
		return nil, err
	}
	aggregationKeys, err := aggregationPropertyKeys(ctx, s.MeterRepo, params.MeterIDs)
	if err != nil {
		return nil, err
	}

	// Keys outside the "properties.<key>" pattern can't be grouped or filtered, so they aren't offered.
	return lo.Filter(propertyKeys, func(key string, _ int) bool {
		_, ok := aggregationKeys[key]
		return !ok && analyticsGroupByPropertyName.MatchString(key)
	}), nil
}

// GetPropertyValues defaults a missing or negative limit to defaultPropertyValuesLimit.
func (s *meterUsageService) GetPropertyValues(ctx context.Context, params *events.PropertyLookupParams) ([]string, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}
	if !analyticsGroupByPropertyName.MatchString(params.Key) {
		return nil, ierr.NewError("invalid property key").
			WithHintf("property key %q is not valid", params.Key).
			Mark(ierr.ErrValidation)
	}
	if params.Limit <= 0 {
		params.Limit = defaultPropertyValuesLimit
	}

	meterIDs, err := resolvePropertyLookupMeters(ctx, s.ServiceParams, params)
	if err != nil {
		return nil, err
	}
	params.MeterIDs = meterIDs
	if len(params.MeterIDs) == 0 {
		return []string{}, nil
	}

	values, err := s.repo.GetPropertyValues(ctx, params)
	if err != nil {
		return nil, err
	}

	return lo.Ternary(values == nil, []string{}, values), nil
}

// resolvePropertyLookupMeters returns the features' meters, or the meters with usage in the window.
func resolvePropertyLookupMeters(ctx context.Context, sp ServiceParams, params *events.PropertyLookupParams) ([]string, error) {
	if len(params.FeatureIDs) > 0 {
		return featureMeterIDs(ctx, sp.FeatureRepo, params.FeatureIDs)
	}
	return sp.MeterUsageRepo.GetDistinctMeterIDs(ctx, &events.MeterUsageQueryParams{
		TenantID:           params.TenantID,
		EnvironmentID:      params.EnvironmentID,
		ExternalCustomerID: params.ExternalCustomerID,
		StartTime:          params.StartTime,
		EndTime:            params.EndTime,
	})
}

// featureMeterIDs returns the distinct meter IDs of the given features.
func featureMeterIDs(ctx context.Context, featureRepo feature.Repository, featureIDs []string) ([]string, error) {
	features, err := featureRepo.ListByIDs(ctx, featureIDs)
	if err != nil {
		return nil, err
	}
	return lo.Uniq(lo.FilterMap(features, func(f *feature.Feature, _ int) (string, bool) { return f.MeterID, f.MeterID != "" })), nil
}

// aggregationPropertyKeys returns the property keys meters aggregate on (the field or expression
// identifiers), across every meter sharing the given meters' event names.
func aggregationPropertyKeys(ctx context.Context, meterRepo meter.Repository, meterIDs []string) (map[string]struct{}, error) {
	meters, err := meterRepo.ListByIDs(ctx, meterIDs)
	if err != nil {
		return nil, err
	}

	keys := make(map[string]struct{})
	evaluator := expression.NewCELEvaluator()
	for _, eventName := range lo.Uniq(lo.Map(meters, func(m *meter.Meter, _ int) string { return m.EventName })) {
		matching, err := meterRepo.GetMatchingMetersByEventName(ctx, eventName)
		if err != nil {
			return nil, err
		}

		for _, m := range matching {
			if m.Aggregation.Expression == "" {
				if m.Aggregation.Field != "" && m.Aggregation.Type != types.AggregationCount && m.Aggregation.Type != types.AggregationCountUnique {
					keys[m.Aggregation.Field] = struct{}{}
				}
				continue
			}

			ids, err := evaluator.Identifiers(m.Aggregation.Expression)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				keys[id] = struct{}{}
			}
		}
	}

	return keys, nil
}
