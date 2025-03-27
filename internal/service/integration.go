package service

import (
	"context"
	"fmt"
	"sync"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integrations"
	"github.com/flexprice/flexprice/internal/types"
)

// GetCredentialsFunc creates a function that retrieves credentials using the secret service
func GetCredentialsFunc(secretService SecretService) func(ctx context.Context, provider types.SecretProvider) (map[string]string, error) {
	return func(ctx context.Context, provider types.SecretProvider) (map[string]string, error) {
		return secretService.GetIntegrationCredentials(ctx, provider)
	}
}

// gatewayService implements the GatewayService interface
type gatewayService struct {
	ServiceParams
	gateways         map[types.SecretProvider]map[string]integrations.IntegrationGateway // provider -> tenant -> gateway
	gatewayFactories map[types.SecretProvider]integrations.GatewayFactory                // provider -> factory
	mutex            sync.RWMutex                                                        // protect gateways map
}

// GatewayService defines an interface for accessing gateways
type GatewayService interface {
	// Register a factory for creating gateways of a specific provider
	RegisterGatewayFactory(provider types.SecretProvider, factory integrations.GatewayFactory)

	// Get a specific gateway by provider name, initializing it if needed
	GetGateway(ctx context.Context, provider types.SecretProvider) (integrations.IntegrationGateway, error)

	// List available providers that have registered factories
	ListAvailableProviders(ctx context.Context) []types.SecretProvider

	// Check if a provider is supported (has a registered factory)
	IsProviderSupported(provider types.SecretProvider) bool

	// Clear the gateway cache
	ClearCache()
}

// NewGatewayService creates a new instance of the gateway service
func NewGatewayService(
	params ServiceParams,
) GatewayService {
	return &gatewayService{
		ServiceParams:    params,
		gateways:         make(map[types.SecretProvider]map[string]integrations.IntegrationGateway),
		gatewayFactories: make(map[types.SecretProvider]integrations.GatewayFactory),
		mutex:            sync.RWMutex{},
	}
}

// RegisterGatewayFactory registers a factory function for creating gateways of a specific provider
func (s *gatewayService) RegisterGatewayFactory(provider types.SecretProvider, factory integrations.GatewayFactory) {
	s.gatewayFactories[provider] = factory
	s.Logger.Infow("registered gateway factory", "provider", provider)
}

// GetGateway returns a gateway for the specified provider and tenant
// It will initialize the gateway on-demand if not already done
func (s *gatewayService) GetGateway(ctx context.Context, provider types.SecretProvider) (integrations.IntegrationGateway, error) {
	tenantID := types.GetTenantID(ctx)

	// First check if we already have this gateway initialized
	s.mutex.RLock()
	if tenantGateways, exists := s.gateways[provider]; exists {
		if gateway, exists := tenantGateways[tenantID]; exists {
			s.mutex.RUnlock()
			return gateway, nil
		}
	}
	s.mutex.RUnlock()

	// Gateway not initialized yet, create it
	return s.initializeGateway(ctx, provider, tenantID)
}

// initializeGateway initializes a gateway for a specific provider and tenant
func (s *gatewayService) initializeGateway(ctx context.Context, provider types.SecretProvider, tenantID string) (integrations.IntegrationGateway, error) {
	// Check if we have a factory for this provider
	factory, exists := s.gatewayFactories[provider]
	if !exists {
		return nil, ierr.NewError(fmt.Sprintf("no gateway factory registered for provider %s", provider)).
			WithHint(fmt.Sprintf("%s integration is not supported", provider)).
			Mark(ierr.ErrNotFound)
	}

	secretService := NewSecretService(s.SecretRepo, s.Config, s.Logger)
	// Get the credentials for this provider
	credentials, err := secretService.GetIntegrationCredentials(ctx, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials for provider %s: %w", provider, err)
	}

	// Create the gateway using the factory
	gateway, err := factory(credentials, s.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway for provider %s: %w", provider, err)
	}

	// Cache the gateway
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, exists := s.gateways[provider]; !exists {
		s.gateways[provider] = make(map[string]integrations.IntegrationGateway)
	}
	s.gateways[provider][tenantID] = gateway

	s.Logger.Infow("initialized gateway",
		"provider", provider,
		"tenant_id", tenantID,
		"capabilities", gateway.GetCapabilities())

	return gateway, nil
}

// ListAvailableGateways returns a list of available gateway providers and their capabilities
// This only returns providers that have registered factories
func (s *gatewayService) ListAvailableProviders(ctx context.Context) []types.SecretProvider {
	providers := make([]types.SecretProvider, 0, len(s.gatewayFactories))
	for provider := range s.gatewayFactories {
		providers = append(providers, provider)
	}
	return providers
}

// IsProviderSupported checks if a provider has a registered factory
func (s *gatewayService) IsProviderSupported(provider types.SecretProvider) bool {
	_, exists := s.gatewayFactories[provider]
	return exists
}

// ClearCache clears the gateway cache for testing or maintenance
func (s *gatewayService) ClearCache() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.gateways = make(map[types.SecretProvider]map[string]integrations.IntegrationGateway)
	s.Logger.Infow("cleared gateway cache")
}
