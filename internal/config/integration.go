package config

// IntegrationEventsConfig holds configuration for the integration events consumer.
// Integration events are published to the same Kafka topic as webhook events
// (system_events) but consumed by a separate consumer group so that each
// subsystem maintains its own independent offset / replay capability.
type IntegrationEventsConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	ConsumerGroup string `mapstructure:"consumer_group"`
	RateLimit     int64  `mapstructure:"rate_limit"`
}
