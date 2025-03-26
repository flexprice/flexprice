package integration

import "context"

// Repository defines the interface for IntegrationEntity repository.
type Repository interface {
	Create(ctx context.Context, connection *IntegrationEntity) error
	Get(ctx context.Context, id string) (*IntegrationEntity, error)
	GetByEntityAndProvider(ctx context.Context, entityType, entityID, providerType string) (*IntegrationEntity, error)
	GetByProviderID(ctx context.Context, entityType, providerID, providerType string) (*IntegrationEntity, error)
	List(ctx context.Context, filter *IntegrationEntityFilter) ([]*IntegrationEntity, error)
	Count(ctx context.Context, filter *IntegrationEntityFilter) (int, error)
	Update(ctx context.Context, connection *IntegrationEntity) error
	Delete(ctx context.Context, id string) error
}
