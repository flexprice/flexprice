package taxconfig

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	// Core operations
	Create(ctx context.Context, taxconfig *TaxConfig) error
	Get(ctx context.Context, id string) (*TaxConfig, error)
	Update(ctx context.Context, taxconfig *TaxConfig) error
	Delete(ctx context.Context, taxconfig *TaxConfig) error
	List(ctx context.Context, filter *types.TaxConfigFilter) ([]*TaxConfig, error)
	Count(ctx context.Context, filter *types.TaxConfigFilter) (int, error)
}
