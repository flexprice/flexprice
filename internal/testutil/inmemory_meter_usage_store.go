package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// InMemoryMeterUsageStore implements events.MeterUsageRepository for testing.
// Records are stored as raw MeterUsage entries; query methods aggregate in-memory.
type InMemoryMeterUsageStore struct {
	mu      sync.RWMutex
	records []*events.MeterUsage
}

func NewInMemoryMeterUsageStore() *InMemoryMeterUsageStore {
	return &InMemoryMeterUsageStore{}
}

func (s *InMemoryMeterUsageStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = nil
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

// matchRecord checks if a record matches the query params.
func matchRecord(r *events.MeterUsage, p *events.MeterUsageQueryParams) bool {
	if p.TenantID != "" && r.TenantID != p.TenantID {
		return false
	}
	if p.EnvironmentID != "" && r.EnvironmentID != p.EnvironmentID {
		return false
	}
	if p.MeterID != "" && r.MeterID != p.MeterID {
		return false
	}
	if len(p.MeterIDs) > 0 {
		found := false
		for _, id := range p.MeterIDs {
			if r.MeterID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if !p.StartTime.IsZero() && r.Timestamp.Before(p.StartTime) {
		return false
	}
	if !p.EndTime.IsZero() && !r.Timestamp.Before(p.EndTime) {
		return false
	}
	if p.ExternalCustomerID != "" && r.ExternalCustomerID != p.ExternalCustomerID {
		return false
	}
	if len(p.ExternalCustomerIDs) > 0 {
		found := false
		for _, id := range p.ExternalCustomerIDs {
			if r.ExternalCustomerID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (s *InMemoryMeterUsageStore) GetUsage(_ context.Context, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := &events.MeterUsageAggregationResult{
		MeterID:         params.MeterID,
		AggregationType: params.AggregationType,
	}

	for _, r := range s.records {
		if !matchRecord(r, params) {
			continue
		}
		result.EventCount++
		switch params.AggregationType {
		case types.AggregationSum, types.AggregationSumWithMultiplier, types.AggregationCount:
			result.TotalValue = result.TotalValue.Add(r.QtyTotal)
		case types.AggregationMax:
			if r.QtyTotal.GreaterThan(result.TotalValue) {
				result.TotalValue = r.QtyTotal
			}
		default:
			result.TotalValue = result.TotalValue.Add(r.QtyTotal)
		}
	}

	return result, nil
}

func (s *InMemoryMeterUsageStore) GetUsageMultiMeter(ctx context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byMeter := make(map[string]*events.MeterUsageAggregationResult)
	for _, r := range s.records {
		if !matchRecord(r, params) {
			continue
		}
		res, ok := byMeter[r.MeterID]
		if !ok {
			res = &events.MeterUsageAggregationResult{
				MeterID:         r.MeterID,
				AggregationType: params.AggregationType,
			}
			byMeter[r.MeterID] = res
		}
		res.EventCount++
		switch params.AggregationType {
		case types.AggregationSum, types.AggregationSumWithMultiplier, types.AggregationCount:
			res.TotalValue = res.TotalValue.Add(r.QtyTotal)
		case types.AggregationMax:
			if r.QtyTotal.GreaterThan(res.TotalValue) {
				res.TotalValue = r.QtyTotal
			}
		default:
			res.TotalValue = res.TotalValue.Add(r.QtyTotal)
		}
	}

	results := make([]*events.MeterUsageAggregationResult, 0, len(byMeter))
	for _, r := range byMeter {
		results = append(results, r)
	}
	return results, nil
}

func (s *InMemoryMeterUsageStore) GetUsageForBucketedMeters(_ context.Context, params *events.MeterUsageQueryParams) (*events.AggregationResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := &events.AggregationResult{}
	bucketValues := make(map[time.Time]decimal.Decimal)

	for _, r := range s.records {
		if !matchRecord(r, params) {
			continue
		}
		result.Value = result.Value.Add(r.QtyTotal)

		// Simple bucketing by day for testing
		bucket := r.Timestamp.Truncate(24 * time.Hour)
		bucketValues[bucket] = bucketValues[bucket].Add(r.QtyTotal)
	}

	for t, v := range bucketValues {
		result.Results = append(result.Results, events.UsageResult{
			WindowSize: t,
			Value:      v,
		})
	}

	return result, nil
}

func (s *InMemoryMeterUsageStore) GetDistinctMeterIDs(_ context.Context, params *events.MeterUsageQueryParams) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]bool)
	for _, r := range s.records {
		if !matchRecord(r, params) {
			continue
		}
		seen[r.MeterID] = true
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *InMemoryMeterUsageStore) GetDetailedAnalytics(_ context.Context, params *events.MeterUsageDetailedAnalyticsParams) ([]*events.MeterUsageDetailedResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Convert to query params for matching
	qp := &events.MeterUsageQueryParams{
		TenantID:            params.TenantID,
		EnvironmentID:       params.EnvironmentID,
		ExternalCustomerID:  params.ExternalCustomerID,
		ExternalCustomerIDs: params.ExternalCustomerIDs,
		MeterIDs:            params.MeterIDs,
		StartTime:           params.StartTime,
		EndTime:             params.EndTime,
	}

	byMeter := make(map[string]*events.MeterUsageDetailedResult)
	for _, r := range s.records {
		if !matchRecord(r, qp) {
			continue
		}
		res, ok := byMeter[r.MeterID]
		if !ok {
			res = &events.MeterUsageDetailedResult{
				MeterID:    r.MeterID,
				Properties: make(map[string]string),
			}
			byMeter[r.MeterID] = res
		}
		res.EventCount++
		res.TotalUsage = res.TotalUsage.Add(r.QtyTotal)
		if r.QtyTotal.GreaterThan(res.MaxUsage) {
			res.MaxUsage = r.QtyTotal
		}
	}

	results := make([]*events.MeterUsageDetailedResult, 0, len(byMeter))
	for _, r := range byMeter {
		results = append(results, r)
	}
	return results, nil
}

// Ensure interface compliance
var _ events.MeterUsageRepository = (*InMemoryMeterUsageStore)(nil)
