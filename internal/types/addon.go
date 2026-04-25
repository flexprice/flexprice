package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// AddonCadence controls whether an addon line item ends at the current period end
// (onetime — billed once for the period) or has no end date and recurs each period (recurring).
// This is set per-add request, not on the addon entity itself.
type AddonCadence string

const (
	AddonCadenceOnetime   AddonCadence = "onetime"
	AddonCadenceRecurring AddonCadence = "recurring"
)

func (c AddonCadence) Validate() error {
	allowed := []AddonCadence{AddonCadenceOnetime, AddonCadenceRecurring}
	if !lo.Contains(allowed, c) {
		return ierr.NewError("invalid addon cadence").
			WithHint("Addon cadence must be onetime or recurring").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// AddonStatus represents the status of a subscription addon
type AddonStatus string

const (
	AddonStatusActive    AddonStatus = "active"
	AddonStatusCancelled AddonStatus = "cancelled"
	AddonStatusPending   AddonStatus = "pending"
)

func (s AddonStatus) Validate() error {
	allowed := []AddonStatus{
		AddonStatusActive,
		AddonStatusCancelled,
		AddonStatusPending,
	}
	if !lo.Contains(allowed, s) {
		return ierr.NewError("invalid addon status").
			WithHint("Addon status must be active, cancelled or pending").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// AddonFilter represents the filter options for addons
type AddonFilter struct {
	*QueryFilter
	*TimeRangeFilter

	*DSLFilter

	AddonIDs   []string `json:"addon_ids,omitempty" form:"addon_ids" validate:"omitempty"`
	LookupKeys []string `json:"lookup_keys,omitempty" form:"lookup_keys" validate:"omitempty"`
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

	if err := f.DSLFilter.Validate(); err != nil {
		return err
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
