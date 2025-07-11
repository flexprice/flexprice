package dto

import (
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/types"
)

// CreateCustomPricingUnitRequest represents the request to create a new custom pricing unit
type CreateCustomPricingUnitRequest struct {
	Name           string           `json:"name" validate:"required"`
	Code           string           `json:"code" validate:"required,len=3"`
	Symbol         string           `json:"symbol" validate:"required,max=10"`
	BaseCurrency   string           `json:"base_currency" validate:"required,len=3"`
	ConversionRate *decimal.Decimal `json:"conversion_rate" validate:"required,gt=0"`
	Precision      int              `json:"precision" validate:"gte=0,lte=8"`
}

// UpdateCustomPricingUnitRequest represents the request to update an existing custom pricing unit
type UpdateCustomPricingUnitRequest struct {
	Name      string       `json:"name,omitempty" validate:"omitempty"`
	Symbol    string       `json:"symbol,omitempty" validate:"omitempty,max=10"`
	Precision int          `json:"precision,omitempty" validate:"omitempty,gte=0,lte=8"`
	Status    types.Status `json:"status" validate:"omitempty,oneof=published archived deleted"`
}

// CustomPricingUnitResponse represents the response for custom pricing unit operations
type CustomPricingUnitResponse struct {
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

// ListCustomPricingUnitsResponse represents the paginated response for listing custom pricing units
type ListCustomPricingUnitsResponse struct {
	Units      []CustomPricingUnitResponse `json:"units"`
	TotalCount int                         `json:"total_count"`
}

// CustomPricingUnitFilter represents the filter options for listing custom pricing units
type CustomPricingUnitFilter struct {
	Status types.Status `json:"status,omitempty" validate:"omitempty,oneof=published archived deleted"`
}
