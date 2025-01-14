package config

import (
	"github.com/flexprice/flexprice/internal/types"
)

// EventConfig holds configuration for event processing
type EventConfig struct {
	PublishDestination types.PublishDestination `mapstructure:"publish_destination" default:"kafka"`
	WebhookURL         string                   `mapstructure:"webhook_url"`
	WebhookSecret      string                   `mapstructure:"webhook_secret"`
}

// WebhookConfig holds webhook settings
type WebhookConfig struct {
	URL    string `mapstructure:"url"`
	Secret string `mapstructure:"secret"`
}
