package integrations

import (
	"context"
	"fmt"

	domainConnection "github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/secret"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integrations"
	"github.com/flexprice/flexprice/internal/integrations/stripe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// TODO: Add cache for gateways
type gatewayManager struct {
	connectionRepo domainConnection.Repository
	secretRepo     secret.Repository
	logger         *logger.Logger

	// Map of provider types to factory functions
	factories map[string]integrations.GatewayFactory
}

// NewGatewayManager creates a gateway manager with registered factories
func NewGatewayManager(
	connectionRepo domainConnection.Repository,
	secretRepo secret.Repository,
	logger *logger.Logger,
) GatewayManager {
	manager := &gatewayManager{
		connectionRepo: connectionRepo,
		secretRepo:     secretRepo,
		logger:         logger,
		factories:      make(map[string]integrations.GatewayFactory),
	}

	// Register factories
	manager.factories[string(types.SecretProviderStripe)] = stripe.NewStripeGatewayFactory()
	return manager
}

// GetGatewayByConnectionCode gets a gateway by connection code
func (m *gatewayManager) GetGatewayByConnectionCode(ctx context.Context, connectionCode string) (integrations.IntegrationGateway, error) {
	// Get connection by code
	conn, err := m.connectionRepo.GetByConnectionCode(ctx, connectionCode)
	if err != nil {
		return nil, err
	}

	// Get factory for provider type
	factory, exists := m.factories[string(conn.ProviderType)]
	if !exists {
		return nil, ierr.NewError(fmt.Sprintf("Unsupported provider type: %s", conn.ProviderType)).
			Mark(ierr.ErrValidation)
	}

	// Get credentials from secret service
	secret, err := m.secretRepo.Get(ctx, conn.SecretID)
	if err != nil {
		return nil, err
	}

	// Create gateway instance
	gateway, err := factory(secret.ProviderData, m.logger)
	if err != nil {
		return nil, err
	}

	return gateway, nil
}

// GetGatewayByProvider gets a gateway by provider type
func (m *gatewayManager) GetGatewaysByProvider(ctx context.Context, providerType string) ([]integrations.IntegrationGateway, error) {
	// Get connection by provider type
	connections, err := m.connectionRepo.GetByProviderType(ctx, types.SecretProvider(providerType))
	if err != nil {
		return nil, err
	}

	gateways := make([]integrations.IntegrationGateway, 0)
	for _, conn := range connections {
		gateway, err := m.GetGatewayByConnectionCode(ctx, conn.ConnectionCode)
		if err != nil {
			return nil, err
		}
		gateways = append(gateways, gateway)
	}

	return gateways, nil
}

func (m *gatewayManager) HasProviderConfiguration(ctx context.Context, providerType string) (bool, error) {
	filter := &types.ConnectionFilter{
		ProviderType: []types.SecretProvider{
			types.SecretProvider(providerType),
		},
		QueryFilter: &types.QueryFilter{
			Limit: lo.ToPtr(1),
		},
	}

	connections, err := m.connectionRepo.List(ctx, filter)
	if err != nil {
		return false, err
	}

	return len(connections) > 0, nil
}
