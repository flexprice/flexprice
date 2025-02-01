package handler

import (
	"context"
	"encoding/json"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pubsub"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/webhook/payload"
)

// Handler interface for processing webhook events
type Handler interface {
	RegisterHandler(router *pubsubRouter.Router)
}

// handler implements handler.Handler using watermill's gochannel
type handler struct {
	pubSub  pubsub.PubSub
	config  *config.Webhook
	factory payload.PayloadBuilderFactory
	client  httpclient.Client
	logger  *logger.Logger
	sentry  *sentry.Service
}

// NewHandler creates a new memory-based handler
func NewHandler(
	pubSub pubsub.PubSub,
	cfg *config.Configuration,
	factory payload.PayloadBuilderFactory,
	client httpclient.Client,
	logger *logger.Logger,
	sentry *sentry.Service,
) (Handler, error) {
	return &handler{
		pubSub:  pubSub,
		config:  &cfg.Webhook,
		factory: factory,
		client:  client,
		logger:  logger,
		sentry:  sentry,
	}, nil
}

func (h *handler) RegisterHandler(router *pubsubRouter.Router) {
	router.AddNoPublishHandler(
		"webhook_handler",
		h.config.Topic,
		h.pubSub,
		h.processMessage,
	)
}

// processMessage processes a single webhook message
func (h *handler) processMessage(msg *message.Message) error {
	ctx := msg.Context()

	// log the context fields like tenant_id, event_name, etc
	h.logger.Debugw("context",
		"tenant_id", types.GetTenantID(ctx),
		"event_name", types.GetRequestID(ctx),
	)

	var event types.WebhookEvent
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		h.logger.Errorw("failed to unmarshal webhook event",
			"error", err,
			"message_uuid", msg.UUID,
		)
		return nil // Don't retry on unmarshal errors
	}

	// Get tenant config
	tenantCfg, ok := h.config.Tenants[event.TenantID]
	if !ok {
		h.logger.Warnw("tenant config not found",
			"tenant_id", event.TenantID,
			"message_uuid", msg.UUID,
		)
		// Don't retry if tenant not found
		return nil
	}

	// Check if tenant webhooks are enabled
	if !tenantCfg.Enabled {
		h.logger.Debugw("webhooks disabled for tenant",
			"tenant_id", event.TenantID,
			"message_uuid", msg.UUID,
		)
		return nil
	}

	// Check if event is excluded
	for _, excludedEvent := range tenantCfg.ExcludedEvents {
		if excludedEvent == event.EventName {
			h.logger.Debugw("event excluded for tenant",
				"tenant_id", event.TenantID,
				"event", event.EventName,
			)
			return nil
		}
	}

	// Build event payload
	builder, err := h.factory.GetBuilder(event.EventName)
	if err != nil {
		return err
	}

	h.logger.Debugw("building webhook payload",
		"event_name", event.EventName,
		"builder", builder,
	)

	// set tenant_id in context
	ctx = context.WithValue(ctx, types.CtxTenantID, event.TenantID)
	webHookPayload, err := builder.BuildPayload(ctx, event.EventName, event.Payload)
	if err != nil {
		return err
	}

	h.logger.Debugw("built webhook payload",
		"event_name", event.EventName,
		"payload", string(webHookPayload),
	)

	// Send webhook
	req := &httpclient.Request{
		Method:  "POST",
		URL:     tenantCfg.Endpoint,
		Headers: tenantCfg.Headers,
		Body:    webHookPayload,
	}

	resp, err := h.client.Send(ctx, req)
	if err != nil {
		h.logger.Errorw("failed to send webhook",
			"error", err,
			"message_uuid", msg.UUID,
			"tenant_id", event.TenantID,
			"event", event.EventName,
		)
		return err
	}

	h.logger.Infow("webhook sent successfully",
		"message_uuid", msg.UUID,
		"tenant_id", event.TenantID,
		"event", event.EventName,
		"status_code", resp.StatusCode,
	)

	return nil
}
