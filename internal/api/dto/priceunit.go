package dto

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/priceunit"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

type CreatePriceUnitRequest struct {
	Name   string `json:"name" validate:"required"`
	Code   string `json:"code" validate:"required"`
	Symbol string `json:"symbol" validate:"required"`

	// base_currency  is the currency that the price unit is based on
	BaseCurrency string `json:"base_currency" validate:"required,len=3"`

	// ConversionRate defines the exchange rate from this price unit to the base currency.
	// This rate is used to convert amounts in the custom price unit to the base currency for storage and billing.
	//
	// Conversion formula:
	//   price_unit_amount * conversion_rate = base_currency_amount
	//
	// Example:
	//   If conversion_rate = "0.01" and base_currency = "usd":
	//   100 price_unit tokens * 0.01 = 1.00 USD
	//
	// Note: Rounding precision is determined by the base currency (e.g., USD uses 2 decimal places, JPY uses 0).
	ConversionRate string         `json:"conversion_rate" validate:"required"`
	Metadata       types.Metadata `json:"metadata,omitempty"`
}

func (r *CreatePriceUnitRequest) Validate() error {
	// Base validation
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Ensure currency is lowercase
	r.BaseCurrency = strings.ToLower(r.BaseCurrency)

	// Validate conversion rate
	conversionRate, err := decimal.NewFromString(r.ConversionRate)
	if err != nil {
		return ierr.NewError("invalid conversion rate format").
			WithHint("Conversion rate must be a valid decimal number").
			WithReportableDetails(map[string]interface{}{
				"conversion_rate": r.ConversionRate,
			}).
			Mark(ierr.ErrValidation)
	}

	// Conversion rate must be positive and non-zero
	if conversionRate.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("conversion rate must be positive and non-zero").
			WithHint("Conversion rate must be positive and non-zero").
			WithReportableDetails(map[string]interface{}{
				"conversion_rate": r.ConversionRate,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (r *CreatePriceUnitRequest) ToPriceUnit(ctx context.Context) (*priceunit.PriceUnit, error) {
	conversionRate, err := decimal.NewFromString(r.ConversionRate)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Conversion rate must be a valid decimal number").
			WithReportableDetails(map[string]interface{}{
				"conversion_rate": r.ConversionRate,
			}).
			Mark(ierr.ErrValidation)
	}

	return &priceunit.PriceUnit{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE_UNIT),
		Name:           r.Name,
		Code:           r.Code,
		Symbol:         r.Symbol,
		BaseCurrency:   r.BaseCurrency,
		ConversionRate: conversionRate,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		Metadata:       r.Metadata,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}, nil
}

type CreatePriceUnitResponse struct {
	*priceunit.PriceUnit
}

type PriceUnitResponse struct {
	*priceunit.PriceUnit
}

type UpdatePriceUnitRequest struct {
	Name     *string        `json:"name,omitempty"`
	Metadata types.Metadata `json:"metadata,omitempty"`
}

func (r *UpdatePriceUnitRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// ListPriceUnitsResponse represents the response for listing price units
type ListPriceUnitsResponse = types.ListResponse[*PriceUnitResponse]
