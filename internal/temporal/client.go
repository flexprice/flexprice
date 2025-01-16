package temporal

import (
	"github.com/flexprice/flexprice/internal/config"
	"go.temporal.io/sdk/client"
)

type TemporalClient struct {
	Client client.Client
}

func NewTemporalClient(cfg *config.TemporalConfig) (*TemporalClient, error) {
	c, err := client.NewClient(cfg.GetClientOptions())
	if err != nil {
		return nil, err
	}

	return &TemporalClient{
		Client: c,
	}, nil
}
