package kafka

import (
	"context"
	"time"

	"github.com/Shopify/sarama"
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

func NewConsumer(cfg *config.Configuration, consumerGroupID string) (*Consumer, error) {
	// enableDebugLogs allows watermill DEBUG messages in debug mode.
	// TRACE is never enabled — it logs every individual message sent/received, which is too noisy.
	enableDebugLogs := cfg.Logging.Level == types.LogLevelDebug

	saramaConfig := GetSaramaConfig(cfg)
	if saramaConfig != nil {
		// Optimize consumer configs for throughput
		// TODO: move this to config
		saramaConfig.Consumer.Group.Session.Timeout = 60000 * time.Millisecond
		saramaConfig.Consumer.Fetch.Min = 1                        // Minimum number of bytes to fetch in a request
		saramaConfig.Consumer.Fetch.Max = 10 * 1024 * 1024         // Maximum number of bytes to fetch (10MB)
		saramaConfig.Consumer.Fetch.Default = 1024 * 1024          // Default fetch size (1MB)
		saramaConfig.Consumer.MaxWaitTime = 100 * time.Millisecond // Max time to wait for new data

		// MaxProcessingTime must exceed the real per-message handler latency,
		// otherwise Sarama treats normal processing as a stall: it stops pumping
		// the partition channel, heartbeats starve, the member is evicted, and the
		// group rebalances — repeatedly. Handlers here do a synchronous ClickHouse
		// INSERT per message (observed p50 ~1s, p99 ~21s), so 500ms was ~2-40x too
		// low and kept the group in a permanent PreparingRebalance loop.
		saramaConfig.Consumer.MaxProcessingTime = 30 * time.Second

		// Sticky assignment preserves each member's previous partitions across a
		// rebalance instead of Range's full reshuffle, so a membership change costs
		// far less redistribution. Still an eager protocol (all partitions are
		// revoked then reassigned) — Sarama does not implement KIP-429 cooperative
		// rebalancing — but convergence is much faster with a large group.
		saramaConfig.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{
			sarama.BalanceStrategySticky,
		}

		// Give slow members time to rejoin before the coordinator drops them.
		saramaConfig.Consumer.Group.Rebalance.Timeout = 120 * time.Second
	}

	subscriber, err := kafka.NewSubscriber(
		kafka.SubscriberConfig{
			Brokers:               cfg.Kafka.Brokers,
			ConsumerGroup:         consumerGroupID,
			Unmarshaler:           kafka.DefaultMarshaler{},
			OverwriteSaramaConfig: saramaConfig,
			ReconnectRetrySleep:   time.Second,
		},
		watermill.NewStdLogger(enableDebugLogs, false),
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
