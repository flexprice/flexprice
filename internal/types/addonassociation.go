package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// AddonAssociationEntityType represents the type of entity that an addon is associated with
type AddonAssociationEntityType string

const (
	AddonAssociationEntityTypeSubscription AddonAssociationEntityType = "subscription"
	AddonAssociationEntityTypePlan         AddonAssociationEntityType = "plan"
	AddonAssociationEntityTypeAddon        AddonAssociationEntityType = "addon"
)

func (a AddonAssociationEntityType) Validate() error {
	allowedTypes := []AddonAssociationEntityType{
		AddonAssociationEntityTypeSubscription,
		AddonAssociationEntityTypePlan,
		AddonAssociationEntityTypeAddon,
	}
	if !lo.Contains(allowedTypes, a) {
		return ierr.NewError("entity type is invalid").
			WithHint("Entity type must be subscription, plan or addon").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// AddonAssociationFilter represents the filter options for addon associations
type AddonAssociationFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// filters allows complex filtering based on multiple fields
	Filters     []*FilterCondition          `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort        []*SortCondition            `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	AddonIDs    []string                    `json:"addon_ids,omitempty" form:"addon_ids" validate:"omitempty"`
	EntityType  *AddonAssociationEntityType `json:"entity_type,omitempty" form:"entity_type" validate:"omitempty"`
	EntityIDs   []string                    `json:"entity_ids,omitempty" form:"entity_ids" validate:"omitempty"`
	AddonStatus *string                     `json:"addon_status,omitempty" form:"addon_status" validate:"omitempty"`
}

// NewAddonAssociationFilter creates a new addon association filter with default options
func NewAddonAssociationFilter() *AddonAssociationFilter {
	return &AddonAssociationFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitAddonAssociationFilter creates a new addon association filter without pagination
func NewNoLimitAddonAssociationFilter() *AddonAssociationFilter {
	return &AddonAssociationFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the filter options
func (f *AddonAssociationFilter) Validate() error {
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

	for _, entityID := range f.EntityIDs {
		if entityID == "" {
			return ierr.NewError("entity id can not be empty").
				WithHint("Entity info can not be empty").
				Mark(ierr.ErrValidation)
		}
	}

	if f.EntityType != nil {
		if err := f.EntityType.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// GetLimit implements BaseFilter interface
func (f *AddonAssociationFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *AddonAssociationFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetStatus implements BaseFilter interface
func (f *AddonAssociationFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetSort implements BaseFilter interface
func (f *AddonAssociationFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *AddonAssociationFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetExpand implements BaseFilter interface
func (f *AddonAssociationFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *AddonAssociationFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
