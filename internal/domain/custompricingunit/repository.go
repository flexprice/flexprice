package custompricingunit

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/shopspring/decimal"
)

// Repository defines the interface for custom pricing unit persistence
// This abstracts away the underlying storage (ent, etc) for use in services and other modules
type Repository interface {
	// GetByID fetches a custom pricing unit by its ID
	GetByID(ctx context.Context, id string) (*ent.CustomPricingUnit, error)

	// GetByCode fetches a custom pricing unit by its code, tenant, and environment (optionally status)
	GetByCode(ctx context.Context, code, tenantID, environmentID string, status string) (*ent.CustomPricingUnit, error)

	// GetConversionRate returns the conversion rate for a given code/tenant/env
	GetConversionRate(ctx context.Context, code, tenantID, environmentID string) (decimal.Decimal, error)

	// GetSymbol returns the symbol for a given code/tenant/env
	GetSymbol(ctx context.Context, code, tenantID, environmentID string) (string, error)

	// ConvertToBaseCurrency returns the converted amount for a given code/tenant/env and amount
	ConvertToBaseCurrency(ctx context.Context, code, tenantID, environmentID string, customAmount decimal.Decimal) (decimal.Decimal, error)
}
