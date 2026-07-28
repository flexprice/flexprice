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
		saramaConfig.Consumer.Group.Session.Timeout = 45000 * time.Millisecond
		saramaConfig.Consumer.Fetch.Min = 1                        // Minimum number of bytes to fetch in a request
		saramaConfig.Consumer.Fetch.Max = 10 * 1024 * 1024         // Maximum number of bytes to fetch (10MB)
		saramaConfig.Consumer.Fetch.Default = 1024 * 1024          // Default fetch size (1MB)
		saramaConfig.Consumer.MaxWaitTime = 100 * time.Millisecond // Max time to wait for new data
		saramaConfig.Consumer.MaxProcessingTime = 500 * time.Millisecond

		// Assignment protocol: RoundRobin preferred, Range as the fallback.
		//
		// GroupStrategies is a priority-ordered list. Kafka requires every member
		// of a group to agree on one protocol, and the coordinator selects the
		// highest entry that ALL members advertise. Keeping Range as the second
		// entry is what makes this safe to roll out incrementally: members on the
		// new image advertise [roundrobin, range] and still negotiate range with
		// members on an older image, so no one is locked out mid-deploy.
		//
		// Dropping Range from this list — or replacing it with a single strategy
		// that other members do not advertise — makes the odd service out fail
		// every join with:
		//
		//   kafka server: The provider group protocol type is incompatible
		//   with the other members
		//
		// It presents as a group stuck Empty or PreparingRebalance with 0
		// partitions owned while the tasks look healthy and loop
		// Starting consuming -> Consuming done. A single-strategy Sticky rollout
		// hit exactly this in production (2026-07-28) and left meter-usage unable
		// to join its own group.
		//
		// The web-consumer, the split main-consumer, the API and the temporal
		// worker all run this same binary and share consumer groups, so RoundRobin
		// only becomes active once every one of them is on an image carrying it.
		saramaConfig.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{
			sarama.BalanceStrategyRoundRobin,
			sarama.BalanceStrategyRange,
		}
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
