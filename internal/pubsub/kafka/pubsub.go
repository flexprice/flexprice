package kafka

import (
	"context"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pubsub"
)

// messagePublisher is the subset of *Producer that PubSub depends on. *Producer satisfies it;
// tests substitute a fake to exercise the dual-write fan-out without a broker (mirrors the
// seam in internal/kafka EventPublisher).
type messagePublisher interface {
	Publish(topic string, messages ...*message.Message) error
	Close() error
}

type PubSub struct {
	producer messagePublisher
	// secondary is the optional second-cluster producer (cfg.KafkaSecondary). When non-nil,
	// every Publish ALSO writes to it (presence-based dual-write for the AWS→GCP migration);
	// nil ⇒ single-cluster. Its failures are logged, never fatal to the primary write.
	secondary messagePublisher
	consumer  *Consumer
	config    *config.Configuration
	logger    *logger.Logger
}

// NewPubSub creates a new kafka-based pubsub. secondary may be nil (single-cluster).
func NewPubSub(
	config *config.Configuration,
	logger *logger.Logger,
	producer *Producer,
	secondary *Producer,
	consumer *Consumer,
) pubsub.PubSub {
	ps := &PubSub{
		producer: producer,
		consumer: consumer,
		config:   config,
		logger:   logger,
	}
	// Only set the interface field when a real producer is present. A typed-nil *Producer
	// stored in an interface is itself non-nil, which would break the `p.secondary != nil`
	// guard in Publish and panic. NewSecondaryProducer returns nil when KafkaSecondary is unset.
	if secondary != nil {
		ps.secondary = secondary
	}
	return ps
}

func NewPubSubFromConfig(
	config *config.Configuration,
	logger *logger.Logger,
	consumerGroupID string,
) (pubsub.PubSub, error) {
	producer, err := NewProducer(config)
	if err != nil {
		logger.Error(context.Background(), "failed to create producer", "error", err)
		return nil, err
	}

	// Optional dual-write producer (present only when cfg.KafkaSecondary is set).
	// Built eagerly like the primary, so a misconfigured/unreachable second cluster
	// surfaces as a boot error (rolling update protects capacity) rather than silent loss.
	secondary, err := NewSecondaryProducer(config)
	if err != nil {
		logger.Error(context.Background(), "failed to create secondary producer", "error", err)
		return nil, err
	}

	consumer, err := NewConsumer(config, consumerGroupID)
	if err != nil {
		logger.Error(context.Background(), "failed to create consumer", "error", err)
		return nil, err
	}

	return NewPubSub(config, logger, producer, secondary, consumer), nil
}

// Publish writes to the local cluster and, when a second cluster is configured, also writes
// there. The two writes are independent: a secondary failure is logged but does not block or
// fail the primary write (mirrors EventPublisher's events dual-write). A fresh message copy is
// handed to the secondary because the watermill publisher consumes the message it is given.
func (p *PubSub) Publish(ctx context.Context, topic string, msg *message.Message) error {
	var secondaryMsg *message.Message
	if p.secondary != nil {
		secondaryMsg = msg.Copy()
	}

	err := p.producer.Publish(topic, msg)

	if p.secondary != nil {
		if serr := p.secondary.Publish(topic, secondaryMsg); serr != nil {
			// Same message + cluster label as EventPublisher.logFailure so one
			// alert/grep ("kafka publish failed", cluster=secondary) covers both
			// dual-write paths.
			p.logger.Error(ctx, "kafka publish failed",
				"cluster", "secondary",
				"topic", topic,
				"message_id", secondaryMsg.UUID,
				"tenant_id", secondaryMsg.Metadata.Get("tenant_id"),
				"error", serr,
			)
		}
	}

	return err
}

// Subscribe starts consuming webhook events
func (p *PubSub) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	return p.consumer.Subscribe(topic)
}

// Close closes the pubsub
func (p *PubSub) Close() error {
	p.producer.Close()
	if p.secondary != nil {
		p.secondary.Close()
	}
	p.consumer.Close()

	return nil
}
