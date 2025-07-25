package priceunit

import (
	"context"

	"github.com/shopspring/decimal"
)

// Repository defines the interface for price unit persistence
type Repository interface {

	// CRUD operations

	// Create creates a new price unit
	Create(ctx context.Context, unit *PriceUnit) error

	// List returns a list of pricing units based on filter
	List(ctx context.Context, filter *PriceUnitFilter) ([]*PriceUnit, error)

	// Count returns the total count of pricing units based on filter
	Count(ctx context.Context, filter *PriceUnitFilter) (int, error)

	// Update updates an existing pricing unit
	Update(ctx context.Context, unit *PriceUnit) error

	// Delete deletes a pricing unit by its ID
	Delete(ctx context.Context, id string) error

	// Get operations

	// GetByCode fetches a pricing unit by its code, tenant, and environment (optionally status)
	GetByCode(ctx context.Context, code, tenantID, environmentID string, status string) (*PriceUnit, error)

	// GetByID fetches a pricing unit by its ID
	GetByID(ctx context.Context, id string) (*PriceUnit, error)

	// GetSymbol returns the symbol for a given code/tenant/env
	GetSymbol(ctx context.Context, code, tenantID, environmentID string) (string, error)

	// GetConversionRate returns the conversion rate for a given code/tenant/env
	GetConversionRate(ctx context.Context, code, tenantID, environmentID string) (decimal.Decimal, error)

	// Convert operations

	// ConvertToBaseCurrency converts an amount from pricing unit to base currency
	// amount in fiat currency = amount in pricing unit * conversion_rate
	ConvertToBaseCurrency(ctx context.Context, code, tenantID, environmentID string, priceUnitAmount decimal.Decimal) (decimal.Decimal, error)

	// ConvertToPriceUnit converts an amount from base currency to custom pricing unit
	// amount in pricing unit = amount in fiat currency / conversion_rate
	ConvertToPriceUnit(ctx context.Context, code, tenantID, environmentID string, fiatAmount decimal.Decimal) (decimal.Decimal, error)
}
