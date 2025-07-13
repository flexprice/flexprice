package dto

import (
	"context"

	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CreateTaxRateRequest represents the request to create a tax rate
type CreateTaxRateRequest struct {
	// name is the human-readable name for the tax rate (required)
	Name string `json:"name" validate:"required"`

	// code is the unique alpxuhanumeric identifier for the tax rate (required)
	Code string `json:"code" validate:"required"`

	// description is an optional text description providing details about the tax rate
	Description string `json:"description,omitempty"`

	// percentage_value is the percentage value (0-100) when tax_rate_type is "percentage"
	PercentageValue *decimal.Decimal `json:"percentage_value,omitempty"`

	// fixed_value is the fixed monetary amount when tax_rate_type is "fixed"
	FixedValue *decimal.Decimal `json:"fixed_value,omitempty"`

	// tax_rate_type determines how the tax is calculated ("percentage" or "fixed")
	TaxRateType types.TaxRateType `json:"tax_rate_type"`

	// scope defines where this tax rate applies
	Scope *types.TaxRateScope `json:"scope,omitempty"`

	// metadata contains additional key-value pairs for storing extra information
	Metadata map[string]string `json:"metadata,omitempty"`
}

// UpdateTaxRateRequest represents the request to update a tax rate
type UpdateTaxRateRequest struct {
	// name is the updated human-readable name for the tax rate
	Name string `json:"name,omitempty"`

	// code is the updated unique alphanumeric identifier for the tax rate
	Code string `json:"code,omitempty"`

	// description is the updated text description for the tax rate
	Description string `json:"description,omitempty"`

	// metadata contains updated key-value pairs that will replace existing metadata
	Metadata map[string]string `json:"metadata,omitempty"`

	// tax_rate_type determines how the tax is calculated ("percentage" or "fixed")
	TaxRateStatus *types.TaxRateStatus `json:"tax_rate_status,omitempty"`
}

// Validate validates the UpdateTaxRateRequest
func (r UpdateTaxRateRequest) Validate() error {
	if err := validator.ValidateRequest(&r); err != nil {
		return err
	}

	if r.TaxRateStatus != nil {
		if err := r.TaxRateStatus.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// Validate validates the CreateTaxRateRequest
func (r CreateTaxRateRequest) Validate() error {
	if err := validator.ValidateRequest(&r); err != nil {
		return err
	}

	if err := r.TaxRateType.Validate(); err != nil {
		return err
	}

	if err := r.Scope.Validate(); err != nil {
		return err
	}

	if r.TaxRateType == types.TaxRateTypePercentage {
		if r.PercentageValue == nil {
			return ierr.NewError("percentage_value is required").
				WithHint("Tax rate percentage value is required").
				Mark(ierr.ErrValidation)
		}

		if r.PercentageValue.IsNegative() || r.PercentageValue.GreaterThan(decimal.NewFromInt(100)) {
			return ierr.NewError("percentage_value cannot be negative").
				WithHint("Tax rate percentage must be in range 0-100").
				Mark(ierr.ErrValidation)
		}

	}

	if r.TaxRateType == types.TaxRateTypeFixed {
		if r.FixedValue == nil {
			return ierr.NewError("fixed_value is required").
				WithHint("Tax rate fixed value is required").
				Mark(ierr.ErrValidation)
		}

		if r.FixedValue.IsNegative() {
			return ierr.NewError("fixed_value cannot be negative").
				WithHint("Tax rate fixed value cannot be less than 0").
				Mark(ierr.ErrValidation)
		}

	}

	if r.PercentageValue != nil && r.FixedValue != nil {
		return ierr.NewError("percentage_value and fixed_value cannot be provided together").
			WithHint("Tax rate must have either a percentage or fixed value, but not both").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ToTaxRate converts a CreateTaxRateRequest to a domain TaxRate
func (r CreateTaxRateRequest) ToTaxRate(ctx context.Context) *taxrate.TaxRate {

	// if taxrate scope is not provided, set it to internal
	if r.Scope == nil {
		r.Scope = lo.ToPtr(types.TaxRateScopeInternal)
	}

	taxRate := &taxrate.TaxRate{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE),
		Name:            r.Name,
		Code:            r.Code,
		Description:     r.Description,
		PercentageValue: r.PercentageValue,
		FixedValue:      r.FixedValue,
		Scope:           lo.FromPtr(r.Scope),
		TaxRateType:     r.TaxRateType,
		Metadata:        r.Metadata,
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	return taxRate
}

// TaxRateResponse represents the response for tax rate operations
type TaxRateResponse struct {
	*taxrate.TaxRate `json:",inline"`
}

// ListTaxRatesResponse represents the response for listing tax rates
type ListTaxRatesResponse = types.ListResponse[*TaxRateResponse]
