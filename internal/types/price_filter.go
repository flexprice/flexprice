package types

// PriceFilter represents filters for price queries
type PriceFilter struct {
	QueryFilter
	PlanIDs []string `json:"plan_ids,omitempty" form:"plan_ids"`
}

// NewPriceFilter creates a new PriceFilter with default values
func NewPriceFilter() PriceFilter {
	return PriceFilter{
		QueryFilter: DefaultQueryFilter,
	}
}

// NewUnlimitedPriceFilter creates a new PriceFilter with no pagination limits
func NewUnlimitedPriceFilter() PriceFilter {
	return PriceFilter{
		QueryFilter: NoLimitQueryFilter,
	}
}

// WithPlanIDs adds plan IDs to the filter
func (f PriceFilter) WithPlanIDs(planIDs []string) PriceFilter {
	f.PlanIDs = planIDs
	return f
}

// WithStatus sets the status on the filter
func (f PriceFilter) WithStatus(status Status) PriceFilter {
	f.Status = &status
	return f
}

// WithExpand sets the expand field on the filter
func (f PriceFilter) WithExpand(expand string) PriceFilter {
	f.Expand = &expand
	return f
}
