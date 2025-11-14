package types

import ierr "github.com/flexprice/flexprice/internal/errors"

// CostSheetFilter represents the filter options for costsheet
type CostSheetFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// filters allows complex filtering based on multiple fields
	Filters      []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort         []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	CostSheetIDs []string           `json:"costsheet_ids,omitempty" form:"costsheet_ids" validate:"omitempty"`
}

// NewCostSheetFilter creates a new costsheet filter with default options
func NewCostSheetFilter() *CostSheetFilter {
	return &CostSheetFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitCostSheetFilter creates a new costsheet filter without pagination
func NewNoLimitCostSheetFilter() *CostSheetFilter {
	return &CostSheetFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the filter options
func (f *CostSheetFilter) Validate() error {
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

	for _, costsheetID := range f.CostSheetIDs {
		if costsheetID == "" {
			return ierr.NewError("costsheet id can not be empty").
				WithHint("Costsheet id can not be empty").
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// GetLimit implements BaseFilter interface
func (f *CostSheetFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *CostSheetFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetStatus implements BaseFilter interface
func (f *CostSheetFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetSort implements BaseFilter interface
func (f *CostSheetFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *CostSheetFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetExpand implements BaseFilter interface
func (f *CostSheetFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *CostSheetFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
