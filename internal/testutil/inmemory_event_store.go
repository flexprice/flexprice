package testutil

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
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
		return fmt.Errorf("event cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[event.ID] = event
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

	switch params.AggregationType {
	case types.AggregationCount:
		result.Value = decimal.NewFromInt(int64(len(filteredEvents)))
	case types.AggregationSum:
		var sum decimal.Decimal
		for _, event := range filteredEvents {
			if val, ok := event.Properties[params.PropertyName]; ok {
				if floatVal, ok := val.(float64); ok {
					sum = sum.Add(decimal.NewFromFloat(floatVal))
				}
			}
		}
		result.Value = sum
	}

	return result, nil
}

func (s *InMemoryEventStore) GetEvents(ctx context.Context, params *events.GetEventsParams) ([]*events.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Convert map to slice for sorting
	var eventsList []*events.Event
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

		// Handle pagination using composite keys (timestamp, id)
		if params.IterFirst != nil {
			// Skip events that are older or equal to the reference point
			if event.Timestamp.Equal(params.IterFirst.Timestamp) {
				// If timestamps are equal, we want to skip this event and all events with smaller IDs
				if event.ID <= params.IterFirst.ID {
					continue
				}
			} else if !event.Timestamp.After(params.IterFirst.Timestamp) {
				continue
			}

		} else if params.IterLast != nil {
			// For IterLast, we want events OLDER than the reference point
			if event.Timestamp.Equal(params.IterLast.Timestamp) {
				if event.ID >= params.IterLast.ID {
					continue
				}
			} else if !event.Timestamp.Before(params.IterLast.Timestamp) {
				continue
			}
		}

		eventsList = append(eventsList, event)
	}

	// Sort by timestamp DESC, id DESC
	sort.Slice(eventsList, func(i, j int) bool {
		if eventsList[i].Timestamp.Equal(eventsList[j].Timestamp) {
			return eventsList[i].ID > eventsList[j].ID
		}
		return eventsList[i].Timestamp.After(eventsList[j].Timestamp)
	})

	// Apply limit
	if len(eventsList) > params.PageSize {
		eventsList = eventsList[:params.PageSize]
	}

	return eventsList, nil
}

func (s *InMemoryEventStore) GetUsageWithFilters(ctx context.Context, params *events.UsageWithFiltersParams) ([]*events.AggregationResult, error) {
	if params == nil || params.UsageParams == nil {
		return nil, fmt.Errorf("params cannot be nil")
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
