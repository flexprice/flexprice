package priceunit

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// PriceUnitFilter represents filter criteria for listing pricing units
type PriceUnitFilter struct {
	Status        types.Status
	TenantID      string
	EnvironmentID string
}

// PriceUnit represents a unit of pricing in the domain
type PriceUnit struct {
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

// NewPriceUnit creates a new pricing unit with validation
func NewPriceUnit(
	name, code, symbol, baseCurrency string,
	conversionRate decimal.Decimal,
	precision int,
	tenantID, environmentID string,
) (*PriceUnit, error) {
	unit := &PriceUnit{
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

// Validate validates the price unit
func (u *PriceUnit) Validate() error {
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

// ConvertToBaseCurrency converts an amount in pricing unit to base currency
// Formula: amount in fiat currency = amount in pricing unit * conversion rate
func (u *PriceUnit) ConvertToBaseCurrency(customAmount decimal.Decimal) decimal.Decimal {
	return customAmount.Mul(u.ConversionRate)
}

// ConvertFromBaseCurrency converts an amount in base currency to pricing unit
// Formula: amount in pricing unit = amount in fiat currency / conversion rate
func (u *PriceUnit) ConvertFromBaseCurrency(baseAmount decimal.Decimal) decimal.Decimal {
	return baseAmount.Div(u.ConversionRate)
}

// FromEnt converts an ent.PriceUnit to a domain PriceUnit
func FromEnt(e *ent.PriceUnit) *PriceUnit {
	if e == nil {
		return nil
	}

	return &PriceUnit{
		ID:             e.ID,
		Name:           e.Name,
		Code:           e.Code,
		Symbol:         e.Symbol,
		BaseCurrency:   e.BaseCurrency,
		ConversionRate: e.ConversionRate,
		Precision:      e.Precision,
		Status:         types.Status(e.Status),
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      e.UpdatedAt,
		TenantID:       e.TenantID,
		EnvironmentID:  e.EnvironmentID,
	}
}

// FromEntList converts a list of ent.PriceUnit to domain PriceUnit
func FromEntList(list []*ent.PriceUnit) []*PriceUnit {
	if list == nil {
		return nil
	}
	units := make([]*PriceUnit, len(list))
	for i, item := range list {
		units[i] = FromEnt(item)
	}
	return units
}
