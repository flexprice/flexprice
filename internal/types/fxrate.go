package types

import (
	"slices"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// FXRateScope defines whose FX rate a row configures. The most specific scope
// wins at resolution: subscription, then customer, then tenant.
type FXRateScope string

const (
	FXRateScopeTenant       FXRateScope = "tenant"
	FXRateScopeCustomer     FXRateScope = "customer"
	FXRateScopeSubscription FXRateScope = "subscription"
)

// FXRateScopeIDTenant is the scope_id stored for a tenant-scoped rate.
const FXRateScopeIDTenant = "tenant"

func (s FXRateScope) String() string {
	return string(s)
}

func (s FXRateScope) Validate() error {
	allowedValues := []string{
		FXRateScopeTenant.String(),
		FXRateScopeCustomer.String(),
		FXRateScopeSubscription.String(),
	}
	if !slices.Contains(allowedValues, string(s)) {
		return ierr.NewError("invalid fx rate scope").
			WithHint("Scope must be one of tenant, customer or subscription").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// FXRateFilter represents filters for fx rate queries.
type FXRateFilter struct {
	*QueryFilter
	*TimeRangeFilter
	Filters      []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort         []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	FXRateIDs    []string           `json:"fx_rate_ids,omitempty" form:"fx_rate_ids" validate:"omitempty"`
	Scope        *FXRateScope       `json:"scope,omitempty" form:"scope" validate:"omitempty"`
	ScopeID      *string            `json:"scope_id,omitempty" form:"scope_id" validate:"omitempty"`
	FromCurrency *string            `json:"from_currency,omitempty" form:"from_currency" validate:"omitempty"`
	ToCurrency   *string            `json:"to_currency,omitempty" form:"to_currency" validate:"omitempty"`
}

// NewFXRateFilter creates a new FXRateFilter with default pagination.
func NewFXRateFilter() *FXRateFilter {
	return &FXRateFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitFXRateFilter creates a new FXRateFilter with no pagination limits.
func NewNoLimitFXRateFilter() *FXRateFilter {
	return &FXRateFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the FXRateFilter.
func (f *FXRateFilter) Validate() error {
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

	if f.Filters != nil {
		for _, filter := range f.Filters {
			if err := filter.Validate(); err != nil {
				return err
			}
		}
	}

	if f.Sort != nil {
		for _, sort := range f.Sort {
			if err := sort.Validate(); err != nil {
				return err
			}
		}
	}

	if f.FXRateIDs != nil {
		for _, id := range f.FXRateIDs {
			if id == "" {
				return ierr.NewError("fx_rate_ids cannot contain empty strings").
					WithHint("FX rate IDs must be non-empty strings").
					Mark(ierr.ErrValidation)
			}
		}
	}

	if f.Scope != nil {
		if err := f.Scope.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// GetLimit returns the limit for the FXRateFilter.
func (f *FXRateFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface.
func (f *FXRateFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface.
func (f *FXRateFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface.
func (f *FXRateFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface.
func (f *FXRateFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface.
func (f *FXRateFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited implements BaseFilter interface.
func (f *FXRateFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewNoLimitQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
