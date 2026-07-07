package testutil

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type InMemoryFeatureUsageStore struct {
	mu    sync.RWMutex
	usage map[string]*events.FeatureUsage
}

func NewInMemoryFeatureUsageStore() *InMemoryFeatureUsageStore {
	return &InMemoryFeatureUsageStore{
		usage: make(map[string]*events.FeatureUsage),
	}
}

func (s *InMemoryFeatureUsageStore) Create(ctx context.Context, featureUsage *events.FeatureUsage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usage[featureUsage.ID] = featureUsage
	return nil
}

func (s *InMemoryFeatureUsageStore) Get(ctx context.Context, id string) (*events.FeatureUsage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	usage, exists := s.usage[id]
	if !exists {
		return nil, errors.New("feature usage not found")
	}
	return usage, nil
}

func (s *InMemoryFeatureUsageStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usage = make(map[string]*events.FeatureUsage)
}

// InsertProcessedEvent inserts a single processed event
func (s *InMemoryFeatureUsageStore) InsertProcessedEvent(ctx context.Context, event *events.FeatureUsage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usage[event.ID] = event
	return nil
}

// BulkInsertProcessedEvents bulk inserts processed events
func (s *InMemoryFeatureUsageStore) BulkInsertProcessedEvents(ctx context.Context, events []*events.FeatureUsage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, event := range events {
		s.usage[event.ID] = event
	}
	return nil
}

// GetProcessedEvents gets processed events with filtering
func (s *InMemoryFeatureUsageStore) GetProcessedEvents(ctx context.Context, params *events.GetProcessedEventsParams) ([]*events.FeatureUsage, uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*events.FeatureUsage, 0)
	for _, usage := range s.usage {
		result = append(result, usage)
	}
	return result, uint64(len(result)), nil
}

// IsDuplicate checks for duplicate events
func (s *InMemoryFeatureUsageStore) IsDuplicate(ctx context.Context, subscriptionID, meterID string, periodID uint64, uniqueHash string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, usage := range s.usage {
		if usage.UniqueHash == uniqueHash {
			return true, nil
		}
	}
	return false, nil
}

// GetDetailedUsageAnalytics provides usage analytics
func (s *InMemoryFeatureUsageStore) GetDetailedUsageAnalytics(ctx context.Context, params *events.UsageAnalyticsParams, maxBucketFeatures map[string]*events.MaxBucketFeatureInfo, sumBucketFeatures map[string]*events.SumBucketFeatureInfo) ([]*events.DetailedUsageAnalytic, error) {
	return []*events.DetailedUsageAnalytic{}, nil
}

// GetFeatureUsageBySubscription gets feature usage by subscription.
// params.Opts is ignored (in-memory has no FINAL concept).
func (s *InMemoryFeatureUsageStore) GetFeatureUsageBySubscription(ctx context.Context, params *events.GetFeatureUsageBySubscriptionParams) (map[string]*events.UsageByFeatureResult, error) {

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*events.UsageByFeatureResult)
	for _, usage := range s.usage {
		if usage.SubscriptionID != params.SubscriptionID {
			continue
		}
		if len(params.CustomerIDs) > 0 && !lo.Contains(params.CustomerIDs, usage.CustomerID) {
			continue
		}

		// For count aggregation, subscription service uses CountDistinctIDs.
		// Use QtyTotal as count when it's a whole number (typical for count meters).
		countDistinctIDs := uint64(0)
		if usage.QtyTotal.IsInteger() {
			countDistinctIDs = uint64(usage.QtyTotal.IntPart())
		}
		result[usage.SubLineItemID] = &events.UsageByFeatureResult{
			SubLineItemID:    usage.SubLineItemID,
			FeatureID:        usage.FeatureID,
			MeterID:          usage.MeterID,
			PriceID:          usage.PriceID,
			SumTotal:         usage.QtyTotal,
			CountDistinctIDs: countDistinctIDs,
		}
	}
	return result, nil
}

// GetFeatureUsageForExport gets feature usage for export
func (s *InMemoryFeatureUsageStore) GetFeatureUsageForExport(ctx context.Context, startTime, endTime time.Time, batchSize int, offset int) ([]*events.FeatureUsage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*events.FeatureUsage, 0)
	count := 0
	for _, usage := range s.usage {
		if count >= offset && count < offset+batchSize {
			result = append(result, usage)
		}
		count++
	}
	return result, nil
}

// GetUsageForBucketedMeters mirrors the ClickHouse bucketed feature_usage query
// (internal/repository/clickhouse/feature_usage.go:2275-2356 and its SQL builder
// getWindowedQuery at feature_usage.go:2358-2501):
//
//   - Rows are filtered by tenant_id + environment_id, sign != 0, external
//     customer IDs, customer_id, feature_id, price_id, meter_id,
//     sub_line_item_id, property filters, and timestamp >= StartTime AND
//     timestamp < EndTime (buildTimeConditions, aggregators.go:243-259 — the
//     end bound is exclusive).
//   - Rows are bucketed by params.UsageParams.WindowSize (with billing-anchor
//     shifted months), mirroring formatWindowSizeWithBillingAnchor
//     (aggregators.go:132-201).
//   - Per bucket the reducer is max(qty_total) by default, or sum(qty_total)
//     when AggregationType == SUM (feature_usage.go:2386-2396).
//   - With a valid "properties.X" GroupBy entry, values are computed per
//     (bucket, group) and every row carries its GroupKey; ordering is
//     bucket_start then group_key (feature_usage.go:2408-2453).
//   - result.Value = sum of all per-bucket (per-group) values — the query's
//     "total" column (feature_usage.go:2432, 2478); result.Type is the
//     aggregation type (feature_usage.go:2305).
func (s *InMemoryFeatureUsageStore) GetUsageForBucketedMeters(ctx context.Context, params *events.FeatureUsageParams) (*events.AggregationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	up := params.UsageParams
	result := &events.AggregationResult{
		Type: up.AggregationType,
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	// External customer filter merges ExternalCustomerID and ExternalCustomerIDs
	// (buildUsageEventCustomerFilters, aggregators.go:78-108).
	allCustomerIDs := lo.Uniq(lo.Without(append([]string{}, append(up.ExternalCustomerIDs, up.ExternalCustomerID)...), ""))

	var matched []*events.FeatureUsage
	for _, usage := range s.usage {
		if usage.TenantID != tenantID || usage.EnvironmentID != environmentID {
			continue
		}
		// PREWHERE ... AND sign != 0 (feature_usage.go:2420, 2465): cancelled
		// pairs cancel out via ReplacingMergeTree, but rows with sign 0 never
		// participate. Fixtures must set Sign (production rows are always ±1).
		if usage.Sign == 0 {
			continue
		}
		if len(allCustomerIDs) > 0 && !lo.Contains(allCustomerIDs, usage.ExternalCustomerID) {
			continue
		}
		if up.CustomerID != "" && usage.CustomerID != up.CustomerID {
			continue
		}
		if params.FeatureID != "" && usage.FeatureID != params.FeatureID {
			continue
		}
		if params.PriceID != "" && usage.PriceID != params.PriceID {
			continue
		}
		if params.MeterID != "" && usage.MeterID != params.MeterID {
			continue
		}
		if params.SubLineItemID != "" && usage.SubLineItemID != params.SubLineItemID {
			continue
		}
		// timestamp >= StartTime AND timestamp < EndTime (aggregators.go:243-259).
		if !up.StartTime.IsZero() && usage.Timestamp.Before(up.StartTime) {
			continue
		}
		if !up.EndTime.IsZero() && !usage.Timestamp.Before(up.EndTime) {
			continue
		}
		if !matchesPropertyFilters(usage.Properties, up.Filters) {
			continue
		}
		matched = append(matched, usage)
	}

	// Group-by only applies for a valid "properties.X" first entry
	// (feature_usage.go:2408, validateGroupByProperty at feature_usage.go:25-38).
	groupByProperty := events.FirstGroupByProperty(up.GroupBy)
	hasGroupBy := groupByProperty != "" && inMemoryValidGroupByPropertyPattern.MatchString(groupByProperty)

	type bucketKey struct {
		start    time.Time
		groupKey string
	}
	buckets := make(map[bucketKey][]*events.FeatureUsage)
	for _, usage := range matched {
		key := bucketKey{start: truncateToWindow(usage.Timestamp, up.WindowSize, up.BillingAnchor)}
		if hasGroupBy {
			key.groupKey = propertyValue(usage.Properties, groupByProperty)
		}
		buckets[key] = append(buckets[key], usage)
	}

	keys := make([]bucketKey, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	// ORDER BY bucket_start[, group_key] (feature_usage.go:2437, 2482).
	sort.Slice(keys, func(i, j int) bool {
		if !keys[i].start.Equal(keys[j].start) {
			return keys[i].start.Before(keys[j].start)
		}
		return keys[i].groupKey < keys[j].groupKey
	})

	result.Results = make([]events.UsageResult, 0, len(keys))
	total := decimal.Zero
	for _, k := range keys {
		value := aggregateFeatureUsageQty(buckets[k], up.AggregationType)
		total = total.Add(value)
		result.Results = append(result.Results, events.UsageResult{
			WindowSize: k.start,
			Value:      value,
			GroupKey:   k.groupKey,
		})
	}
	result.Value = total
	return result, nil
}

// aggregateFeatureUsageQty reduces qty_total over a bucket: sum(qty_total) for
// SUM, max(qty_total) otherwise — MAX is the default for backward compatibility
// (feature_usage.go:2386-2396).
func aggregateFeatureUsageQty(rows []*events.FeatureUsage, aggType types.AggregationType) decimal.Decimal {
	if len(rows) == 0 {
		return decimal.Zero
	}
	if aggType == types.AggregationSum {
		sum := decimal.Zero
		for _, r := range rows {
			sum = sum.Add(r.QtyTotal)
		}
		return sum
	}
	max := rows[0].QtyTotal
	for _, r := range rows[1:] {
		if r.QtyTotal.GreaterThan(max) {
			max = r.QtyTotal
		}
	}
	return max
}

// matchesPropertyFilters mirrors buildFilterConditions (aggregators.go:203-231):
// every filter key with values must match the row's property (string compare);
// keys with empty value lists are skipped.
func matchesPropertyFilters(properties map[string]interface{}, filters map[string][]string) bool {
	for key, values := range filters {
		if len(values) == 0 {
			continue
		}
		if !lo.Contains(values, propertyValue(properties, key)) {
			return false
		}
	}
	return true
}

func (s *InMemoryFeatureUsageStore) GetFeatureUsageByEventIDs(ctx context.Context, eventIDs []string) ([]*events.FeatureUsage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*events.FeatureUsage

	return result, nil
}

func (s *InMemoryFeatureUsageStore) DeleteByReprocessScopeBeforeCheckpoint(ctx context.Context, params *events.DeleteFeatureUsageScopeParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, usage := range s.usage {
		if params.GetEventsParams.ExternalCustomerID != "" && usage.ExternalCustomerID != params.GetEventsParams.ExternalCustomerID {
			continue
		}
		if params.GetEventsParams.EventName != "" && usage.EventName != params.GetEventsParams.EventName {
			continue
		}
		if !params.GetEventsParams.StartTime.IsZero() && usage.Timestamp.Before(params.GetEventsParams.StartTime) {
			continue
		}
		if !params.GetEventsParams.EndTime.IsZero() && usage.Timestamp.After(params.GetEventsParams.EndTime) {
			continue
		}
		if usage.ProcessedAt.IsZero() || !usage.ProcessedAt.Before(params.RunStartTime) {
			continue
		}

		delete(s.usage, id)
	}

	return nil
}
