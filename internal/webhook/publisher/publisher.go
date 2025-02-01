package publisher

import (
	"context"
	"encoding/json"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pubsub"
	"github.com/flexprice/flexprice/internal/types"
)

// WebhookPublisher interface for producing webhook events
type WebhookPublisher interface {
	PublishWebhook(ctx context.Context, event *types.WebhookEvent) error
	Close() error
}

// Handler implements handler.Handler using watermill's gochannel
type webhookPublisher struct {
	pubSub pubsub.PubSub
	config *config.Webhook
	logger *logger.Logger
}

// NewHandler creates a new memory-based handler
func NewPublisher(
	pubSub pubsub.PubSub,
	cfg *config.Configuration,
	logger *logger.Logger,
) (WebhookPublisher, error) {
	return &webhookPublisher{
		pubSub: pubSub,
		config: &cfg.Webhook,
		logger: logger,
	}, nil
}

func (p *webhookPublisher) PublishWebhook(ctx context.Context, event *types.WebhookEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	messageID := event.ID
	if messageID == "" {
		messageID = watermill.NewUUID()
	}

	msg := message.NewMessage(messageID, payload)
	msg.Metadata.Set("tenant_id", event.TenantID)

	p.logger.Debugw("publishing webhook event",
		"event_id", event.ID,
		"event_name", event.EventName,
		"tenant_id", event.TenantID,
		"topic", p.config.Topic,
		"payload", string(payload),
	)

	if err := p.pubSub.Publish(ctx, p.config.Topic, msg); err != nil {
		p.logger.Errorw("failed to publish webhook event",
			"error", err,
			"event_id", event.ID,
			"event_name", event.EventName,
			"tenant_id", event.TenantID,
		)
		return err
	}

	p.logger.Infow("successfully published webhook event",
		"event_id", event.ID,
		"event_name", event.EventName,
		"tenant_id", event.TenantID,
	)

	return nil
}

// Close closes the publisher
func (p *webhookPublisher) Close() error {
	return p.pubSub.Close()
}
