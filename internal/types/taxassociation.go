package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// TaxConfigFilter represents the filter options for listing tax configs
type TaxConfigFilter struct {
	*QueryFilter
	*TimeRangeFilter
	TaxConfigIDs []string `json:"tax_config_ids,omitempty" form:"tax_config_ids"`
	TaxRateIDs   []string `json:"tax_rate_ids,omitempty" form:"tax_rate_ids"`
	EntityType   string   `json:"entity_type,omitempty" form:"entity_type"`
	EntityID     string   `json:"entity_id,omitempty" form:"entity_id"`
	Currency     string   `json:"currency,omitempty" form:"currency"`
	AutoApply    *bool    `json:"auto_apply,omitempty" form:"auto_apply"`
}

// NewTaxConfigFilter creates a new tax config filter with default options
func NewTaxConfigFilter() *TaxConfigFilter {
	return &TaxConfigFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitTaxConfigFilter creates a new tax config filter without pagination
func NewNoLimitTaxConfigFilter() *TaxConfigFilter {
	return &TaxConfigFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the tax config filter
func (f *TaxConfigFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return ierr.WithError(err).WithHint("invalid query filter").Mark(ierr.ErrValidation)
		}
	}
	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return ierr.WithError(err).WithHint("invalid time range").Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// GetLimit implements BaseFilter interface
func (f *TaxConfigFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *TaxConfigFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *TaxConfigFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *TaxConfigFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *TaxConfigFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *TaxConfigFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *TaxConfigFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
