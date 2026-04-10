package testutil

import (
	"context"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// InMemoryMeterUsageStore implements events.MeterUsageRepository for tests.
type InMemoryMeterUsageStore struct {
	mu      sync.RWMutex
	records []*events.MeterUsage
}

func NewInMemoryMeterUsageStore() *InMemoryMeterUsageStore {
	return &InMemoryMeterUsageStore{
		records: make([]*events.MeterUsage, 0),
	}
}

func (s *InMemoryMeterUsageStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = make([]*events.MeterUsage, 0)
}

func (s *InMemoryMeterUsageStore) BulkInsertMeterUsage(_ context.Context, records []*events.MeterUsage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, records...)
	return nil
}

func (s *InMemoryMeterUsageStore) IsDuplicate(_ context.Context, meterID, uniqueHash string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.records {
		if r.MeterID == meterID && r.UniqueHash == uniqueHash {
			return true, nil
		}
	}
	return false, nil
}

// matchesParams checks if a record matches the query parameters.
func (s *InMemoryMeterUsageStore) matchesParams(r *events.MeterUsage, params *events.MeterUsageQueryParams) bool {
	if params.TenantID != "" && r.TenantID != params.TenantID {
		return false
	}
	if params.EnvironmentID != "" && r.EnvironmentID != params.EnvironmentID {
		return false
	}
	if len(params.ExternalCustomerIDs) > 0 && !lo.Contains(params.ExternalCustomerIDs, r.ExternalCustomerID) {
		return false
	}
	if params.ExternalCustomerID != "" && r.ExternalCustomerID != params.ExternalCustomerID {
		return false
	}
	if params.MeterID != "" && r.MeterID != params.MeterID {
		return false
	}
	if len(params.MeterIDs) > 0 && !lo.Contains(params.MeterIDs, r.MeterID) {
		return false
	}
	if !params.StartTime.IsZero() && r.Timestamp.Before(params.StartTime) {
		return false
	}
	if !params.EndTime.IsZero() && !r.Timestamp.Before(params.EndTime) {
		return false
	}
	return true
}

func (s *InMemoryMeterUsageStore) GetUsage(_ context.Context, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := decimal.Zero
	var count uint64
	for _, r := range s.records {
		if !s.matchesParams(r, params) {
			continue
		}
		switch params.AggregationType {
		case types.AggregationSum:
			total = total.Add(r.QtyTotal)
		case types.AggregationCount:
			total = total.Add(decimal.NewFromInt(1))
		case types.AggregationMax:
			if r.QtyTotal.GreaterThan(total) {
				total = r.QtyTotal
			}
		default:
			total = total.Add(r.QtyTotal)
		}
		count++
	}

	return &events.MeterUsageAggregationResult{
		MeterID:         params.MeterID,
		AggregationType: params.AggregationType,
		TotalValue:      total,
		EventCount:      count,
	}, nil
}

func (s *InMemoryMeterUsageStore) GetUsageMultiMeter(_ context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byMeter := make(map[string]decimal.Decimal)
	countByMeter := make(map[string]uint64)
	for _, mID := range params.MeterIDs {
		byMeter[mID] = decimal.Zero
		countByMeter[mID] = 0
	}

	for _, r := range s.records {
		if !s.matchesParams(r, params) {
			continue
		}
		switch params.AggregationType {
		case types.AggregationSum:
			byMeter[r.MeterID] = byMeter[r.MeterID].Add(r.QtyTotal)
		case types.AggregationCount:
			byMeter[r.MeterID] = byMeter[r.MeterID].Add(decimal.NewFromInt(1))
		case types.AggregationMax:
			if r.QtyTotal.GreaterThan(byMeter[r.MeterID]) {
				byMeter[r.MeterID] = r.QtyTotal
			}
		default:
			byMeter[r.MeterID] = byMeter[r.MeterID].Add(r.QtyTotal)
		}
		countByMeter[r.MeterID]++
	}

	results := make([]*events.MeterUsageAggregationResult, 0, len(params.MeterIDs))
	for _, mID := range params.MeterIDs {
		results = append(results, &events.MeterUsageAggregationResult{
			MeterID:         mID,
			AggregationType: params.AggregationType,
			TotalValue:      byMeter[mID],
			EventCount:      countByMeter[mID],
		})
	}
	return results, nil
}

func (s *InMemoryMeterUsageStore) GetUsageForBucketedMeters(_ context.Context, _ *events.MeterUsageQueryParams) (*events.AggregationResult, error) {
	return &events.AggregationResult{
		Results: make([]events.UsageResult, 0),
		Value:   decimal.Zero,
	}, nil
}

func (s *InMemoryMeterUsageStore) GetDistinctMeterIDs(_ context.Context, params *events.MeterUsageQueryParams) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]bool)
	for _, r := range s.records {
		if !s.matchesParams(r, params) {
			continue
		}
		seen[r.MeterID] = true
	}

	result := make([]string, 0, len(seen))
	for mID := range seen {
		result = append(result, mID)
	}
	return result, nil
}
