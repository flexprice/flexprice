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
	batchBuffer   []*events.Event
	batchSize     int
	flushInterval time.Duration
	flushTicker   *time.Ticker
	stopCh        chan struct{}
	wg            sync.WaitGroup
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
		batchSize:              params.Config.EventProcessing.BatchSize,
		flushInterval:          time.Duration(params.Config.EventProcessing.BatchFlushSeconds) * time.Second,
		batchBuffer:            make([]*events.Event, 0, params.Config.EventProcessing.BatchSize),
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
		s.processMessage,
		throttle.Middleware,
	)

	s.Logger.Infow("registered event consumption lazy handler",
		"topic", cfg.EventProcessingLazy.Topic,
		"rate_limit", cfg.EventProcessingLazy.RateLimit,
	)
}

// processMessage processes a single event message from Kafka
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

	// Add events to batch
	if err := s.addToBatch(eventsToAdd); err != nil {
		s.Logger.Errorw("failed to add events to batch",
			"error", err,
			"event_id", event.ID,
			"event_name", event.EventName,
		)
		// Return error for retry
		return ierr.WithError(err).
			WithHint("Failed to add events to batch").
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

// addToBatch adds events to the batch buffer, flushing if batch size is reached
func (s *eventConsumptionService) addToBatch(events []*events.Event) error {
	s.batchMu.Lock()
	defer s.batchMu.Unlock()

	// Add events to buffer
	s.batchBuffer = append(s.batchBuffer, events...)

	s.Logger.Debugw("added events to batch",
		"batch_size", len(s.batchBuffer),
		"events_added", len(events),
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

	ctx := context.Background()

	s.Logger.Infow("flushing batch to ClickHouse",
		"batch_size", len(s.batchBuffer),
	)

	startTime := time.Now()

	// Insert events into ClickHouse
	if err := s.eventRepo.BulkInsertEvents(ctx, s.batchBuffer); err != nil {
		s.Logger.Errorw("failed to flush batch to ClickHouse",
			"error", err,
			"batch_size", len(s.batchBuffer),
		)
		s.sentryService.CaptureException(err)
		return fmt.Errorf("failed to flush batch: %w", err)
	}

	s.Logger.Infow("successfully flushed batch to ClickHouse",
		"batch_size", len(s.batchBuffer),
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
