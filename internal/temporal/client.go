package temporal

import (
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"go.temporal.io/sdk/client"
)

// TemporalClient wraps the Temporal SDK client for application use.
type TemporalClient struct {
	Client client.Client
}

// NewTemporalClient creates a new Temporal client using the given configuration.
func NewTemporalClient(cfg *config.TemporalConfig, log *logger.Logger) (*TemporalClient, error) {
	c, err := client.NewClient(client.Options{
		HostPort:  cfg.Address,
		Namespace: cfg.Namespace,
	})
	if err != nil {
		log.Error("Failed to create temporal client", "error", err)
		return nil, err
	}

	log.Info("Temporal client created successfully")
	return &TemporalClient{Client: c}, nil
}
