package kafka

import (
	"crypto/tls"
	"time"

	"github.com/Shopify/sarama"
	"github.com/flexprice/flexprice/internal/config"
)

func GetSaramaConfig(cfg *config.Configuration) *sarama.Config {
	saramaConfig := sarama.NewConfig()
	saramaConfig.Version = sarama.V2_1_0_0

	// Configure client ID regardless of SASL
	saramaConfig.ClientID = cfg.Kafka.ClientID

	// Set consumer offset reset policy to ensure we don't miss messages
	// "earliest" ensures that when a consumer starts with no initial offset or
	// current offset is out of range, it will start from the earliest message
	saramaConfig.Consumer.Offsets.Initial = sarama.OffsetOldest

	// Enable auto commit to ensure offsets are committed regularly
	saramaConfig.Consumer.Offsets.AutoCommit.Enable = true
	saramaConfig.Consumer.Offsets.AutoCommit.Interval = 5000 * time.Millisecond // 5 seconds

	// When rebalancing happens, use the last committed offset
	saramaConfig.Consumer.Offsets.Retry.Max = 3

	if cfg.Kafka.TLS {
		saramaConfig.Net.TLS.Enable = true
		saramaConfig.Net.TLS.Config = &tls.Config{
			InsecureSkipVerify: false,
		}
	}

	if !cfg.Kafka.UseSASL {
		return saramaConfig
	}

	// SASL specific configs
	saramaConfig.Net.SASL.Enable = true
	saramaConfig.Net.TLS.Enable = true

	// sasl configs
	saramaConfig.Net.SASL.Mechanism = cfg.Kafka.SASLMechanism
	saramaConfig.Net.SASL.User = cfg.Kafka.SASLUser
	saramaConfig.Net.SASL.Password = cfg.Kafka.SASLPassword

	return saramaConfig
}
