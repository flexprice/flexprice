package handler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ThreeDotsLabs/watermill"
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
	// DeliverWebhook builds the outbound payload and delivers it (Svix or native HTTP)
	// without publishing to Kafka. Used for synchronous retries (e.g. API retrigger).
	DeliverWebhook(ctx context.Context, event *types.WebhookEvent) error
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

	// Retry transient failures (e.g. publish-before-commit race: invoice not yet visible
	// when consumer picks up the Kafka message). processMessage returns nil for permanent
	// errors so only genuinely transient errors reach this middleware.
	retryMiddleware := middleware.Retry{
		MaxRetries:          5,
		InitialInterval:     5 * time.Second,
		MaxInterval:         30 * time.Second,
		Multiplier:          2.0,
		RandomizationFactor: 0.3,
		Logger:              watermill.NewStdLogger(false, false),
	}.Middleware

	// Add the handler
	router.AddNoPublishHandler(
		"webhook_handler",
		h.config.Topic,
		h.pubSub,
		h.processMessage,
		retryMiddleware,
		throttle.Middleware,
	)
	h.logger.Debugw("registered webhook handler",
		"topic", h.config.Topic,
		"consumer_group", h.config.ConsumerGroup,
		"rate_limit", rateLimit,
	)
}

// DeliverWebhook implements synchronous delivery for API-driven retries.
func (h *handler) DeliverWebhook(ctx context.Context, event *types.WebhookEvent) error {
	if !h.config.Enabled {
		return ierr.NewError("webhook delivery is disabled").
			WithHint("Enable webhooks in configuration to retrigger system events.").
			Mark(ierr.ErrInvalidOperation)
	}
	if event == nil {
		return ierr.NewError("webhook event is required").
			Mark(ierr.ErrValidation)
	}

	ctx = context.WithValue(ctx, types.CtxTenantID, event.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, event.EnvironmentID)
	ctx = context.WithValue(ctx, types.CtxUserID, event.UserID)

	messageUUID := types.GenerateUUID()
	h.logger.Debugw("delivering webhook synchronously",
		"message_uuid", messageUUID,
		"event_name", event.EventName,
		"tenant_id", event.TenantID,
		"event_id", event.ID,
	)

	var deliveryErr error
	if h.config.Svix.Enabled {
		deliveryErr = h.deliverSvix(ctx, event, messageUUID)
	} else {
		deliveryErr = h.deliverNative(ctx, event, messageUUID)
	}
	if deliveryErr != nil {
		h.logger.Errorw("failed to deliver webhook synchronously",
			"error", deliveryErr,
			"event_id", event.ID,
			"event_name", event.EventName,
			"tenant_id", event.TenantID,
			"message_uuid", messageUUID,
		)
		if h.systemEventRepo != nil && event.ID != "" {
			if dbErr := h.systemEventRepo.OnFailed(ctx, event.ID, deliveryErr.Error()); dbErr != nil {
				h.logger.Warnw("failed to persist webhook failure_reason",
					"error", dbErr,
					"event_id", event.ID,
				)
			}
		}
	}
	return deliveryErr
}

// webhookMissingDataError is true when the failure is permanent (referenced entity missing).
// Those cases should ack and not consume router-level retries or DLQ.
func webhookMissingDataError(err error) bool {
	if err == nil {
		return false
	}
	return ierr.IsNotFound(err) || ent.IsNotFound(err)
}

// absorbDeliveryError logs delivery failures for the Kafka consumer path and always acks.
// It also persists the failure reason on the system_events row so it is never silently dropped.
func (h *handler) absorbDeliveryError(ctx context.Context, transport string, err error, event *types.WebhookEvent, messageUUID string) {
	if err == nil {
		return
	}
	if webhookMissingDataError(err) {
		h.logger.Errorw("skipping webhook; referenced data not found (ack, no retry)",
			"transport", transport,
			"error", err,
			"message_uuid", messageUUID,
			"event_name", event.EventName,
			"tenant_id", event.TenantID,
		)
	} else {
		h.logger.Errorw("failed to send webhook",
			"transport", transport,
			"error", err,
			"message_uuid", messageUUID,
			"tenant_id", event.TenantID,
			"event", event.EventName,
		)
	}
	if h.systemEventRepo != nil && event.ID != "" {
		if dbErr := h.systemEventRepo.OnFailed(ctx, event.ID, err.Error()); dbErr != nil {
			h.logger.Warnw("failed to persist webhook failure_reason",
				"error", dbErr,
				"event_id", event.ID,
			)
		}
	}

}

// processMessage processes a single webhook message from the system_events topic:
// 1) unmarshal and verify, 2) call deliverSvix/deliverNative to send to Svix or native HTTP.
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

	if h.config.Svix.Enabled {
		err := h.deliverSvix(ctx, &event, msg.UUID)
		h.absorbDeliveryError(ctx, "svix", err, &event, msg.UUID)
		return err
	}

	err := h.deliverNative(ctx, &event, msg.UUID)
	h.absorbDeliveryError(ctx, "native", err, &event, msg.UUID)
	return err
}

// deliverSvix sends a webhook via Svix.
func (h *handler) deliverSvix(ctx context.Context, event *types.WebhookEvent, messageUUID string) error {
	appID, err := h.svixClient.GetOrCreateApplication(ctx, event.TenantID, event.EnvironmentID)
	if err != nil {
		if err.Error() == "application not found" {
			return ierr.NewError("Svix application not found for this tenant and environment").
				WithHint("Configure Svix before retriggering this webhook.").
				Mark(ierr.ErrInvalidOperation)
		}
		return err
	}

	builder, err := h.factory.GetBuilder(event.EventName)
	if err != nil {
		return err
	}

	h.logger.Debugw("building webhook payload",
		"event_name", event.EventName,
		"builder", builder,
	)

	webHookPayload, err := builder.BuildPayload(ctx, event.EventName, event.Payload)
	if err != nil {
		h.logger.Errorw("failed to build webhook payload (deliverSvix)",
			"error", err,
			"event_name", event.EventName,
			"builder", builder,
			"payload", string(event.Payload),
		)
		return err
	}

	svixOut, err := h.svixClient.SendMessage(ctx, appID, event.EventName, json.RawMessage(webHookPayload))
	if err != nil {
		h.logger.Errorw("failed to send webhook message via Svix (deliverSvix)",
			"error", err,
			"event_name", event.EventName,
			"app_id", appID,
		)
		return err
	}

	if svixOut == "" {
		return ierr.NewError("webhook was not delivered (Svix returned no message id)").
			WithHint("Check Svix configuration and application status for this environment.").
			Mark(ierr.ErrInvalidOperation)
	}

	if err := h.systemEventRepo.OnDelivered(ctx, event.ID, lo.ToPtr(svixOut)); err != nil {
		h.logger.Warnw("system_events OnDelivered failed",
			"error", err,
			"event_id", event.ID,
			"event_name", event.EventName,
		)
		return err
	}

	h.logger.Infow("webhook sent successfully via Svix",
		"message_uuid", messageUUID,
		"tenant_id", event.TenantID,
		"event", event.EventName,
	)

	return nil
}

// deliverNative sends a webhook to the configured HTTP endpoint.
func (h *handler) deliverNative(ctx context.Context, event *types.WebhookEvent, messageUUID string) error {
	tenantCfg, ok := h.config.Tenants[event.TenantID]
	if !ok {
		return ierr.NewError("native webhook is not configured for this tenant").
			WithHint("Add the tenant to webhook.tenants in configuration.").
			Mark(ierr.ErrNotFound)
	}

	if !tenantCfg.Enabled {
		return ierr.NewError("webhooks are disabled for this tenant in native configuration").
			Mark(ierr.ErrInvalidOperation)
	}

	for _, excludedEvent := range tenantCfg.ExcludedEvents {
		if excludedEvent == event.EventName {
			return ierr.NewErrorf("event %q is excluded from native webhooks for this tenant", event.EventName).
				Mark(ierr.ErrInvalidOperation)
		}
	}

	builder, err := h.factory.GetBuilder(event.EventName)
	if err != nil {
		return err
	}

	h.logger.Debugw("building webhook payload",
		"event_name", event.EventName,
		"builder", builder,
	)

	webHookPayload, err := builder.BuildPayload(ctx, event.EventName, event.Payload)
	if err != nil {
		return err
	}

	h.logger.Debugw("built webhook payload",
		"event_name", event.EventName,
		"payload", string(webHookPayload),
	)

	req := &httpclient.Request{
		Method:  "POST",
		URL:     tenantCfg.Endpoint,
		Headers: tenantCfg.Headers,
		Body:    webHookPayload,
	}

	resp, err := h.client.Send(ctx, req)
	if err != nil {
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
		return err
	}

	return nil
}
