package config

import (
	"github.com/flexprice/flexprice/internal/types"
)

// EventConfig holds configuration for event processing
type EventConfig struct {
	PublishDestination types.PublishDestination `mapstructure:"publish_destination" default:"kafka"`
}
