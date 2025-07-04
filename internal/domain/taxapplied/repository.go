package taxapplied

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	Create(ctx context.Context, taxapplied *TaxApplied) error
	Get(ctx context.Context, id string) (*TaxApplied, error)
	Update(ctx context.Context, taxapplied *TaxApplied) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.TaxAppliedFilter) ([]*TaxApplied, error)
}
