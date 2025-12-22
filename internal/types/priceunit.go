package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// PriceUnitFilter represents the filter options for price units
type PriceUnitFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// filters allows complex filtering based on multiple fields
	Filters      []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort         []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	PriceUnitIDs []string           `json:"price_unit_ids,omitempty" form:"price_unit_ids" validate:"omitempty"`
}

// NewPriceUnitFilter creates a new price unit filter with default options
func NewPriceUnitFilter() *PriceUnitFilter {
	return &PriceUnitFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitPriceUnitFilter creates a new price unit filter without pagination
func NewNoLimitPriceUnitFilter() *PriceUnitFilter {
	return &PriceUnitFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the filter options
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

	for _, priceUnitID := range f.PriceUnitIDs {
		if priceUnitID == "" {
			return ierr.NewError("price unit ID cannot be empty").
				WithHint("Price unit ID cannot be empty").
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// GetLimit implements BaseFilter interface
func (f *PriceUnitFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *PriceUnitFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetStatus implements BaseFilter interface
func (f *PriceUnitFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetSort implements BaseFilter interface
func (f *PriceUnitFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *PriceUnitFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetExpand implements BaseFilter interface
func (f *PriceUnitFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *PriceUnitFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
