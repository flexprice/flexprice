package temporal

import (
	"github.com/flexprice/flexprice/internal/config"
	"go.temporal.io/sdk/client"
)

// NewClient creates a new Temporal client
func NewClient(cfg *config.Configuration) (client.Client, error) {
	options := client.Options{
		HostPort:  cfg.Temporal.Address,
		Namespace: cfg.Temporal.Namespace,
		Identity:  cfg.Temporal.ClientName,
	}

	return client.NewClient(options)
}
