package testutil

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
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

	var result float64
	for _, event := range s.events {
		if event.EventName != params.EventName ||
			event.ExternalCustomerID != params.ExternalCustomerID ||
			event.Timestamp.Before(params.StartTime) ||
			event.Timestamp.After(params.EndTime) {
			continue
		}

		switch params.AggregationType {
		case types.AggregationCount:
			result++
		case types.AggregationSum:
			if params.PropertyName != "" {
				if val, ok := event.Properties[params.PropertyName]; ok {
					if numVal, ok := val.(float64); ok {
						result += numVal
					}
				}
			}
		}
	}

	return &events.AggregationResult{
		Value:     result,
		EventName: params.EventName,
		Type:      params.AggregationType,
	}, nil
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

func (s *InMemoryEventStore) HasEvent(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.events[id]
	return exists
}