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
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

// RawEventConsumptionService handles consuming raw event batches from Kafka and transforming them
type RawEventConsumptionService interface {
	// RegisterHandler registers the message handler with the router (consumer side)
	RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration)

	// BulkIngestRawEvents publishes a batch of raw Bento-format event payloads directly
	// to the raw_events Kafka topic. The consumer (processMessage) will pick them up
	// exactly as it would if Bento had written them.
	BulkIngestRawEvents(ctx context.Context, events []json.RawMessage) error
}

type rawEventConsumptionService struct {
	ServiceParams
	pubSub        pubsub.PubSub
	outputPubSub  pubsub.PubSub
	sentryService *sentry.Service
}

// RawEventBatch represents the batch structure from Bento
type RawEventBatch struct {
	Data          []json.RawMessage `json:"data"`
	TenantID      string            `json:"tenant_id"`
	EnvironmentID string            `json:"environment_id"`
}

// NewRawEventConsumptionService creates a new raw event consumption service
func NewRawEventConsumptionService(
	params ServiceParams,
	sentryService *sentry.Service,
) RawEventConsumptionService {
	ev := &rawEventConsumptionService{
		ServiceParams: params,
		sentryService: sentryService,
	}

	// Consumer pubsub for raw_events topic
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

	// Output pubsub for publishing transformed events to events topic
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

	return ev
}

// RegisterHandler registers the raw event consumption handler with the router
func (s *rawEventConsumptionService) RegisterHandler(
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

// loadIngestionFilter fetches the EventIngestionFilterConfig from the settings DB
// and returns a ready-to-use allowlist map.
//
// Error handling:
//   - Setting absent (ErrNotFound) → filter disabled, returns (false, nil, nil)
//   - Any other repo error → returns the error so the caller can fail the batch and retry
//   - Parsing failure → same: error is returned for retry
//   - Setting present but enabled=false → returns (false, nil, nil)
func (s *rawEventConsumptionService) loadIngestionFilter(ctx context.Context) (enabled bool, allowlist map[string]struct{}, err error) {
	setting, err := s.SettingsRepo.GetByKey(ctx, types.SettingKeyEventIngestionFilter)
	if err != nil {
		if ierr.IsNotFound(err) {
			// Setting not configured is expected — filter simply disabled.
			return false, nil, nil
		}
		// Transient DB or other operational error — bubble up so the batch retries.
		return false, nil, fmt.Errorf("failed to load event ingestion filter setting: %w", err)
	}

	cfg, err := utils.ToStruct[types.EventIngestionFilterConfig](setting.Value)
	if err != nil {
		return false, nil, fmt.Errorf("failed to parse event ingestion filter config: %w", err)
	}

	if !cfg.Enabled {
		return false, nil, nil
	}

	allowlist = make(map[string]struct{}, len(cfg.AllowedExternalCustomerIDs))
	for _, id := range cfg.AllowedExternalCustomerIDs {
		allowlist[id] = struct{}{}
	}

	s.Logger.Infow("event ingestion filter loaded",
		"allowlist_size", len(allowlist),
	)
	return true, allowlist, nil
}

// processMessage processes a batch of raw events from Kafka
func (s *rawEventConsumptionService) processMessage(msg *message.Message) error {
	s.Logger.Debugw("processing raw event batch from message queue",
		"message_uuid", msg.UUID,
	)

	// Unmarshal the batch first so we have tenant/environment IDs.
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

	// Build a context from the message's own context so cancellation/tracing propagates,
	// then attach tenant and environment IDs so the settings repo can scope its query.
	ctx := types.SetTenantID(msg.Context(), tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)

	// Fetch the ingestion filter once per batch (one DB read per Kafka message).
	// On a real settings-store error (not ErrNotFound) we fail the batch so Kafka retries it.
	filterEnabled, allowlist, err := s.loadIngestionFilter(ctx)
	if err != nil {
		s.Logger.Errorw("failed to load ingestion filter, failing batch for retry",
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"error", err,
		)
		return fmt.Errorf("ingestion filter load error: %w", err)
	}

	// Counters for tracking
	successCount := 0
	skipCount := 0
	errorCount := 0

	// Process each raw event in the batch
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

		// Apply ingestion filter: skip events for customer IDs not in the allowlist.
		// Raw event is still stored upstream; we only skip forwarding to the events topic.
		if filterEnabled {
			if _, ok := allowlist[transformedEvent.ExternalCustomerID]; !ok {
				skipCount++
				s.Logger.Debugw("event filtered by ingestion allowlist - skipped",
					"batch_position", i+1,
				)
				continue
			}
		}

		// Publish the transformed event to events topic
		if err := s.publishTransformedEvent(ctx, transformedEvent); err != nil {
			errorCount++
			s.Logger.Errorw("failed to publish transformed event",
				"event_id", transformedEvent.ID,
				"event_name", transformedEvent.EventName,
				"external_customer_id", transformedEvent.ExternalCustomerID,
				"batch_position", i+1,
				"error", err.Error(),
			)
			// Continue processing other events even if one fails
			continue
		}

		successCount++
		s.Logger.Debugw("successfully transformed and published event",
			"event_id", transformedEvent.ID,
			"event_name", transformedEvent.EventName,
			"batch_position", i+1,
		)
	}

	s.Logger.Infow("completed raw event batch processing",
		"batch_size", len(batch.Data),
		"success_count", successCount,
		"skip_count", skipCount,
		"error_count", errorCount,
		"message_uuid", msg.UUID,
	)

	// Return error if any events failed (causes batch retry)
	// Skip count is acceptable (validation failures), but error count requires retry
	if errorCount > 0 {
		return fmt.Errorf("failed to process %d events in batch, retrying entire batch", errorCount)
	}

	return nil
}

// BulkIngestRawEvents publishes a batch of raw Bento-format event payloads to the
// raw_events Kafka topic. The consumer picks them up in the same format as events
// produced by the Bento collector — there is no difference from the consumer's perspective.
func (s *rawEventConsumptionService) BulkIngestRawEvents(ctx context.Context, events []json.RawMessage) error {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	if tenantID == "" || environmentID == "" {
		return fmt.Errorf("BulkIngestRawEvents: tenant_id and environment_id must be set in context (got tenant=%q env=%q)", tenantID, environmentID)
	}

	batch := RawEventBatch{
		Data:          events,
		TenantID:      tenantID,
		EnvironmentID: environmentID,
	}

	payload, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("failed to marshal raw event batch: %w", err)
	}

	uniqueID := fmt.Sprintf("%s-%d-%d", types.GenerateUUID(), time.Now().UnixNano(), rand.Int63())
	msg := message.NewMessage(uniqueID, payload)
	msg.Metadata.Set("tenant_id", tenantID)
	msg.Metadata.Set("environment_id", environmentID)

	topic := s.Config.RawEventConsumption.Topic
	if err := s.pubSub.Publish(ctx, topic, msg); err != nil {
		return fmt.Errorf("failed to publish raw event batch: %w", err)
	}

	s.Logger.Infow("published raw event batch to kafka",
		"batch_size", len(events),
		"tenant_id", tenantID,
		"environment_id", environmentID,
		"topic", topic,
	)
	return nil
}

// publishTransformedEvent publishes a transformed event to the events topic
func (s *rawEventConsumptionService) publishTransformedEvent(ctx context.Context, event *events.Event) error {
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

	s.Logger.DebugwCtx(ctx, "publishing transformed event to kafka",
		"event_id", event.ID,
		"event_name", event.EventName,
		"partition_key", partitionKey,
		"topic", topic,
	)

	// Publish to Kafka
	if err := s.outputPubSub.Publish(ctx, topic, msg); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to publish transformed event to Kafka").
			Mark(ierr.ErrSystem)
	}

	return nil
}
