package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/events/transform"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	workflowModels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
)

// RawEventsReprocessingService handles raw event reprocessing operations
type RawEventsReprocessingService interface {
	// ReprocessRawEvents reprocesses raw events with given parameters
	ReprocessRawEvents(ctx context.Context, params *events.ReprocessRawEventsParams) (*ReprocessRawEventsResult, error)

	// TriggerReprocessRawEventsWorkflow triggers a Temporal workflow to reprocess raw events asynchronously
	TriggerReprocessRawEventsWorkflow(ctx context.Context, req *ReprocessRawEventsRequest) (*workflowModels.TemporalWorkflowResult, error)
}

type rawEventsReprocessingService struct {
	ServiceParams
	rawEventRepo events.RawEventRepository
	pubSub       pubsub.PubSub
}

// ReprocessRawEventsResult contains the result of raw event reprocessing
type ReprocessRawEventsResult struct {
	TotalEventsFound          int `json:"total_events_found"`
	TotalEventsPublished      int `json:"total_events_published"`
	TotalEventsFailed         int `json:"total_events_failed"`
	TotalEventsDropped        int `json:"total_events_dropped"`        // Events that failed validation
	TotalTransformationErrors int `json:"total_transformation_errors"` // Events that errored during transformation
	ProcessedBatches          int `json:"processed_batches"`
}

// ReprocessRawEventsRequest represents the request to reprocess raw events
type ReprocessRawEventsRequest struct {
	ExternalCustomerIDs []string `json:"external_customer_ids"`
	EventNames          []string `json:"event_names"`
	StartDate           string   `json:"start_date" validate:"required"`
	EndDate             string   `json:"end_date" validate:"required"`
	BatchSize           int      `json:"batch_size"`
	EventIDs            []string `json:"event_ids"`
	UseUnprocessed      bool     `json:"use_unprocessed"`
}

// NewRawEventsReprocessingService creates a new raw events reprocessing service
func NewRawEventsReprocessingService(
	params ServiceParams,
) RawEventsReprocessingService {
	// Create a dedicated Kafka PubSub for raw events output
	pubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		"raw-events-reprocessing-producer",
	)
	if err != nil {
		params.Logger.Fatalw("failed to create pubsub for raw events reprocessing", "error", err)
	}

	return &rawEventsReprocessingService{
		ServiceParams: params,
		rawEventRepo:  params.RawEventRepo,
		pubSub:        pubSub,
	}
}

// ReprocessRawEvents reprocesses raw events with given parameters
func (s *rawEventsReprocessingService) ReprocessRawEvents(ctx context.Context, params *events.ReprocessRawEventsParams) (*ReprocessRawEventsResult, error) {
	s.Logger.Infow("starting raw event reprocessing",
		"external_customer_ids", params.ExternalCustomerIDs,
		"event_names", params.EventNames,
		"start_time", params.StartTime,
		"end_time", params.EndTime,
	)

	// Set default batch size if not provided
	batchSize := params.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	// Create find params from reprocess params
	findParams := &events.FindRawEventsParams{
		ExternalCustomerIDs: params.ExternalCustomerIDs,
		EventNames:          params.EventNames,
		StartTime:           params.StartTime,
		EndTime:             params.EndTime,
		BatchSize:           batchSize,
		EventIDs:            params.EventIDs,
	}

	// Process in batches to avoid memory issues with large datasets
	result := &ReprocessRawEventsResult{}
	offset := 0

	// Keep processing batches until we're done
	for {
		// Update offset for next batch
		findParams.Offset = offset

		// Find raw events using the appropriate method
		var rawEvents []*events.RawEvent
		var err error
		if params.UseUnprocessed {
			rawEvents, err = s.rawEventRepo.FindUnprocessedRawEvents(ctx, findParams)
		} else {
			rawEvents, err = s.rawEventRepo.FindRawEvents(ctx, findParams)
		}
		if err != nil {
			return result, ierr.WithError(err).
				WithHint("Failed to find raw events").
				WithReportableDetails(map[string]interface{}{
					"external_customer_ids": params.ExternalCustomerIDs,
					"event_names":           params.EventNames,
					"batch":                 result.ProcessedBatches,
				}).
				Mark(ierr.ErrDatabase)
		}

		eventsCount := len(rawEvents)
		result.TotalEventsFound += eventsCount
		s.Logger.Infow("found raw events",
			"batch", result.ProcessedBatches,
			"count", eventsCount,
			"total_found", result.TotalEventsFound,
		)

		// If no more events, we're done
		if eventsCount == 0 {
			break
		}

		// Track dropped and errored events for this batch
		batchDropped := 0
		batchTransformErrors := 0
		batchPublished := 0
		batchPublishFailed := 0

		// Transform each event individually to track which ones fail
		for i, rawEvent := range rawEvents {
			// Transform the event
			transformedEvent, err := transform.TransformBentoToEvent(rawEvent.Payload, rawEvent.TenantID, rawEvent.EnvironmentID)

			if err != nil {
				// Transformation error (parsing/processing error)
				batchTransformErrors++
				result.TotalTransformationErrors++
				s.Logger.Warnw("transformation error - event skipped",
					"raw_event_id", rawEvent.ID,
					"external_customer_id", rawEvent.ExternalCustomerID,
					"event_name", rawEvent.EventName,
					"timestamp", rawEvent.Timestamp,
					"batch", result.ProcessedBatches,
					"batch_position", i+1,
					"error", err.Error(),
				)
				continue
			}

			if transformedEvent == nil {
				// Event failed validation and was dropped
				batchDropped++
				result.TotalEventsDropped++
				s.Logger.Infow("validation failed - event dropped",
					"raw_event_id", rawEvent.ID,
					"external_customer_id", rawEvent.ExternalCustomerID,
					"event_name", rawEvent.EventName,
					"timestamp", rawEvent.Timestamp,
					"batch", result.ProcessedBatches,
					"batch_position", i+1,
					"reason", "failed_bento_validation",
				)
				continue
			}

			// Publish the transformed event
			if err := s.publishEvent(ctx, transformedEvent); err != nil {
				batchPublishFailed++
				result.TotalEventsFailed++
				s.Logger.Errorw("failed to publish transformed event",
					"raw_event_id", rawEvent.ID,
					"transformed_event_id", transformedEvent.ID,
					"event_name", transformedEvent.EventName,
					"external_customer_id", transformedEvent.ExternalCustomerID,
					"timestamp", transformedEvent.Timestamp,
					"batch", result.ProcessedBatches,
					"batch_position", i+1,
					"error", err.Error(),
				)
				continue
			}

			batchPublished++
			result.TotalEventsPublished++
		}

		// Log batch summary
		s.Logger.Infow("batch processing complete",
			"batch", result.ProcessedBatches,
			"batch_size", eventsCount,
			"batch_published", batchPublished,
			"batch_dropped", batchDropped,
			"batch_transform_errors", batchTransformErrors,
			"batch_publish_failed", batchPublishFailed,
			"total_found", result.TotalEventsFound,
			"total_published", result.TotalEventsPublished,
			"total_dropped", result.TotalEventsDropped,
			"total_transform_errors", result.TotalTransformationErrors,
			"total_failed", result.TotalEventsFailed,
		)

		// Update for next batch
		result.ProcessedBatches++
		offset += eventsCount // Move offset by the number of rows we actually got

		// If we didn't get a full batch, we're done
		if eventsCount < batchSize {
			break
		}
	}

	// Calculate success rate
	successRate := 0.0
	if result.TotalEventsFound > 0 {
		successRate = float64(result.TotalEventsPublished) / float64(result.TotalEventsFound) * 100
	}

	s.Logger.Infow("completed raw event reprocessing",
		"external_customer_ids", params.ExternalCustomerIDs,
		"event_names", params.EventNames,
		"batches_processed", result.ProcessedBatches,
		"total_events_found", result.TotalEventsFound,
		"total_events_published", result.TotalEventsPublished,
		"total_events_dropped", result.TotalEventsDropped,
		"total_transformation_errors", result.TotalTransformationErrors,
		"total_events_failed", result.TotalEventsFailed,
		"success_rate_percent", fmt.Sprintf("%.2f", successRate),
	)

	return result, nil
}

// publishEvent publishes an event to the raw events output topic (prod_events_v4)
func (s *rawEventsReprocessingService) publishEvent(ctx context.Context, event *events.Event) error {
	// Create message payload
	payload, err := json.Marshal(event)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal event for raw events reprocessing").
			Mark(ierr.ErrValidation)
	}

	// Create a deterministic partition key based on tenant_id and external_customer_id
	partitionKey := event.TenantID
	if event.ExternalCustomerID != "" {
		partitionKey = fmt.Sprintf("%s:%s", event.TenantID, event.ExternalCustomerID)
	}

	// Make UUID truly unique by adding nanosecond precision timestamp and random bytes
	uniqueID := fmt.Sprintf("%s-%d-%d", event.ID, time.Now().UnixNano(), rand.Int63())

	msg := message.NewMessage(uniqueID, payload)

	// Set metadata for additional context
	msg.Metadata.Set("tenant_id", event.TenantID)
	msg.Metadata.Set("environment_id", event.EnvironmentID)
	msg.Metadata.Set("partition_key", partitionKey)

	topic := s.Config.RawEventsReprocessing.OutputTopic

	s.Logger.Debugw("publishing transformed event to kafka",
		"event_id", event.ID,
		"event_name", event.EventName,
		"partition_key", partitionKey,
		"topic", topic,
	)

	// Publish to Kafka
	if err := s.pubSub.Publish(ctx, topic, msg); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to publish transformed event to Kafka").
			Mark(ierr.ErrSystem)
	}

	return nil
}

// TriggerReprocessRawEventsWorkflow triggers a Temporal workflow to reprocess raw events asynchronously
func (s *rawEventsReprocessingService) TriggerReprocessRawEventsWorkflow(ctx context.Context, req *ReprocessRawEventsRequest) (*workflowModels.TemporalWorkflowResult, error) {
	// Parse dates
	startDate, err := time.Parse(time.RFC3339, req.StartDate)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid start_date format. Use RFC3339 format (e.g., 2006-01-02T15:04:05Z07:00)").
			Mark(ierr.ErrValidation)
	}

	endDate, err := time.Parse(time.RFC3339, req.EndDate)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid end_date format. Use RFC3339 format (e.g., 2006-01-02T15:04:05Z07:00)").
			Mark(ierr.ErrValidation)
	}

	// Validate dates
	if startDate.After(endDate) {
		return nil, ierr.NewError("start_date must be before end_date").
			WithHint("Start date must be before end date").
			Mark(ierr.ErrValidation)
	}

	// Build workflow input
	workflowInput := map[string]interface{}{
		"external_customer_ids": req.ExternalCustomerIDs,
		"event_names":           req.EventNames,
		"start_date":            req.StartDate,
		"end_date":              req.EndDate,
		"batch_size":            req.BatchSize,
		"event_ids":             req.EventIDs,
		"use_unprocessed":       req.UseUnprocessed,
	}

	// Get global temporal service
	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return nil, ierr.NewError("temporal service not available").
			WithHint("Reprocess raw events workflow requires Temporal service").
			Mark(ierr.ErrInternal)
	}

	// Execute workflow
	workflowRun, err := temporalSvc.ExecuteWorkflow(
		ctx,
		types.TemporalReprocessRawEventsWorkflow,
		workflowInput,
	)
	if err != nil {
		return nil, err
	}

	// Convert WorkflowRun to TemporalWorkflowResult
	return &workflowModels.TemporalWorkflowResult{
		Message:    "Raw events reprocessing workflow started successfully",
		WorkflowID: workflowRun.GetID(),
		RunID:      workflowRun.GetRunID(),
	}, nil
}
