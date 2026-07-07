package testutil

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type InMemoryEventStore struct {
	mu     sync.RWMutex
	events map[string]*events.Event
}

func NewInMemoryEventStore() *InMemoryEventStore {
	return &InMemoryEventStore{
		events: make(map[string]*events.Event),
	}
}

func (s *InMemoryEventStore) InsertEvent(ctx context.Context, event *events.Event) error {
	if event == nil {
		return ierr.NewError("event cannot be nil").
			WithHint("Event cannot be nil").
			Mark(ierr.ErrValidation)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[event.ID] = event
	return nil
}

func (s *InMemoryEventStore) BulkInsertEvents(ctx context.Context, events []*events.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, event := range events {
		s.events[event.ID] = event
	}
	return nil
}

func (s *InMemoryEventStore) GetEventByID(ctx context.Context, eventID string) (*events.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	event, exists := s.events[eventID]
	if !exists {
		return nil, ierr.NewError("event not found").
			WithHint("Event with the specified ID does not exist").
			Mark(ierr.ErrNotFound)
	}

	// Check tenant ID and environment ID from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	if event.TenantID != tenantID {
		return nil, ierr.NewError("event not found").
			WithHint("Event with the specified ID does not exist for this tenant").
			Mark(ierr.ErrNotFound)
	}

	if event.EnvironmentID != environmentID {
		return nil, ierr.NewError("event not found").
			WithHint("Event with the specified ID does not exist for this environment").
			Mark(ierr.ErrNotFound)
	}

	return event, nil
}

func (s *InMemoryEventStore) GetUsage(ctx context.Context, params *events.UsageParams) (*events.AggregationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filteredEvents []*events.Event

	// Build a deduplicated union of both customer ID sources once, outside the loop.
	allCustomerIDs := lo.Uniq(lo.Without(append(params.ExternalCustomerIDs, params.ExternalCustomerID), ""))

	// Filter events based on basic criteria
	for _, event := range s.events {
		if event.EventName != params.EventName {
			continue
		}

		if len(allCustomerIDs) > 0 && !lo.Contains(allCustomerIDs, event.ExternalCustomerID) {
			continue
		}

		if event.Timestamp.Before(params.StartTime) || event.Timestamp.After(params.EndTime) {
			continue
		}

		// Apply property filters
		matchesFilters := true
		for key, expectedValues := range params.Filters {
			if propertyValue, exists := event.Properties[key]; exists {
				valueStr := fmt.Sprintf("%v", propertyValue)
				valueMatches := false
				for _, expectedValue := range expectedValues {
					if valueStr == expectedValue {
						valueMatches = true
						break
					}
				}
				if !valueMatches {
					matchesFilters = false
					break
				}
			} else {
				matchesFilters = false
				break
			}
		}

		if matchesFilters {
			filteredEvents = append(filteredEvents, event)
		}
	}

	// Calculate aggregation
	result := &events.AggregationResult{
		EventName: params.EventName,
		Type:      params.AggregationType,
	}

	// Branch order mirrors the real aggregator dispatch: MaxAggregator.GetQuery /
	// SumAggregator.GetQuery check BucketSize before anything else
	// (internal/repository/clickhouse/aggregators.go:264-271, 653-660), so a
	// bucketed-meter query with both BucketSize and WindowSize set is bucketed
	// by BucketSize and WindowSize is ignored.
	if (params.AggregationType == types.AggregationMax || params.AggregationType == types.AggregationSum) && params.BucketSize != "" {
		s.aggregateBucketed(filteredEvents, params, result)
		return result, nil
	}

	// Windowed aggregation (any WindowSize): mirrors the windowed scan loop in
	// internal/repository/clickhouse/event.go:286-371 — one UsageResult per
	// window with events, ordered by window start. Note the real repo does NOT
	// populate result.Value for pure WindowSize queries (it is only assigned in
	// the BucketSize path at event.go:317), so neither do we.
	if params.WindowSize != "" {
		windows := make(map[time.Time][]*events.Event)
		for _, event := range filteredEvents {
			// Window bucketing mirrors formatWindowSizeWithBillingAnchor
			// (aggregators.go:132-201): toStartOfDay/toStartOfMonth etc., with
			// billing-anchor-shifted months when a BillingAnchor is provided.
			start := truncateToWindow(event.Timestamp, params.WindowSize, params.BillingAnchor)
			windows[start] = append(windows[start], event)
		}

		starts := make([]time.Time, 0, len(windows))
		for start := range windows {
			starts = append(starts, start)
		}
		sort.Slice(starts, func(i, j int) bool { return starts[i].Before(starts[j]) })

		result.Results = make([]events.UsageResult, 0, len(starts))
		for _, start := range starts {
			result.Results = append(result.Results, events.UsageResult{
				WindowSize: start,
				// The real scan clamps every windowed value to zero:
				// MAX/SUM via clampToZero (event.go:336), AVG/LATEST/
				// SUM_WITH_MULTIPLIER/WEIGHTED_SUM via the explicit negative
				// check (event.go:352-355). COUNT/COUNT_UNIQUE are uint64.
				Value: clampUsageValueToZero(aggregateUsageValue(windows[start], params)),
			})
		}
		return result, nil
	}

	// Standard aggregation without windowing (event.go:372-414). Negative
	// totals are clamped to zero for all aggregation types (event.go:400-403).
	result.Value = clampUsageValueToZero(aggregateUsageValue(filteredEvents, params))
	return result, nil
}

// aggregateBucketed implements the bucketed (BucketSize) MAX/SUM queries:
//   - MaxAggregator.getWindowedQuery (aggregators.go:714-799): max per bucket,
//     total = sum of bucket maxes; with a valid "properties.X" GroupBy entry,
//     max per (bucket, group) and total = sum of all group values.
//   - SumAggregator.getWindowedQuery (aggregators.go:325-366): sum per bucket,
//     total = sum of bucket sums (no group-by support, like the SQL).
//
// The repo scan (event.go:305-327 via scanBucketedRow) sets result.Value to the
// query's total and appends one UsageResult per row.
func (s *InMemoryEventStore) aggregateBucketed(filteredEvents []*events.Event, params *events.UsageParams, result *events.AggregationResult) {
	// Group-by is only supported for MAX (event.go:308-310, aggregators.go:727).
	groupByProperty := events.FirstGroupByProperty(params.GroupBy)
	hasGroupBy := params.AggregationType == types.AggregationMax &&
		groupByProperty != "" &&
		inMemoryValidGroupByPropertyPattern.MatchString(groupByProperty)

	type bucketKey struct {
		start    time.Time
		groupKey string
	}
	buckets := make(map[bucketKey][]*events.Event)
	for _, event := range filteredEvents {
		key := bucketKey{start: truncateToWindow(event.Timestamp, params.BucketSize, params.BillingAnchor)}
		if hasGroupBy {
			key.groupKey = propertyValue(event.Properties, groupByProperty)
		}
		buckets[key] = append(buckets[key], event)
	}

	keys := make([]bucketKey, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	// ORDER BY bucket_start[, group_key] (aggregators.go:752, 788).
	sort.Slice(keys, func(i, j int) bool {
		if !keys[i].start.Equal(keys[j].start) {
			return keys[i].start.Before(keys[j].start)
		}
		return keys[i].groupKey < keys[j].groupKey
	})

	result.Results = make([]events.UsageResult, 0, len(keys))
	total := decimal.Zero
	for _, k := range keys {
		value := aggregateUsageValue(buckets[k], params)
		total = total.Add(value)
		result.Results = append(result.Results, events.UsageResult{
			WindowSize: k.start,
			Value:      value,
			GroupKey:   k.groupKey,
		})
	}
	// total = sum of per-bucket (per-group) values (aggregators.go:747, 784).
	result.Value = total
}

// inMemoryValidGroupByPropertyPattern mirrors validGroupByPropertyPattern in
// internal/repository/clickhouse/feature_usage.go:21.
var inMemoryValidGroupByPropertyPattern = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)

// eventNumericValue extracts the numeric value of an event property, mirroring
// JSONExtractFloat(assumeNotNull(properties), '<prop>') in the aggregator SQL:
// a missing or non-numeric property contributes 0 (numeric strings are parsed,
// which the in-memory store has always accepted).
func eventNumericValue(event *events.Event, propertyName string) decimal.Decimal {
	val, ok := event.Properties[propertyName]
	if !ok {
		return decimal.Zero
	}
	switch v := val.(type) {
	case float64:
		return decimal.NewFromFloat(v)
	case int:
		return decimal.NewFromInt(int64(v))
	case int64:
		return decimal.NewFromInt(v)
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return decimal.NewFromFloat(f)
		}
	}
	return decimal.Zero
}

// aggregateUsageValue reduces a set of events to a single value for the given
// aggregation type, mirroring the per-window/per-bucket SQL aggregates in
// internal/repository/clickhouse/aggregators.go:
//
//	COUNT                count(DISTINCT id)                    (aggregators.go:390-402)
//	COUNT_UNIQUE         count(DISTINCT property string value) (aggregators.go:441-458)
//	SUM                  sum(value)                            (aggregators.go:292-309)
//	SUM_WITH_MULTIPLIER  sum(value) * multiplier               (aggregators.go:612-629)
//	AVG                  avg(value)                            (aggregators.go:500-517)
//	MAX                  max(value)                            (aggregators.go:681-698, 767-789)
//	LATEST               argMax(value, timestamp)              (aggregators.go:555-568)
//	WEIGHTED_SUM         sum((value/total_seconds) * dateDiff('second', ts, period_end))
//	                                                           (aggregators.go:825-848)
//
// Events are already unique by ID in the store, so the SQL's per-id
// deduplication (GROUP BY id / count(DISTINCT id)) is implicit.
func aggregateUsageValue(evts []*events.Event, params *events.UsageParams) decimal.Decimal {
	switch params.AggregationType {
	case types.AggregationCount:
		return decimal.NewFromInt(int64(len(evts)))
	case types.AggregationCountUnique:
		seen := make(map[string]struct{}, len(evts))
		for _, event := range evts {
			// JSONExtractString semantics: missing property → "" (still counted
			// as a distinct value, like the SQL's count(DISTINCT property_value)).
			seen[propertyValue(event.Properties, params.PropertyName)] = struct{}{}
		}
		return decimal.NewFromInt(int64(len(seen)))
	case types.AggregationSum:
		sum := decimal.Zero
		for _, event := range evts {
			sum = sum.Add(eventNumericValue(event, params.PropertyName))
		}
		return sum
	case types.AggregationSumWithMultiplier:
		sum := decimal.Zero
		for _, event := range evts {
			sum = sum.Add(eventNumericValue(event, params.PropertyName))
		}
		multiplier := decimal.NewFromInt(1)
		if params.Multiplier != nil {
			multiplier = *params.Multiplier
		}
		return sum.Mul(multiplier)
	case types.AggregationAvg:
		if len(evts) == 0 {
			return decimal.Zero
		}
		sum := decimal.Zero
		for _, event := range evts {
			sum = sum.Add(eventNumericValue(event, params.PropertyName))
		}
		return sum.Div(decimal.NewFromInt(int64(len(evts))))
	case types.AggregationMax:
		if len(evts) == 0 {
			return decimal.Zero
		}
		max := eventNumericValue(evts[0], params.PropertyName)
		for _, event := range evts[1:] {
			if v := eventNumericValue(event, params.PropertyName); v.GreaterThan(max) {
				max = v
			}
		}
		return max
	case types.AggregationLatest:
		if len(evts) == 0 {
			return decimal.Zero
		}
		latest := evts[0]
		for _, event := range evts[1:] {
			if event.Timestamp.After(latest.Timestamp) {
				latest = event
			}
		}
		return eventNumericValue(latest, params.PropertyName)
	case types.AggregationWeightedSum:
		// sum((value / nullIf(total_seconds, 0)) * dateDiff('second', timestamp, period_end)).
		totalSeconds := int64(params.EndTime.Sub(params.StartTime).Seconds())
		if totalSeconds == 0 {
			return decimal.Zero
		}
		sum := decimal.Zero
		divisor := decimal.NewFromInt(totalSeconds)
		for _, event := range evts {
			remaining := decimal.NewFromInt(int64(params.EndTime.Sub(event.Timestamp).Seconds()))
			sum = sum.Add(eventNumericValue(event, params.PropertyName).Div(divisor).Mul(remaining))
		}
		return sum
	}
	return decimal.Zero
}

// clampUsageValueToZero mirrors the repo's clamping of negative aggregation
// values to zero on scan (event.go:336, 352-355, 400-403).
func clampUsageValueToZero(d decimal.Decimal) decimal.Decimal {
	if d.LessThan(decimal.Zero) {
		return decimal.Zero
	}
	return d
}

func (s *InMemoryEventStore) GetEvents(ctx context.Context, params *events.GetEventsParams) ([]*events.Event, uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// First, collect all events that match the base criteria (without iterator filters)
	var allMatchingEvents []*events.Event
	for _, event := range s.events {
		// Apply filters
		if params.ExternalCustomerID != "" && event.ExternalCustomerID != params.ExternalCustomerID {
			continue
		}
		if params.EventName != "" && event.EventName != params.EventName {
			continue
		}
		if !params.StartTime.IsZero() && event.Timestamp.Before(params.StartTime) {
			continue
		}
		if !params.EndTime.IsZero() && event.Timestamp.After(params.EndTime) {
			continue
		}

		// Apply property filters
		if len(params.PropertyFilters) > 0 {
			propertyFilterMatched := true
			for property, values := range params.PropertyFilters {
				if len(values) == 0 {
					continue
				}

				if propValue, ok := event.Properties[property]; !ok {
					propertyFilterMatched = false
					break
				} else {
					// Convert property value to string for comparison
					propValueStr := fmt.Sprintf("%v", propValue)

					valueMatched := false
					for _, value := range values {
						if propValueStr == value {
							valueMatched = true
							break
						}
					}

					if !valueMatched {
						propertyFilterMatched = false
						break
					}
				}
			}

			if !propertyFilterMatched {
				continue
			}
		}

		allMatchingEvents = append(allMatchingEvents, event)
	}

	// Sort all matching events by timestamp DESC, id DESC
	sort.Slice(allMatchingEvents, func(i, j int) bool {
		if allMatchingEvents[i].Timestamp.Equal(allMatchingEvents[j].Timestamp) {
			return allMatchingEvents[i].ID > allMatchingEvents[j].ID
		}
		return allMatchingEvents[i].Timestamp.After(allMatchingEvents[j].Timestamp)
	})

	// Total count of all matching events (before any pagination)
	totalCount := uint64(len(allMatchingEvents))

	// Now apply iterator filters to get the correct page
	var filteredEvents []*events.Event
	if params.IterFirst != nil {
		for _, event := range allMatchingEvents {
			if event.Timestamp.Equal(params.IterFirst.Timestamp) {
				if event.ID <= params.IterFirst.ID {
					continue
				}
			} else if !event.Timestamp.After(params.IterFirst.Timestamp) {
				continue
			}
			filteredEvents = append(filteredEvents, event)
		}
	} else if params.IterLast != nil {
		for _, event := range allMatchingEvents {
			if event.Timestamp.Equal(params.IterLast.Timestamp) {
				if event.ID >= params.IterLast.ID {
					continue
				}
			} else if !event.Timestamp.Before(params.IterLast.Timestamp) {
				continue
			}
			filteredEvents = append(filteredEvents, event)
		}
	} else {
		// If no iterators, use all matching events
		filteredEvents = allMatchingEvents
	}

	// Apply offset
	if params.Offset > 0 && params.Offset < len(filteredEvents) {
		filteredEvents = filteredEvents[params.Offset:]
	}

	// Apply page size limit
	if params.PageSize > 0 && params.PageSize < len(filteredEvents) {
		filteredEvents = filteredEvents[:params.PageSize]
	}

	return filteredEvents, totalCount, nil
}

func (s *InMemoryEventStore) GetUsageWithFilters(ctx context.Context, params *events.UsageWithFiltersParams) ([]*events.AggregationResult, error) {
	if params == nil || params.UsageParams == nil {
		return nil, ierr.NewError("params cannot be nil").
			WithHint("Params cannot be nil").
			Mark(ierr.ErrValidation)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Process each filter group and calculate usage
	var results []*events.AggregationResult
	for _, group := range params.FilterGroups {
		// Filter events based on base filters and group filters
		var filteredEvents []*events.Event
		for _, event := range s.events {
			if !s.matchesBaseFilters(ctx, event, params.UsageParams) {
				continue
			}

			if !s.matchesFilterGroup(event, group) {
				continue
			}

			filteredEvents = append(filteredEvents, event)
		}

		// Calculate usage for filtered events
		var value decimal.Decimal
		switch params.AggregationType {
		case types.AggregationCount:
			value = decimal.NewFromInt(int64(len(filteredEvents)))
		case types.AggregationSum, types.AggregationAvg:
			var sum decimal.Decimal
			count := 0
			for _, event := range filteredEvents {
				if val, ok := event.Properties[params.PropertyName]; ok {
					// Try to convert the value to float64
					var floatVal float64
					switch v := val.(type) {
					case float64:
						floatVal = v
					case int64:
						floatVal = float64(v)
					case int:
						floatVal = float64(v)
					case string:
						var err error
						floatVal, err = strconv.ParseFloat(v, 64)
						if err != nil {
							continue
						}
					default:
						continue
					}
					sum = sum.Add(decimal.NewFromFloat(floatVal))
					count++
				}
			}
			if count > 0 {
				if params.AggregationType == types.AggregationAvg {
					value = sum.Div(decimal.NewFromInt(int64(count)))
				} else {
					value = sum
				}
			}
			log.Printf("Calculated %s: sum=%v, count=%d, value=%v",
				params.AggregationType, sum, count, value)
		}
		result := &events.AggregationResult{
			EventName: params.EventName,
			Type:      params.AggregationType,
			Metadata: map[string]string{
				"filter_group_id": group.ID,
			},
			Value: value,
		}
		results = append(results, result)
	}

	return results, nil
}

func (s *InMemoryEventStore) GetDistinctEventNames(ctx context.Context, externalCustomerIDs []string, startTime, endTime time.Time) ([]string, error) {
	if len(externalCustomerIDs) == 0 {
		return nil, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	idSet := make(map[string]struct{}, len(externalCustomerIDs))
	for _, id := range externalCustomerIDs {
		idSet[id] = struct{}{}
	}

	var eventNames []string
	for _, event := range s.events {
		if _, ok := idSet[event.ExternalCustomerID]; !ok {
			continue
		}
		// Use inclusive comparison: event.Timestamp >= startTime && event.Timestamp < endTime
		if (event.Timestamp.Equal(startTime) || event.Timestamp.After(startTime)) &&
			event.Timestamp.Before(endTime) {
			eventNames = append(eventNames, event.EventName)
		}
	}

	eventNames = lo.Uniq(eventNames)
	sort.Strings(eventNames)

	return eventNames, nil
}

func (s *InMemoryEventStore) GetDistinctExternalCustomerIDs(ctx context.Context, startTime, endTime time.Time) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	var externalIDs []string
	for _, event := range s.events {
		if tenantID != "" && event.TenantID != tenantID {
			continue
		}
		if environmentID != "" && event.EnvironmentID != environmentID {
			continue
		}

		// Mirror ClickHouse repo semantics: startTime is >=, endTime is <= when provided.
		if !startTime.IsZero() && event.Timestamp.Before(startTime) {
			continue
		}
		if !endTime.IsZero() && event.Timestamp.After(endTime) {
			continue
		}
		if !event.Timestamp.IsZero() {
			if event.ExternalCustomerID != "" {
				externalIDs = append(externalIDs, event.ExternalCustomerID)
			}
		}
	}

	externalIDs = lo.Uniq(externalIDs)
	sort.Strings(externalIDs)
	if externalIDs == nil {
		return []string{}, nil
	}
	return externalIDs, nil
}

func (s *InMemoryEventStore) matchesBaseFilters(ctx context.Context, event *events.Event, params *events.UsageParams) bool {
	// check tenant ID
	tenantID := types.GetTenantID(ctx)
	if event.TenantID != tenantID {
		return false
	}

	// Check customer ID — union of ExternalCustomerIDs and ExternalCustomerID
	allCustomerIDs := lo.Uniq(lo.Without(append(params.ExternalCustomerIDs, params.ExternalCustomerID), ""))
	if len(allCustomerIDs) > 0 && !lo.Contains(allCustomerIDs, event.ExternalCustomerID) {
		return false
	}

	// Check event name
	if event.EventName != params.EventName {
		return false
	}

	// Check time range
	if !event.Timestamp.IsZero() {
		if !params.StartTime.IsZero() && event.Timestamp.Before(params.StartTime) {
			return false
		}
		if !params.EndTime.IsZero() && event.Timestamp.After(params.EndTime) {
			return false
		}
	}

	// Check base filters
	if params.Filters != nil {
		for key, values := range params.Filters {
			if propValue, ok := event.Properties[key]; !ok {
				log.Printf("Event %s missing property %s", event.ID, key)
				return false
			} else {
				found := false
				for _, value := range values {
					if fmt.Sprintf("%v", propValue) == value {
						found = true
						break
					}
				}
				if !found {
					log.Printf("Event %s property %s=%v not in values %v",
						event.ID, key, propValue, values)
					return false
				}
			}
		}
	}

	return true
}

func (s *InMemoryEventStore) matchesFilterGroup(event *events.Event, group events.FilterGroup) bool {
	if len(group.Filters) == 0 {
		return true
	}

	for key, values := range group.Filters {
		if propValue, ok := event.Properties[key]; !ok {
			return false
		} else {
			found := false
			for _, value := range values {
				if fmt.Sprintf("%v", propValue) == value {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	return true
}

func (s *InMemoryEventStore) HasEvent(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.events[id]
	return exists
}

func (s *InMemoryEventStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = make(map[string]*events.Event)
}

func (s *InMemoryEventStore) FindUnprocessedEvents(ctx context.Context, params *events.FindUnprocessedEventsParams) ([]*events.Event, error) {
	return nil, ierr.NewError("not implemented").
		WithHint("not implemented").
		Mark(ierr.ErrSystem)
}

func (s *InMemoryEventStore) FindUnprocessedEventsFromFeatureUsage(ctx context.Context, params *events.FindUnprocessedEventsParams) ([]*events.Event, error) {
	return nil, ierr.NewError("not implemented").
		WithHint("not implemented").
		Mark(ierr.ErrSystem)
}

// GetTotalEventCount returns the total count of events in the given time range with optional windowed time-series data
func (s *InMemoryEventStore) GetTotalEventCount(ctx context.Context, startTime, endTime time.Time, windowSize types.WindowSize) (*events.EventCountResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := &events.EventCountResult{
		TotalCount: 0,
		Points:     []events.EventCountPoint{},
	}

	// If window size is provided, group events by time windows
	if windowSize != "" {
		windowCounts := make(map[time.Time]uint64)

		for _, event := range s.events {
			// Check if event is within time range
			if !event.Timestamp.Before(startTime) && event.Timestamp.Before(endTime) {
				windowStart := s.getWindowStart(event.Timestamp, windowSize)
				windowCounts[windowStart]++
				result.TotalCount++
			}
		}

		// Convert map to sorted slice of points
		for windowStart, count := range windowCounts {
			result.Points = append(result.Points, events.EventCountPoint{
				Timestamp:  windowStart,
				EventCount: count,
			})
		}

		// Sort points by timestamp
		sort.Slice(result.Points, func(i, j int) bool {
			return result.Points[i].Timestamp.Before(result.Points[j].Timestamp)
		})
	} else {
		// No window size, just get total count
		for _, event := range s.events {
			// Check if event is within time range
			if !event.Timestamp.Before(startTime) && event.Timestamp.Before(endTime) {
				result.TotalCount++
			}
		}
	}

	return result, nil
}

// getWindowStart returns the start of the time window for a given timestamp
func (s *InMemoryEventStore) getWindowStart(t time.Time, windowSize types.WindowSize) time.Time {
	switch windowSize {
	case types.WindowSizeMinute:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, t.Location())
	case types.WindowSize15Min:
		minute := (t.Minute() / 15) * 15
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), minute, 0, 0, t.Location())
	case types.WindowSize30Min:
		minute := (t.Minute() / 30) * 30
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), minute, 0, 0, t.Location())
	case types.WindowSizeHour:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
	case types.WindowSize3Hour:
		hour := (t.Hour() / 3) * 3
		return time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, t.Location())
	case types.WindowSize6Hour:
		hour := (t.Hour() / 6) * 6
		return time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, t.Location())
	case types.WindowSize12Hour:
		hour := (t.Hour() / 12) * 12
		return time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, t.Location())
	case types.WindowSizeDay:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	case types.WindowSizeWeek:
		// Get the Monday of the week
		weekday := int(t.Weekday())
		if weekday == 0 {
			weekday = 7 // Sunday is 7
		}
		return time.Date(t.Year(), t.Month(), t.Day()-(weekday-1), 0, 0, 0, 0, t.Location())
	case types.WindowSizeMonth:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	default:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
	}
}
