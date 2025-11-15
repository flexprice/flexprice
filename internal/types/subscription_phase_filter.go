package types

import "time"

// SubscriptionPhaseFilter defines filters for querying subscription phases
type SubscriptionPhaseFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// Specific filters
	SubscriptionIDs []string   `json:"subscription_ids,omitempty" form:"subscription_ids"`
	PhaseIDs        []string   `json:"phase_ids,omitempty" form:"phase_ids"`
	ActiveOnly      bool       `json:"active_only,omitempty" form:"active_only"`
	ActiveAt        *time.Time `json:"active_at,omitempty" form:"active_at"`
}

// NewSubscriptionPhaseFilter creates a new subscription phase filter with default options
func NewSubscriptionPhaseFilter() *SubscriptionPhaseFilter {
	return &SubscriptionPhaseFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitSubscriptionPhaseFilter creates a new subscription phase filter without pagination
func NewNoLimitSubscriptionPhaseFilter() *SubscriptionPhaseFilter {
	return &SubscriptionPhaseFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the subscription phase filter
func (f *SubscriptionPhaseFilter) Validate() error {
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

// GetLimit returns the limit value for the filter
func (f *SubscriptionPhaseFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset returns the offset value for the filter
func (f *SubscriptionPhaseFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort returns the sort value for the filter
func (f *SubscriptionPhaseFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder returns the order value for the filter
func (f *SubscriptionPhaseFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus returns the status value for the filter
func (f *SubscriptionPhaseFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand returns the expand value for the filter
func (f *SubscriptionPhaseFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns whether the filter has unlimited pagination
func (f *SubscriptionPhaseFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return false
	}
	return f.QueryFilter.IsUnlimited()
}
