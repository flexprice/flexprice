package types

import (
	"time"
)

type ApplicationStatus string

const (
	ApplicationStatusScheduled ApplicationStatus = "scheduled"
	ApplicationStatusApplied   ApplicationStatus = "applied"
	ApplicationStatusFailed    ApplicationStatus = "failed"
	ApplicationStatusSkipped   ApplicationStatus = "skipped"
	ApplicationStatusCancelled ApplicationStatus = "cancelled"
	ApplicationStatusPending   ApplicationStatus = "pending"
)

type CreditGrantApplicationFilter struct {
	*QueryFilter
	*TimeRangeFilter

	Filters []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort    []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`

	// filters allows complex filtering based on multiple fields
	ApplicationIDs    []string          `json:"application_ids,omitempty" form:"application_ids" validate:"omitempty"`
	CreditGrantIDs    []string          `json:"credit_grant_ids,omitempty" form:"credit_grant_ids" validate:"omitempty"`
	SubscriptionIDs   []string          `json:"subscription_ids,omitempty" form:"subscription_ids" validate:"omitempty"`
	ScheduledFor      *time.Time        `json:"scheduled_for,omitempty" form:"scheduled_for" validate:"omitempty"`
	AppliedAt         *time.Time        `json:"applied_at,omitempty" form:"applied_at" validate:"omitempty"`
	ApplicationStatus ApplicationStatus `json:"application_status,omitempty" form:"application_status" validate:"omitempty"`
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
