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
}
