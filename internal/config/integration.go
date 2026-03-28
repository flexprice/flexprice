package config

// IntegrationEventsConfig holds configuration for the integration events consumer.
// Integration events are published to the same Kafka topic as webhook events
// (system_events) but consumed by a separate consumer group so that each
// subsystem maintains its own independent offset / replay capability.
type IntegrationEventsConfig struct {
	// Enabled controls whether the integration event handler is registered.
	// Defaults to true; set to false to disable without removing wiring.
	Enabled bool `mapstructure:"enabled" default:"true"`

	// ConsumerGroup is the Kafka consumer group used by the integration events
	// handler. Must be unique across all consumers reading system_events.
	// Default: "integration-events-consumer"
	ConsumerGroup string `mapstructure:"consumer_group" default:"integration-events-consumer"`

	// RateLimit caps the number of messages processed per second.
	// Default: 10 (integration events are low-volume compared to raw events).
	RateLimit int64 `mapstructure:"rate_limit" default:"10"`
}
