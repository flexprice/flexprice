package types

// GroupFilter represents the filter options for groups
type GroupFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// filters allows complex filtering based on multiple fields
	Filters []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	// Group specific filters
	GroupIDs   []string `form:"group_ids" json:"group_ids"`
	LookupKey  string   `form:"lookup_key" json:"lookup_key"`
	EntityType string   `form:"entity_type" json:"entity_type"`
	Name       string   `form:"name" json:"name"`
}

// NewGroupFilter creates a new group filter with default options
func NewGroupFilter() *GroupFilter {
	return &GroupFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitGroupFilter creates a new group filter without pagination
func NewNoLimitGroupFilter() *GroupFilter {
	return &GroupFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// GetLimit returns the limit value, handling nil QueryFilter
func (f *GroupFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return 0
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset returns the offset value, handling nil QueryFilter
func (f *GroupFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return 0
	}
	return f.QueryFilter.GetOffset()
}

// GetStatus returns the status value, handling nil QueryFilter
func (f *GroupFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return ""
	}
	return f.QueryFilter.GetStatus()
}

// GetSort returns the sort field, handling nil QueryFilter
func (f *GroupFilter) GetSort() string {
	if f.QueryFilter == nil {
		return ""
	}
	return f.QueryFilter.GetSort()
}

// GetOrder returns the sort order, handling nil QueryFilter
func (f *GroupFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return ""
	}
	return f.QueryFilter.GetOrder()
}

// Validate validates the group filter
func (f *GroupFilter) Validate() error {
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
