package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// -----------------------------------------------------------------------------
// Enums used on entitlement configs (extends the existing entitlement row) and
// on entitlement_grants rows.
// -----------------------------------------------------------------------------

// EntitlementGrantType controls whether an entitlement config opens grants at
// all, and if so with what cadence. Legacy entitlements default to `none` and
// behave exactly as before.
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

// EntitlementGrantMeasure selects which counter a grant tracks: raw quantity
// (SUM/COUNT/COUNT_UNIQUE meter output) or priced amount (currency units).
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

// EntitlementGrantScopeEntityType is the shape of thing a grant is metering.
// Phase 1 only writes `feature` — every grant we open comes from a
// feature-scoped entitlement config. `subscription` and `group` are declared
// here so downstream consumers can pattern-match on the full set the moment we
// add sub-level / group-level grants (see ERD §7.1 open extensions).
type EntitlementGrantScopeEntityType string

const (
	// EntitlementGrantScopeFeature : classic quota on a single feature/meter.
	// scope_entity_id references the feature.
	EntitlementGrantScopeFeature EntitlementGrantScopeEntityType = "feature"

	// EntitlementGrantScopeSubscription : real-time spend cap across an entire
	// subscription. Not written by any Phase 1 code path; today the equivalent
	// is expressed as a subscription-scoped alert row in alert_logs.
	// scope_entity_id references the subscription.
	EntitlementGrantScopeSubscription EntitlementGrantScopeEntityType = "subscription"

	// EntitlementGrantScopeGroup : shared budget across a feature group. Not
	// written by any Phase 1 code path; alerts already support the scope
	// (AlertEntityTypeGroup) but the grant primitive does not.
	// scope_entity_id references the group.
	EntitlementGrantScopeGroup EntitlementGrantScopeEntityType = "group"
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

// EntitlementGrantDurationUnit expresses grant_duration as (value, unit). We
// keep the value/unit split (rather than storing raw nanoseconds) so admins
// can see and edit "5 hour" in the DB without decoding.
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

// EntitlementGrantDurationOf converts (value, unit) into a wall-clock
// time.Duration. Grants never span cycle boundaries (see ERD §8.4), so
// calendar-vs-wall-clock ambiguity — e.g. what "1 month" means across DST —
// never comes up in practice.
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

// EntitlementGrantMinDuration is the product-mandated floor. Trailing windows
// shorter than this (e.g. only 30 minutes left in a cycle) are skipped rather
// than opening a tiny stub grant — see ERD §8.4.
const EntitlementGrantMinDuration = time.Hour

// -----------------------------------------------------------------------------
// entitlement_grants row state
// -----------------------------------------------------------------------------

// EntitlementGrantStatus tracks a grant across its lifetime. The partial
// unique index on (entitlement_config_id, customer_id) treats `active` and
// `exhausted` as "live" — an EG in either state blocks a new grant from
// opening on the same EC/customer slot. `expired` and `superseded` free the
// slot.
type EntitlementGrantStatus string

const (
	// EntitlementGrantStatusActive : usage < quota AND now < valid_to.
	EntitlementGrantStatusActive EntitlementGrantStatus = "active"

	// EntitlementGrantStatusExhausted : usage >= quota AND now < valid_to.
	// The workflow flips active → exhausted when firing the exhaustion alert.
	// Additional usage in the same window flows through the pricing pipeline
	// as billable overage (ERD §8.6).
	EntitlementGrantStatusExhausted EntitlementGrantStatus = "exhausted"

	// EntitlementGrantStatusExpired : now >= valid_to. Set opportunistically
	// by ensureGrants immediately before opening the next grant on the same
	// slot; expired rows are otherwise invisible to the workflow.
	EntitlementGrantStatusExpired EntitlementGrantStatus = "expired"

	// EntitlementGrantStatusSuperseded : reserved for admin edits (future,
	// see ERD §3.2 non-goals for Phase 1). Not written by any Phase 1 code
	// path; declared here so downstream consumers can pattern-match on it now.
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

// IsLive returns true when the grant still occupies the unique slot for its
// EC (active or exhausted). Callers filtering "grants currently blocking a
// new open" should use this.
func (s EntitlementGrantStatus) IsLive() bool {
	return s == EntitlementGrantStatusActive || s == EntitlementGrantStatusExhausted
}

// LiveEntitlementGrantStatuses is the set of statuses treated as "still
// occupying a slot on the (config, customer) unique index". Exposed as a
// slice for convenient IN (...) SQL filters.
var LiveEntitlementGrantStatuses = []EntitlementGrantStatus{
	EntitlementGrantStatusActive,
	EntitlementGrantStatusExhausted,
}

// CycleOverlapEntitlementGrantStatuses is the set of statuses that must be
// considered when summing per-grant overage against a subscription cycle: any
// grant that lived (fully or partially) inside the cycle contributed usage,
// regardless of whether its window has since closed.
var CycleOverlapEntitlementGrantStatuses = []EntitlementGrantStatus{
	EntitlementGrantStatusActive,
	EntitlementGrantStatusExhausted,
	EntitlementGrantStatusExpired,
}

// -----------------------------------------------------------------------------
// Filter
// -----------------------------------------------------------------------------

// EntitlementGrantFilter is the query surface for the repository List/Count
// pair. All fields are optional; empty slices/nil pointers mean "no
// predicate".
type EntitlementGrantFilter struct {
	*QueryFilter
	*TimeRangeFilter

	Filters []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	// Identity filters
	IDs                  []string `json:"ids,omitempty" form:"ids"`
	EntitlementConfigIDs []string `json:"entitlement_config_ids,omitempty" form:"entitlement_config_ids"`
	CustomerIDs          []string `json:"customer_ids,omitempty" form:"customer_ids"`
	SubscriptionIDs      []string `json:"subscription_ids,omitempty" form:"subscription_ids"`

	// Scope filters — the grant target. Feature-scoped grants are the only
	// shape Phase 1 writes, but the query surface is generic so future
	// subscription/group grants can be filtered without a schema change.
	ScopeEntityType *EntitlementGrantScopeEntityType `json:"scope_entity_type,omitempty" form:"scope_entity_type"`
	ScopeEntityIDs  []string                         `json:"scope_entity_ids,omitempty" form:"scope_entity_ids"`

	// State
	Statuses []EntitlementGrantStatus `json:"statuses,omitempty" form:"statuses"`
	Measure  *EntitlementGrantMeasure `json:"measure,omitempty" form:"measure"`

	// Time-range predicates over the grant window itself. These are additive
	// on top of TimeRangeFilter (which filters on created_at). Used by the
	// two canonical filters in the ERD:
	//
	//   Alert path      : ValidAtOrAfter (grant currently in window)
	//   Billing overage : ValidFromBefore + ValidToAfter (window overlaps cycle)
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

// -----------------------------------------------------------------------------
// Filter fluent helpers — mirrors the shape used across the codebase.
// -----------------------------------------------------------------------------

func (f *EntitlementGrantFilter) WithCustomerIDs(ids ...string) *EntitlementGrantFilter {
	f.CustomerIDs = append(f.CustomerIDs, ids...)
	return f
}

func (f *EntitlementGrantFilter) WithSubscriptionIDs(ids ...string) *EntitlementGrantFilter {
	f.SubscriptionIDs = append(f.SubscriptionIDs, ids...)
	return f
}

// WithScopeEntityIDs filters grants targeting any of the given entity IDs.
// The scope-entity-type filter is optional but usually pairs with it, e.g.
// WithScopeEntityType(feature).WithScopeEntityIDs(feat_a, feat_b).
func (f *EntitlementGrantFilter) WithScopeEntityIDs(ids ...string) *EntitlementGrantFilter {
	f.ScopeEntityIDs = append(f.ScopeEntityIDs, ids...)
	return f
}

// WithScopeEntityType narrows to one scope kind. Useful when a caller only
// wants feature grants (Phase 1) or wants to defensively exclude the future
// subscription/group shapes.
func (f *EntitlementGrantFilter) WithScopeEntityType(t EntitlementGrantScopeEntityType) *EntitlementGrantFilter {
	f.ScopeEntityType = &t
	return f
}

// WithFeatureIDs is sugar for the Phase 1 common case: "give me grants on
// these features." Equivalent to WithScopeEntityType(feature) +
// WithScopeEntityIDs(...).
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

// WithLiveOnly is shorthand for the alert-path filter — grants still
// occupying the unique index slot (active + exhausted) whose window has not
// yet closed.
func (f *EntitlementGrantFilter) WithLiveOnly(at time.Time) *EntitlementGrantFilter {
	f.Statuses = append(f.Statuses, LiveEntitlementGrantStatuses...)
	f.ValidAtOrAfter = &at
	return f
}

// WithCycleOverlap is shorthand for the billing-path filter — every grant
// whose window overlaps [cycleStart, cycleEnd), regardless of whether it is
// now expired. Overage from grants that expired mid-cycle still contributes
// to the customer's bill for that cycle (ERD §8.6).
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

// GetStatus implements BaseFilter interface (delegates to QueryFilter's
// row-level status, not the grant's business status — those are distinct
// concepts).
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
