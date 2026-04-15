package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// MeterUsageService handles read-side meter usage queries.
// It is the central orchestration point for all flows that need to query
// meter_usage: cost analytics, invoice preview, subscription usage, balance, etc.
type MeterUsageService interface {
	GetUsage(ctx context.Context, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error)
	GetUsageMultiMeter(ctx context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error)

	// QueryUsageByMeters is the central orchestration method for fetching usage across a set of meters.
	// It handles the full pipeline:
	//   1. GetDistinctMeterIDs — skips meters with no data in the period (avoids N×zero-result queries)
	//   2. Splits meters into bucketed (MAX/SUM with BucketSize) vs regular
	//   3. Regular: groups by aggregation type → one GetUsageMultiMeter call per group (not per meter)
	//   4. Bucketed: one GetUsageForBucketedMeters call per meter (needs per-meter window config)
	//
	// params should carry: TenantID, EnvironmentID, ExternalCustomerID/ExternalCustomerIDs,
	// StartTime, EndTime, WindowSize (for regular meters), UseFinal.
	// params.MeterIDs and params.AggregationType are derived from the meters map and ignored.
	//
	// Only meters with actual data are present in the returned result.
	QueryUsageByMeters(ctx context.Context, meters map[string]*meter.Meter, params *events.MeterUsageQueryParams) (*events.MeterUsageQueryResult, error)
}

type meterUsageService struct {
	repo   events.MeterUsageRepository
	logger *logger.Logger
}

func NewMeterUsageService(repo events.MeterUsageRepository, logger *logger.Logger) MeterUsageService {
	return &meterUsageService{
		repo:   repo,
		logger: logger,
	}
}

func (s *meterUsageService) GetUsage(ctx context.Context, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}
	return s.repo.GetUsage(ctx, params)
}

func (s *meterUsageService) GetUsageMultiMeter(ctx context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error) {
	if params == nil || len(params.MeterIDs) == 0 {
		return nil, ierr.NewError("params with meter_ids are required").Mark(ierr.ErrValidation)
	}
	return s.repo.GetUsageMultiMeter(ctx, params)
}

func (s *meterUsageService) QueryUsageByMeters(
	ctx context.Context,
	meters map[string]*meter.Meter,
	params *events.MeterUsageQueryParams,
) (*events.MeterUsageQueryResult, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	result := &events.MeterUsageQueryResult{
		Regular:  make(map[string]*events.MeterUsageAggregationResult),
		Bucketed: make(map[string]*events.AggregationResult),
	}

	if len(meters) == 0 {
		return result, nil
	}

	// Collect all meter IDs
	allMeterIDs := make([]string, 0, len(meters))
	for id := range meters {
		allMeterIDs = append(allMeterIDs, id)
	}

	// Step 1: filter to meters with actual data in the period
	distinctFilter := *params
	distinctFilter.MeterIDs = allMeterIDs
	activeMeterIDs, err := s.repo.GetDistinctMeterIDs(ctx, &distinctFilter)
	if err != nil {
		// Non-fatal: log and proceed with all meters rather than returning zero results
		s.logger.WarnwCtx(ctx, "GetDistinctMeterIDs failed, proceeding with all meters", "error", err)
		activeMeterIDs = allMeterIDs
	}
	if len(activeMeterIDs) == 0 {
		return result, nil
	}
	activeSet := make(map[string]bool, len(activeMeterIDs))
	for _, id := range activeMeterIDs {
		activeSet[id] = true
	}

	// Step 2: split bucketed vs regular
	bucketedMeters := make(map[string]*meter.Meter)
	aggTypeToMeterIDs := make(map[types.AggregationType][]string)
	for _, id := range activeMeterIDs {
		m, ok := meters[id]
		if !ok {
			continue
		}
		if m.IsBucketedMaxMeter() || m.IsBucketedSumMeter() {
			bucketedMeters[id] = m
		} else {
			aggTypeToMeterIDs[m.Aggregation.Type] = append(aggTypeToMeterIDs[m.Aggregation.Type], id)
		}
	}

	// Step 3: batch query regular meters grouped by aggregation type
	for aggType, meterIDs := range aggTypeToMeterIDs {
		batchParams := *params
		batchParams.MeterIDs = meterIDs
		batchParams.AggregationType = aggType

		results, err := s.repo.GetUsageMultiMeter(ctx, &batchParams)
		if err != nil {
			return nil, fmt.Errorf("GetUsageMultiMeter failed for agg_type %s: %w", aggType, err)
		}
		for _, r := range results {
			result.Regular[r.MeterID] = r
		}
	}

	// Step 4: query bucketed meters individually (each has its own BucketSize/GroupBy config)
	for id, m := range bucketedMeters {
		bucketParams := *params
		bucketParams.MeterID = id
		bucketParams.MeterIDs = nil
		bucketParams.AggregationType = m.Aggregation.Type
		bucketParams.WindowSize = types.WindowSize(m.Aggregation.BucketSize)
		if m.IsBucketedMaxMeter() {
			bucketParams.GroupByProperty = m.Aggregation.GroupBy
		}

		r, err := s.repo.GetUsageForBucketedMeters(ctx, &bucketParams)
		if err != nil {
			s.logger.WarnwCtx(ctx, "GetUsageForBucketedMeters failed, skipping meter",
				"meter_id", id, "error", err)
			continue
		}
		result.Bucketed[id] = r
	}

	s.logger.DebugwCtx(ctx, "QueryUsageByMeters complete",
		"input_meters", len(meters),
		"active_meters", len(activeMeterIDs),
		"regular_results", len(result.Regular),
		"bucketed_results", len(result.Bucketed),
	)

	return result, nil
}
