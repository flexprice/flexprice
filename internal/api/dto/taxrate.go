package dto

import (
	"context"
	"time"

	taxrate "github.com/flexprice/flexprice/internal/domain/tax"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// TaxRateResponse represents the response for tax rate operations
// @Description Tax rate response object containing all tax rate information
type TaxRateResponse struct {
	*taxrate.TaxRate `json:",inline"`
}

// ListTaxRatesResponse represents the response for listing tax rates
// @Description Response object for listing tax rates with pagination
type ListTaxRatesResponse struct {
	// items contains the list of tax rate objects
	Items      []*TaxRateResponse        `json:"items"`
	Pagination *types.PaginationResponse `json:"pagination,omitempty"`
}

// CreateTaxRateRequest represents the request to create a tax rate
// @Description Request object for creating a new tax rate
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

	// valid_from is the ISO 8601 timestamp when this tax rate becomes effective
	ValidFrom *time.Time `json:"valid_from"`

	// valid_to is the ISO 8601 timestamp when this tax rate expires
	ValidTo *time.Time `json:"valid_to"`

	// tax_rate_type determines how the tax is calculated ("percentage" or "fixed")
	TaxRateType types.TaxRateType `json:"tax_rate_type"`

	// scope defines where this tax rate applies
	Scope types.TaxRateScope `json:"scope"`

	// metadata contains additional key-value pairs for storing extra information
	Metadata map[string]string `json:"metadata,omitempty"`

	// currency is the currency of the tax rate
	Currency string `json:"currency" binding:"required"`
}

// UpdateTaxRateRequest represents the request to update a tax rate
// @Description Request object for updating an existing tax rate. All fields are optional - only provided fields will be updated
type UpdateTaxRateRequest struct {
	// name is the updated human-readable name for the tax rate
	Name string `json:"name,omitempty"`

	// code is the updated unique alphanumeric identifier for the tax rate
	Code string `json:"code,omitempty"`

	// description is the updated text description for the tax rate
	Description string `json:"description,omitempty"`

	// valid_from is the updated ISO 8601 timestamp when this tax rate becomes effective
	ValidFrom *time.Time `json:"valid_from,omitempty"`

	// valid_to is the updated ISO 8601 timestamp when this tax rate expires
	ValidTo *time.Time `json:"valid_to,omitempty"`

	// metadata contains updated key-value pairs that will replace existing metadata
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Validate validates the CreateTaxRateRequest
func (r CreateTaxRateRequest) Validate() error {
	if r.Name == "" {
		return ierr.NewError("name is required").
			WithHint("Tax rate name is required").
			Mark(ierr.ErrValidation)
	}

	if r.Code == "" {
		return ierr.NewError("code is required").
			WithHint("Tax rate code is required").
			Mark(ierr.ErrValidation)
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

	if r.ValidFrom != nil && r.ValidFrom.Before(time.Now().UTC()) {
		return ierr.NewError("valid_from cannot be in the past").
			WithHint("Valid from date cannot be in the past").
			Mark(ierr.ErrValidation)
	}

	if r.ValidFrom != nil && r.ValidTo != nil && r.ValidFrom.After(lo.FromPtr(r.ValidTo)) {
		return ierr.NewError("valid_from cannot be after valid_to").
			WithHint("Valid from date cannot be after valid to date").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ToTaxRate converts a CreateTaxRateRequest to a domain TaxRate
func (r CreateTaxRateRequest) ToTaxRate(ctx context.Context) *taxrate.TaxRate {
	id := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE)

	now := time.Now().UTC()
	var validFrom *time.Time
	if r.ValidFrom != nil {
		validFrom = r.ValidFrom
	} else {
		validFrom = lo.ToPtr(now)
	}

	var validTo *time.Time
	if r.ValidTo != nil {
		validTo = r.ValidTo
	}

	taxRate := &taxrate.TaxRate{
		ID:              id,
		Name:            r.Name,
		Code:            r.Code,
		Description:     r.Description,
		PercentageValue: r.PercentageValue,
		FixedValue:      r.FixedValue,
		Currency:        r.Currency,
		Scope:           r.Scope,
		ValidFrom:       validFrom,
		ValidTo:         validTo,
		TaxRateType:     r.TaxRateType,
		Metadata:        r.Metadata,
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	return taxRate
}
