package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// DefaultTaxRateConfigFilter represents the filter options for listing default tax rate configs
type DefaultTaxRateConfigFilter struct {
	*QueryFilter
	*TimeRangeFilter
	DefaultTaxRateConfigIDs []string `json:"default_tax_rate_config_ids,omitempty" form:"default_tax_rate_config_ids"`
	TaxRateIDs              []string `json:"tax_rate_ids,omitempty" form:"tax_rate_ids"`
	EntityType              string   `json:"entity_type,omitempty" form:"entity_type"`
	EntityID                string   `json:"entity_id,omitempty" form:"entity_id"`
	Currency                string   `json:"currency,omitempty" form:"currency"`
}

// NewDefaultTaxRateConfigFilter creates a new default tax rate config filter with default options
func NewDefaultTaxRateConfigFilter() *DefaultTaxRateConfigFilter {
	return &DefaultTaxRateConfigFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitDefaultTaxRateConfigFilter creates a new default tax rate config filter without pagination
func NewNoLimitDefaultTaxRateConfigFilter() *DefaultTaxRateConfigFilter {
	return &DefaultTaxRateConfigFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the default tax rate config filter
func (f *DefaultTaxRateConfigFilter) Validate() error {
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
func (f *DefaultTaxRateConfigFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *DefaultTaxRateConfigFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *DefaultTaxRateConfigFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *DefaultTaxRateConfigFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *DefaultTaxRateConfigFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *DefaultTaxRateConfigFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *DefaultTaxRateConfigFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
