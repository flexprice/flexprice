package types

import (
	"errors"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// EventDeletionStatus tracks one customer's leg of a bulk event deletion.
//
//	DELETING ──► COMPLETED
//	    └──────► FAILED
type EventDeletionStatus string

const (
	EventDeletionStatusDeleting  EventDeletionStatus = "DELETING"
	EventDeletionStatusCompleted EventDeletionStatus = "COMPLETED"
	EventDeletionStatusFailed    EventDeletionStatus = "FAILED"
)

func (s EventDeletionStatus) String() string { return string(s) }

func (s EventDeletionStatus) Validate() error {
	allowed := []EventDeletionStatus{
		EventDeletionStatusDeleting,
		EventDeletionStatusCompleted,
		EventDeletionStatusFailed,
	}
	if !lo.Contains(allowed, s) {
		return ierr.NewError("invalid event deletion status").
			WithHint("Deletion status must be one of: DELETING, COMPLETED, FAILED").
			WithReportableDetails(map[string]interface{}{"deletion_status": s}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// EventDeletionFailureReason is the one reason a refused or failed deletion
// reports. It is stored on the state row and returned to the caller.
type EventDeletionFailureReason string

const (
	EventDeletionReasonMarketplaceCustomer    EventDeletionFailureReason = "MARKETPLACE_CUSTOMER"
	EventDeletionReasonFinalizedInvoiceExists EventDeletionFailureReason = "FINALIZED_INVOICE_EXISTS"
	EventDeletionReasonMultipleMetersPerEvent EventDeletionFailureReason = "MULTIPLE_METERS_PER_EVENT"
	EventDeletionReasonTooManyEvents          EventDeletionFailureReason = "TOO_MANY_EVENTS"
	EventDeletionReasonMutationFailed         EventDeletionFailureReason = "MUTATION_FAILED"
	EventDeletionReasonMutationTimeout        EventDeletionFailureReason = "MUTATION_TIMEOUT"
	EventDeletionReasonDeletionInProgress     EventDeletionFailureReason = "DELETION_IN_PROGRESS"
	EventDeletionReasonConnectionLost         EventDeletionFailureReason = "CONNECTION_LOST"
)

func (r EventDeletionFailureReason) String() string { return string(r) }

// ErrMutationTimeout is the cause of a delete that was submitted and is still
// running when the wait gives up, so what was deleted is unknown.
var ErrMutationTimeout = errors.New("delete mutation did not finish in time")

// ErrEventDeletionInProgress fails an event that belongs to a deletion still
// running. The consumer must neither write it nor acknowledge it, so the router
// keeps it out of the dead letter queue and lets Kafka redeliver it until the
// deletion finishes.
var ErrEventDeletionInProgress = errors.New("event belongs to an event deletion in progress")

// EventDeletionRequestFilter filters event_deletion_requests. Every read of that
// table goes through it: the deletion guard, the ingestion window, the invoice gate.
type EventDeletionRequestFilter struct {
	*QueryFilter

	ExternalCustomerIDs []string            `json:"external_customer_ids,omitempty" form:"external_customer_ids" validate:"omitempty"`
	DeletionStatus      EventDeletionStatus `json:"deletion_status,omitempty" form:"deletion_status" validate:"omitempty"`

	// PeriodStartLTE with PeriodEndGTE is the period overlap test.
	PeriodStartLTE *time.Time `json:"period_start_lte,omitempty" form:"period_start_lte" validate:"omitempty"`
	PeriodEndGTE   *time.Time `json:"period_end_gte,omitempty" form:"period_end_gte" validate:"omitempty"`
}

// NewDefaultEventDeletionRequestFilter creates a filter with default pagination.
func NewDefaultEventDeletionRequestFilter() *EventDeletionRequestFilter {
	return &EventDeletionRequestFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitEventDeletionRequestFilter creates a filter with no pagination limits.
func NewNoLimitEventDeletionRequestFilter() *EventDeletionRequestFilter {
	return &EventDeletionRequestFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the event deletion request filter.
func (f *EventDeletionRequestFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	if f.DeletionStatus != "" {
		if err := f.DeletionStatus.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// GetLimit implements BaseFilter interface
func (f *EventDeletionRequestFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *EventDeletionRequestFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}
