package kafka

import (
	"context"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pubsub"
)

type PubSub struct {
	producer *Producer
	consumer *Consumer
	config   *config.Configuration
	logger   *logger.Logger
}

// NewPubSub creates a new kafka-based pubsub
func NewPubSub(
	config *config.Configuration,
	logger *logger.Logger,
	producer *Producer,
	consumer *Consumer,
) pubsub.PubSub {
	return &PubSub{
		producer: producer,
		consumer: consumer,
		config:   config,
		logger:   logger,
	}
}

func NewPubSubFromConfig(
	config *config.Configuration,
	logger *logger.Logger,
	consumerGroupID string,
) (pubsub.PubSub, error) {
	producer, err := NewProducer(config)
	if err != nil {
		logger.Errorw("failed to create producer", "error", err)
		return nil, err
	}

	consumer, err := NewConsumer(config, consumerGroupID)
	if err != nil {
		logger.Errorw("failed to create consumer", "error", err)
		return nil, err
	}

	return NewPubSub(config, logger, producer, consumer), nil
}

// Publish publishes a webhook event
func (p *PubSub) Publish(ctx context.Context, topic string, msg *message.Message) error {
	return p.producer.Publish(topic, msg)
}

// Subscribe starts consuming webhook events
func (p *PubSub) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	return p.consumer.Subscribe(topic)
}

// Close closes the pubsub
func (p *PubSub) Close() error {
	p.producer.Close()
	p.consumer.Close()

	return nil
}
