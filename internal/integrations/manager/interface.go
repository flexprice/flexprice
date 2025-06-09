package integrations

import (
	"context"

	"github.com/flexprice/flexprice/internal/integrations"
)

// GatewayManager provides access to integration gateways using connection codes
type GatewayManager interface {
	// Get a gateway instance using a specific connection code
	GetGatewayByConnectionCode(ctx context.Context, connectionCode string) (integrations.IntegrationGateway, error)

	// Get a gateway instance using provider type
	GetGatewaysByProvider(ctx context.Context, providerType string) ([]integrations.IntegrationGateway, error)

	// Check if a provider has any configurations
	HasProviderConfiguration(ctx context.Context, providerType string) (bool, error)
}
