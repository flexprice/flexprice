package taxassociation

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	// Core operations
	Create(ctx context.Context, taxassociation *TaxAssociation) error
	Get(ctx context.Context, id string) (*TaxAssociation, error)
	Update(ctx context.Context, taxassociation *TaxAssociation) error
	Delete(ctx context.Context, taxassociation *TaxAssociation) error
	List(ctx context.Context, filter *types.TaxAssociationFilter) ([]*TaxAssociation, error)
	Count(ctx context.Context, filter *types.TaxAssociationFilter) (int, error)
}
