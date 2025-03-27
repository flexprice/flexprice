package integration

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for IntegrationEntity repository.
type Repository interface {
	Create(ctx context.Context, connection *IntegrationEntity) error
	Get(ctx context.Context, id string) (*IntegrationEntity, error)
	GetByEntityAndProvider(ctx context.Context, entityType types.EntityType, entityID string, providerType types.SecretProvider) (*IntegrationEntity, error)
	GetByProviderID(ctx context.Context, entityType types.EntityType, providerID string, providerType types.SecretProvider) (*IntegrationEntity, error)
	List(ctx context.Context, filter *IntegrationEntityFilter) ([]*IntegrationEntity, error)
	Count(ctx context.Context, filter *IntegrationEntityFilter) (int, error)
	Update(ctx context.Context, connection *IntegrationEntity) error
	Delete(ctx context.Context, id string) error
}
