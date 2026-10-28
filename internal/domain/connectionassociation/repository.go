package connectionassociation

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for connection association data operations
type Repository interface {
	Create(ctx context.Context, association *ConnectionAssociation) error
	Get(ctx context.Context, id string) (*ConnectionAssociation, error)
	List(ctx context.Context, filter *types.ConnectionAssociationFilter) ([]*ConnectionAssociation, error)
	// Delete archives the association.
	Delete(ctx context.Context, association *ConnectionAssociation) error
}
