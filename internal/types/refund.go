package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// RefundStatus represents the lifecycle state of a gateway refund.
type RefundStatus string

const (
	RefundStatusPending    RefundStatus = "PENDING"
	RefundStatusProcessing RefundStatus = "PROCESSING"
	RefundStatusSucceeded  RefundStatus = "SUCCEEDED"
	RefundStatusFailed     RefundStatus = "FAILED"
	RefundStatusCancelled  RefundStatus = "CANCELLED"
)

func (s RefundStatus) String() string {
	return string(s)
}

// IsTerminal returns true if the refund has reached a final state with no further transitions.
func (s RefundStatus) IsTerminal() bool {
	return s == RefundStatusSucceeded || s == RefundStatusFailed || s == RefundStatusCancelled
}

func (s RefundStatus) Validate() error {
	allowed := []RefundStatus{
		RefundStatusPending,
		RefundStatusProcessing,
		RefundStatusSucceeded,
		RefundStatusFailed,
		RefundStatusCancelled,
	}
	if !lo.Contains(allowed, s) {
		return ierr.NewError("invalid refund status").
			WithHint("Please provide a valid refund status").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// RefundReason is the reason a gateway refund was issued.
type RefundReason string

const (
	RefundReasonDuplicate           RefundReason = "DUPLICATE"
	RefundReasonFraudulent          RefundReason = "FRAUDULENT"
	RefundReasonRequestedByCustomer RefundReason = "REQUESTED_BY_CUSTOMER"
	RefundReasonOrderChange         RefundReason = "ORDER_CHANGE"
	RefundReasonServiceIssue        RefundReason = "SERVICE_ISSUE"
	RefundReasonOther               RefundReason = "OTHER"
)

func (r RefundReason) String() string {
	return string(r)
}

func (r RefundReason) Validate() error {
	allowed := []RefundReason{
		RefundReasonDuplicate,
		RefundReasonFraudulent,
		RefundReasonRequestedByCustomer,
		RefundReasonOrderChange,
		RefundReasonServiceIssue,
		RefundReasonOther,
	}
	if !lo.Contains(allowed, r) {
		return ierr.NewError("invalid refund reason").
			WithHint("Please provide a valid refund reason").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// RefundFilter defines query parameters for listing refunds.
type RefundFilter struct {
	PaymentID *string
	Status    *RefundStatus
	Gateway   *string
	*QueryFilter
	*TimeRangeFilter
}

// Validate validates the refund filter.
func (f *RefundFilter) Validate() error {
	if f == nil {
		return nil
	}
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

// GetLimit implements BaseFilter interface.
func (f *RefundFilter) GetLimit() int {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface.
func (f *RefundFilter) GetOffset() int {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface.
func (f *RefundFilter) GetSort() string {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface.
func (f *RefundFilter) GetOrder() string {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface.
func (f *RefundFilter) GetStatus() string {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface.
func (f *RefundFilter) GetExpand() Expand {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited returns true if the filter has no limit.
func (f *RefundFilter) IsUnlimited() bool {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
