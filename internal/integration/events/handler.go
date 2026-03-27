package events

import (
	"context"
	"encoding/json"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
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
	InvoiceRepo    invoice.Repository
	SubRepo        subscription.Repository
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
func NewHandler(deps Deps) Handler {
	h := &handler{deps: deps}
	h.processors = map[types.WebhookEventName]eventProcessor{
		types.WebhookEventInvoiceUpdateFinalized: h.processInvoiceUpdateFinalized,
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

	return processor(ctx, &event, msg)
}

func (h *handler) processInvoiceUpdateFinalized(
	ctx context.Context,
	event *types.WebhookEvent,
	msg *message.Message,
) error {
	var pl struct {
		InvoiceID string `json:"invoice_id"`
	}
	if err := json.Unmarshal(event.Payload, &pl); err != nil || pl.InvoiceID == "" {
		h.deps.Logger.Errorw("integration_events: invalid invoice payload on finalized event, dropping",
			"message_uuid", msg.UUID,
			"error", err,
		)
		return nil
	}

	inv, err := h.deps.InvoiceRepo.Get(ctx, pl.InvoiceID)
	if err != nil {
		h.deps.Logger.Errorw("integration_events: failed to load invoice for sync dispatch",
			"invoice_id", pl.InvoiceID,
			"error", err,
		)
		return err
	}

	collectionMethod := ""
	if inv.SubscriptionID != nil && h.deps.SubRepo != nil {
		sub, err := h.deps.SubRepo.Get(ctx, *inv.SubscriptionID)
		if err != nil {
			h.deps.Logger.Warnw("integration_events: failed to get subscription for collection method",
				"invoice_id", inv.ID,
				"subscription_id", *inv.SubscriptionID,
				"error", err)
		} else if sub != nil {
			collectionMethod = sub.CollectionMethod
		}
	}

	in := InvoiceVendorSyncInput{
		TenantID:         event.TenantID,
		EnvironmentID:    event.EnvironmentID,
		UserID:           event.UserID,
		InvoiceID:        inv.ID,
		CustomerID:       inv.CustomerID,
		CollectionMethod: collectionMethod,
	}

	if err := DispatchInvoiceVendorSync(ctx, h.deps.Config, h.deps.ConnectionRepo, h.deps.Logger, in); err != nil {
		h.deps.Logger.Errorw("integration_events: invoice vendor sync dispatch failed",
			"invoice_id", inv.ID,
			"error", err,
		)
		return err
	}

	return nil
}
