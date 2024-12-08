package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/errors"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
)

type EventService interface {
	CreateEvent(ctx context.Context, createEventRequest *dto.IngestEventRequest) error
	GetUsage(ctx context.Context, getUsageRequest *dto.GetUsageRequest) (*events.AggregationResult, error)
	GetUsageByMeter(ctx context.Context, getUsageByMeterRequest *dto.GetUsageByMeterRequest) (*events.AggregationResult, error)
	GetEvents(ctx context.Context, req *dto.GetEventsRequest) (*dto.GetEventsResponse, error)
}

type eventService struct {
	producer  kafka.MessageProducer
	eventRepo events.Repository
	meterRepo meter.Repository
	validator *validator.Validate
}

func NewEventService(producer kafka.MessageProducer, eventRepo events.Repository, meterRepo meter.Repository) EventService {
	return &eventService{
		producer:  producer,
		eventRepo: eventRepo,
		meterRepo: meterRepo,
		validator: validator.New(),
	}
}

func (s *eventService) CreateEvent(ctx context.Context, createEventRequest *dto.IngestEventRequest) error {
	if err := s.validator.Struct(createEventRequest); err != nil {
		return fmt.Errorf("validation failed: %w", err)
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

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if err := s.producer.PublishWithID("events", payload, event.ID); err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	createEventRequest.EventID = event.ID
	return nil
}

func (s *eventService) GetUsage(ctx context.Context, getUsageRequest *dto.GetUsageRequest) (*events.AggregationResult, error) {
	if getUsageRequest.AggregationType == "" || getUsageRequest.PropertyName == "" {
		getUsageRequest.AggregationType = string(types.AggregationCount)
	}

	result, err := s.eventRepo.GetUsage(
		ctx,
		&events.UsageParams{
			ExternalCustomerID: getUsageRequest.ExternalCustomerID,
			EventName:          getUsageRequest.EventName,
			PropertyName:       getUsageRequest.PropertyName,
			AggregationType:    types.AggregationType(getUsageRequest.AggregationType),
			WindowSize:         getUsageRequest.WindowSize,
			StartTime:          getUsageRequest.StartTime,
			EndTime:            getUsageRequest.EndTime,
			Filters:            getUsageRequest.Filters,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get usage: %w", err)
	}

	return result, nil
}

func (s *eventService) GetUsageByMeter(ctx context.Context, getUsageByMeterRequest *dto.GetUsageByMeterRequest) (*events.AggregationResult, error) {
	meter, err := s.meterRepo.GetMeter(ctx, getUsageByMeterRequest.MeterID)
	if err != nil {
		return nil, errors.NewAttributeNotFoundError("meter")
	}

	switch meter.ResetUsage {
	case types.ResetUsageBillingPeriod:
		return s.getUsageForBillingPeriod(ctx, meter, getUsageByMeterRequest)
	case types.ResetUsageNever:
		return s.getUsageForNeverReset(ctx, meter, getUsageByMeterRequest)
	default:
		return nil, errors.NewInvalidInputError("invalid reset_usage type")
	}
}

func (s *eventService) getUsageForBillingPeriod(ctx context.Context, meter *meter.Meter, req *dto.GetUsageByMeterRequest) (*events.AggregationResult, error) {
	return s.calculateUsage(ctx, meter, req.ExternalCustomerID, req.StartTime, req.EndTime)
}

func (s *eventService) getUsageForNeverReset(ctx context.Context, meter *meter.Meter, req *dto.GetUsageByMeterRequest) (*events.AggregationResult, error) {
	if req.StartTime.IsZero() && req.EndTime.IsZero() {
		return s.calculateUsage(ctx, meter, req.ExternalCustomerID, req.StartTime, req.EndTime)
	}

	if !req.StartTime.IsZero() && req.EndTime.IsZero() {
		beforeUsage, err := s.calculateUsage(ctx, meter, req.ExternalCustomerID, time.Time{}, req.StartTime)
		if err != nil {
			return nil, fmt.Errorf("calculate before usage: %w", err)
		}

		afterUsage, err := s.calculateUsage(ctx, meter, req.ExternalCustomerID, req.StartTime, time.Time{})
		if err != nil {
			return nil, fmt.Errorf("calculate after usage: %w", err)
		}

		return s.combineResults(beforeUsage, afterUsage, meter), nil
	}

	if !req.StartTime.IsZero() && !req.EndTime.IsZero() {
		return s.calculateUsage(ctx, meter, req.ExternalCustomerID, req.StartTime, req.EndTime)
	}

	return nil, errors.NewInvalidInputError("invalid date range")
}

func (s *eventService) calculateUsage(ctx context.Context, meter *meter.Meter, externalCustomerID string, startTime, endTime time.Time) (*events.AggregationResult, error) {
	filterMap := make(map[string][]string)
	for _, filter := range meter.Filters {
		filterMap[filter.Key] = filter.Values
	}

	return s.GetUsage(ctx, &dto.GetUsageRequest{
		ExternalCustomerID: externalCustomerID,
		EventName:          meter.EventName,
		PropertyName:       meter.Aggregation.Field,
		AggregationType:    string(meter.Aggregation.Type),
		StartTime:          startTime,
		EndTime:            endTime,
		Filters:            filterMap,
	})
}

func (s *eventService) combineResults(beforeUsage, afterUsage *events.AggregationResult, meter *meter.Meter) *events.AggregationResult {
	var totalValue float64

	if beforeUsage != nil && beforeUsage.Value != nil {
		switch v := beforeUsage.Value.(type) {
		case float64:
			totalValue += v
		case int64:
			totalValue += float64(v)
		}
	}

	if afterUsage != nil && afterUsage.Value != nil {
		switch v := afterUsage.Value.(type) {
		case float64:
			totalValue += v
		case int64:
			totalValue += float64(v)
		}
	}

	return &events.AggregationResult{
		Value:     totalValue,
		EventName: meter.EventName,
		Type:      meter.Aggregation.Type,
	}
}

func (s *eventService) GetEvents(ctx context.Context, req *dto.GetEventsRequest) (*dto.GetEventsResponse, error) {
	if req.PageSize <= 0 || req.PageSize > 50 {
		req.PageSize = 50
	}

	iterFirst, err := parseEventIteratorToStruct(req.IterFirstKey)
	if err != nil {
		return nil, fmt.Errorf("invalid before cursor key: %w", err)
	}

	iterLast, err := parseEventIteratorToStruct(req.IterLastKey)
	if err != nil {
		return nil, fmt.Errorf("invalid after cursor key: %w", err)
	}

	eventsList, err := s.eventRepo.GetEvents(ctx, &events.GetEventsParams{
		ExternalCustomerID: req.ExternalCustomerID,
		EventName:          req.EventName,
		StartTime:          req.StartTime,
		EndTime:            req.EndTime,
		IterFirst:          iterFirst,
		IterLast:           iterLast,
		PageSize:           req.PageSize + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}

	hasMore := len(eventsList) > req.PageSize
	if hasMore {
		eventsList = eventsList[:req.PageSize]
	}

	response := &dto.GetEventsResponse{
		Events:  make([]dto.Event, len(eventsList)),
		HasMore: hasMore,
	}

	if len(eventsList) > 0 {
		firstEvent := eventsList[0]
		lastEvent := eventsList[len(eventsList)-1]
		response.IterFirstKey = createEventIteratorKey(firstEvent.Timestamp, firstEvent.ID)

		if hasMore {
			response.IterLastKey = createEventIteratorKey(lastEvent.Timestamp, lastEvent.ID)
		}
	}

	for i, event := range eventsList {
		response.Events[i] = dto.Event{
			ID:                 event.ID,
			ExternalCustomerID: event.ExternalCustomerID,
			CustomerID:         event.CustomerID,
			EventName:          event.EventName,
			Timestamp:          event.Timestamp,
			Properties:         event.Properties,
			Source:             event.Source,
		}
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
