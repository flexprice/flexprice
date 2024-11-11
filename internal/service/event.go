package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/repository/clickhouse"
	"github.com/flexprice/flexprice/internal/types"
)

type EventService interface {
	CreateEvent(ctx context.Context, createEventRequest *dto.IngestEventRequest) error
	GetUsage(ctx context.Context, getUsageRequest *dto.GetUsageRequest) (*events.AggregationResult, error)
}

type eventService struct {
	producer  *kafka.Producer
	eventRepo events.Repository
	meterRepo meter.Repository
}

func NewEventService(producer *kafka.Producer, eventRepo events.Repository, meterRepo meter.Repository) EventService {
	return &eventService{producer: producer, eventRepo: eventRepo, meterRepo: meterRepo}
}

func (s *eventService) CreateEvent(ctx context.Context, createEventRequest *dto.IngestEventRequest) error {
	// TODO: Remove this once we have a way to set the tenant ID
	tenantID := "default"

	event := events.NewEvent(
		createEventRequest.ID,
		tenantID,
		createEventRequest.ExternalCustomerID,
		createEventRequest.EventName,
		createEventRequest.Timestamp,
		createEventRequest.Properties,
	)

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if err := s.producer.PublishWithID("events", payload, event.ID); err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	return nil
}

func (s *eventService) GetUsage(ctx context.Context, getUsageRequest *dto.GetUsageRequest) (*events.AggregationResult, error) {
	// Get appropriate aggregator
	aggType := types.AggregationType(getUsageRequest.AggregationType)
	aggregator := clickhouse.GetAggregator(aggType)
	if aggregator == nil {
		return nil, fmt.Errorf("invalid aggregation type: %s", getUsageRequest.AggregationType)
	}

	result, err := s.eventRepo.GetUsage(
		ctx,
		&events.UsageParams{
			ExternalCustomerID: getUsageRequest.ExternalCustomerID,
			EventName:          getUsageRequest.EventName,
			PropertyName:       getUsageRequest.PropertyName,
			AggregationType:    aggType,
			StartTime:          getUsageRequest.StartTime,
			EndTime:            getUsageRequest.EndTime,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get usage: %w", err)
	}

	return result, nil
}