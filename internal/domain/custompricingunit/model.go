package custompricingunit

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CustomPricingUnitFilter represents filter criteria for listing custom pricing units
type CustomPricingUnitFilter struct {
	Status        types.Status
	TenantID      string
	EnvironmentID string
}

// CustomPricingUnit represents a custom unit of pricing in the domain
type CustomPricingUnit struct {
	ID             string
	Name           string
	Code           string
	Symbol         string
	BaseCurrency   string
	ConversionRate decimal.Decimal
	Precision      int
	Status         types.Status
	CreatedAt      time.Time
	UpdatedAt      time.Time
	TenantID       string
	EnvironmentID  string
}

// NewCustomPricingUnit creates a new custom pricing unit with validation
func NewCustomPricingUnit(
	name, code, symbol, baseCurrency string,
	conversionRate decimal.Decimal,
	precision int,
	tenantID, environmentID string,
) (*CustomPricingUnit, error) {
	unit := &CustomPricingUnit{
		Name:           name,
		Code:           code,
		Symbol:         symbol,
		BaseCurrency:   baseCurrency,
		ConversionRate: conversionRate,
		Precision:      precision,
		Status:         types.StatusPublished,
		TenantID:       tenantID,
		EnvironmentID:  environmentID,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := unit.Validate(); err != nil {
		return nil, err
	}

	return unit, nil
}

// Validate validates the custom pricing unit
func (u *CustomPricingUnit) Validate() error {
	if u.Name == "" {
		return ierr.NewError("name is required").
			WithHint("Name is required").
			Mark(ierr.ErrValidation)
	}

	if len(u.Code) != 3 {
		return ierr.NewError("code must be exactly 3 characters").
			WithHint("Code must be exactly 3 characters").
			Mark(ierr.ErrValidation)
	}

	if len(u.Symbol) > 10 {
		return ierr.NewError("symbol cannot exceed 10 characters").
			WithHint("Symbol cannot exceed 10 characters").
			Mark(ierr.ErrValidation)
	}

	if len(u.BaseCurrency) != 3 {
		return ierr.NewError("base currency must be exactly 3 characters").
			WithHint("Base currency must be exactly 3 characters").
			Mark(ierr.ErrValidation)
	}

	if u.ConversionRate.IsZero() || u.ConversionRate.IsNegative() {
		return ierr.NewError("conversion rate must be positive").
			WithHint("Conversion rate must be positive").
			Mark(ierr.ErrValidation)
	}

	if u.Precision < 0 || u.Precision > 8 {
		return ierr.NewError("precision must be between 0 and 8").
			WithHint("Precision must be between 0 and 8").
			Mark(ierr.ErrValidation)
	}

	if u.Status != types.StatusPublished && u.Status != types.StatusArchived && u.Status != types.StatusDeleted {
		return ierr.NewError("invalid status").
			WithHint("Status must be one of: published, archived, deleted").
			Mark(ierr.ErrValidation)
	}

	if u.TenantID == "" {
		return ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}

	if u.EnvironmentID == "" {
		return ierr.NewError("environment ID is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// ConvertToBaseCurrency converts an amount in custom currency to base currency
// Formula: amount in fiat currency = amount in custom currency * conversion rate
func (u *CustomPricingUnit) ConvertToBaseCurrency(customAmount decimal.Decimal) decimal.Decimal {
	return customAmount.Mul(u.ConversionRate)
}

// ConvertFromBaseCurrency converts an amount in base currency to custom currency
// Formula: amount in custom currency = amount in fiat currency / conversion rate
func (u *CustomPricingUnit) ConvertFromBaseCurrency(baseAmount decimal.Decimal) decimal.Decimal {
	return baseAmount.Div(u.ConversionRate)
}

// Update updates the mutable fields of the custom pricing unit
func (u *CustomPricingUnit) Update(
	name, symbol string,
	conversionRate decimal.Decimal,
	precision int,
	status types.Status,
) error {
	if name != "" {
		u.Name = name
	}
	if symbol != "" {
		u.Symbol = symbol
	}
	if !conversionRate.IsZero() && !conversionRate.IsNegative() {
		u.ConversionRate = conversionRate
	}
	if precision >= 0 && precision <= 8 {
		u.Precision = precision
	}
	if status != "" && (status == types.StatusPublished || status == types.StatusArchived || status == types.StatusDeleted) {
		u.Status = status
	}

	u.UpdatedAt = time.Now()
	return u.Validate()
}
