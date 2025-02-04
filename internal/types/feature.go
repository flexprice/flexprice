package types

import (
	"fmt"

	"github.com/samber/lo"
)

type FeatureType string

const (
	FeatureTypeMetered FeatureType = "metered"
	FeatureTypeBoolean FeatureType = "boolean"
	FeatureTypeStatic  FeatureType = "static"
)

func (f FeatureType) String() string {
	return string(f)
}

func (f FeatureType) Validate() error {
	if f == "" {
		return nil
	}

	allowed := []FeatureType{
		FeatureTypeMetered,
		FeatureTypeBoolean,
		FeatureTypeStatic,
	}
	if !lo.Contains(allowed, f) {
		return fmt.Errorf("invalid feature type: %s", f)
	}
	return nil
}

type FeatureFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// Feature specific filters
	FeatureIDs []string `form:"feature_ids" json:"feature_ids"`
	LookupKey  string   `form:"lookup_key" json:"lookup_key"`
}

func NewDefaultFeatureFilter() *FeatureFilter {
	return &FeatureFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

func NewNoLimitFeatureFilter() *FeatureFilter {
	return &FeatureFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

func (f *FeatureFilter) Validate() error {
	if f == nil {
		return nil
	}

	if f.QueryFilter == nil {
		f.QueryFilter = NewDefaultQueryFilter()
	}

	if err := f.QueryFilter.Validate(); err != nil {
		return err
	}

	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}

	if !f.GetExpand().IsEmpty() {
		if err := f.GetExpand().Validate(FeatureExpandConfig); err != nil {
			return err
		}
	}

	return nil
}

func (f *FeatureFilter) GetLimit() int {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

func (f *FeatureFilter) GetOffset() int {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

func (f *FeatureFilter) GetSort() string {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

func (f *FeatureFilter) GetStatus() string {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

func (f *FeatureFilter) GetOrder() string {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetExpand returns the expand filter
func (f *FeatureFilter) GetExpand() Expand {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *FeatureFilter) IsUnlimited() bool {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}

// FeatureExpandConfig defines the allowed expand fields for features
var FeatureExpandConfig = ExpandConfig{
	AllowedFields: []ExpandableField{ExpandMeters},
	NestedExpands: map[ExpandableField][]ExpandableField{
		ExpandMeters: {},
	},
}
