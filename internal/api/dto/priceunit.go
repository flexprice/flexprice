package dto

import (
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/types"
)

// CreatePriceUnitRequest represents the request to create a new pricing unit
type CreatePriceUnitRequest struct {
	Name           string           `json:"name" validate:"required"`
	Code           string           `json:"code" validate:"required,len=3"`
	Symbol         string           `json:"symbol" validate:"required,max=10"`
	BaseCurrency   string           `json:"base_currency" validate:"required,len=3"`
	ConversionRate *decimal.Decimal `json:"conversion_rate" validate:"required,gt=0"`
	Precision      int              `json:"precision" validate:"gte=0,lte=8"`
}

// UpdatePricingUnitRequest represents the request to update an existing pricing unit
type UpdatePriceUnitRequest struct {
	Name           string           `json:"name,omitempty" validate:"omitempty"`
	Symbol         string           `json:"symbol,omitempty" validate:"omitempty,max=10"`
	Precision      int              `json:"precision,omitempty" validate:"omitempty,gte=0,lte=8"`
	ConversionRate *decimal.Decimal `json:"conversion_rate,omitempty" validate:"omitempty,gt=0"`
}

// PricingUnitResponse represents the response for pricing unit operations
type PriceUnitResponse struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Code           string          `json:"code"`
	Symbol         string          `json:"symbol"`
	BaseCurrency   string          `json:"base_currency"`
	ConversionRate decimal.Decimal `json:"conversion_rate"`
	Precision      int             `json:"precision"`
	Status         types.Status    `json:"status"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
}

// ListPricingUnitsResponse represents the paginated response for listing pricing units
type ListPriceUnitsResponse = types.ListResponse[*PriceUnitResponse]
