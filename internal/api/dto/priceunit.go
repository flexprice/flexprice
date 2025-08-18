package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"

	domainPriceUnit "github.com/flexprice/flexprice/internal/domain/priceunit"
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

func (r *CreatePriceUnitRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	return nil
}

func (r *CreatePriceUnitRequest) ToPriceUnit(ctx context.Context) *domainPriceUnit.PriceUnit {
	return &domainPriceUnit.PriceUnit{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE_UNIT),
		Name:           r.Name,
		Code:           r.Code,
		Symbol:         r.Symbol,
		BaseCurrency:   r.BaseCurrency,
		ConversionRate: *r.ConversionRate,
		Precision:      r.Precision,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
}

// UpdatePricingUnitRequest represents the request to update an existing pricing unit
type UpdatePriceUnitRequest struct {
	Name   string `json:"name,omitempty" validate:"omitempty"`
	Symbol string `json:"symbol,omitempty" validate:"omitempty,max=10"`
}

// PricingUnitResponse represents the response for pricing unit operations
type PriceUnitResponse struct {
	*domainPriceUnit.PriceUnit
}

// ListPricingUnitsResponse represents the paginated response for listing pricing units
type ListPriceUnitsResponse = types.ListResponse[*PriceUnitResponse]
