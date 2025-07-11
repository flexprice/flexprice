package custompricingunit

import (
	"context"

	"github.com/shopspring/decimal"
)

// Repository defines the interface for custom pricing unit persistence
type Repository interface {
	// Create creates a new custom pricing unit
	Create(ctx context.Context, unit *CustomPricingUnit) error

	// GetByID fetches a custom pricing unit by its ID
	GetByID(ctx context.Context, id string) (*CustomPricingUnit, error)

	// List returns a list of custom pricing units based on filter
	List(ctx context.Context, filter *CustomPricingUnitFilter) ([]*CustomPricingUnit, error)

	// Count returns the total count of custom pricing units based on filter
	Count(ctx context.Context, filter *CustomPricingUnitFilter) (int, error)

	// Update updates an existing custom pricing unit
	Update(ctx context.Context, unit *CustomPricingUnit) error

	// Delete deletes a custom pricing unit by its ID
	Delete(ctx context.Context, id string) error

	// GetByCode fetches a custom pricing unit by its code, tenant, and environment (optionally status)
	GetByCode(ctx context.Context, code, tenantID, environmentID string, status string) (*CustomPricingUnit, error)

	// GetConversionRate returns the conversion rate for a given code/tenant/env
	GetConversionRate(ctx context.Context, code, tenantID, environmentID string) (decimal.Decimal, error)

	// ConvertToBaseCurrency converts an amount from custom pricing unit to base currency
	// amount in fiat currency = amount in custom currency * conversion_rate
	ConvertToBaseCurrency(ctx context.Context, code, tenantID, environmentID string, customAmount decimal.Decimal) (decimal.Decimal, error)

	// ConvertToPriceUnit converts an amount from base currency to custom pricing unit
	// amount in custom currency = amount in fiat currency / conversion_rate
	ConvertToPriceUnit(ctx context.Context, code, tenantID, environmentID string, fiatAmount decimal.Decimal) (decimal.Decimal, error)
}
