package entityintegrationmapping

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for entity integration mapping data access
type Repository interface {
	Create(ctx context.Context, mapping *EntityIntegrationMapping) error
	Get(ctx context.Context, id string) (*EntityIntegrationMapping, error)
	List(ctx context.Context, filter *types.EntityIntegrationMappingFilter) ([]*EntityIntegrationMapping, error)
	Count(ctx context.Context, filter *types.EntityIntegrationMappingFilter) (int, error)
	Update(ctx context.Context, mapping *EntityIntegrationMapping) error
	Delete(ctx context.Context, mapping *EntityIntegrationMapping) error

	// GetByEntity looks up a mapping by its (entity_type, entity_id, provider_type)
	// tuple — the same tuple the unique index is built on. Returns ierr.ErrNotFound
	// if no mapping exists.
	GetByEntity(ctx context.Context, entityType types.IntegrationEntityType, entityID string, providerType string) (*EntityIntegrationMapping, error)
}
