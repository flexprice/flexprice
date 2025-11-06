package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

	// Process a raw event payload (used for AWS Lambda and direct processing)
	ProcessRawEvent(ctx context.Context, payload []byte) error
}

type eventConsumptionService struct {
	ServiceParams
	pubSub                 pubsub.PubSub
	lazyPubSub             pubsub.PubSub
	eventRepo              events.Repository
	sentryService          *sentry.Service
	eventPostProcessingSvc EventPostProcessingService
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

// ProcessRawEvent processes a raw event payload (used for AWS Lambda and direct processing)
func (s *eventConsumptionService) ProcessRawEvent(ctx context.Context, payload []byte) error {
	// Start a transaction for this event processing
	transaction, ctx := s.sentryService.StartTransaction(ctx, "event.process")
	if transaction != nil {
		defer transaction.Finish()
	}

	// Unmarshal the event
	var event events.Event
	if err := json.Unmarshal(payload, &event); err != nil {
		s.Logger.Errorw("failed to unmarshal event",
			"error", err,
			"payload", string(payload),
		)
		s.sentryService.CaptureException(err)
		return fmt.Errorf("failed to unmarshal event: %w", err)
	}

	s.Logger.Debugw("processing raw event",
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
		return fmt.Errorf("failed to insert events: %w", err)
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
			return fmt.Errorf("failed to publish event for post-processing: %w", err)
		}
	}

	s.Logger.Debugw("successfully processed raw event",
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
