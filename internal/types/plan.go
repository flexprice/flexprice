package types

import "fmt"

// PlanFilter represents the filter options for plans
type PlanFilter struct {
	*QueryFilter
	*TimeRangeFilter
	PlanIDs []string `json:"plan_ids,omitempty" form:"plan_ids"`
}

// NewPlanFilter creates a new plan filter with default options
func NewPlanFilter() *PlanFilter {
	return &PlanFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitPlanFilter creates a new plan filter without pagination
func NewNoLimitPlanFilter() *PlanFilter {
	return &PlanFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the filter options
func (f *PlanFilter) Validate() error {
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

	for _, planID := range f.PlanIDs {
		if planID == "" {
			return fmt.Errorf("plan id can not be empty")
		}
	}
	return nil
}

// GetLimit implements BaseFilter interface
func (f *PlanFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *PlanFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetStatus implements BaseFilter interface
func (f *PlanFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetSort implements BaseFilter interface
func (f *PlanFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *PlanFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetExpand implements BaseFilter interface
func (f *PlanFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *PlanFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
