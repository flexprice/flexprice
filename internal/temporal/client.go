package temporal

import (
	"github.com/flexprice/flexprice/internal/config"
	"go.temporal.io/sdk/client"
)

// TemporalClient wraps the Temporal SDK client for application use.
type TemporalClient struct {
	Client client.Client
}

// NewTemporalClient creates a new Temporal client using the given configuration.
func NewTemporalClient(cfg *config.TemporalConfig) (*TemporalClient, error) {
	c, err := client.NewClient(cfg.GetClientOptions())
	if err != nil {
		return nil, err
	}
	return &TemporalClient{Client: c}, nil
}
