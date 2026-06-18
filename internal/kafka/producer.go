package kafka

import (
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-kafka/v2/pkg/kafka"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
)

type Producer struct {
	*kafka.Publisher
}

// NewProducer builds the producer for this deployment's local Kafka cluster (cfg.Kafka) —
// the cluster it consumes from and always writes to.
func NewProducer(cfg *config.Configuration) (*Producer, error) {
	return newProducerForCluster(&cfg.Kafka, cfg.Logging.Level == types.LogLevelDebug)
}

// SecondaryProducer wraps the optional second-cluster producer so it can be provided
// through fx as a type distinct from the local *Producer. The embedded *Producer is nil
// when the second cluster is not write-enabled or not configured, in which case publishing
// is single-cluster (the wrapper itself is always non-nil so fx has a value to inject).
type SecondaryProducer struct {
	*Producer
}

// NewSecondaryProducer is the fx provider for the optional second Kafka cluster
// (cfg.KafkaSecondary). Dual-write is presence-based: it builds a real producer only when
// cfg.KafkaSecondary is configured, otherwise it returns an empty wrapper (nil inner) so it
// is always injectable. A configured cluster that fails to build is a boot-time error; a
// runtime outage of the second cluster is tolerated by the publish path (its failure is
// logged, not fatal). See infrastructure/docs/GCP-CUTOVER-STEPWISE.md.
func NewSecondaryProducer(cfg *config.Configuration) (*SecondaryProducer, error) {
	if cfg.KafkaSecondary == nil {
		return &SecondaryProducer{}, nil
	}
	producer, err := newProducerForCluster(cfg.KafkaSecondary, cfg.Logging.Level == types.LogLevelDebug)
	if err != nil {
		return nil, err
	}
	return &SecondaryProducer{Producer: producer}, nil
}

func newProducerForCluster(kafkaCfg *config.KafkaConfig, enableDebugLogs bool) (*Producer, error) {
	// enableDebugLogs allows watermill DEBUG messages in debug mode.
	// TRACE is never enabled — it logs every individual message sent/received, which is too noisy.
	saramaConfig := GetSaramaConfig(kafkaCfg)
	if saramaConfig != nil {
		// add producer configs
		saramaConfig.Producer.Return.Successes = true
		saramaConfig.Producer.Return.Errors = true
	}

	publisher, err := kafka.NewPublisher(
		kafka.PublisherConfig{
			Brokers:               kafkaCfg.Brokers,
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
