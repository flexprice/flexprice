package config

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Webhook represents the configuration for the webhook system
type Webhook struct {
	Enabled         bool                           `mapstructure:"enabled"`
	Topic           string                         `mapstructure:"topic" default:"webhooks"`
	PubSub          types.PubSubType               `mapstructure:"pubsub" default:"memory"`
	MaxRetries      int                            `mapstructure:"max_retries" default:"3"`
	InitialInterval time.Duration                  `mapstructure:"initial_interval" default:"1s"`
	MaxInterval     time.Duration                  `mapstructure:"max_interval" default:"10s"`
	Multiplier      float64                        `mapstructure:"multiplier" default:"2.0"`
	MaxElapsedTime  time.Duration                  `mapstructure:"max_elapsed_time" default:"2m"`
	Tenants         map[string]TenantWebhookConfig `mapstructure:"tenants"`
}

// TenantWebhookConfig represents webhook configuration for a specific tenant
type TenantWebhookConfig struct {
	Endpoint       string            `mapstructure:"endpoint"`
	Headers        map[string]string `mapstructure:"headers"`
	Enabled        bool              `mapstructure:"enabled"`
	ExcludedEvents []string          `mapstructure:"excluded_events"`
}
