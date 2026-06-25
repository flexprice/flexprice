package kafka

import (
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-kafka/v2/pkg/kafka"
	"github.com/flexprice/flexprice/internal/config"
	mainkafka "github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/types"
)

type Producer struct {
	*kafka.Publisher
}

func NewProducer(cfg *config.Configuration) (*Producer, error) {
	// enableDebugLogs allows watermill DEBUG messages in debug mode.
	// TRACE is never enabled — it logs every individual message sent/received, which is too noisy.
	enableDebugLogs := cfg.Logging.Level == types.LogLevelDebug

	saramaConfig := GetSaramaConfig(cfg)
	if saramaConfig != nil {
		// add producer configs
		saramaConfig.Producer.Return.Successes = true
		saramaConfig.Producer.Return.Errors = true
	}

	publisher, err := kafka.NewPublisher(
		kafka.PublisherConfig{
			Brokers:               cfg.Kafka.Brokers,
			Marshaler:             kafka.DefaultMarshaler{},
			OverwriteSaramaConfig: saramaConfig,
		},
		watermill.NewStdLogger(enableDebugLogs, false),
	)
	if err != nil {
		return nil, err
	}

	return &Producer{Publisher: publisher}, nil
}

// NewSecondaryProducer builds the optional second-cluster producer for the pubsub path
// (cfg.KafkaSecondary). It enables presence-based dual-write of the non-event topics that
// flow through PubSub (onboarding, wallet alerts, post-processing, feature usage) to the
// second cluster during the AWS→GCP migration. Returns (nil, nil) when no second cluster
// is configured, so callers stay single-cluster. Builds the Sarama config from the second
// cluster's own KafkaConfig (reusing internal/kafka's per-cluster builder), so OAUTHBEARER
// for GMK works identically to the events dual-write. See infrastructure/docs/GCP-CUTOVER-STEPWISE.md.
func NewSecondaryProducer(cfg *config.Configuration) (*Producer, error) {
	if cfg.KafkaSecondary == nil {
		return nil, nil
	}
	enableDebugLogs := cfg.Logging.Level == types.LogLevelDebug

	saramaConfig := mainkafka.GetSaramaConfig(cfg.KafkaSecondary)
	if saramaConfig != nil {
		saramaConfig.Producer.Return.Successes = true
		saramaConfig.Producer.Return.Errors = true
	}

	publisher, err := kafka.NewPublisher(
		kafka.PublisherConfig{
			Brokers:               cfg.KafkaSecondary.Brokers,
			Marshaler:             kafka.DefaultMarshaler{},
			OverwriteSaramaConfig: saramaConfig,
		},
		watermill.NewStdLogger(enableDebugLogs, false),
	)
	if err != nil {
		return nil, err
	}

	return &Producer{Publisher: publisher}, nil
}
