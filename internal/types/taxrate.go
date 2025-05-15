package types

import ierr "github.com/flexprice/flexprice/internal/errors"

// TaxRateFilter represents filters for taxrate queries
type TaxRateFilter struct {
	*QueryFilter
	*TimeRangeFilter
	Filters    []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort       []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	TaxRateIDs []string           `json:"taxrate_ids,omitempty" form:"taxrate_ids" validate:"omitempty"`
	Code       string             `json:"code,omitempty" form:"code" validate:"omitempty"`
}

// NewTaxRateFilter creates a new TaxRateFilter with default values
func NewTaxRateFilter() *TaxRateFilter {
	return &TaxRateFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitTaxRateFilter creates a new TaxRateFilter with no pagination limits
func NewNoLimitTaxRateFilter() *TaxRateFilter {
	return &TaxRateFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the TaxRateFilter
func (f TaxRateFilter) Validate() error {
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

	return nil
}

// GetLimit returns the limit for the TaxRateFilter
func (f TaxRateFilter) GetLimit() int {
	return f.QueryFilter.GetLimit()
}
