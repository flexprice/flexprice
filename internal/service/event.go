package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/publisher"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
	"github.com/shopspring/decimal"
)

type EventService interface {
	CreateEvent(ctx context.Context, createEventRequest *dto.IngestEventRequest) error
	GetUsage(ctx context.Context, getUsageRequest *dto.GetUsageRequest) (*events.AggregationResult, error)
	GetUsageByMeter(ctx context.Context, getUsageByMeterRequest *dto.GetUsageByMeterRequest) (*events.AggregationResult, error)
	GetUsageByMeterWithFilters(ctx context.Context, req *dto.GetUsageByMeterRequest, filterGroups map[string]map[string][]string) ([]*events.AggregationResult, error)
	GetEvents(ctx context.Context, req *dto.GetEventsRequest) (*dto.GetEventsResponse, error)
}

type eventService struct {
	eventRepo events.Repository
	meterRepo meter.Repository
	publisher publisher.EventPublisher
	logger    *logger.Logger
}

func NewEventService(
	eventRepo events.Repository,
	meterRepo meter.Repository,
	publisher publisher.EventPublisher,
	logger *logger.Logger,
) EventService {
	return &eventService{
		eventRepo: eventRepo,
		meterRepo: meterRepo,
		publisher: publisher,
		logger:    logger,
	}
}

func (s *eventService) CreateEvent(ctx context.Context, createEventRequest *dto.IngestEventRequest) error {
	if err := validator.New().Struct(createEventRequest); err != nil {
		return ierr.WithError(err).
			WithHint("Invalid event format").
			Mark(ierr.ErrValidation)
	}

	tenantID := types.GetTenantID(ctx)
	event := events.NewEvent(
		createEventRequest.EventName,
		tenantID,
		createEventRequest.ExternalCustomerID,
		createEventRequest.Properties,
		createEventRequest.Timestamp,
		createEventRequest.EventID,
		createEventRequest.CustomerID,
		createEventRequest.Source,
	)

	if err := s.publisher.Publish(ctx, event); err != nil {
		// Log the error but don't fail the request
		s.logger.With(
			"event_id", event.ID,
			"error", err,
		).Error("failed to publish event")
	}

	createEventRequest.EventID = event.ID
	return nil
}

func (s *eventService) GetUsage(ctx context.Context, getUsageRequest *dto.GetUsageRequest) (*events.AggregationResult, error) {
	if err := getUsageRequest.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid usage request format").
			Mark(ierr.ErrValidation)
	}

	result, err := s.eventRepo.GetUsage(ctx, getUsageRequest.ToUsageParams())
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve usage data").
			WithReportableDetails(map[string]interface{}{
				"event_name":       getUsageRequest.EventName,
				"aggregation_type": getUsageRequest.AggregationType,
			}).
			Mark(ierr.ErrDatabase)
	}

	return result, nil
}

func (s *eventService) GetUsageByMeter(ctx context.Context, req *dto.GetUsageByMeterRequest) (*events.AggregationResult, error) {
	m, err := s.meterRepo.GetMeter(ctx, req.MeterID)
	if err != nil {
		s.logger.Errorf("failed to get meter: %v", err)
		return nil, ierr.WithError(err).
			WithHint("Meter not found").
			WithReportableDetails(map[string]interface{}{
				"meter_id": req.MeterID,
			}).
			Mark(ierr.ErrNotFound)
	}

	getUsageRequest := dto.GetUsageRequest{
		EventName:       m.EventName,
		AggregationType: m.Aggregation.Type,
		PropertyName:    m.Aggregation.Field,
		StartTime:       req.StartTime,
		EndTime:         req.EndTime,
		WindowSize:      req.WindowSize,
	}

	// For historical usage, we need to get both historical and current usage
	if req.IncludeHistorical && !req.StartTime.IsZero() {
		historicalResult, err := s.GetUsage(ctx, &getUsageRequest)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to retrieve historical usage data").
				WithReportableDetails(map[string]interface{}{
					"meter_id": req.MeterID,
				}).
				Mark(ierr.ErrDatabase)
		}

		// If we have current usage, combine them
		if req.CurrentStartTime != nil && req.CurrentEndTime != nil {
			currentRequest := getUsageRequest
			currentRequest.StartTime = *req.CurrentStartTime
			currentRequest.EndTime = *req.CurrentEndTime

			currentResult, err := s.GetUsage(ctx, &currentRequest)
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to retrieve current usage data").
					WithReportableDetails(map[string]interface{}{
						"meter_id": req.MeterID,
					}).
					Mark(ierr.ErrDatabase)
			}

			return s.combineResults(historicalResult, currentResult, m), nil
		}

		return historicalResult, nil
	}

	// Regular usage query
	return s.GetUsage(ctx, &getUsageRequest)
}

func (s *eventService) GetUsageByMeterWithFilters(ctx context.Context, req *dto.GetUsageByMeterRequest, filterGroups map[string]map[string][]string) ([]*events.AggregationResult, error) {
	m, err := s.meterRepo.GetMeter(ctx, req.MeterID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Meter not found").
			WithReportableDetails(map[string]interface{}{
				"meter_id": req.MeterID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Convert filter groups to the format expected by the repository
	convertedFilterGroups := make([]events.FilterGroup, 0, len(filterGroups))
	for id, filters := range filterGroups {
		convertedFilterGroups = append(convertedFilterGroups, events.FilterGroup{
			ID:      id,
			Filters: filters,
		})
	}

	params := &events.UsageWithFiltersParams{
		UsageParams: &events.UsageParams{
			EventName:       m.EventName,
			AggregationType: m.Aggregation.Type,
			PropertyName:    m.Aggregation.Field,
			StartTime:       req.StartTime,
			EndTime:         req.EndTime,
			WindowSize:      req.WindowSize,
		},
		FilterGroups: convertedFilterGroups,
	}

	results, err := s.eventRepo.GetUsageWithFilters(ctx, params)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve usage data with filters").
			WithReportableDetails(map[string]interface{}{
				"meter_id": req.MeterID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return results, nil
}

func (s *eventService) combineResults(historicUsage, currentUsage *events.AggregationResult, m *meter.Meter) *events.AggregationResult {
	var totalValue decimal.Decimal

	if historicUsage != nil {
		totalValue = totalValue.Add(historicUsage.Value)
	}

	if currentUsage != nil {
		totalValue = totalValue.Add(currentUsage.Value)
	}

	return &events.AggregationResult{
		Value:     totalValue,
		Results:   currentUsage.Results,
		EventName: m.EventName,
		Type:      m.Aggregation.Type,
	}
}

func (s *eventService) GetEvents(ctx context.Context, req *dto.GetEventsRequest) (*dto.GetEventsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Convert DTO to repository params
	params := &events.GetEventsParams{
		EventName:          req.EventName,
		ExternalCustomerID: req.ExternalCustomerID,
		StartTime:          req.StartTime,
		EndTime:            req.EndTime,
		PageSize:           req.PageSize,
		EventID:            req.EventID,
	}

	// Handle pagination
	if req.NextCursor != "" {
		params.IterFirst = &req.NextCursor
	}

	// Get events from repository
	eventsList, err := s.eventRepo.GetEvents(ctx, params)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve events").
			WithReportableDetails(map[string]interface{}{
				"event_name": req.EventName,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Build response
	response := &dto.GetEventsResponse{
		Events: make([]dto.EventResponse, 0, len(eventsList)),
	}

	// Convert events to DTO
	for _, event := range eventsList {
		response.Events = append(response.Events, dto.EventResponse{
			ID:                 event.ID,
			EventName:          event.EventName,
			ExternalCustomerID: event.ExternalCustomerID,
			CustomerID:         event.CustomerID,
			Timestamp:          event.Timestamp,
			Source:             event.Source,
			Properties:         event.Properties,
		})
	}

	// Set pagination cursors
	if len(eventsList) > 0 {
		lastEvent := eventsList[len(eventsList)-1]
		response.NextCursor = createEventIteratorKey(lastEvent.Timestamp, lastEvent.ID)
	}

	return response, nil
}

func parseEventIteratorToStruct(key string) (*events.EventIterator, error) {
	if key == "" {
		return nil, nil
	}

	parts := strings.Split(key, "::")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid cursor key format")
	}

	timestampNanoseconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp while parsing cursor: %w", err)
	}

	timestamp := time.Unix(0, timestampNanoseconds)

	return &events.EventIterator{
		Timestamp: timestamp,
		ID:        parts[1],
	}, nil
}

func createEventIteratorKey(timestamp time.Time, id string) string {
	return fmt.Sprintf("%d::%s", timestamp.UnixNano(), id)
}
