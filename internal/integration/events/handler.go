package events

import (
	"context"
	"encoding/json"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pubsub"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
)

// Handler is the read-side of the integration events bus.
// It subscribes to the system_events Kafka topic using its own consumer group
// and dispatches selected webhook-shaped system events to integration workflows.
type Handler interface {
	RegisterHandler(router *pubsubRouter.Router)
}

// Deps holds the external dependencies injected into the handler.
type Deps struct {
	ConnectionRepo connection.Repository
	EIMRepo        entityintegrationmapping.Repository
	Logger         *logger.Logger
	Config         *config.Configuration
	PubSub         pubsub.PubSub
}

type handler struct {
	deps       Deps
	processors map[types.WebhookEventName]eventProcessor
}

type eventProcessor func(context.Context, *types.WebhookEvent, *message.Message) error

// NewHandler constructs the integration events handler.
// Each event type maps directly to the dispatch function that owns its resolution logic.
func NewHandler(deps Deps) Handler {
	h := &handler{deps: deps}
	h.processors = map[types.WebhookEventName]eventProcessor{
		types.WebhookEventCustomerCreated: func(ctx context.Context, event *types.WebhookEvent, msg *message.Message) error {
			return DispatchCustomerVendorSync(
				ctx,
				h.deps.Config,
				h.deps.ConnectionRepo,
				h.deps.EIMRepo,
				h.deps.Logger,
				event,
				msg.UUID,
			)
		},
		types.WebhookEventInvoiceUpdateFinalized: func(ctx context.Context, event *types.WebhookEvent, msg *message.Message) error {
			return DispatchInvoiceVendorSync(
				ctx,
				h.deps.Config,
				h.deps.ConnectionRepo,
				h.deps.EIMRepo,
				h.deps.Logger,
				event,
				msg.UUID,
			)
		},
	}
	return h
}

// RegisterHandler wires the handler into the Watermill router, subscribing to
// the system_events topic under the integration-events consumer group.
func (h *handler) RegisterHandler(router *pubsubRouter.Router) {
	cfg := h.deps.Config.IntegrationEvents
	if !cfg.Enabled {
		h.deps.Logger.Info("integration_events: handler disabled by configuration, skipping registration")
		return
	}

	topic := h.deps.Config.Webhook.Topic
	h.deps.Logger.Debugw("integration_events: registering handler",
		"topic", topic,
		"consumer_group", cfg.ConsumerGroup,
	)

	router.AddNoPublishHandler(
		"integration_events_handler",
		topic,
		h.deps.PubSub,
		h.processMessage,
	)
}

// integrationMissingDataError is true when the failure is permanent (referenced entity missing).
// Those cases should ack and not consume router-level retries or DLQ.
func integrationMissingDataError(err error) bool {
	if err == nil {
		return false
	}
	return ierr.IsNotFound(err) || ent.IsNotFound(err)
}

// processIntegrationError turns missing-data errors into nil so the Kafka offset commits.
// Other errors propagate to pubsub.Router middleware (Retry, then PoisonQueue / ack).
func (h *handler) processIntegrationError(err error, event *types.WebhookEvent, messageUUID, step string) error {
	if err == nil {
		return nil
	}
	if !integrationMissingDataError(err) {
		return err
	}
	h.deps.Logger.Errorw("integration_events: skipping event; referenced data not found (ack, no retry)",
		"error", err,
		"step", step,
		"message_uuid", messageUUID,
		"event_name", event.EventName,
		"tenant_id", event.TenantID,
	)
	return nil
}

// processMessage unmarshals types.WebhookEvent (same envelope as customer webhooks).
// It dispatches to event-specific processors; unknown events are ACKed and ignored.
func (h *handler) processMessage(msg *message.Message) error {
	ctx := msg.Context()
	var event types.WebhookEvent
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		h.deps.Logger.Errorw("integration_events: failed to unmarshal WebhookEvent, dropping message",
			"message_uuid", msg.UUID,
			"error", err,
		)
		return nil
	}

	processor, ok := h.processors[event.EventName]
	if !ok {
		return nil
	}

	ctx = context.WithValue(ctx, types.CtxTenantID, event.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, event.EnvironmentID)
	ctx = context.WithValue(ctx, types.CtxUserID, event.UserID)

	h.deps.Logger.Debugw("integration_events: consumed webhook-shaped system event",
		"message_uuid", msg.UUID,
		"event_name", event.EventName,
		"tenant_id", event.TenantID,
		"environment_id", event.EnvironmentID,
	)

	return h.processIntegrationError(processor(ctx, &event, msg), &event, msg.UUID, "dispatch")
}
