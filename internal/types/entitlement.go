package types

// EntitlementFilter defines filters for querying entitlements
type EntitlementFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// Specific filters for entitlements
	PlanIDs     []string     `form:"plan_ids" json:"plan_ids,omitempty"`
	FeatureIDs  []string     `form:"feature_ids" json:"feature_ids,omitempty"`
	FeatureType *FeatureType `form:"feature_type" json:"feature_type,omitempty"`
	IsEnabled   *bool        `form:"is_enabled" json:"is_enabled,omitempty"`
}

// NewDefaultEntitlementFilter creates a new EntitlementFilter with default values
func NewDefaultEntitlementFilter() *EntitlementFilter {
	return &EntitlementFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitEntitlementFilter creates a new EntitlementFilter with no pagination limits
func NewNoLimitEntitlementFilter() *EntitlementFilter {
	return &EntitlementFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the filter fields
func (f EntitlementFilter) Validate() error {
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

// WithPlanIDs adds plan IDs to the filter
func (f *EntitlementFilter) WithPlanIDs(planIDs []string) *EntitlementFilter {
	f.PlanIDs = planIDs
	return f
}

// WithFeatureID adds feature ID to the filter
func (f *EntitlementFilter) WithFeatureID(featureID string) *EntitlementFilter {
	if f.FeatureIDs == nil {
		f.FeatureIDs = []string{}
	}
	f.FeatureIDs = append(f.FeatureIDs, featureID)
	return f
}

// WithFeatureType adds feature type to the filter
func (f *EntitlementFilter) WithFeatureType(featureType FeatureType) *EntitlementFilter {
	f.FeatureType = &featureType
	return f
}

// WithIsEnabled adds is_enabled to the filter
func (f *EntitlementFilter) WithIsEnabled(isEnabled bool) *EntitlementFilter {
	f.IsEnabled = &isEnabled
	return f
}

// WithStatus sets the status on the filter
func (f *EntitlementFilter) WithStatus(status Status) *EntitlementFilter {
	f.Status = &status
	return f
}

// WithExpand sets the expand on the filter
func (f *EntitlementFilter) WithExpand(expand string) *EntitlementFilter {
	f.Expand = &expand
	return f
}

// GetLimit implements BaseFilter interface
func (f *EntitlementFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *EntitlementFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *EntitlementFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *EntitlementFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *EntitlementFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *EntitlementFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns true if this is an unlimited query
func (f *EntitlementFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
