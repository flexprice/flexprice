package config

import (
	"github.com/flexprice/flexprice/internal/types"
)

// EventConfig holds configuration for event processing
type EventConfig struct {
	PublishDestination types.PublishDestination `mapstructure:"publish_destination" default:"kafka"`
	// BulkPublishEnabled makes POST /v1/events/bulk publish batched messages on
	// kafka.topic_bulk instead of one message per event. Off by default: batched events do
	// not reach the consumers subscribed to `events`, so only enable it where a bulk
	// consumer is deployed.
	BulkPublishEnabled bool `mapstructure:"bulk_publish_enabled" default:"false"`
}
