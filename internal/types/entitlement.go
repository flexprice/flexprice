package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

type EntitlementUsageResetPeriod string

const (
	// For BILLING_CADENCE_RECURRING
	ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY   EntitlementUsageResetPeriod = "MONTHLY"
	ENTITLEMENT_USAGE_RESET_PERIOD_ANNUAL    EntitlementUsageResetPeriod = "ANNUAL"
	ENTITLEMENT_USAGE_RESET_PERIOD_WEEKLY    EntitlementUsageResetPeriod = "WEEKLY"
	ENTITLEMENT_USAGE_RESET_PERIOD_DAILY     EntitlementUsageResetPeriod = "DAILY"
	ENTITLEMENT_USAGE_RESET_PERIOD_QUARTER   EntitlementUsageResetPeriod = "QUARTERLY"
	ENTITLEMENT_USAGE_RESET_PERIOD_HALF_YEAR EntitlementUsageResetPeriod = "HALF_YEARLY"
	ENTITLEMENT_USAGE_RESET_PERIOD_NEVER     EntitlementUsageResetPeriod = "NEVER"
)

func (e EntitlementUsageResetPeriod) Validate() error {

	allowed := []EntitlementUsageResetPeriod{
		ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
		ENTITLEMENT_USAGE_RESET_PERIOD_ANNUAL,
		ENTITLEMENT_USAGE_RESET_PERIOD_WEEKLY,
		ENTITLEMENT_USAGE_RESET_PERIOD_DAILY,
		ENTITLEMENT_USAGE_RESET_PERIOD_QUARTER,
		ENTITLEMENT_USAGE_RESET_PERIOD_HALF_YEAR,
		ENTITLEMENT_USAGE_RESET_PERIOD_NEVER,
	}

	if !lo.Contains(allowed, e) {
		return ierr.NewError("invalid entitlement usage reset period").
			WithHint("Invalid entitlement usage reset period").
			Mark(ierr.ErrValidation)
	}
	return nil
}

type EntitlementEntityType string

const (
	ENTITLEMENT_ENTITY_TYPE_PLAN         EntitlementEntityType = "PLAN"
	ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION EntitlementEntityType = "SUBSCRIPTION"
	ENTITLEMENT_ENTITY_TYPE_ADDON        EntitlementEntityType = "ADDON"
)

func (e EntitlementEntityType) Validate() error {
	allowed := []EntitlementEntityType{
		ENTITLEMENT_ENTITY_TYPE_PLAN,
		ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION,
		ENTITLEMENT_ENTITY_TYPE_ADDON,
	}
	if !lo.Contains(allowed, e) {
		return ierr.NewError("invalid entitlement entity type").
			WithHint("Invalid entitlement entity type").
			WithReportableDetails(map[string]interface{}{
				"entity_type": e,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// EntitlementFilter defines filters for querying entitlements
type EntitlementFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// Specific filters for entitlements
	Filters     []*FilterCondition     `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort        []*SortCondition       `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	EntityType  *EntitlementEntityType `form:"entity_type" json:"entity_type,omitempty"`
	EntityIDs   []string               `form:"entity_ids" json:"entity_ids,omitempty"`
	FeatureIDs  []string               `form:"feature_ids" json:"feature_ids,omitempty"`
	FeatureType *FeatureType           `form:"feature_type" json:"feature_type,omitempty"`
	IsEnabled   *bool                  `form:"is_enabled" json:"is_enabled,omitempty"`
	PlanIDs     []string               `form:"plan_ids" json:"plan_ids,omitempty"`
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
	f.EntityType = lo.ToPtr(ENTITLEMENT_ENTITY_TYPE_PLAN)
	f.EntityIDs = planIDs
	return f
}

// WithEntityType adds entity type to the filter
func (f *EntitlementFilter) WithEntityType(entityType EntitlementEntityType) *EntitlementFilter {
	f.EntityType = &entityType
	return f
}

// WithEntityIDs adds entity IDs to the filter
func (f *EntitlementFilter) WithEntityIDs(entityIDs []string) *EntitlementFilter {
	f.EntityIDs = entityIDs
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
