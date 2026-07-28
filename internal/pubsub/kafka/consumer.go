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

		// JoinGroup on the events topic does not finish inside the 60s default.
		//
		// Sarama sends this value as JoinGroupRequest.RebalanceTimeout
		// (consumer_group.go:431) and waits that long for the coordinator's
		// response. The events topic has 100 partitions and tens of millions of
		// messages of committed-offset state, so with dozens of members the
		// coordinator does not answer in time. Production showed the read failing
		// at exactly 60s — errors 60.008s apart, all "read tcp ...:9096: i/o
		// timeout" — after which the member reconnects and retries forever. The
		// group ends up with one or two members holding every partition while the
		// rest loop, and the small-topic groups those same processes join stay
		// fully populated.
		//
		// Net.ReadTimeout is raised alongside it: at its 30s default it would clip
		// the same response before the rebalance timeout could apply.
		saramaConfig.Consumer.Group.Rebalance.Timeout = 300 * time.Second
		saramaConfig.Net.ReadTimeout = 300 * time.Second

		// DO NOT set Consumer.Group.Rebalance.GroupStrategies here without
		// deploying every service that shares a consumer group at the same time.
		//
		// The web-consumer and the split main-consumer both join
		// system_events, onboarding_events and integration-events. Kafka requires
		// all members of a group to agree on the assignment protocol, so if one
		// service runs Sticky and the other the Range default, the odd one out
		// fails every join with:
		//
		//   kafka server: The provider group protocol type is incompatible
		//   with the other members
		//
		// It presents as a group stuck Empty or PreparingRebalance with 0
		// partitions owned while the tasks look healthy and loop
		// Starting consuming -> Consuming done. A rolling deploy cannot cross
		// this boundary; both services need scale-to-0 and back.
		//
		// A Sticky rollout hit exactly this in production (2026-07-28) and left
		// meter-usage unable to join its own group. Range (the Sarama default) is
		// used deliberately.
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
