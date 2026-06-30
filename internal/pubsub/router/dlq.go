package router

import (
	"context"
	"fmt"

	"github.com/ThreeDotsLabs/watermill"
	watermillKafka "github.com/ThreeDotsLabs/watermill-kafka/v2/pkg/kafka"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/message/router/middleware"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
)

// DynamicPoisonQueue mirrors watermill's PoisonQueue middleware but reads the target
// topic from msg.Metadata["dlq_topic"] at runtime instead of requiring it at
// construction time. This lets a single router-level middleware handle per-handler DLQ
// routing without a registry.
//
// Placement: must be the outermost router middleware so it fires only after the Retry
// middleware has exhausted all attempts.
//
// Behaviour:
//   - dlq_topic set   → stamp poison metadata, publish to that topic via pub, ack.
//   - dlq_topic unset → ack silently (handler has no DLQ).
//   - publish fails   → propagate error so the message is nacked and redelivered.
func DynamicPoisonQueue(pub message.Publisher, log *logger.Logger) message.HandlerMiddleware {
	return func(h message.HandlerFunc) message.HandlerFunc {
		return func(msg *message.Message) ([]*message.Message, error) {
			events, err := h(msg)
			if err == nil {
				return events, nil
			}

			dlqTopic := msg.Metadata.Get("dlq_topic")
			if dlqTopic == "" {
				log.Info(context.Background(), "no DLQ configured for handler, acking silently after retries",
					"handler", message.HandlerNameFromCtx(msg.Context()),
					"message_uuid", msg.UUID,
					"error", err,
				)
				return events, nil
			}

			// Stamp standard poison metadata (same keys as watermill's PoisonQueue).
			msg.Metadata.Set(middleware.ReasonForPoisonedKey, err.Error())
			msg.Metadata.Set(middleware.PoisonedTopicKey, message.SubscribeTopicFromCtx(msg.Context()))
			msg.Metadata.Set(middleware.PoisonedHandlerKey, message.HandlerNameFromCtx(msg.Context()))
			msg.Metadata.Set(middleware.PoisonedSubscriberKey, message.SubscriberNameFromCtx(msg.Context()))

			if pubErr := pub.Publish(dlqTopic, msg); pubErr != nil {
				return events, fmt.Errorf("handler error: %w; DLQ publish to %s failed: %v", err, dlqTopic, pubErr)
			}

			log.Info(context.Background(), "message published to DLQ",
				"handler", message.HandlerNameFromCtx(msg.Context()),
				"dlq_topic", dlqTopic,
				"message_uuid", msg.UUID,
			)
			return events, nil
		}
	}
}

// NewDLQPublisher creates a Kafka publisher used to write failed messages to DLQ topics.
// The publisher is shared across all per-handler DLQ topics — the target topic is passed
// at publish time, so a single producer handles all of them.
func NewDLQPublisher(cfg *config.Configuration, log *logger.Logger) (message.Publisher, error) {
	kc := &cfg.Kafka
	saramaConfig := kafka.GetSaramaConfig(kc)
	if saramaConfig != nil {
		saramaConfig.Producer.Return.Successes = true
		saramaConfig.Producer.Return.Errors = true
	}

	publisher, err := watermillKafka.NewPublisher(
		watermillKafka.PublisherConfig{
			Brokers:               kc.Brokers,
			Marshaler:             watermillKafka.DefaultMarshaler{},
			OverwriteSaramaConfig: saramaConfig,
		},
		watermill.NewStdLogger(false, false),
	)
	if err != nil {
		return nil, err
	}

	log.Info(context.Background(), "DLQ publisher initialized", "brokers", kc.Brokers)
	return publisher, nil
}
