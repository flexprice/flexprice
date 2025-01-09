package types

import "github.com/samber/lo"

// QueryFilter represents a generic query filter with optional fields
type QueryFilter struct {
	Limit  *int    `json:"limit,omitempty" form:"limit"`
	Offset *int    `json:"offset,omitempty" form:"offset"`
	Status *Status `json:"status,omitempty" form:"status"`
	Sort   *string `json:"sort,omitempty" form:"sort"`
	Order  *string `json:"order,omitempty" form:"order"`
	Expand *string `json:"expand,omitempty" form:"expand"`
}

// DefaultQueryFilter defines default values for query filters
var DefaultQueryFilter = QueryFilter{
	Limit:  lo.ToPtr(50),
	Offset: lo.ToPtr(0),
	Status: lo.ToPtr(StatusPublished),
	Sort:   lo.ToPtr("created_at"),
	Order:  lo.ToPtr("desc"),
}

// NoLimitQueryFilter returns a filter with no pagination limits
var NoLimitQueryFilter = QueryFilter{
	Status: lo.ToPtr(StatusPublished),
	Sort:   lo.ToPtr("created_at"),
	Order:  lo.ToPtr("desc"),
}

// GetLimit returns the limit value or default if not set
func (f QueryFilter) GetLimit() int {
	if f.Limit == nil {
		return DefaultQueryFilter.GetLimit()
	}
	return *f.Limit
}

// GetOffset returns the offset value or default if not set
func (f QueryFilter) GetOffset() int {
	if f.Offset == nil {
		return DefaultQueryFilter.GetOffset()
	}
	return *f.Offset
}

// GetStatus returns the status value or default if not set
func (f QueryFilter) GetStatus() Status {
	if f.Status == nil {
		return *DefaultQueryFilter.Status
	}
	return *f.Status
}

// GetSort returns the sort value or default if not set
func (f QueryFilter) GetSort() string {
	if f.Sort == nil {
		return *DefaultQueryFilter.Sort
	}
	return *f.Sort
}

// GetOrder returns the order value or default if not set
func (f QueryFilter) GetOrder() string {
	if f.Order == nil {
		return *DefaultQueryFilter.Order
	}
	return *f.Order
}

// GetExpand returns the parsed Expand object from the filter
func (f QueryFilter) GetExpand() Expand {
	if f.Expand == nil {
		return NewExpand("")
	}
	return NewExpand(*f.Expand)
}

// Merge merges another filter into this one, taking values from other if they are set
func (f *QueryFilter) Merge(other QueryFilter) {
	if other.Limit != nil {
		f.Limit = other.Limit
	}
	if other.Offset != nil {
		f.Offset = other.Offset
	}
	if other.Status != nil {
		f.Status = other.Status
	}
	if other.Sort != nil {
		f.Sort = other.Sort
	}
	if other.Order != nil {
		f.Order = other.Order
	}
	if other.Expand != nil {
		f.Expand = other.Expand
	}
}
