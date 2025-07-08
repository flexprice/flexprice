package types

import ierr "github.com/flexprice/flexprice/internal/errors"

// TaxAppliedFilter represents filters for taxapplied queries
type TaxAppliedFilter struct {
	*QueryFilter
	*TimeRangeFilter
	Filters          []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort             []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	TaxRateIDs       []string           `json:"taxrate_ids,omitempty" form:"taxrate_ids" validate:"omitempty"`
	EntityType       TaxrateEntityType  `json:"entity_type,omitempty" form:"entity_type" validate:"omitempty"`
	EntityID         string             `json:"entity_id,omitempty" form:"entity_id" validate:"omitempty"`
	TaxAssociationID string             `json:"tax_association_id,omitempty" form:"tax_association_id" validate:"omitempty"`
}

// NewTaxAppliedFilter creates a new TaxAppliedFilter with default values
func NewTaxAppliedFilter() *TaxAppliedFilter {
	return &TaxAppliedFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

func NewDefaultTaxAppliedFilter() *TaxAppliedFilter {
	return &TaxAppliedFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitTaxAppliedFilter creates a new TaxAppliedFilter with no pagination limits
func NewNoLimitTaxAppliedFilter() *TaxAppliedFilter {
	return &TaxAppliedFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the TaxAppliedFilter
func (f *TaxAppliedFilter) Validate() error {
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

	if f.TaxRateIDs != nil {
		for _, id := range f.TaxRateIDs {
			if id == "" {
				return ierr.NewError("taxrate_ids cannot contain empty strings").
					WithHint("Taxrate IDs must be non-empty strings").
					Mark(ierr.ErrValidation)
			}
		}
	}

	if f.EntityType != "" {
		if err := f.EntityType.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// GetLimit returns the limit for the TaxAppliedFilter
func (f *TaxAppliedFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *TaxAppliedFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *TaxAppliedFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *TaxAppliedFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *TaxAppliedFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *TaxAppliedFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited implements BaseFilter interface
func (f *TaxAppliedFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewNoLimitQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
