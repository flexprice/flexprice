package kafka

import (
	"github.com/Shopify/sarama"
	"github.com/flexprice/flexprice/internal/config"
)

func GetSaramaConfig(cfg *config.Configuration) *sarama.Config {
	if !cfg.Kafka.UseSASL {
		return nil
	}

	saramaConfig := sarama.NewConfig()

	// default configs
	saramaConfig.Version = sarama.V2_1_0_0
	saramaConfig.Net.SASL.Enable = true
	saramaConfig.Net.TLS.Enable = true

	// sasl configs
	saramaConfig.Net.SASL.Mechanism = cfg.Kafka.SASLMechanism
	saramaConfig.Net.SASL.User = cfg.Kafka.SASLUser
	saramaConfig.Net.SASL.Password = cfg.Kafka.SASLPassword
	saramaConfig.ClientID = cfg.Kafka.ClientID

	return saramaConfig
}
