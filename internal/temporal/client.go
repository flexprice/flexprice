package temporal

import (
	"context"
	"crypto/tls"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"go.temporal.io/sdk/client"
)

// APIKeyProvider provides headers for API key authentication
type APIKeyProvider struct {
	APIKey    string
	Namespace string
}

// GetHeaders implements client.HeadersProvider
func (a *APIKeyProvider) GetHeaders(_ context.Context) (map[string]string, error) {
	return map[string]string{
		"Authorization":      "Bearer " + a.APIKey,
		"temporal-namespace": a.Namespace,
	}, nil
}

// TemporalClient wraps the Temporal SDK client for application use.
type TemporalClient struct {
	Client client.Client
}

// NewTemporalClient creates a new Temporal client using the given configuration.
func NewTemporalClient(cfg *config.TemporalConfig, log *logger.Logger) (*TemporalClient, error) {
	log.Info("Creating Temporal client with API key provider")

	apiKeyProvider := &APIKeyProvider{
		APIKey:    cfg.APIKey,
		Namespace: cfg.Namespace,
	}

	c, err := client.Dial(client.Options{
		HostPort:        cfg.Address,
		Namespace:       cfg.Namespace,
		HeadersProvider: apiKeyProvider,
		ConnectionOptions: client.ConnectionOptions{
			TLS: &tls.Config{},
		},
	})
	if err != nil {
		log.Error("Failed to create temporal client", "error", err)
		return nil, err
	}

	log.Info("Temporal client created successfully")
	return &TemporalClient{Client: c}, nil
}
