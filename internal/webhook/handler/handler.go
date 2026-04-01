package handler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pubsub"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	repoent "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/svix"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/webhook/payload"
	"github.com/samber/lo"
)

// Handler interface for processing webhook events
type Handler interface {
	RegisterHandler(router *pubsubRouter.Router)
}

// handler implements handler.Handler using watermill's gochannel
type handler struct {
	pubSub          pubsub.PubSub
	config          *config.Webhook
	factory         payload.PayloadBuilderFactory
	client          httpclient.Client
	logger          *logger.Logger
	sentry          *sentry.Service
	svixClient      *svix.Client
	systemEventRepo *repoent.SystemEventRepository
}

// NewHandler creates a new memory-based handler
func NewHandler(
	pubSub pubsub.PubSub,
	cfg *config.Configuration,
	factory payload.PayloadBuilderFactory,
	client httpclient.Client,
	logger *logger.Logger,
	sentry *sentry.Service,
	svixClient *svix.Client,
	systemEventRepo *repoent.SystemEventRepository,
) (Handler, error) {
	return &handler{
		pubSub:          pubSub,
		config:          &cfg.Webhook,
		factory:         factory,
		client:          client,
		logger:          logger,
		sentry:          sentry,
		svixClient:      svixClient,
		systemEventRepo: systemEventRepo,
	}, nil
}

func (h *handler) RegisterHandler(router *pubsubRouter.Router) {
	if !h.config.Enabled {
		h.logger.Info("webhook handler disabled by configuration, skipping registration")
		return
	}
	rateLimit := h.config.RateLimit
	if rateLimit <= 0 {
		h.logger.Errorw("webhook rate limit is invalid", "rate_limit", rateLimit)
		return
	}
	throttle := middleware.NewThrottle(rateLimit, time.Second)
	router.AddNoPublishHandler(
		"webhook_handler",
		h.config.Topic,
		h.pubSub,
		h.processMessage,
		throttle.Middleware,
	)
	h.logger.Debugw("registered webhook handler",
		"topic", h.config.Topic,
		"consumer_group", h.config.ConsumerGroup,
		"rate_limit", rateLimit,
	)
}

// webhookMissingDataError is true when the failure is permanent (referenced entity missing).
// Those cases should ack and not consume router-level retries or DLQ.
func webhookMissingDataError(err error) bool {
	if err == nil {
		return false
	}
	return ierr.IsNotFound(err) || ent.IsNotFound(err)
}

// processWebhookError turns missing-data errors into nil so the Kafka offset commits.
// Other errors propagate to pubsub.Router middleware (Retry, then PoisonQueue / ack).
func (h *handler) processWebhookError(err error, event *types.WebhookEvent, messageUUID, step string) error {
	if err == nil {
		return nil
	}
	if !webhookMissingDataError(err) {
		return err
	}
	h.logger.Errorw("skipping webhook; referenced data not found (ack, no retry)",
		"error", err,
		"step", step,
		"message_uuid", messageUUID,
		"event_name", event.EventName,
		"tenant_id", event.TenantID,
	)
	return nil
}

// processMessage processes a single webhook message from the system_events topic:
// 1) unmarshal and verify, 2) call deliverWebhook to send to Svix or native HTTP.
func (h *handler) processMessage(msg *message.Message) error {
	ctx := msg.Context()

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

	ctx = context.WithValue(ctx, types.CtxTenantID, event.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, event.EnvironmentID)
	ctx = context.WithValue(ctx, types.CtxUserID, event.UserID)

	h.logger.Debugw("consumed webhook from topic and delivering",
		"topic", h.config.Topic,
		"message_uuid", msg.UUID,
		"event_name", event.EventName,
		"tenant_id", event.TenantID,
	)

	// Log inbound — create a bare system_events row (entity info populated on delivery).
	if err := h.systemEventRepo.OnConsumed(ctx, &event); err != nil {
		h.logger.Warnw("system_events OnConsumed failed",
			"error", err,
			"event_id", event.ID,
			"event_name", event.EventName,
		)
	}

	// After verify: deliver the webhook (Svix or native endpoint)
	if h.config.Svix.Enabled {
		return h.processMessageSvix(ctx, &event, msg.UUID)
	}

	return h.processMessageNative(ctx, &event, msg.UUID)
}

// processMessageSvix processes a webhook message using Svix
func (h *handler) processMessageSvix(ctx context.Context, event *types.WebhookEvent, messageUUID string) error {
	// Get or create Svix application
	appID, err := h.svixClient.GetOrCreateApplication(ctx, event.TenantID, event.EnvironmentID)
	if err != nil {
		// If error indicates no application exists, silently continue
		if err.Error() == "application not found" {
			h.logger.Debugw("no Svix application found, skipping webhook",
				"tenant_id", event.TenantID,
				"environment_id", event.EnvironmentID,
			)
			return nil
		}
		return err
	}

	// Build event payload
	builder, err := h.factory.GetBuilder(event.EventName)
	if err != nil {
		return h.processWebhookError(err, event, messageUUID, "get_builder")
	}

	h.logger.Debugw("building webhook payload",
		"event_name", event.EventName,
		"builder", builder,
	)

	webHookPayload, err := builder.BuildPayload(ctx, event.EventName, event.Payload)
	if err != nil {
		return h.processWebhookError(err, event, messageUUID, "build_payload")
	}

	// Send to Svix — capture the Svix message id.
	svixOut, err := h.svixClient.SendMessage(ctx, appID, event.EventName, json.RawMessage(webHookPayload))
	if err != nil {
		h.logger.Errorw("failed to send webhook via Svix",
			"error", err,
			"message_uuid", messageUUID,
			"tenant_id", event.TenantID,
			"event", event.EventName,
		)
		return err
	}

	// svixOut == "" means Svix was disabled or had no application for this tenant — message was not sent.
	// OnConsumed already recorded the row; don't mark it as published.
	if svixOut == "" {
		return nil
	}

	if err := h.systemEventRepo.OnDelivered(ctx, event.ID, lo.ToPtr(svixOut)); err != nil {
		h.logger.Warnw("system_events OnDelivered failed",
			"error", err,
			"event_id", event.ID,
			"event_name", event.EventName,
		)
	}

	h.logger.Infow("webhook sent successfully via Svix",
		"message_uuid", messageUUID,
		"tenant_id", event.TenantID,
		"event", event.EventName,
	)

	return nil
}

// processMessageNative processes a webhook message using native webhook system
func (h *handler) processMessageNative(ctx context.Context, event *types.WebhookEvent, messageUUID string) error {
	// Get tenant config
	tenantCfg, ok := h.config.Tenants[event.TenantID]
	if !ok {
		h.logger.Warnw("tenant config not found",
			"tenant_id", event.TenantID,
			"message_uuid", messageUUID,
		)
		// Don't retry if tenant not found
		return nil
	}

	// Check if tenant webhooks are enabled
	if !tenantCfg.Enabled {
		h.logger.Debugw("webhooks disabled for tenant",
			"tenant_id", event.TenantID,
			"message_uuid", messageUUID,
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
		return h.processWebhookError(err, event, messageUUID, "get_builder")
	}

	h.logger.Debugw("building webhook payload",
		"event_name", event.EventName,
		"builder", builder,
	)

	webHookPayload, err := builder.BuildPayload(ctx, event.EventName, event.Payload)
	if err != nil {
		return h.processWebhookError(err, event, messageUUID, "build_payload")
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
			"message_uuid", messageUUID,
			"tenant_id", event.TenantID,
			"event", event.EventName,
		)
		return err
	}

	h.logger.Infow("webhook sent successfully",
		"message_uuid", messageUUID,
		"tenant_id", event.TenantID,
		"event", event.EventName,
		"status_code", resp.StatusCode,
	)

	if err := h.systemEventRepo.OnDelivered(ctx, event.ID, nil); err != nil {
		h.logger.Warnw("system_events OnDelivered failed",
			"error", err,
			"event_id", event.ID,
			"event_name", event.EventName,
		)
	}

	return nil
}
