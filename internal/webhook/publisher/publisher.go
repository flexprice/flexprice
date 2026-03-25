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

// MessagePublisher publishes messages to a topic (e.g. Kafka producer).
// Used when webhook delivery is Kafka-backed so the publisher can use the shared producer.
// Signature matches watermill message.Publisher (variadic Publish).
type MessagePublisher interface {
	Publish(topic string, msg ...*message.Message) error
}

// WebhookPublisher interface for producing webhook events
type WebhookPublisher interface {
	PublishWebhook(ctx context.Context, event *types.WebhookEvent) error
	Close() error
}

// webhookPublisher publishes webhook events to a topic (memory PubSub or shared Kafka producer).
type webhookPublisher struct {
	pubSub   pubsub.PubSub       // used when pubsub is memory
	producer MessagePublisher    // used when pubsub is kafka (shared producer); Close is no-op
	config   *config.Webhook
	logger   *logger.Logger
}

// NewPublisher creates a webhook publisher backed by a PubSub (e.g. in-memory for tests/local).
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

// NewPublisherFromProducer creates a webhook publisher backed by a shared message.Publisher (e.g. Kafka producer).
// Close() is a no-op; the shared producer is closed by fx lifecycle.
func NewPublisherFromProducer(
	producer MessagePublisher,
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

	if p.producer != nil {
		if err := p.producer.Publish(p.config.Topic, msg); err != nil {
			p.logger.Errorw("failed to publish webhook event",
				"error", err,
				"event_id", event.ID,
				"event_name", event.EventName,
				"tenant_id", event.TenantID,
			)
			return err
		}
	} else {
		if err := p.pubSub.Publish(ctx, p.config.Topic, msg); err != nil {
			p.logger.Errorw("failed to publish webhook event",
				"error", err,
				"event_id", event.ID,
				"event_name", event.EventName,
				"tenant_id", event.TenantID,
			)
			return err
		}
	}

	p.logger.Infow("successfully published webhook event",
		"event_id", event.ID,
		"event_name", event.EventName,
		"tenant_id", event.TenantID,
	)

	return nil
}

// Close closes the publisher. No-op when using shared Kafka producer (lifecycle-managed).
func (p *webhookPublisher) Close() error {
	if p.producer != nil {
		return nil
	}
	return p.pubSub.Close()
}
