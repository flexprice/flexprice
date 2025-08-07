package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// AddonType represents the type of addon
type AddonType string

const (
	AddonTypeSingleInstance   AddonType = "single"
	AddonTypeMultipleInstance AddonType = "multiple"
)

func (at AddonType) Validate() error {
	if at != AddonTypeSingleInstance && at != AddonTypeMultipleInstance {
		return ierr.NewError("invalid addon type").
			WithHint("Addon type must be single or multiple").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// AddonStatus represents the status of a subscription addon
type AddonStatus string

const (
	AddonStatusActive    AddonStatus = "active"
	AddonStatusCancelled AddonStatus = "cancelled"
	AddonStatusPaused    AddonStatus = "paused"
)

// AddonFilter represents the filter options for addons
type AddonFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// filters allows complex filtering based on multiple fields
	Filters []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	AddonIDs   []string  `json:"addon_ids,omitempty" form:"addon_ids" validate:"omitempty"`
	AddonType  AddonType `json:"addon_type,omitempty" form:"addon_type" validate:"omitempty"`
	LookupKeys []string  `json:"lookup_keys,omitempty" form:"lookup_keys" validate:"omitempty"`
}

// NewAddonFilter creates a new addon filter with default options
func NewAddonFilter() *AddonFilter {
	return &AddonFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitAddonFilter creates a new addon filter without pagination
func NewNoLimitAddonFilter() *AddonFilter {
	return &AddonFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the filter options
func (f *AddonFilter) Validate() error {
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

	for _, addonID := range f.AddonIDs {
		if addonID == "" {
			return ierr.NewError("addon id can not be empty").
				WithHint("Addon info can not be empty").
				Mark(ierr.ErrValidation)
		}
	}

	for _, lookupKey := range f.LookupKeys {
		if lookupKey == "" {
			return ierr.NewError("lookup key can not be empty").
				WithHint("Lookup key can not be empty").
				Mark(ierr.ErrValidation)
		}
	}

	if f.AddonType != "" {
		if err := f.AddonType.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// GetLimit implements BaseFilter interface
func (f *AddonFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *AddonFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *AddonFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *AddonFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *AddonFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *AddonFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *AddonFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}

// SubscriptionAddonFilter represents the filter options for subscription addons
type SubscriptionAddonFilter struct {
	*QueryFilter
	*TimeRangeFilter

	SubscriptionIDs []string      `json:"subscription_ids,omitempty" form:"subscription_ids" validate:"omitempty"`
	AddonIDs        []string      `json:"addon_ids,omitempty" form:"addon_ids" validate:"omitempty"`
	AddonStatuses   []AddonStatus `json:"addon_statuses,omitempty" form:"addon_statuses" validate:"omitempty"`
}

// NewSubscriptionAddonFilter creates a new subscription addon filter with default options
func NewSubscriptionAddonFilter() *SubscriptionAddonFilter {
	return &SubscriptionAddonFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitSubscriptionAddonFilter creates a new subscription addon filter without pagination
func NewNoLimitSubscriptionAddonFilter() *SubscriptionAddonFilter {
	return &SubscriptionAddonFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the filter options
func (f *SubscriptionAddonFilter) Validate() error {
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

	for _, subscriptionID := range f.SubscriptionIDs {
		if subscriptionID == "" {
			return ierr.NewError("subscription id can not be empty").
				WithHint("Subscription info can not be empty").
				Mark(ierr.ErrValidation)
		}
	}

	for _, addonID := range f.AddonIDs {
		if addonID == "" {
			return ierr.NewError("addon id can not be empty").
				WithHint("Addon info can not be empty").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// GetLimit implements BaseFilter interface
func (f *SubscriptionAddonFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *SubscriptionAddonFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *SubscriptionAddonFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *SubscriptionAddonFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *SubscriptionAddonFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *SubscriptionAddonFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *SubscriptionAddonFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
