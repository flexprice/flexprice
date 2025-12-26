package types

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

type ApplicationStatus string

const (
	// application_status applied is the status of a credit grant application that has been applied
	// This is the terminal state of a credit grant application
	// This is set when application is applied by cron
	ApplicationStatusApplied ApplicationStatus = "applied"

	// application_status failed is the status of a credit grant application that has failed
	// This is set when application fails to be applied by cron
	ApplicationStatusFailed ApplicationStatus = "failed"

	// application_status pending is the status of a credit grant application that is pending
	// This is the initial state of a credit grant application
	// This is set when application is created as well is ready to be applied by cron
	ApplicationStatusPending ApplicationStatus = "pending"

	// application_status skipped is the status of a credit grant application that has been skipped
	// This is set when subscription has been paused so we skip giving credits for that period
	ApplicationStatusSkipped ApplicationStatus = "skipped"

	// application_status cancelled is the status of a credit grant application that has been cancelled
	// This is set when subscription has been cancelled
	// This is the terminal state of a credit grant application
	ApplicationStatusCancelled ApplicationStatus = "cancelled"
)

func (s ApplicationStatus) String() string {
	return string(s)
}

func (s ApplicationStatus) Validate() error {
	allowedStatuses := []ApplicationStatus{
		ApplicationStatusApplied,
		ApplicationStatusFailed,
		ApplicationStatusPending,
		ApplicationStatusSkipped,
		ApplicationStatusCancelled,
	}

	if !lo.Contains(allowedStatuses, s) {
		return ierr.NewError("invalid credit grant application status").
			WithHint("Please provide a valid credit grant application status").
			WithReportableDetails(map[string]any{
				"allowed": allowedStatuses,
				"status":  s,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// CreditGrantApplicationReason defines the reason why a credit grant application is being created.
type CreditGrantApplicationReason string

const (
	// ApplicationReasonFirstTimeRecurringCreditGrant is used when a recurring credit is being granted
	// for the first time for a subscription. Typically applied at the start of a recurring billing cycle.
	ApplicationReasonFirstTimeRecurringCreditGrant CreditGrantApplicationReason = "first_time_recurring_credit_grant"

	// ApplicationReasonRecurringCreditGrant is used for recurring credit grants that are applied
	// on a regular interval (e.g. monthly, annually) after the initial credit grant has been processed.
	ApplicationReasonRecurringCreditGrant CreditGrantApplicationReason = "recurring_credit_grant"

	// ApplicationReasonOnetimeCreditGrant is used when a one-time credit is granted during subscription creation.
	ApplicationReasonOnetimeCreditGrant CreditGrantApplicationReason = "onetime_credit_grant"
)

func (r CreditGrantApplicationReason) Validate() error {

	allowedReasons := []CreditGrantApplicationReason{
		ApplicationReasonFirstTimeRecurringCreditGrant,
		ApplicationReasonRecurringCreditGrant,
		ApplicationReasonOnetimeCreditGrant,
	}

	if !lo.Contains(allowedReasons, r) {
		return ierr.NewError("invalid credit grant application reason").
			WithHint("Please provide a valid credit grant application reason").
			WithReportableDetails(map[string]any{
				"allowed": allowedReasons,
				"reason":  r,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (r CreditGrantApplicationReason) String() string {
	return string(r)
}

type CreditGrantApplicationFilter struct {
	*QueryFilter
	*TimeRangeFilter

	Filters []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	// filters allows complex filtering based on multiple fields
	ApplicationIDs      []string            `json:"application_ids,omitempty" form:"application_ids" validate:"omitempty"`
	CreditGrantIDs      []string            `json:"credit_grant_ids,omitempty" form:"credit_grant_ids" validate:"omitempty"`
	SubscriptionIDs     []string            `json:"subscription_ids,omitempty" form:"subscription_ids" validate:"omitempty"`
	ScheduledFor        *time.Time          `json:"scheduled_for,omitempty" form:"scheduled_for" validate:"omitempty"`
	AppliedAt           *time.Time          `json:"applied_at,omitempty" form:"applied_at" validate:"omitempty"`
	ApplicationStatuses []ApplicationStatus `json:"application_statuses,omitempty" form:"application_statuses" validate:"omitempty"`
}

// NewCreditGrantApplicationFilter creates a new CreditGrantApplicationFilter with default values
func NewCreditGrantApplicationFilter() *CreditGrantApplicationFilter {
	return &CreditGrantApplicationFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitCreditGrantApplicationFilter creates a new CreditGrantApplicationFilter with no pagination limits
func NewNoLimitCreditGrantApplicationFilter() *CreditGrantApplicationFilter {
	return &CreditGrantApplicationFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the credit grant application filter
func (f CreditGrantApplicationFilter) Validate() error {
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

// GetLimit implements BaseFilter interface
func (f *CreditGrantApplicationFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *CreditGrantApplicationFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *CreditGrantApplicationFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *CreditGrantApplicationFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *CreditGrantApplicationFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *CreditGrantApplicationFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *CreditGrantApplicationFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
