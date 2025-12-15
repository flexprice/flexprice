package testutil

import (
	"context"
	"fmt"
	"log"
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

func (s *InMemoryEventStore) GetUsage(ctx context.Context, params *events.UsageParams) (*events.AggregationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filteredEvents []*events.Event

	// Filter events based on basic criteria
	for _, event := range s.events {
		if event.EventName != params.EventName {
			continue
		}

		if params.ExternalCustomerID != "" && event.ExternalCustomerID != params.ExternalCustomerID {
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

	// Handle window size for daily aggregation
	if params.WindowSize == types.WindowSizeDay {
		// Group events by day
		dailyBuckets := make(map[time.Time][]*events.Event)
		for _, event := range filteredEvents {
			dayStart := truncateToBucket(event.Timestamp, types.WindowSizeDay)
			dailyBuckets[dayStart] = append(dailyBuckets[dayStart], event)
		}

		// Sort days
		days := make([]time.Time, 0, len(dailyBuckets))
		for day := range dailyBuckets {
			days = append(days, day)
		}
		sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })

		// Calculate aggregation for each day
		result.Results = make([]events.UsageResult, 0, len(days))
		var totalValue decimal.Decimal

		for _, day := range days {
			dayEvents := dailyBuckets[day]
			var dayValue decimal.Decimal

			switch params.AggregationType {
			case types.AggregationCount:
				dayValue = decimal.NewFromInt(int64(len(dayEvents)))
			case types.AggregationSum:
				for _, event := range dayEvents {
					if val, ok := event.Properties[params.PropertyName]; ok {
						switch v := val.(type) {
						case float64:
							dayValue = dayValue.Add(decimal.NewFromFloat(v))
						case int:
							dayValue = dayValue.Add(decimal.NewFromInt(int64(v)))
						case int64:
							dayValue = dayValue.Add(decimal.NewFromInt(v))
						case string:
							if f, err := strconv.ParseFloat(v, 64); err == nil {
								dayValue = dayValue.Add(decimal.NewFromFloat(f))
							}
						}
					}
				}
			}

			result.Results = append(result.Results, events.UsageResult{
				WindowSize: day,
				Value:      dayValue,
			})
			totalValue = totalValue.Add(dayValue)
		}

		result.Value = totalValue
		return result, nil
	}

	// Handle window size for monthly aggregation
	if params.WindowSize == types.WindowSizeMonth {
		// Group events by month
		monthlyBuckets := make(map[time.Time][]*events.Event)
		for _, event := range filteredEvents {
			monthStart := truncateToBucket(event.Timestamp, types.WindowSizeMonth)
			monthlyBuckets[monthStart] = append(monthlyBuckets[monthStart], event)
		}

		// Sort months
		months := make([]time.Time, 0, len(monthlyBuckets))
		for month := range monthlyBuckets {
			months = append(months, month)
		}
		sort.Slice(months, func(i, j int) bool { return months[i].Before(months[j]) })

		// Calculate aggregation for each month
		result.Results = make([]events.UsageResult, 0, len(months))
		var totalValue decimal.Decimal

		for _, month := range months {
			monthEvents := monthlyBuckets[month]
			var monthValue decimal.Decimal

			switch params.AggregationType {
			case types.AggregationCount:
				monthValue = decimal.NewFromInt(int64(len(monthEvents)))
			case types.AggregationSum:
				for _, event := range monthEvents {
					if val, ok := event.Properties[params.PropertyName]; ok {
						switch v := val.(type) {
						case float64:
							monthValue = monthValue.Add(decimal.NewFromFloat(v))
						case int:
							monthValue = monthValue.Add(decimal.NewFromInt(int64(v)))
						case int64:
							monthValue = monthValue.Add(decimal.NewFromInt(v))
						case string:
							if f, err := strconv.ParseFloat(v, 64); err == nil {
								monthValue = monthValue.Add(decimal.NewFromFloat(f))
							}
						}
					}
				}
			}

			result.Results = append(result.Results, events.UsageResult{
				WindowSize: month,
				Value:      monthValue,
			})
			totalValue = totalValue.Add(monthValue)
		}

		result.Value = totalValue
		return result, nil
	}

	// Handle bucket size for MAX aggregation (existing logic)
	if params.AggregationType == types.AggregationMax && params.BucketSize != "" {
		// Group events into buckets by bucket start time
		buckets := make(map[time.Time]decimal.Decimal)
		var overallMax decimal.Decimal

		for _, event := range filteredEvents {
			if val, ok := event.Properties[params.PropertyName]; ok {
				var f float64
				switch v := val.(type) {
				case float64:
					f = v
				case int:
					f = float64(v)
				case int64:
					f = float64(v)
				case string:
					if parsed, err := strconv.ParseFloat(v, 64); err == nil {
						f = parsed
					} else {
						continue
					}
				default:
					continue
				}

				bucketStart := truncateToBucket(event.Timestamp, params.BucketSize)
				current := buckets[bucketStart]
				if current.IsZero() || decimal.NewFromFloat(f).GreaterThan(current) {
					buckets[bucketStart] = decimal.NewFromFloat(f)
				}

				if overallMax.IsZero() || decimal.NewFromFloat(f).GreaterThan(overallMax) {
					overallMax = decimal.NewFromFloat(f)
				}
			}
		}

		// Convert buckets to sorted results
		keys := make([]time.Time, 0, len(buckets))
		for k := range buckets {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })

		result.Results = make([]events.UsageResult, 0, len(keys))
		for _, k := range keys {
			result.Results = append(result.Results, events.UsageResult{
				WindowSize: k,
				Value:      buckets[k],
			})
		}
		result.Value = overallMax
		return result, nil
	}

	// Standard aggregation without windowing
	switch params.AggregationType {
	case types.AggregationCount:
		result.Value = decimal.NewFromInt(int64(len(filteredEvents)))
	case types.AggregationSum:
		var sum decimal.Decimal
		for _, event := range filteredEvents {
			if val, ok := event.Properties[params.PropertyName]; ok {
				switch v := val.(type) {
				case float64:
					sum = sum.Add(decimal.NewFromFloat(v))
				case int:
					sum = sum.Add(decimal.NewFromInt(int64(v)))
				case int64:
					sum = sum.Add(decimal.NewFromInt(v))
				case string:
					if f, err := strconv.ParseFloat(v, 64); err == nil {
						sum = sum.Add(decimal.NewFromFloat(f))
					}
				}
			}
		}
		result.Value = sum
	case types.AggregationMax:
		// Simple max across all filtered events
		var maxVal decimal.Decimal
		for _, event := range filteredEvents {
			if val, ok := event.Properties[params.PropertyName]; ok {
				var f float64
				switch v := val.(type) {
				case float64:
					f = v
				case int:
					f = float64(v)
				case int64:
					f = float64(v)
				case string:
					if parsed, err := strconv.ParseFloat(v, 64); err == nil {
						f = parsed
					} else {
						continue
					}
				default:
					continue
				}
				if maxVal.IsZero() || decimal.NewFromFloat(f).GreaterThan(maxVal) {
					maxVal = decimal.NewFromFloat(f)
				}
			}
		}
		result.Value = maxVal
	}

	return result, nil
}

// truncateToBucket truncates t to the start of the given bucket size in UTC.
func truncateToBucket(t time.Time, size types.WindowSize) time.Time {
	t = t.UTC()
	switch size {
	case types.WindowSizeMinute:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
	case types.WindowSize15Min:
		m := (t.Minute() / 15) * 15
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), m, 0, 0, time.UTC)
	case types.WindowSize30Min:
		m := (t.Minute() / 30) * 30
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), m, 0, 0, time.UTC)
	case types.WindowSizeHour:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
	case types.WindowSize3Hour:
		h := (t.Hour() / 3) * 3
		return time.Date(t.Year(), t.Month(), t.Day(), h, 0, 0, 0, time.UTC)
	case types.WindowSize6Hour:
		h := (t.Hour() / 6) * 6
		return time.Date(t.Year(), t.Month(), t.Day(), h, 0, 0, 0, time.UTC)
	case types.WindowSize12Hour:
		h := (t.Hour() / 12) * 12
		return time.Date(t.Year(), t.Month(), t.Day(), h, 0, 0, 0, time.UTC)
	case types.WindowSizeDay:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	case types.WindowSizeWeek:
		// Start of week (Monday) at 00:00 UTC
		weekday := int(t.Weekday())
		if weekday == 0 { // Sunday
			weekday = 7
		}
		start := t.AddDate(0, 0, -(weekday - 1))
		return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	case types.WindowSizeMonth:
		// Start of month at 00:00 UTC
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return t
	}
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

func (s *InMemoryEventStore) GetDistinctEventNames(ctx context.Context, externalCustomerID string, startTime, endTime time.Time) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var eventNames []string
	for _, event := range s.events {
		// Use inclusive comparison: event.Timestamp >= startTime && event.Timestamp < endTime
		if event.ExternalCustomerID == externalCustomerID &&
			(event.Timestamp.Equal(startTime) || event.Timestamp.After(startTime)) &&
			event.Timestamp.Before(endTime) {
			eventNames = append(eventNames, event.EventName)
		}
	}

	eventNames = lo.Uniq(eventNames)
	sort.Strings(eventNames)

	return eventNames, nil
}

func (s *InMemoryEventStore) matchesBaseFilters(ctx context.Context, event *events.Event, params *events.UsageParams) bool {
	// check tenant ID
	tenantID := types.GetTenantID(ctx)
	if event.TenantID != tenantID {
		return false
	}

	// Check customer ID
	if params.ExternalCustomerID != "" && event.ExternalCustomerID != params.ExternalCustomerID {
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
