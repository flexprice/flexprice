package types

import (
	"fmt"

	"github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// CreditGrantScope defines the scope of a credit grant
type CreditGrantScope string

const (
	CreditGrantScopePlan         CreditGrantScope = "PLAN"
	CreditGrantScopeSubscription CreditGrantScope = "SUBSCRIPTION"
)

// CreditGrantCadence defines the cadence of a credit grant
type CreditGrantCadence string

const (
	CreditGrantCadenceOneTime   CreditGrantCadence = "ONETIME"
	CreditGrantCadenceRecurring CreditGrantCadence = "RECURRING"
)

// CreditGrantPeriod defines the period for recurring credit grants
type CreditGrantPeriod string

const (
	CreditGrantPeriodDaily    CreditGrantPeriod = "DAILY"
	CreditGrantPeriodWeekly   CreditGrantPeriod = "WEEKLY"
	CreditGrantPeriodMonthly  CreditGrantPeriod = "MONTHLY"
	CreditGrantPeriodYearly   CreditGrantPeriod = "YEARLY"
	CreditGrantPeriodQuarter  CreditGrantPeriod = "QUARTERLY"
	CreditGrantPeriodHalfYear CreditGrantPeriod = "HALFYEARLY"
)

// CreditGrantExpiryType defines the type of expiry configuration
type CreditGrantExpiryType string

const (
	// Credits stay available until they’re completely used—no time limit.
	CreditGrantExpiryTypeNever CreditGrantExpiryType = "NEVER"
	// Any unused credits disappear X days after they’re granted.
	CreditGrantExpiryTypeDuration CreditGrantExpiryType = "DURATION"
	// Unused credits reset at the end of each subscription period (matches the customer’s billing schedule).
	CreditGrantExpiryTypeBillingCycle CreditGrantExpiryType = "BILLING_CYCLE"
)

// CreditGrantExpiryDurationUnit defines time units for duration-based expiry
type CreditGrantExpiryDurationUnit string

const (
	// Any unused credits disappear X days after they’re granted.
	CreditGrantExpiryDurationUnitDays   CreditGrantExpiryDurationUnit = "DAY"
	CreditGrantExpiryDurationUnitWeeks  CreditGrantExpiryDurationUnit = "WEEK"
	CreditGrantExpiryDurationUnitMonths CreditGrantExpiryDurationUnit = "MONTH"
	CreditGrantExpiryDurationUnitYears  CreditGrantExpiryDurationUnit = "YEAR"
)

// Validate validates the credit grant scope
func (s CreditGrantScope) Validate() error {
	allowedValues := []CreditGrantScope{
		CreditGrantScopePlan,
		CreditGrantScopeSubscription,
	}

	if !lo.Contains(allowedValues, s) {
		return errors.NewError("invalid credit grant scope").
			WithHint(fmt.Sprintf("Credit grant scope must be one of: %v", allowedValues)).
			Mark(errors.ErrValidation)
	}

	return nil
}

// Validate validates the credit grant cadence
func (c CreditGrantCadence) Validate() error {
	allowedValues := []CreditGrantCadence{
		CreditGrantCadenceOneTime,
		CreditGrantCadenceRecurring,
	}

	if !lo.Contains(allowedValues, c) {
		return errors.NewError("invalid credit grant cadence").
			WithHint(fmt.Sprintf("Credit grant cadence must be one of: %v", allowedValues)).
			Mark(errors.ErrValidation)
	}

	return nil
}

// Validate validates the credit grant period
func (p CreditGrantPeriod) Validate() error {
	allowedValues := []CreditGrantPeriod{
		CreditGrantPeriodDaily,
		CreditGrantPeriodWeekly,
		CreditGrantPeriodMonthly,
		CreditGrantPeriodYearly,
		CreditGrantPeriodQuarter,
		CreditGrantPeriodHalfYear,
	}

	if !lo.Contains(allowedValues, p) {
		return errors.NewError("invalid credit grant period").
			WithHint(fmt.Sprintf("Credit grant period must be one of: %v", allowedValues)).
			Mark(errors.ErrValidation)
	}

	return nil
}

// CreditGrantFilter defines filters for querying credit grants
type CreditGrantFilter struct {
	*QueryFilter
	*TimeRangeFilter

	// Specific filters for credit grants
	PlanIDs         []string `form:"plan_ids" json:"plan_ids,omitempty"`
	SubscriptionIDs []string `form:"subscription_ids" json:"subscription_ids,omitempty"`
}

// NewDefaultCreditGrantFilter creates a new CreditGrantFilter with default values
func NewDefaultCreditGrantFilter() *CreditGrantFilter {
	return &CreditGrantFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitCreditGrantFilter creates a new CreditGrantFilter with no pagination limits
func NewNoLimitCreditGrantFilter() *CreditGrantFilter {
	return &CreditGrantFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the filter fields
func (f CreditGrantFilter) Validate() error {
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
func (f *CreditGrantFilter) WithPlanIDs(planIDs []string) *CreditGrantFilter {
	f.PlanIDs = planIDs
	return f
}

// WithSubscriptionIDs adds subscription IDs to the filter
func (f *CreditGrantFilter) WithSubscriptionIDs(subscriptionIDs []string) *CreditGrantFilter {
	f.SubscriptionIDs = subscriptionIDs
	return f
}

// WithStatus sets the status on the filter
func (f *CreditGrantFilter) WithStatus(status Status) *CreditGrantFilter {
	f.Status = &status
	return f
}

// WithExpand sets the expand on the filter
func (f *CreditGrantFilter) WithExpand(expand string) *CreditGrantFilter {
	f.Expand = &expand
	return f
}

// GetLimit implements BaseFilter interface
func (f *CreditGrantFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *CreditGrantFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *CreditGrantFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *CreditGrantFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *CreditGrantFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *CreditGrantFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns true if this is an unlimited query
func (f *CreditGrantFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
