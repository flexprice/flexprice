package kafka

import (
	"context"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-kafka/v2/pkg/kafka"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
)

type MessageConsumer interface {
	Subscribe(topic string) (<-chan *message.Message, error)
	Close() error
}

type Consumer struct {
	subscriber message.Subscriber
	cfg        *config.Configuration
}

func NewConsumer(cfg *config.Configuration) (MessageConsumer, error) {
	enableDebugLogs := cfg.Logging.Level == types.LogLevelDebug

	saramaConfig := GetSaramaConfig(cfg)
	if saramaConfig != nil {
		// Optimize consumer configs for throughput
		// TODO: move this to config
		saramaConfig.Consumer.Group.Session.Timeout = 45000 * time.Millisecond
		saramaConfig.Consumer.Fetch.Min = 1                        // Minimum number of bytes to fetch in a request
		saramaConfig.Consumer.Fetch.Max = 10 * 1024 * 1024         // Maximum number of bytes to fetch (10MB)
		saramaConfig.Consumer.Fetch.Default = 1024 * 1024          // Default fetch size (1MB)
		saramaConfig.Consumer.MaxWaitTime = 100 * time.Millisecond // Max time to wait for new data
		saramaConfig.Consumer.MaxProcessingTime = 500 * time.Millisecond
	}

	subscriber, err := kafka.NewSubscriber(
		kafka.SubscriberConfig{
			Brokers:               cfg.Kafka.Brokers,
			ConsumerGroup:         cfg.Kafka.ConsumerGroup,
			Unmarshaler:           kafka.DefaultMarshaler{},
			OverwriteSaramaConfig: saramaConfig,
			ReconnectRetrySleep:   time.Second,
		},
		watermill.NewStdLogger(enableDebugLogs, enableDebugLogs),
	)
	if err != nil {
		return nil, err
	}

	return &Consumer{
		subscriber: subscriber,
		cfg:        cfg,
	}, nil
}

func (c *Consumer) Subscribe(topic string) (<-chan *message.Message, error) {
	return c.subscriber.Subscribe(context.Background(), topic)
}

func (c *Consumer) Close() error {
	return c.subscriber.Close()
}
