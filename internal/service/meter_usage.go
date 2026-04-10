package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
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

	return s.repo.GetDetailedAnalytics(ctx, params)
}
