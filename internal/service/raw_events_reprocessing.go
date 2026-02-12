package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/events/transform"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/sentry"
	workflowModels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
)

// RawEventsService handles both raw event consumption from Kafka and reprocessing operations
type RawEventsService interface {
	// RegisterHandler registers the raw event consumption handler with the router
	RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration)

	// ReprocessRawEvents reprocesses raw events with given parameters
	ReprocessRawEvents(ctx context.Context, params *events.ReprocessRawEventsParams) (*ReprocessRawEventsResult, error)

	// TriggerReprocessRawEventsWorkflow triggers a Temporal workflow to reprocess raw events asynchronously
	TriggerReprocessRawEventsWorkflow(ctx context.Context, req *ReprocessRawEventsRequest) (*workflowModels.TemporalWorkflowResult, error)
}

type rawEventsService struct {
	ServiceParams
	// From raw_event_consumption
	pubSub        pubsub.PubSub // Consumer for raw_events topic
	outputPubSub  pubsub.PubSub // Producer for transformed events (consumption)
	sentryService *sentry.Service
	// From raw_events_reprocessing
	rawEventRepo    events.RawEventRepository
	reprocessPubSub pubsub.PubSub // Producer for reprocessing
}

// RawEventBatch represents the batch structure from Bento
type RawEventBatch struct {
	Data          []json.RawMessage `json:"data"`
	TenantID      string            `json:"tenant_id"`
	EnvironmentID string            `json:"environment_id"`
}

// BulkEventBatch represents a batch of transformed events for bulk publishing
type BulkEventBatch struct {
	TenantID      string          `json:"tenant_id"`
	EnvironmentID string          `json:"environment_id"`
	Events        []*events.Event `json:"events"`
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
	ExternalCustomerID string `json:"external_customer_id"`
	EventName          string `json:"event_name"`
	StartDate          string `json:"start_date" validate:"required"`
	EndDate            string `json:"end_date" validate:"required"`
	BatchSize          int    `json:"batch_size"`
}

// NewRawEventsService creates a new unified raw events service
func NewRawEventsService(
	params ServiceParams,
	sentryService *sentry.Service,
) RawEventsService {
	ev := &rawEventsService{
		ServiceParams: params,
		sentryService: sentryService,
		rawEventRepo:  params.RawEventRepo,
	}

	// Consumer pubsub for raw_events topic (consumption)
	pubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.RawEventConsumption.ConsumerGroup,
	)
	if err != nil {
		params.Logger.Fatalw("failed to create pubsub for raw event consumption", "error", err)
		return nil
	}
	ev.pubSub = pubSub

	// Output pubsub for publishing transformed events (consumption)
	outputPubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		"raw-event-consumption-producer",
	)
	if err != nil {
		params.Logger.Fatalw("failed to create output pubsub for raw event consumption", "error", err)
		return nil
	}
	ev.outputPubSub = outputPubSub

	// Reprocess pubsub for publishing during reprocessing
	reprocessPubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		"raw-events-reprocessing-producer",
	)
	if err != nil {
		params.Logger.Fatalw("failed to create pubsub for raw events reprocessing", "error", err)
		return nil
	}
	ev.reprocessPubSub = reprocessPubSub

	return ev
}

// RegisterHandler registers the raw event consumption handler with the router
func (s *rawEventsService) RegisterHandler(
	router *pubsubRouter.Router,
	cfg *config.Configuration,
) {
	if !cfg.RawEventConsumption.Enabled {
		s.Logger.Infow("raw event consumption handler disabled by configuration")
		return
	}

	// Add throttle middleware to this specific handler
	throttle := middleware.NewThrottle(cfg.RawEventConsumption.RateLimit, time.Second)

	// Add the handler
	router.AddNoPublishHandler(
		"raw_event_consumption_handler",
		cfg.RawEventConsumption.Topic,
		s.pubSub,
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Infow("registered raw event consumption handler",
		"topic", cfg.RawEventConsumption.Topic,
		"rate_limit", cfg.RawEventConsumption.RateLimit,
	)
}

// processMessage processes a batch of raw events from Kafka
func (s *rawEventsService) processMessage(msg *message.Message) error {
	s.Logger.Debugw("processing raw event batch from message queue",
		"message_uuid", msg.UUID,
	)

	// Unmarshal the batch
	var batch RawEventBatch
	if err := json.Unmarshal(msg.Payload, &batch); err != nil {
		s.Logger.Errorw("failed to unmarshal raw event batch",
			"error", err,
			"payload", string(msg.Payload),
		)
		s.sentryService.CaptureException(err)
		return fmt.Errorf("non-retriable unmarshal error: %w", err)
	}

	s.Logger.Infow("processing raw event batch",
		"batch_size", len(batch.Data),
		"message_uuid", msg.UUID,
		"bulk_mode_enabled", s.Config.RawEventConsumption.BulkEvents.Enabled,
	)

	// Get tenant and environment IDs from batch payload (priority)
	// Fall back to config if not provided in batch
	tenantID := batch.TenantID
	if tenantID == "" {
		tenantID = s.Config.Billing.TenantID
	}

	environmentID := batch.EnvironmentID
	if environmentID == "" {
		environmentID = s.Config.Billing.EnvironmentID
	}

	s.Logger.Debugw("using tenant and environment context",
		"tenant_id", tenantID,
		"environment_id", environmentID,
		"source", func() string {
			if batch.TenantID != "" {
				return "batch_payload"
			}
			return "config"
		}(),
	)

	// Transform all events in the batch (reusable transformation logic)
	transformedEvents, skipCount, errorCount := s.transformBatchEvents(batch, tenantID, environmentID)

	// Route to appropriate processing mode based on configuration
	var processingErr error
	if s.Config.RawEventConsumption.BulkEvents.Enabled {
		// Bulk event mode: batch and publish as bulk payloads
		processingErr = s.processBulkEventMode(context.Background(), transformedEvents, tenantID, environmentID, skipCount, errorCount)
	} else {
		// Single event mode: publish each event individually (existing behavior)
		processingErr = s.processSingleEventMode(context.Background(), transformedEvents, skipCount, errorCount)
	}

	s.Logger.Infow("completed raw event batch processing",
		"batch_size", len(batch.Data),
		"transformed_count", len(transformedEvents),
		"skip_count", skipCount,
		"error_count", errorCount,
		"message_uuid", msg.UUID,
		"bulk_mode", s.Config.RawEventConsumption.BulkEvents.Enabled,
	)

	return processingErr
}

// publishTransformedEvent publishes a transformed event to the events topic
func (s *rawEventsService) publishTransformedEvent(ctx context.Context, event *events.Event) error {
	// Create message payload
	payload, err := json.Marshal(event)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal event for publishing").
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

	// Publish to events topic (from raw_event_consumption config)
	topic := s.Config.RawEventConsumption.OutputTopic

	s.Logger.Debugw("publishing transformed event to kafka",
		"event_id", event.ID,
		"event_name", event.EventName,
		"partition_key", partitionKey,
		"topic", topic,
	)

	// Publish to Kafka using output pubsub
	if err := s.outputPubSub.Publish(ctx, topic, msg); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to publish transformed event to Kafka").
			Mark(ierr.ErrSystem)
	}

	return nil
}

// transformBatchEvents transforms all raw events in a batch (SHARED LOGIC)
// Returns successfully transformed events, skip count, and error count
func (s *rawEventsService) transformBatchEvents(
	batch RawEventBatch,
	tenantID, environmentID string,
) ([]*events.Event, int, int) {
	var transformedEvents []*events.Event
	skipCount := 0
	errorCount := 0

	for i, rawEventPayload := range batch.Data {
		// Transform the raw event using existing transformer
		transformedEvent, err := transform.TransformBentoToEvent(
			string(rawEventPayload),
			tenantID,
			environmentID,
		)

		if err != nil {
			// Transformation error
			errorCount++
			s.Logger.Warnw("transformation error - event skipped",
				"batch_position", i+1,
				"error", err.Error(),
			)
			continue
		}

		if transformedEvent == nil {
			// Event failed validation and was dropped
			skipCount++
			s.Logger.Debugw("validation failed - event dropped",
				"batch_position", i+1,
			)
			continue
		}

		// Add to transformed events collection
		transformedEvents = append(transformedEvents, transformedEvent)
	}

	return transformedEvents, skipCount, errorCount
}

// processSingleEventMode processes events one-by-one (existing behavior)
func (s *rawEventsService) processSingleEventMode(
	ctx context.Context,
	transformedEvents []*events.Event,
	skipCount, errorCount int,
) error {
	successCount := 0
	publishFailures := 0

	// Publish each event individually to the single event output topic
	for i, transformedEvent := range transformedEvents {
		if err := s.publishTransformedEvent(ctx, transformedEvent); err != nil {
			publishFailures++
			s.Logger.Errorw("failed to publish transformed event",
				"event_id", transformedEvent.ID,
				"event_name", transformedEvent.EventName,
				"external_customer_id", transformedEvent.ExternalCustomerID,
				"batch_position", i+1,
				"error", err.Error(),
			)
			continue
		}

		successCount++
		s.Logger.Debugw("successfully transformed and published event",
			"event_id", transformedEvent.ID,
			"event_name", transformedEvent.EventName,
			"batch_position", i+1,
		)
	}

	s.Logger.Infow("single event mode processing complete",
		"success_count", successCount,
		"publish_failures", publishFailures,
	)

	// Return error if any events failed publishing (causes batch retry)
	totalErrors := errorCount + publishFailures
	if totalErrors > 0 {
		return fmt.Errorf("failed to process %d events in batch, retrying entire batch", totalErrors)
	}

	return nil
}

// processBulkEventMode batches events and publishes as bulk payloads
func (s *rawEventsService) processBulkEventMode(
	ctx context.Context,
	transformedEvents []*events.Event,
	tenantID, environmentID string,
	skipCount, errorCount int,
) error {
	batchSize := s.Config.RawEventConsumption.BulkEvents.BatchSize
	if batchSize <= 0 {
		batchSize = 100 // Default batch size
	}

	s.Logger.Infow("bulk event mode enabled",
		"total_events", len(transformedEvents),
		"batch_size", batchSize,
		"output_topic", s.Config.RawEventConsumption.BulkEvents.OutputTopic,
	)

	// Split transformed events into batches
	bulkBatchCount := 0
	publishFailures := 0

	for i := 0; i < len(transformedEvents); i += batchSize {
		end := i + batchSize
		if end > len(transformedEvents) {
			end = len(transformedEvents)
		}

		// Create bulk event batch
		bulkBatch := &BulkEventBatch{
			TenantID:      tenantID,
			EnvironmentID: environmentID,
			Events:        transformedEvents[i:end],
		}

		// Publish bulk batch
		if err := s.publishBulkEventBatch(ctx, bulkBatch, bulkBatchCount); err != nil {
			publishFailures++
			s.Logger.Errorw("failed to publish bulk event batch",
				"bulk_batch_index", bulkBatchCount,
				"bulk_batch_size", len(bulkBatch.Events),
				"error", err.Error(),
			)
			continue
		}

		s.Logger.Infow("successfully published bulk event batch",
			"bulk_batch_index", bulkBatchCount,
			"bulk_batch_size", len(bulkBatch.Events),
		)

		bulkBatchCount++
	}

	s.Logger.Infow("bulk event mode processing complete",
		"bulk_batches_published", bulkBatchCount,
		"bulk_batches_failed", publishFailures,
		"total_events_in_batches", len(transformedEvents),
	)

	// Return error if any bulk batches failed (causes retry)
	totalErrors := errorCount + publishFailures
	if totalErrors > 0 {
		return fmt.Errorf("failed to process %d events/batches, retrying entire batch", totalErrors)
	}

	return nil
}

// publishBulkEventBatch publishes a batch of events as a single bulk message
func (s *rawEventsService) publishBulkEventBatch(
	ctx context.Context,
	batch *BulkEventBatch,
	batchIndex int,
) error {
	// Create message payload
	payload, err := json.Marshal(batch)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal bulk event batch for publishing").
			Mark(ierr.ErrValidation)
	}

	// Create a deterministic partition key based on tenant_id
	partitionKey := batch.TenantID

	// Make UUID unique by adding timestamp and batch index
	uniqueID := fmt.Sprintf("bulk-%s-%d-%d", batch.TenantID, time.Now().UnixNano(), batchIndex)

	msg := message.NewMessage(uniqueID, payload)

	// Set metadata for additional context
	msg.Metadata.Set("tenant_id", batch.TenantID)
	msg.Metadata.Set("environment_id", batch.EnvironmentID)
	msg.Metadata.Set("partition_key", partitionKey)
	msg.Metadata.Set("bulk_batch_size", fmt.Sprintf("%d", len(batch.Events)))

	topic := s.Config.RawEventConsumption.BulkEvents.OutputTopic

	s.Logger.Debugw("publishing bulk event batch to kafka",
		"bulk_batch_index", batchIndex,
		"bulk_batch_size", len(batch.Events),
		"partition_key", partitionKey,
		"topic", topic,
	)

	// Publish to Kafka using output pubsub
	if err := s.outputPubSub.Publish(ctx, topic, msg); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to publish bulk event batch to Kafka").
			Mark(ierr.ErrSystem)
	}

	return nil
}

// ReprocessRawEvents reprocesses raw events with given parameters
func (s *rawEventsService) ReprocessRawEvents(ctx context.Context, params *events.ReprocessRawEventsParams) (*ReprocessRawEventsResult, error) {
	s.Logger.Infow("starting raw event reprocessing",
		"external_customer_id", params.ExternalCustomerID,
		"event_name", params.EventName,
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
		ExternalCustomerID: params.ExternalCustomerID,
		EventName:          params.EventName,
		StartTime:          params.StartTime,
		EndTime:            params.EndTime,
		BatchSize:          batchSize,
	}

	// Process in batches to avoid memory issues with large datasets
	result := &ReprocessRawEventsResult{}
	offset := 0

	// Keep processing batches until we're done
	for {
		// Update offset for next batch
		findParams.Offset = offset

		// Find raw events
		rawEvents, err := s.rawEventRepo.FindRawEvents(ctx, findParams)
		if err != nil {
			return result, ierr.WithError(err).
				WithHint("Failed to find raw events").
				WithReportableDetails(map[string]interface{}{
					"external_customer_id": params.ExternalCustomerID,
					"event_name":           params.EventName,
					"batch":                result.ProcessedBatches,
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

		// Convert raw events to RawEventBatch format for transformation
		var rawEventPayloads []json.RawMessage
		var tenantID, environmentID string

		for _, rawEvent := range rawEvents {
			rawEventPayloads = append(rawEventPayloads, json.RawMessage(rawEvent.Payload))
			// Use the first event's tenant/environment (they should all be the same in a batch)
			if tenantID == "" {
				tenantID = rawEvent.TenantID
				environmentID = rawEvent.EnvironmentID
			}
		}

		batch := RawEventBatch{
			Data:          rawEventPayloads,
			TenantID:      tenantID,
			EnvironmentID: environmentID,
		}

		// Transform all events in the batch using shared transformation logic
		transformedEvents, skipCount, errorCount := s.transformBatchEvents(batch, tenantID, environmentID)

		// Update counters
		result.TotalEventsDropped += skipCount
		result.TotalTransformationErrors += errorCount

		// Publish transformed events using shared publishing logic
		// Check if bulk mode is enabled
		if s.Config.RawEventConsumption.BulkEvents.Enabled {
			// Use bulk publishing mode
			if err := s.processBulkEventMode(ctx, transformedEvents, tenantID, environmentID, skipCount, errorCount); err != nil {
				s.Logger.Errorw("failed to publish bulk events during reprocessing",
					"batch", result.ProcessedBatches,
					"error", err.Error(),
				)
				result.TotalEventsFailed += len(transformedEvents)
			} else {
				result.TotalEventsPublished += len(transformedEvents)
			}
		} else {
			// Use single event publishing mode
			if err := s.processSingleEventMode(ctx, transformedEvents, skipCount, errorCount); err != nil {
				s.Logger.Errorw("failed to publish events during reprocessing",
					"batch", result.ProcessedBatches,
					"error", err.Error(),
				)
				result.TotalEventsFailed += len(transformedEvents)
			} else {
				result.TotalEventsPublished += len(transformedEvents)
			}
		}

		// Log batch summary
		s.Logger.Infow("batch processing complete",
			"batch", result.ProcessedBatches,
			"batch_size", eventsCount,
			"batch_published", len(transformedEvents),
			"batch_dropped", skipCount,
			"batch_transform_errors", errorCount,
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
		"external_customer_id", params.ExternalCustomerID,
		"event_name", params.EventName,
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

// TriggerReprocessRawEventsWorkflow triggers a Temporal workflow to reprocess raw events asynchronously
func (s *rawEventsService) TriggerReprocessRawEventsWorkflow(ctx context.Context, req *ReprocessRawEventsRequest) (*workflowModels.TemporalWorkflowResult, error) {
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
		"external_customer_id": req.ExternalCustomerID,
		"event_name":           req.EventName,
		"start_date":           req.StartDate,
		"end_date":             req.EndDate,
		"batch_size":           req.BatchSize,
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
