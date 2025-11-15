package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/pubsub/kafka"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/types"
)

// EventConsumptionService handles consuming raw events from Kafka and inserting them into ClickHouse
type EventConsumptionService interface {
	// Register message handler with the router
	RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration)

	// Register message handler with the router
	RegisterHandlerLazy(router *pubsubRouter.Router, cfg *config.Configuration)

	// Shutdown gracefully flushes remaining events and stops the service
	Shutdown(ctx context.Context) error
}

type eventConsumptionService struct {
	ServiceParams
	pubSub                 pubsub.PubSub
	lazyPubSub             pubsub.PubSub
	eventRepo              events.Repository
	sentryService          *sentry.Service
	eventPostProcessingSvc EventPostProcessingService

	// Batching fields
	batchMu       sync.Mutex
	batchBuffer   []*BatchMessage
	batchSize     int
	flushInterval time.Duration
	flushTicker   *time.Ticker
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// BatchMessage represents a message in the batch with its associated events and ACK/NACK functions
type BatchMessage struct {
	Message  *message.Message // Original Watermill message
	Events   []*events.Event  // Events to insert (original + billing if applicable)
	Context  context.Context  // Context with tenant and environment IDs
	AckFunc  func() error     // Function to acknowledge the message
	NackFunc func() error     // Function to negative-acknowledge the message
}

// NewEventConsumptionService creates a new event consumption service
func NewEventConsumptionService(
	params ServiceParams,
	eventRepo events.Repository,
	sentryService *sentry.Service,
	eventPostProcessingSvc EventPostProcessingService,
) EventConsumptionService {
	ev := &eventConsumptionService{
		ServiceParams:          params,
		eventRepo:              eventRepo,
		sentryService:          sentryService,
		eventPostProcessingSvc: eventPostProcessingSvc,
		batchSize:              params.Config.EventProcessingLazy.BatchSize,
		flushInterval:          time.Duration(params.Config.EventProcessingLazy.BatchFlushSeconds) * time.Second,
		batchBuffer:            make([]*BatchMessage, 0, params.Config.EventProcessingLazy.BatchSize),
		stopCh:                 make(chan struct{}),
	}

	pubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.EventProcessing.ConsumerGroup,
	)
	if err != nil {
		params.Logger.Fatalw("failed to create pubsub", "error", err)
		return nil
	}
	ev.pubSub = pubSub

	lazyPubSub, err := kafka.NewPubSubFromConfig(
		params.Config,
		params.Logger,
		params.Config.EventProcessingLazy.ConsumerGroup,
	)
	if err != nil {
		params.Logger.Fatalw("failed to create lazy pubsub", "error", err)
		return nil
	}
	ev.lazyPubSub = lazyPubSub

	// Start the background batch flusher
	ev.startBatchFlusher()

	params.Logger.Infow("initialized event consumption service with batching",
		"batch_size", ev.batchSize,
		"flush_interval", ev.flushInterval,
	)

	return ev
}

// RegisterHandler registers the event consumption handler with the router
func (s *eventConsumptionService) RegisterHandler(
	router *pubsubRouter.Router,
	cfg *config.Configuration,
) {
	// Add throttle middleware to this specific handler
	throttle := middleware.NewThrottle(cfg.EventProcessing.RateLimit, time.Second)

	// Add the handler
	router.AddNoPublishHandler(
		"event_consumption_handler",
		cfg.EventProcessing.Topic,
		s.pubSub,
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Infow("registered event consumption handler",
		"topic", cfg.EventProcessing.Topic,
		"rate_limit", cfg.EventProcessing.RateLimit,
	)
}

// RegisterHandler registers the event consumption handler with the router
func (s *eventConsumptionService) RegisterHandlerLazy(
	router *pubsubRouter.Router,
	cfg *config.Configuration,
) {
	// Add throttle middleware to this specific handler
	throttle := middleware.NewThrottle(cfg.EventProcessingLazy.RateLimit, time.Second)

	// Add the handler
	router.AddNoPublishHandler(
		"event_consumption_lazy_handler",
		cfg.EventProcessingLazy.Topic,
		s.lazyPubSub,
		s.processMessageLazy,
		throttle.Middleware,
	)

	s.Logger.Infow("registered event consumption lazy handler",
		"topic", cfg.EventProcessingLazy.Topic,
		"rate_limit", cfg.EventProcessingLazy.RateLimit,
	)
}

func (s *eventConsumptionService) processMessage(msg *message.Message) error {

	partitionKey := msg.Metadata.Get("partition_key")
	tenantID := msg.Metadata.Get("tenant_id")
	environmentID := msg.Metadata.Get("environment_id")

	s.Logger.Debugw("processing event from message queue",
		"message_uuid", msg.UUID,
		"partition_key", partitionKey,
		"tenant_id", tenantID,
		"environment_id", environmentID,
	)

	// Create a background context with tenant ID
	ctx := context.Background()
	if tenantID != "" {
		ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	}

	if environmentID != "" {
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	}

	// Unmarshal the event
	var event events.Event
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		s.Logger.Errorw("failed to unmarshal event",
			"error", err,
			"payload", string(msg.Payload),
		)
		s.sentryService.CaptureException(err)

		// Return error for non-retriable parse errors
		// Watermill's poison queue middleware will handle moving it to DLQ
		if !s.shouldRetryError(err) {
			return fmt.Errorf("non-retriable unmarshal error: %w", err)
		}
		return err
	}

	s.Logger.Debugw("processing event",
		"event_id", event.ID,
		"event_name", event.EventName,
		"tenant_id", event.TenantID,
		"timestamp", event.Timestamp,
	)

	// Prepare events to insert
	eventsToInsert := []*events.Event{&event}

	// Create billing event if configured
	if s.Config.Billing.TenantID != "" {
		billingEvent := events.NewEvent(
			"tenant_event", // Standardized event name for billing
			s.Config.Billing.TenantID,
			event.TenantID, // Use original tenant ID as external customer ID
			map[string]interface{}{
				"original_event_id":   event.ID,
				"original_event_name": event.EventName,
				"original_timestamp":  event.Timestamp,
				"tenant_id":           event.TenantID,
				"source":              event.Source,
			},
			time.Now(),
			"", // Customer ID will be looked up by external ID
			"", // Generate new ID
			"system",
			s.Config.Billing.EnvironmentID,
		)
		eventsToInsert = append(eventsToInsert, billingEvent)
	}

	// Insert events into ClickHouse
	if err := s.eventRepo.BulkInsertEvents(ctx, eventsToInsert); err != nil {
		s.Logger.Errorw("failed to insert events",
			"error", err,
			"event_id", event.ID,
			"event_name", event.EventName,
		)

		// Return error for retry
		return ierr.WithError(err).
			WithHint("Failed to insert events into ClickHouse").
			Mark(ierr.ErrSystem)
	}

	// Publish event to post-processing service
	// Only for the tenants that are forced to v1
	if s.Config.FeatureFlag.ForceV1ForTenant != "" && event.TenantID == s.Config.FeatureFlag.ForceV1ForTenant {
		if err := s.eventPostProcessingSvc.PublishEvent(ctx, &event, false); err != nil {
			s.Logger.Errorw("failed to publish event to post-processing service",
				"error", err,
				"event_id", event.ID,
				"event_name", event.EventName,
			)

			// Return error for retry
			return ierr.WithError(err).
				WithHint("Failed to publish event for post-processing").
				Mark(ierr.ErrSystem)
		}
	}

	s.Logger.Debugw("successfully processed event",
		"event_id", event.ID,
		"event_name", event.EventName,
		"lag_ms", time.Since(event.Timestamp).Milliseconds(),
	)

	return nil
}

// processMessage processes a single event message from Kafka
func (s *eventConsumptionService) processMessageLazy(msg *message.Message) error {

	partitionKey := msg.Metadata.Get("partition_key")
	tenantID := msg.Metadata.Get("tenant_id")
	environmentID := msg.Metadata.Get("environment_id")

	s.Logger.Debugw("processing event from message queue",
		"message_uuid", msg.UUID,
		"partition_key", partitionKey,
		"tenant_id", tenantID,
		"environment_id", environmentID,
	)

	// Create a background context with tenant ID
	ctx := context.Background()
	if tenantID != "" {
		ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	}

	if environmentID != "" {
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	}

	// Unmarshal the event
	var event events.Event
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		s.Logger.Errorw("failed to unmarshal event",
			"error", err,
			"payload", string(msg.Payload),
		)
		s.sentryService.CaptureException(err)

		// Return error for non-retriable parse errors
		// Watermill's poison queue middleware will handle moving it to DLQ
		if !s.shouldRetryError(err) {
			return fmt.Errorf("non-retriable unmarshal error: %w", err)
		}
		return err
	}

	s.Logger.Debugw("processing event",
		"event_id", event.ID,
		"event_name", event.EventName,
		"tenant_id", event.TenantID,
		"timestamp", event.Timestamp,
	)

	// Prepare events to add to batch
	eventsToAdd := []*events.Event{&event}

	// Create billing event if configured
	if s.Config.Billing.TenantID != "" {
		billingEvent := events.NewEvent(
			"tenant_event", // Standardized event name for billing
			s.Config.Billing.TenantID,
			event.TenantID, // Use original tenant ID as external customer ID
			map[string]interface{}{
				"original_event_id":   event.ID,
				"original_event_name": event.EventName,
				"original_timestamp":  event.Timestamp,
				"tenant_id":           event.TenantID,
				"source":              event.Source,
			},
			time.Now(),
			"", // Customer ID will be looked up by external ID
			"", // Generate new ID
			"system",
			s.Config.Billing.EnvironmentID,
		)
		eventsToAdd = append(eventsToAdd, billingEvent)
	}

	// Create BatchMessage with ACK/NACK functions
	// These closures capture the msg variable and allow us to ACK/NACK later
	batchMsg := &BatchMessage{
		Message: msg,
		Events:  eventsToAdd,
		Context: ctx,
		AckFunc: func() error {
			msg.Ack() // Call Watermill's ACK
			return nil
		},
		NackFunc: func() error {
			msg.Nack() // Call Watermill's NACK (negative ACK)
			return nil
		},
	}

	// Add to batch
	if err := s.addToBatch(batchMsg); err != nil {
		s.Logger.Errorw("failed to add message to batch",
			"error", err,
			"event_id", event.ID,
			"event_name", event.EventName,
		)
		// Return error for retry
		return ierr.WithError(err).
			WithHint("Failed to add message to batch").
			Mark(ierr.ErrSystem)
	}

	s.Logger.Debugw("successfully added event to batch",
		"event_id", event.ID,
		"event_name", event.EventName,
		"lag_ms", time.Since(event.Timestamp).Milliseconds(),
	)

	// Return nil to indicate handler processed the message
	// Manual ACK/NACK will happen later in flushBatchUnlocked
	return nil
}

// shouldRetryError determines if an error should trigger a message retry
func (s *eventConsumptionService) shouldRetryError(err error) bool {
	// Don't retry parsing errors which are not likely to succeed on retry
	errMsg := err.Error()
	if strings.Contains(errMsg, "unmarshal") ||
		strings.Contains(errMsg, "parse") ||
		strings.Contains(errMsg, "invalid") {
		return false
	}

	// Retry all other errors (database issues, network issues, etc.)
	return true
}

// startBatchFlusher starts a background goroutine that periodically flushes the batch
func (s *eventConsumptionService) startBatchFlusher() {
	s.flushTicker = time.NewTicker(s.flushInterval)
	s.wg.Add(1)

	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-s.flushTicker.C:
				s.Logger.Debugw("periodic batch flush triggered")
				if err := s.flushBatch(); err != nil {
					s.Logger.Errorw("failed to flush batch on timer", "error", err)
				}
			case <-s.stopCh:
				s.Logger.Infow("stopping batch flusher")
				s.flushTicker.Stop()
				return
			}
		}
	}()
}

// addToBatch adds a message to the batch buffer, flushing if batch size is reached
func (s *eventConsumptionService) addToBatch(batchMsg *BatchMessage) error {
	s.batchMu.Lock()
	defer s.batchMu.Unlock()

	// Add message to buffer
	s.batchBuffer = append(s.batchBuffer, batchMsg)

	s.Logger.Debugw("added message to batch",
		"batch_size", len(s.batchBuffer),
		"events_in_message", len(batchMsg.Events),
	)

	// Check if we should flush
	if len(s.batchBuffer) >= s.batchSize {
		s.Logger.Debugw("batch size reached, flushing",
			"batch_size", len(s.batchBuffer),
			"threshold", s.batchSize,
		)
		return s.flushBatchUnlocked()
	}

	return nil
}

// flushBatch flushes the current batch to ClickHouse (with locking)
func (s *eventConsumptionService) flushBatch() error {
	s.batchMu.Lock()
	defer s.batchMu.Unlock()

	return s.flushBatchUnlocked()
}

// flushBatchUnlocked flushes the current batch to ClickHouse (caller must hold lock)
func (s *eventConsumptionService) flushBatchUnlocked() error {
	if len(s.batchBuffer) == 0 {
		return nil
	}

	// Make a copy of the batch buffer to process
	batchMessages := make([]*BatchMessage, len(s.batchBuffer))
	copy(batchMessages, s.batchBuffer)

	// Extract all events from batch messages
	eventsToInsert := make([]*events.Event, 0)
	for _, batchMsg := range batchMessages {
		eventsToInsert = append(eventsToInsert, batchMsg.Events...)
	}

	s.Logger.Infow("flushing batch to ClickHouse",
		"batch_size", len(batchMessages),
		"total_events", len(eventsToInsert),
	)

	startTime := time.Now()

	// Insert events into ClickHouse
	ctx := context.Background()
	if err := s.eventRepo.BulkInsertEvents(ctx, eventsToInsert); err != nil {
		s.Logger.Errorw("failed to flush batch to ClickHouse",
			"error", err,
			"batch_size", len(batchMessages),
			"total_events", len(eventsToInsert),
		)
		s.sentryService.CaptureException(err)

		// NACK all messages in the batch so they can be retried
		var nackErrors int
		for _, batchMsg := range batchMessages {
			if err := batchMsg.NackFunc(); err != nil {
				s.Logger.Errorw("failed to NACK message",
					"error", err,
					"message_uuid", batchMsg.Message.UUID,
				)
				nackErrors++
			}
		}

		s.Logger.Errorw("NACKed all messages in failed batch",
			"batch_size", len(batchMessages),
			"nack_errors", nackErrors,
		)

		// Clear the buffer since we've handled all messages (NACKed them)
		s.batchBuffer = s.batchBuffer[:0]

		return fmt.Errorf("failed to flush batch: %w", err)
	}

	s.Logger.Infow("successfully flushed batch to ClickHouse",
		"batch_size", len(batchMessages),
		"total_events", len(eventsToInsert),
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	// Post-process events (only for original events, not billing events)
	// Only for tenants that are forced to v1
	var postProcessErrors int
	if s.Config.FeatureFlag.ForceV1ForTenant != "" {
		for _, batchMsg := range batchMessages {
			// Only post-process the first event (original event), not billing events
			if len(batchMsg.Events) > 0 {
				originalEvent := batchMsg.Events[0]
				if originalEvent.TenantID == s.Config.FeatureFlag.ForceV1ForTenant {
					if err := s.eventPostProcessingSvc.PublishEvent(batchMsg.Context, originalEvent, false); err != nil {
						s.Logger.Errorw("failed to publish event to post-processing service",
							"error", err,
							"event_id", originalEvent.ID,
							"event_name", originalEvent.EventName,
						)
						postProcessErrors++
						// Continue processing other events - post-processing errors don't cause NACKs
					}
				}
			}
		}
	}

	// ACK all messages in the batch after successful processing
	var ackErrors int
	for _, batchMsg := range batchMessages {
		if err := batchMsg.AckFunc(); err != nil {
			s.Logger.Errorw("failed to ACK message",
				"error", err,
				"message_uuid", batchMsg.Message.UUID,
			)
			ackErrors++
		}
	}

	s.Logger.Infow("batch processing completed",
		"batch_size", len(batchMessages),
		"total_events", len(eventsToInsert),
		"ack_errors", ackErrors,
		"post_process_errors", postProcessErrors,
		"duration_ms", time.Since(startTime).Milliseconds(),
	)

	// Clear the buffer
	s.batchBuffer = s.batchBuffer[:0]

	return nil
}

// Shutdown gracefully shuts down the service, flushing any remaining events
func (s *eventConsumptionService) Shutdown(ctx context.Context) error {
	s.Logger.Infow("shutting down event consumption service")

	// Signal the flusher to stop
	close(s.stopCh)

	// Wait for the flusher goroutine to exit
	s.wg.Wait()

	// Flush any remaining events
	if err := s.flushBatch(); err != nil {
		s.Logger.Errorw("failed to flush remaining events on shutdown", "error", err)
		return err
	}

	s.Logger.Infow("event consumption service shutdown complete")
	return nil
}
