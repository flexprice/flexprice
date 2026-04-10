package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// MeterUsageService handles read-side meter usage queries.
// This sits between the API handler and MeterUsageRepository,
// keeping the handler HTTP/DTO-only.
type MeterUsageService interface {
	GetUsage(ctx context.Context, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error)
	GetUsageMultiMeter(ctx context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error)
	GetDetailedAnalytics(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams) ([]*events.MeterUsageDetailedResult, error)
}

type meterUsageService struct {
	repo      events.MeterUsageRepository
	meterRepo meter.Repository
	logger    *logger.Logger
}

func NewMeterUsageService(repo events.MeterUsageRepository, meterRepo meter.Repository, logger *logger.Logger) MeterUsageService {
	return &meterUsageService{
		repo:      repo,
		meterRepo: meterRepo,
		logger:    logger,
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

func (s *meterUsageService) GetDetailedAnalytics(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams) ([]*events.MeterUsageDetailedResult, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	// Set defaults
	if params.EndTime.IsZero() {
		params.EndTime = time.Now().UTC()
	}
	if params.StartTime.IsZero() {
		params.StartTime = params.EndTime.Add(-6 * time.Hour)
	}
	if len(params.GroupBy) == 0 {
		params.GroupBy = []string{"meter_id"}
	}

	// Fetch meter configs to identify bucketed meters
	meters, err := s.fetchMeters(ctx, params)
	if err != nil {
		return nil, err
	}

	// Build meter map and collect aggregation types
	meterMap := make(map[string]*meter.Meter, len(meters))
	var aggTypes []types.AggregationType
	for _, m := range meters {
		meterMap[m.ID] = m
		if m.Aggregation.Type != "" {
			aggTypes = append(aggTypes, m.Aggregation.Type)
		}
	}

	// Set aggregation types from meters if not explicitly provided
	if len(params.AggregationTypes) == 0 {
		params.AggregationTypes = lo.Uniq(aggTypes)
	}

	// Split meters into bucketed and standard
	var bucketedMaxMeterIDs, bucketedSumMeterIDs, standardMeterIDs []string
	for _, m := range meters {
		switch {
		case m.IsBucketedMaxMeter():
			bucketedMaxMeterIDs = append(bucketedMaxMeterIDs, m.ID)
		case m.IsBucketedSumMeter():
			bucketedSumMeterIDs = append(bucketedSumMeterIDs, m.ID)
		default:
			standardMeterIDs = append(standardMeterIDs, m.ID)
		}
	}

	var allResults []*events.MeterUsageDetailedResult

	// Process bucketed MAX meters
	for _, meterID := range bucketedMaxMeterIDs {
		m := meterMap[meterID]
		results, err := s.getBucketedMeterAnalytics(ctx, params, m)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
	}

	// Process bucketed SUM meters
	for _, meterID := range bucketedSumMeterIDs {
		m := meterMap[meterID]
		results, err := s.getBucketedMeterAnalytics(ctx, params, m)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
	}

	// Process standard meters via the detailed analytics query
	if len(standardMeterIDs) > 0 || len(params.MeterIDs) == 0 {
		standardParams := *params
		if len(standardMeterIDs) > 0 {
			standardParams.MeterIDs = standardMeterIDs
		}
		results, err := s.repo.GetDetailedAnalytics(ctx, &standardParams)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
	}

	return allResults, nil
}

// fetchMeters fetches meter configurations for the requested meter IDs.
// If no meter IDs are specified, fetches all meters for the tenant.
func (s *meterUsageService) fetchMeters(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams) ([]*meter.Meter, error) {
	filter := types.NewNoLimitMeterFilter()
	if len(params.MeterIDs) > 0 {
		filter.MeterIDs = params.MeterIDs
	}

	meters, err := s.meterRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch meters for detailed analytics").
			Mark(ierr.ErrDatabase)
	}

	// If no specific meter IDs were requested, populate params.MeterIDs from fetched meters
	if len(params.MeterIDs) == 0 {
		meterIDs := make([]string, len(meters))
		for i, m := range meters {
			meterIDs[i] = m.ID
		}
		params.MeterIDs = meterIDs
	}

	return meters, nil
}

// getBucketedMeterAnalytics handles analytics for a single bucketed meter (MAX or SUM with bucket_size).
// It uses GetUsageForBucketedMeters for the total, then fetches time-series points if windowed.
func (s *meterUsageService) getBucketedMeterAnalytics(
	ctx context.Context,
	params *events.MeterUsageDetailedAnalyticsParams,
	m *meter.Meter,
) ([]*events.MeterUsageDetailedResult, error) {
	// Build query params for the bucketed query
	bucketParams := &events.MeterUsageQueryParams{
		TenantID:           params.TenantID,
		EnvironmentID:      params.EnvironmentID,
		ExternalCustomerID: params.ExternalCustomerID,
		MeterID:            m.ID,
		StartTime:          params.StartTime,
		EndTime:            params.EndTime,
		AggregationType:    m.Aggregation.Type,
		WindowSize:         m.Aggregation.BucketSize, // use meter's bucket size for aggregation
		GroupByProperty:    m.Aggregation.GroupBy,
		UseFinal:           params.UseFinal,
		BillingAnchor:      params.BillingAnchor,
	}

	if len(params.ExternalCustomerIDs) > 0 {
		bucketParams.ExternalCustomerIDs = params.ExternalCustomerIDs
	}

	aggResult, err := s.repo.GetUsageForBucketedMeters(ctx, bucketParams)
	if err != nil {
		s.logger.Errorw("failed to get bucketed meter usage",
			"error", err,
			"meter_id", m.ID,
		)
		return nil, err
	}

	result := &events.MeterUsageDetailedResult{
		MeterID:    m.ID,
		Properties: make(map[string]string),
	}

	// Map the aggregation result based on meter type
	if m.IsBucketedMaxMeter() {
		result.MaxUsage = aggResult.Value
		result.TotalUsage = aggResult.Value // total = SUM(MAX per bucket)
	} else {
		result.TotalUsage = aggResult.Value
	}

	// Convert bucketed points to detailed points
	if params.WindowSize != "" && len(aggResult.Results) > 0 {
		points := make([]events.MeterUsageDetailedPoint, 0, len(aggResult.Results))
		for _, r := range aggResult.Results {
			p := events.MeterUsageDetailedPoint{
				WindowStart: r.WindowSize,
			}
			if m.IsBucketedMaxMeter() {
				p.MaxUsage = r.Value
				p.TotalUsage = r.Value
			} else {
				p.TotalUsage = r.Value
			}
			points = append(points, p)
		}
		result.Points = points
	}

	// Count events separately for bucketed meters (the bucketed query doesn't return event counts)
	eventCount, err := s.getEventCountForMeter(ctx, params, m.ID)
	if err != nil {
		s.logger.Warnw("failed to get event count for bucketed meter, defaulting to 0",
			"error", err,
			"meter_id", m.ID,
		)
	} else {
		result.EventCount = eventCount
	}

	return []*events.MeterUsageDetailedResult{result}, nil
}

// getEventCountForMeter fetches the event count for a specific meter using a simple scalar query.
func (s *meterUsageService) getEventCountForMeter(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams, meterID string) (uint64, error) {
	countParams := &events.MeterUsageQueryParams{
		TenantID:           params.TenantID,
		EnvironmentID:      params.EnvironmentID,
		ExternalCustomerID: params.ExternalCustomerID,
		MeterID:            meterID,
		StartTime:          params.StartTime,
		EndTime:            params.EndTime,
		AggregationType:    types.AggregationCount,
		UseFinal:           params.UseFinal,
	}

	if len(params.ExternalCustomerIDs) > 0 {
		countParams.ExternalCustomerIDs = params.ExternalCustomerIDs
	}

	result, err := s.repo.GetUsage(ctx, countParams)
	if err != nil {
		return 0, err
	}

	// For COUNT aggregation, TotalValue holds the count as a decimal
	return result.TotalValue.BigInt().Uint64(), nil
}

// ensure meterUsageService implements MeterUsageService
var _ MeterUsageService = (*meterUsageService)(nil)
