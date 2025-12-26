package priceunit

import (
	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// PriceUnit represents a unit of pricing in the domain
type PriceUnit struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Code           string          `json:"code"`
	Symbol         string          `json:"symbol"`
	BaseCurrency   string          `json:"base_currency"`
	ConversionRate decimal.Decimal `json:"conversion_rate" swaggertype:"string"`
	Precision      int             `json:"precision"`
	EnvironmentID  string          `json:"environment_id"`
	types.BaseModel
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
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
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

// PriceUnitFilter represents filter criteria for listing pricing units
type PriceUnitFilter struct {
	// QueryFilter contains pagination and basic query parameters
	QueryFilter *types.QueryFilter

	// TimeRangeFilter allows filtering by time periods
	TimeRangeFilter *types.TimeRangeFilter

	// Filters allows complex filtering based on multiple fields
	Filters []*types.FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`

	// Sort allows sorting by multiple fields
	Sort []*types.SortCondition `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	// Status filters by price unit status
	Status types.Status `json:"status,omitempty" form:"status"`

	// TenantID filters by specific tenant ID
	TenantID string `json:"tenant_id,omitempty" form:"tenant_id"`

	// EnvironmentID filters by specific environment ID
	EnvironmentID string `json:"environment_id,omitempty" form:"environment_id"`
}

// GetLimit implements BaseFilter interface
func (f *PriceUnitFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *PriceUnitFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *PriceUnitFilter) GetSort() string {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *PriceUnitFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *PriceUnitFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *PriceUnitFilter) GetExpand() types.Expand {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns true if this is an unlimited query
func (f *PriceUnitFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return types.NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}

// Validate validates the filter
func (f *PriceUnitFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}

	return nil
}
