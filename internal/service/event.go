package service

import (
	"context"
	"fmt"
	"sort"
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
	"github.com/shopspring/decimal"
)

type EventService interface {
	CreateEvent(ctx context.Context, createEventRequest *dto.IngestEventRequest) error
	BulkCreateEvents(ctx context.Context, createEventRequest *dto.BulkIngestEventRequest) error
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
	if err := createEventRequest.Validate(); err != nil {
		return err
	}

	event := createEventRequest.ToEvent(ctx)

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

// CreateBulkEvents creates multiple events in a single operation
func (s *eventService) BulkCreateEvents(ctx context.Context, events *dto.BulkIngestEventRequest) error {
	if len(events.Events) == 0 {
		return nil
	}

	// publish events to Kafka for downstream processing
	for _, event := range events.Events {
		if err := s.CreateEvent(ctx, event); err != nil {
			return err
		}
	}

	return nil
}

func (s *eventService) GetUsage(ctx context.Context, getUsageRequest *dto.GetUsageRequest) (*events.AggregationResult, error) {
	if err := getUsageRequest.Validate(); err != nil {
		return nil, err
	}

	result, err := s.eventRepo.GetUsage(ctx, getUsageRequest.ToUsageParams())
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *eventService) GetUsageByMeter(ctx context.Context, req *dto.GetUsageByMeterRequest) (*events.AggregationResult, error) {
	m, err := s.meterRepo.GetMeter(ctx, req.MeterID)
	if err != nil {
		return nil, err
	}
	getUsageRequest := dto.GetUsageRequest{
		ExternalCustomerID: req.ExternalCustomerID,
		CustomerID:         req.CustomerID,
		EventName:          m.EventName,
		PropertyName:       m.Aggregation.Field,
		AggregationType:    m.Aggregation.Type,
		StartTime:          req.StartTime,
		WindowSize:         req.WindowSize,
		EndTime:            req.EndTime,
		Filters:            req.Filters,
	}

	usage, err := s.GetUsage(ctx, &getUsageRequest)
	if err != nil {
		return nil, err
	}

	if m.ResetUsage == types.ResetUsageNever {
		getHistoricUsageRequest := getUsageRequest
		getHistoricUsageRequest.StartTime = time.Time{}
		getHistoricUsageRequest.EndTime = req.StartTime
		getHistoricUsageRequest.WindowSize = ""

		historicUsage, err := s.GetUsage(ctx, &getHistoricUsageRequest)
		if err != nil {
			return nil, err
		}

		return s.combineResults(historicUsage, usage, m), nil
	}

	return usage, nil
}

func (s *eventService) GetUsageByMeterWithFilters(ctx context.Context, req *dto.GetUsageByMeterRequest, filterGroups map[string]map[string][]string) ([]*events.AggregationResult, error) {
	m, err := s.meterRepo.GetMeter(ctx, req.MeterID)
	if err != nil {
		return nil, err
	}

	meterFilters := make(map[string][]string)
	for _, filter := range m.Filters {
		meterFilters[filter.Key] = filter.Values
	}

	// Extract and sort priceIDs for stable ordering
	priceIDs := make([]string, 0, len(filterGroups))
	for priceID := range filterGroups {
		priceIDs = append(priceIDs, priceID)
	}
	sort.Strings(priceIDs)

	prioritizedGroups := make([]events.FilterGroup, 0, len(filterGroups))
	// Assign priority deterministically
	for i, priceID := range priceIDs {
		priority := len(priceIDs) - i
		prioritizedGroups = append(prioritizedGroups, events.FilterGroup{
			ID:       priceID,
			Priority: priority,
			Filters:  filterGroups[priceID],
		})
	}

	params := &events.UsageWithFiltersParams{
		UsageParams: &events.UsageParams{
			EventName:          m.EventName,
			PropertyName:       m.Aggregation.Field,
			AggregationType:    m.Aggregation.Type,
			ExternalCustomerID: req.ExternalCustomerID,
			StartTime:          req.StartTime,
			EndTime:            req.EndTime,
			Filters:            meterFilters,
		},
		FilterGroups: prioritizedGroups,
	}

	results, err := s.eventRepo.GetUsageWithFilters(ctx, params)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		s.logger.Debugw("no usage found for meter with filters",
			"meter_id", m.ID,
			"filter_groups", len(filterGroups))
		return results, nil
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
	// Set up pagination params
	var useOffsetPagination bool

	if req.IterFirstKey != "" || req.IterLastKey != "" {
		useOffsetPagination = false
	} else {
		useOffsetPagination = true
	}

	// Initialize pageSize from request
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}

	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	iterFirst, err := parseEventIteratorToStruct(req.IterFirstKey)
	if err != nil {
		return nil, err
	}

	iterLast, err := parseEventIteratorToStruct(req.IterLastKey)
	if err != nil {
		return nil, err
	}

	// Set up params for repository call
	params := &events.GetEventsParams{
		ExternalCustomerID: req.ExternalCustomerID,
		EventName:          req.EventName,
		EventID:            req.EventID,
		StartTime:          req.StartTime,
		EndTime:            req.EndTime,
		PropertyFilters:    req.PropertyFilters,
		CountTotal:         true,
	}

	if !useOffsetPagination {
		params.IterFirst = iterFirst
		params.IterLast = iterLast
		params.PageSize = pageSize + 1 // +1 to check for more results
	} else {
		params.PageSize = pageSize
		params.Offset = offset
	}

	eventsList, totalCount, err := s.eventRepo.GetEvents(ctx, params)
	if err != nil {
		return nil, err
	}

	response := &dto.GetEventsResponse{
		Events: make([]dto.Event, 0, len(eventsList)),
	}
	response.TotalCount = totalCount

	// Handle cursor-based pagination results
	if !useOffsetPagination {
		hasMore := len(eventsList) > pageSize
		if hasMore {
			eventsList = eventsList[:pageSize]
		}
		response.HasMore = hasMore

		if len(eventsList) > 0 {
			firstEvent := eventsList[0]
			lastEvent := eventsList[len(eventsList)-1]
			response.IterFirstKey = createEventIteratorKey(firstEvent.Timestamp, firstEvent.ID)

			if hasMore {
				response.IterLastKey = createEventIteratorKey(lastEvent.Timestamp, lastEvent.ID)
			}
		}
	} else {
		if len(eventsList) > 0 {
			response.IterFirstKey = createEventIteratorKey(eventsList[0].Timestamp, eventsList[0].ID)
			response.IterLastKey = createEventIteratorKey(eventsList[len(eventsList)-1].Timestamp, eventsList[len(eventsList)-1].ID)
		}

		if totalCount > uint64(pageSize) && uint64(offset+pageSize) < totalCount {
			response.HasMore = true
		}

		response.Offset = offset + pageSize
	}

	// Add events to response
	for _, event := range eventsList {
		response.Events = append(response.Events, dto.Event{
			ID:                 event.ID,
			ExternalCustomerID: event.ExternalCustomerID,
			CustomerID:         event.CustomerID,
			EventName:          event.EventName,
			Timestamp:          event.Timestamp,
			Properties:         event.Properties,
			Source:             event.Source,
		})
	}

	return response, nil
}

func parseEventIteratorToStruct(key string) (*events.EventIterator, error) {
	if key == "" {
		return nil, nil
	}

	parts := strings.Split(key, "::")
	if len(parts) != 2 {
		return nil, ierr.NewError("invalid cursor key format").
			WithHint("Invalid cursor key format").
			Mark(ierr.ErrValidation)
	}

	timestampNanoseconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid timestamp while parsing cursor").
			Mark(ierr.ErrValidation)
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
