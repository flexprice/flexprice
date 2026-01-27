package testutil

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
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

// GetFeatureUsageBySubscription gets feature usage by subscription
func (s *InMemoryFeatureUsageStore) GetFeatureUsageBySubscription(ctx context.Context, subscriptionID, externalCustomerID string, startTime, endTime time.Time) (map[string]*events.UsageByFeatureResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*events.UsageByFeatureResult)
	for _, usage := range s.usage {
		if usage.SubscriptionID == subscriptionID {
			result[usage.SubLineItemID] = &events.UsageByFeatureResult{
				SubLineItemID: usage.SubLineItemID,
				FeatureID:     usage.FeatureID,
				MeterID:       usage.MeterID,
				PriceID:       usage.PriceID,
				SumTotal:      usage.QtyTotal,
			}
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

func (s *InMemoryFeatureUsageStore) GetUsageForMaxMetersWithBuckets(ctx context.Context, params *events.FeatureUsageParams) (*events.AggregationResult, error) {
	return &events.AggregationResult{
		Results: make([]events.UsageResult, 0),
		Value:   decimal.NewFromInt(0),
	}, nil
}

func (s *InMemoryFeatureUsageStore) GetFeatureUsageByEventIDs(ctx context.Context, eventIDs []string, externalCustomerID string) ([]*events.FeatureUsage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*events.FeatureUsage

	return result, nil
}
