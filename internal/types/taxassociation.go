package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// TaxAssociationFilter represents the filter options for listing tax configs
type TaxAssociationFilter struct {
	*QueryFilter
	*TimeRangeFilter
	TaxAssociationIDs []string          `json:"tax_association_ids,omitempty" form:"tax_association_ids"`
	TaxRateIDs        []string          `json:"tax_rate_ids,omitempty" form:"tax_rate_ids"`
	EntityType        TaxrateEntityType `json:"entity_type,omitempty" form:"entity_type"`
	EntityID          string            `json:"entity_id,omitempty" form:"entity_id"`
	Currency          string            `json:"currency,omitempty" form:"currency"`
	AutoApply         *bool             `json:"auto_apply,omitempty" form:"auto_apply"`
}

// NewTaxAssociationFilter creates a new tax association filter with default options
func NewTaxAssociationFilter() *TaxAssociationFilter {
	return &TaxAssociationFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitTaxAssociationFilter creates a new tax association filter without pagination
func NewNoLimitTaxAssociationFilter() *TaxAssociationFilter {
	return &TaxAssociationFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the tax association filter
func (f *TaxAssociationFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint("invalid query filter").
				Mark(ierr.ErrValidation)
		}
	}
	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint("invalid time range").
				Mark(ierr.ErrValidation)
		}
	}
	if f.EntityType != "" {
		if err := f.EntityType.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint("invalid entity type").
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// GetLimit implements BaseFilter interface
func (f *TaxAssociationFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *TaxAssociationFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *TaxAssociationFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *TaxAssociationFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *TaxAssociationFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *TaxAssociationFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *TaxAssociationFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
