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

func (s RefundStatus) IsSettled() bool {
	return s == RefundStatusSucceeded
}

var refundStatusTransitions = map[RefundStatus][]RefundStatus{
	RefundStatusPending: {
		RefundStatusProcessing,
		RefundStatusSucceeded,
		RefundStatusFailed,
		RefundStatusCancelled,
	},
	RefundStatusProcessing: {
		RefundStatusSucceeded,
		RefundStatusFailed,
		RefundStatusCancelled,
	},
	RefundStatusSucceeded: {},
	RefundStatusFailed:    {},
	RefundStatusCancelled: {},
}

// ValidateTransitionTo reports whether a refund may move from s to target.
// Terminal states admit nothing, so a redelivered gateway webhook settling an
// already-SUCCEEDED row is rejected here rather than paying the customer twice.
func (s RefundStatus) ValidateTransitionTo(target RefundStatus) error {
	if err := target.Validate(); err != nil {
		return err
	}

	allowed, ok := refundStatusTransitions[s]
	if !ok {
		return ierr.NewError("invalid current refund status").
			WithHintf("Refund is in an unrecognised status: %s", s).
			Mark(ierr.ErrValidation)
	}

	if lo.Contains(allowed, target) {
		return nil
	}

	return ierr.NewError("invalid refund status transition").
		WithHintf("Refund status cannot change from %s to %s", s, target).
		WithReportableDetails(map[string]any{
			"current_status":   s,
			"allowed_statuses": allowed,
		}).
		Mark(ierr.ErrValidation)
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

// RefundDestination is where the refunded money goes.
type RefundDestination string

const (
	RefundDestinationGateway   RefundDestination = "GATEWAY"
	RefundDestinationWallet    RefundDestination = "WALLET"
	RefundDestinationOutOfBand RefundDestination = "OUT_OF_BAND"
)

func (t RefundDestination) String() string {
	return string(t)
}

func (t RefundDestination) Validate() error {
	allowed := []RefundDestination{
		RefundDestinationGateway,
		RefundDestinationWallet,
		RefundDestinationOutOfBand,
	}
	if !lo.Contains(allowed, t) {
		return ierr.NewError("invalid refund destination").
			WithHint("Please provide a valid refund destination").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// RefundTarget is what a caller asks for. It is deliberately not RefundDestination:
// the caller knows whether the money should come back the way it arrived, not which
// gateway (if any) carried it.
type RefundTarget string

const (
	RefundTargetPrepaidWallet RefundTarget = "PREPAID_WALLET"
	RefundTargetBackToSource  RefundTarget = "BACK_TO_SOURCE"
)

func (t RefundTarget) String() string {
	return string(t)
}

func (t RefundTarget) Validate() error {
	allowed := []RefundTarget{RefundTargetPrepaidWallet, RefundTargetBackToSource}
	if !lo.Contains(allowed, t) {
		return ierr.NewError("invalid refund target").
			WithHint("A refund can go back to the original payment method or into the customer's prepaid wallet").
			WithReportableDetails(map[string]any{
				"allowed": allowed,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// AllowsBackToSource is false for an unset target: money stays with the customer as
// credit unless the caller asks for it to be returned.
func (t *RefundTarget) AllowsBackToSource() bool {
	return t != nil && *t == RefundTargetBackToSource
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
	*QueryFilter
	*TimeRangeFilter

	PaymentIDs         []string            `json:"payment_ids,omitempty" form:"payment_ids"`
	CreditNoteIDs      []string            `json:"credit_note_ids,omitempty" form:"credit_note_ids"`
	InvoiceIDs         []string            `json:"invoice_ids,omitempty" form:"invoice_ids"`
	RefundStatuses     []RefundStatus      `json:"refund_statuses,omitempty" form:"refund_statuses"`
	RefundDestinations []RefundDestination `json:"refund_destinations,omitempty" form:"refund_destinations"`
	Gateway            *string             `json:"gateway,omitempty" form:"gateway"`
	OnlySettled        *bool               `json:"only_settled,omitempty" form:"only_settled"`
}

// NewRefundFilter creates a refund filter with default pagination.
func NewRefundFilter() *RefundFilter {
	return &RefundFilter{QueryFilter: NewDefaultQueryFilter()}
}

// NewNoLimitRefundFilter creates a refund filter without pagination.
func NewNoLimitRefundFilter() *RefundFilter {
	return &RefundFilter{QueryFilter: NewNoLimitQueryFilter()}
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
