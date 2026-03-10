package publisher

import (
	"context"
	"encoding/json"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// WebhookPublisher interface for producing webhook events
type WebhookPublisher interface {
	PublishWebhook(ctx context.Context, event *types.WebhookEvent) error
	Close() error
}

// webhookPublisher publishes webhook events to Kafka
type webhookPublisher struct {
	producer *kafka.Producer
	config   *config.Webhook
	logger   *logger.Logger
}

// NewPublisher creates a new Kafka-backed webhook publisher
func NewPublisher(
	producer *kafka.Producer,
	cfg *config.Configuration,
	logger *logger.Logger,
) (WebhookPublisher, error) {
	return &webhookPublisher{
		producer: producer,
		config:   &cfg.Webhook,
		logger:   logger,
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
	msg.Metadata.Set("environment_id", event.EnvironmentID)
	msg.Metadata.Set("user_id", event.UserID)

	p.logger.Debugw("publishing webhook event",
		"event_id", event.ID,
		"event_name", event.EventName,
		"tenant_id", event.TenantID,
		"topic", p.config.Topic,
		"payload", string(payload),
	)

	if err := p.producer.Publish(p.config.Topic, msg); err != nil {
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

// Close is a no-op since the Kafka producer is shared and managed globally
func (p *webhookPublisher) Close() error {
	return nil
}
