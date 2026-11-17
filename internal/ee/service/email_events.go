package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

func (s *emailNotificationService) RegisterHandler(router *pubsubRouter.Router, cfg *config.Configuration) {
	if !cfg.EmailEvents.Enabled || !cfg.Email.IsProviderConfigured() {
		s.Logger.Info(context.Background(), "email events handler disabled by configuration")
		return
	}

	throttle := middleware.NewThrottle(cfg.EmailEvents.RateLimit, time.Second)

	router.AddNoPublishHandler(
		"email_events_handler",
		cfg.Webhook.Topic,
		cfg.Kafka.TopicDLQ,
		s.EmailEventsPubSub.PubSub,
		s.processEmailEvent,
		throttle.Middleware,
	)

	s.Logger.Info(context.Background(), "registered email events handler",
		"topic", cfg.Webhook.Topic,
		"consumer_group", cfg.EmailEvents.ConsumerGroup,
		"rate_limit", cfg.EmailEvents.RateLimit,
	)
}

// processEmailEvent auto-sends the email for a finalized invoice; other events are acked and ignored.
func (s *emailNotificationService) processEmailEvent(ctx context.Context, msg *message.Message) error {
	var event types.WebhookEvent
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		s.Logger.Error(ctx, "failed to unmarshal email event, dropping message", "error", err, "message_uuid", msg.UUID)
		return nil
	}
	if event.EventName != types.WebhookEventInvoiceUpdateFinalized {
		return nil
	}

	var payload webhookDto.InternalInvoiceEvent
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.InvoiceID == "" {
		s.Logger.Error(ctx, "invalid invoice payload, dropping auto email", "error", err, "message_uuid", msg.UUID)
		return nil
	}

	ctx = context.WithValue(ctx, types.CtxTenantID, event.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, event.EnvironmentID)
	ctx = context.WithValue(ctx, types.CtxUserID, event.UserID)

	err := s.SendAutoInvoiceEmail(ctx, payload.InvoiceID)
	if ierr.IsNotFound(err) {
		s.Logger.Info(ctx, "auto email not sent, referenced data not found",
			"invoice_id", payload.InvoiceID,
			"message_uuid", msg.UUID,
			"error", err,
		)
		return nil
	}
	return err
}
