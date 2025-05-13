package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/taxrate"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// TaxRateResponse represents the response for tax rate operations
type TaxRateResponse struct {
	*taxrate.TaxRate `json:",inline"`
}

// ListTaxRatesResponse represents the response for listing tax rates
type ListTaxRatesResponse struct {
	Items      []*TaxRateResponse        `json:"items"`
	Pagination *types.PaginationResponse `json:"pagination,omitempty"`
}

// AppliedTax represents a tax that has been applied to an amount
type AppliedTax struct {
	TaxRateID  string          `json:"tax_rate_id"`
	Code       string          `json:"code"`
	Percentage decimal.Decimal `json:"percentage"`
	FixedValue decimal.Decimal `json:"fixed_value"`
	Amount     decimal.Decimal `json:"amount"`
}

// CreateTaxRateRequest represents the request to create a tax rate
type CreateTaxRateRequest struct {
	Name        string     `json:"name" validate:"required"`
	Code        string     `json:"code" validate:"required"`
	Description string     `json:"description,omitempty"`
	Percentage  float64    `json:"percentage" default:"0"`
	FixedValue  float64    `json:"fixed_value" default:"0"`
	IsCompound  bool       `json:"is_compound"`
	ValidFrom   *time.Time `json:"valid_from"`
	ValidTo     *time.Time `json:"valid_to"`
}

// UpdateTaxRateRequest represents the request to update a tax rate
type UpdateTaxRateRequest struct {
	Name        string     `json:"name,omitempty"`
	Code        string     `json:"code,omitempty"`
	Description string     `json:"description,omitempty"`
	Percentage  *float64   `json:"percentage,omitempty"`
	FixedValue  *float64   `json:"fixed_value,omitempty"`
	IsCompound  *bool      `json:"is_compound,omitempty"`
	ValidFrom   *time.Time `json:"valid_from,omitempty"`
	ValidTo     *time.Time `json:"valid_to,omitempty"`
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

	if r.Percentage < 0 {
		return ierr.NewError("percentage cannot be negative").
			WithHint("Tax rate percentage must be non-negative").
			Mark(ierr.ErrValidation)
	}

	if r.FixedValue < 0 {
		return ierr.NewError("fixed_value cannot be negative").
			WithHint("Tax rate fixed value must be non-negative").
			Mark(ierr.ErrValidation)
	}

	// Ensure exactly one of percentage or fixedValue is provided
	if (r.Percentage > 0 && r.FixedValue > 0) || (r.Percentage == 0 && r.FixedValue == 0) {
		return ierr.NewError("exactly one of percentage or fixed_value must be provided").
			WithHint("Tax rate must have either a percentage or fixed value, but not both").
			Mark(ierr.ErrValidation)
	}

	// If percentage is provided, ensure it does not exceed 100%
	if r.Percentage > 100 {
		return ierr.NewError("percentage cannot exceed 100%").
			WithHint("Tax rate percentage must be between 0 and 100").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ToTaxRate converts a CreateTaxRateRequest to a domain TaxRate
func (r CreateTaxRateRequest) ToTaxRate(ctx context.Context) (*taxrate.TaxRate, error) {
	id := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_RATE)

	now := time.Now().UTC()
	var validFrom *time.Time
	if r.ValidFrom != nil && !r.ValidFrom.IsZero() {
		validFrom = r.ValidFrom
	} else {
		validFrom = lo.ToPtr(now)
	}

	var validTo *time.Time
	if r.ValidTo != nil && !r.ValidTo.IsZero() {
		validTo = r.ValidTo
	}

	// Set either percentage or fixedValue, but not both
	percentage := r.Percentage
	fixedValue := r.FixedValue

	if percentage > 0 {
		fixedValue = 0 // Zero out fixedValue if percentage is set
	} else if fixedValue > 0 {
		percentage = 0 // Zero out percentage if fixedValue is set
	}

	return &taxrate.TaxRate{
		ID:          id,
		Name:        r.Name,
		Code:        r.Code,
		Description: r.Description,
		Percentage:  percentage,
		FixedValue:  fixedValue,
		IsCompound:  r.IsCompound,
		ValidFrom:   validFrom,
		ValidTo:     validTo,
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}, nil
}
