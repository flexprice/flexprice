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

	// DefaultTaxRateConfig operations
	GetDefaultTaxRateConfigByID(ctx context.Context, id string) (*DefaultTaxRateConfig, error)
	ListDefaultTaxRateConfigs(ctx context.Context, f *types.DefaultTaxRateConfigFilter) ([]*DefaultTaxRateConfig, error)
	CountDefaultTaxRateConfigs(ctx context.Context, f *types.DefaultTaxRateConfigFilter) (int, error)
	UpdateDefaultTaxRateConfig(ctx context.Context, id string, config *DefaultTaxRateConfig) error
	CreateDefaultTaxRateConfig(ctx context.Context, config *DefaultTaxRateConfig) error
	DeleteDefaultTaxRateConfig(ctx context.Context, id string) error
}
