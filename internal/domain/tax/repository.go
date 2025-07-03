package taxrate

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for taxrate persistence operations
type Repository interface {
	// Core operations
	Create(ctx context.Context, taxrate *TaxRate) error
	Get(ctx context.Context, id string) (*TaxRate, error)
	List(ctx context.Context, filter *types.TaxRateFilter) ([]*TaxRate, error)
	ListAll(ctx context.Context, filter *types.TaxRateFilter) ([]*TaxRate, error)
	Count(ctx context.Context, filter *types.TaxRateFilter) (int, error)
	Update(ctx context.Context, taxrate *TaxRate) error
	Delete(ctx context.Context, taxrate *TaxRate) error

	// Lookup operations
	GetByCode(ctx context.Context, code string) (*TaxRate, error)
}
