package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// EntitlementGrantType picks the grant cadence on an entitlement.
type EntitlementGrantType string

const (
	EntitlementGrantTypeNone      EntitlementGrantType = "none"
	EntitlementGrantTypeTimeBoxed EntitlementGrantType = "time_boxed"
)

func (t EntitlementGrantType) Validate() error {
	if t == "" {
		return nil
	}
	allowed := []EntitlementGrantType{
		EntitlementGrantTypeNone,
		EntitlementGrantTypeTimeBoxed,
	}
	if !lo.Contains(allowed, t) {
		return ierr.NewError("invalid entitlement grant type").
			WithHint("grant_type must be one of none or time_boxed").
			WithReportableDetails(map[string]interface{}{"grant_type": t}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (t EntitlementGrantType) String() string { return string(t) }

// EntitlementGrantMeasure selects the counter a grant tracks: raw quantity or priced amount.
type EntitlementGrantMeasure string

const (
	EntitlementGrantMeasureQuantity EntitlementGrantMeasure = "quantity"
	EntitlementGrantMeasureAmount   EntitlementGrantMeasure = "amount"
)

func (m EntitlementGrantMeasure) Validate() error {
	if m == "" {
		return nil
	}
	allowed := []EntitlementGrantMeasure{
		EntitlementGrantMeasureQuantity,
		EntitlementGrantMeasureAmount,
	}
	if !lo.Contains(allowed, m) {
		return ierr.NewError("invalid entitlement grant measure").
			WithHint("grant_measure must be quantity or amount").
			WithReportableDetails(map[string]interface{}{"grant_measure": m}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (m EntitlementGrantMeasure) String() string { return string(m) }

// EntitlementGrantAggregationMode picks how multiple entitlements on the same
// feature combine: `additive` sums quotas; `parallel` gives each its own bucket.
type EntitlementGrantAggregationMode string

const (
	EntitlementGrantAggregationModeAdditive EntitlementGrantAggregationMode = "additive"
	EntitlementGrantAggregationModeParallel EntitlementGrantAggregationMode = "parallel"
)

func (m EntitlementGrantAggregationMode) Validate() error {
	if m == "" {
		return nil
	}
	allowed := []EntitlementGrantAggregationMode{
		EntitlementGrantAggregationModeAdditive,
		EntitlementGrantAggregationModeParallel,
	}
	if !lo.Contains(allowed, m) {
		return ierr.NewError("invalid entitlement grant aggregation mode").
			WithHint("aggregation_mode must be additive or parallel").
			WithReportableDetails(map[string]interface{}{"aggregation_mode": m}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (m EntitlementGrantAggregationMode) String() string { return string(m) }

// EntitlementGrantScopeEntityType identifies what a grant meters.
// Only `feature` is written today; `subscription` and `group` are reserved.
type EntitlementGrantScopeEntityType string

const (
	EntitlementGrantScopeFeature      EntitlementGrantScopeEntityType = "feature"
	EntitlementGrantScopeSubscription EntitlementGrantScopeEntityType = "subscription"
	EntitlementGrantScopeGroup        EntitlementGrantScopeEntityType = "group"
)

func (t EntitlementGrantScopeEntityType) Validate() error {
	if t == "" {
		return nil
	}
	allowed := []EntitlementGrantScopeEntityType{
		EntitlementGrantScopeFeature,
		EntitlementGrantScopeSubscription,
		EntitlementGrantScopeGroup,
	}
	if !lo.Contains(allowed, t) {
		return ierr.NewError("invalid entitlement grant scope entity type").
			WithHint("scope_entity_type must be feature, subscription, or group").
			WithReportableDetails(map[string]interface{}{"scope_entity_type": t}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (t EntitlementGrantScopeEntityType) String() string { return string(t) }

// EntitlementGrantDurationUnit is the unit half of a (value, unit) grant duration.
type EntitlementGrantDurationUnit string

const (
	EntitlementGrantDurationUnitHour EntitlementGrantDurationUnit = "hour"
	EntitlementGrantDurationUnitDay  EntitlementGrantDurationUnit = "day"
	EntitlementGrantDurationUnitWeek EntitlementGrantDurationUnit = "week"
)

func (u EntitlementGrantDurationUnit) Validate() error {
	if u == "" {
		return nil
	}
	allowed := []EntitlementGrantDurationUnit{
		EntitlementGrantDurationUnitHour,
		EntitlementGrantDurationUnitDay,
		EntitlementGrantDurationUnitWeek,
	}
	if !lo.Contains(allowed, u) {
		return ierr.NewError("invalid entitlement grant duration unit").
			WithHint("grant_duration_unit must be hour, day, or week").
			WithReportableDetails(map[string]interface{}{"grant_duration_unit": u}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (u EntitlementGrantDurationUnit) String() string { return string(u) }

// EntitlementGrantDurationOf converts (value, unit) into a time.Duration.
func EntitlementGrantDurationOf(value int, unit EntitlementGrantDurationUnit) (time.Duration, error) {
	if value <= 0 {
		return 0, ierr.NewError("grant_duration_value must be positive").
			WithHint("Provide a positive integer for grant_duration_value").
			Mark(ierr.ErrValidation)
	}
	switch unit {
	case EntitlementGrantDurationUnitHour:
		return time.Duration(value) * time.Hour, nil
	case EntitlementGrantDurationUnitDay:
		return time.Duration(value) * 24 * time.Hour, nil
	case EntitlementGrantDurationUnitWeek:
		return time.Duration(value) * 7 * 24 * time.Hour, nil
	default:
		return 0, ierr.NewError("invalid entitlement grant duration unit").
			WithHint("grant_duration_unit must be hour, day, or week").
			WithReportableDetails(map[string]interface{}{"grant_duration_unit": unit}).
			Mark(ierr.ErrValidation)
	}
}

// EntitlementGrantMinDuration is the minimum grant window; shorter trails are skipped.
const EntitlementGrantMinDuration = time.Hour

// EntitlementGrantStatus tracks a grant's lifecycle. `active` and `exhausted`
// are live and block a new grant on the same slot; `expired` and `superseded` free it.
type EntitlementGrantStatus string

const (
	EntitlementGrantStatusActive     EntitlementGrantStatus = "active"
	EntitlementGrantStatusExhausted  EntitlementGrantStatus = "exhausted"
	EntitlementGrantStatusExpired    EntitlementGrantStatus = "expired"
	EntitlementGrantStatusSuperseded EntitlementGrantStatus = "superseded"
)

func (s EntitlementGrantStatus) Validate() error {
	if s == "" {
		return nil
	}
	allowed := []EntitlementGrantStatus{
		EntitlementGrantStatusActive,
		EntitlementGrantStatusExhausted,
		EntitlementGrantStatusExpired,
		EntitlementGrantStatusSuperseded,
	}
	if !lo.Contains(allowed, s) {
		return ierr.NewError("invalid entitlement grant status").
			WithHint("status must be active, exhausted, expired, or superseded").
			WithReportableDetails(map[string]interface{}{"status": s}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (s EntitlementGrantStatus) String() string { return string(s) }

// IsLive reports whether the grant still occupies the slot on its EC.
func (s EntitlementGrantStatus) IsLive() bool {
	return s == EntitlementGrantStatusActive || s == EntitlementGrantStatusExhausted
}

// LiveEntitlementGrantStatuses is the IN (...) form of IsLive.
var LiveEntitlementGrantStatuses = []EntitlementGrantStatus{
	EntitlementGrantStatusActive,
	EntitlementGrantStatusExhausted,
}

// CycleOverlapEntitlementGrantStatuses is the billing-path status set: includes
// expired so overage from grants closed mid-cycle still counts.
var CycleOverlapEntitlementGrantStatuses = []EntitlementGrantStatus{
	EntitlementGrantStatusActive,
	EntitlementGrantStatusExhausted,
	EntitlementGrantStatusExpired,
}

// EntitlementGrantFilter is the query surface for grant List/Count.
type EntitlementGrantFilter struct {
	*QueryFilter
	*TimeRangeFilter

	Filters []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	IDs                  []string `json:"ids,omitempty" form:"ids"`
	EntitlementConfigIDs []string `json:"entitlement_config_ids,omitempty" form:"entitlement_config_ids"`
	CustomerIDs          []string `json:"customer_ids,omitempty" form:"customer_ids"`
	SubscriptionIDs      []string `json:"subscription_ids,omitempty" form:"subscription_ids"`

	ScopeEntityType *EntitlementGrantScopeEntityType `json:"scope_entity_type,omitempty" form:"scope_entity_type"`
	ScopeEntityIDs  []string                         `json:"scope_entity_ids,omitempty" form:"scope_entity_ids"`

	Statuses []EntitlementGrantStatus `json:"statuses,omitempty" form:"statuses"`
	Measure  *EntitlementGrantMeasure `json:"measure,omitempty" form:"measure"`

	// Grant-window predicates: TimeRangeFilter covers created_at, these cover valid_from/valid_to.
	ValidAtOrAfter  *time.Time `json:"valid_at_or_after,omitempty" form:"valid_at_or_after"`
	ValidFromBefore *time.Time `json:"valid_from_before,omitempty" form:"valid_from_before"`
	ValidToAfter    *time.Time `json:"valid_to_after,omitempty" form:"valid_to_after"`
}

func NewDefaultEntitlementGrantFilter() *EntitlementGrantFilter {
	return &EntitlementGrantFilter{QueryFilter: NewDefaultQueryFilter()}
}

func NewNoLimitEntitlementGrantFilter() *EntitlementGrantFilter {
	return &EntitlementGrantFilter{QueryFilter: NewNoLimitQueryFilter()}
}

func (f EntitlementGrantFilter) Validate() error {
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
	for _, s := range f.Statuses {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	if f.Measure != nil {
		if err := f.Measure.Validate(); err != nil {
			return err
		}
	}
	if f.ScopeEntityType != nil {
		if err := f.ScopeEntityType.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (f *EntitlementGrantFilter) WithCustomerIDs(ids ...string) *EntitlementGrantFilter {
	f.CustomerIDs = append(f.CustomerIDs, ids...)
	return f
}

func (f *EntitlementGrantFilter) WithSubscriptionIDs(ids ...string) *EntitlementGrantFilter {
	f.SubscriptionIDs = append(f.SubscriptionIDs, ids...)
	return f
}

func (f *EntitlementGrantFilter) WithScopeEntityIDs(ids ...string) *EntitlementGrantFilter {
	f.ScopeEntityIDs = append(f.ScopeEntityIDs, ids...)
	return f
}

func (f *EntitlementGrantFilter) WithScopeEntityType(t EntitlementGrantScopeEntityType) *EntitlementGrantFilter {
	f.ScopeEntityType = &t
	return f
}

// WithFeatureIDs is sugar for scope=feature + these ids.
func (f *EntitlementGrantFilter) WithFeatureIDs(ids ...string) *EntitlementGrantFilter {
	f.ScopeEntityType = lo.ToPtr(EntitlementGrantScopeFeature)
	f.ScopeEntityIDs = append(f.ScopeEntityIDs, ids...)
	return f
}

func (f *EntitlementGrantFilter) WithEntitlementConfigIDs(ids ...string) *EntitlementGrantFilter {
	f.EntitlementConfigIDs = append(f.EntitlementConfigIDs, ids...)
	return f
}

func (f *EntitlementGrantFilter) WithStatuses(statuses ...EntitlementGrantStatus) *EntitlementGrantFilter {
	f.Statuses = append(f.Statuses, statuses...)
	return f
}

// WithLiveOnly is the alert-path filter: live grants still in window at `at`.
func (f *EntitlementGrantFilter) WithLiveOnly(at time.Time) *EntitlementGrantFilter {
	f.Statuses = append(f.Statuses, LiveEntitlementGrantStatuses...)
	f.ValidAtOrAfter = &at
	return f
}

// WithCycleOverlap is the billing-path filter: any grant overlapping
// [cycleStart, cycleEnd), including already-expired ones.
func (f *EntitlementGrantFilter) WithCycleOverlap(cycleStart, cycleEnd time.Time) *EntitlementGrantFilter {
	f.Statuses = append(f.Statuses, CycleOverlapEntitlementGrantStatuses...)
	f.ValidFromBefore = &cycleEnd
	f.ValidToAfter = &cycleStart
	return f
}

// GetLimit implements BaseFilter interface.
func (f *EntitlementGrantFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface.
func (f *EntitlementGrantFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface.
func (f *EntitlementGrantFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface.
func (f *EntitlementGrantFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter — row-level status, not the grant's business status.
func (f *EntitlementGrantFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface.
func (f *EntitlementGrantFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns true if this is an unlimited query.
func (f *EntitlementGrantFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
