package priceunit

import (
	"context"

	"github.com/shopspring/decimal"
	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for price unit persistence
type Repository interface {
	// CRUD operations

	// Create creates a new price unit and returns the created price unit
	Create(ctx context.Context, priceUnit *PriceUnit) (*PriceUnit, error)

	// List returns a list of pricing units based on filter
	List(ctx context.Context, filter *types.PriceUnitFilter) ([]*PriceUnit, error)

	// Count returns the total count of pricing units based on filter
	Count(ctx context.Context, filter *types.PriceUnitFilter) (int, error)

	// Update updates an existing pricing unit and returns the updated price unit
	Update(ctx context.Context, priceUnit *PriceUnit) (*PriceUnit, error)

	// Delete deletes a pricing unit by its ID
	Delete(ctx context.Context, id string) error

	// Get operations

	// Get fetches a pricing unit by its ID
	Get(ctx context.Context, id string) (*PriceUnit, error)

	// GetByCode fetches a pricing unit by its code, tenant, and environment (optionally status)
	GetByCode(ctx context.Context, code, tenantID, environmentID string, status string) (*PriceUnit, error)

	// Validation operations

	// ExistsByCode checks if a pricing unit with the given code exists for a tenant and environment
	ExistsByCode(ctx context.Context, code string) (bool, error)

	// IsUsedByPrices checks if a pricing unit is being used by any prices
	IsUsedByPrices(ctx context.Context, priceUnitID string) (bool, error)

	// Convert operations

	// ConvertToBaseCurrency converts an amount from pricing unit to base currency
	// amount in fiat currency = amount in pricing unit * conversion_rate
	ConvertToBaseCurrency(ctx context.Context, code, tenantID, environmentID string, priceUnitAmount decimal.Decimal) (decimal.Decimal, error)

	// ConvertToPriceUnit converts an amount from base currency to custom pricing unit
	// amount in pricing unit = amount in fiat currency / conversion_rate
	ConvertToPriceUnit(ctx context.Context, code, tenantID, environmentID string, fiatAmount decimal.Decimal) (decimal.Decimal, error)
}
}
