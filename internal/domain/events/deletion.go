package events

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// DeletionRequest is one customer's leg of a bulk event deletion: the fence
// invoice finalization reads, the window ingestion reads, and the outcome.
type DeletionRequest struct {
	ID            string `json:"id"`
	EnvironmentID string `json:"environment_id"`

	// RequestID is generated, shared by every row of one request, and stamped on
	// every audit row.
	RequestID string `json:"request_id"`

	ExternalCustomerID string    `json:"external_customer_id"`
	PeriodStart        time.Time `json:"period_start"`
	PeriodEnd          time.Time `json:"period_end"`

	DeletionStatus types.EventDeletionStatus `json:"deletion_status"`

	StartedAt     time.Time  `json:"started_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	FailedAt      *time.Time `json:"failed_at,omitempty"`
	FailureReason *string    `json:"failure_reason,omitempty"`
	SkipPayload   bool       `json:"skip_payload"`

	types.BaseModel
}

func DeletionRequestFromEnt(e *ent.EventDeletionRequest) *DeletionRequest {
	if e == nil {
		return nil
	}
	return &DeletionRequest{
		ID:                 e.ID,
		EnvironmentID:      e.EnvironmentID,
		RequestID:          e.RequestID,
		ExternalCustomerID: e.ExternalCustomerID,
		PeriodStart:        e.PeriodStart,
		PeriodEnd:          e.PeriodEnd,
		DeletionStatus:     e.DeletionStatus,
		StartedAt:          e.StartedAt,
		CompletedAt:        e.CompletedAt,
		FailedAt:           e.FailedAt,
		FailureReason:      e.FailureReason,
		SkipPayload:        e.SkipPayload,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

func DeletionRequestListFromEnt(list []*ent.EventDeletionRequest) []*DeletionRequest {
	if list == nil {
		return nil
	}
	return lo.Map(list, func(item *ent.EventDeletionRequest, _ int) *DeletionRequest {
		return DeletionRequestFromEnt(item)
	})
}

// IncludesTimestamp reports whether ts falls in the half-open period being
// deleted. Nil-safe: a customer with no deletion running includes nothing.
func (r *DeletionRequest) IncludesTimestamp(ts time.Time) bool {
	if r == nil {
		return false
	}
	return !ts.Before(r.PeriodStart) && ts.Before(r.PeriodEnd)
}

// Validate checks the fields the state row cannot be written without. request_id
// is not among them: it comes from the context, not the caller.
func (r *DeletionRequest) Validate() error {
	if r.ExternalCustomerID == "" {
		return ierr.NewError("external_customer_id is required").
			WithHint("external_customer_id cannot be empty").
			Mark(ierr.ErrValidation)
	}
	if r.PeriodStart.IsZero() || r.PeriodEnd.IsZero() || !r.PeriodStart.Before(r.PeriodEnd) {
		return ierr.NewError("period_start must be before period_end").
			WithHint("Provide a period whose start is before its end").
			Mark(ierr.ErrValidation)
	}
	return r.DeletionStatus.Validate()
}

// DeletionStamp is what every audit row of one request carries. DeletedAt is one
// value for the whole request, so batches cannot disagree.
type DeletionStamp struct {
	RequestID   string
	DeletedBy   string
	DeletedAt   time.Time
	SkipPayload bool
}

// DeletionTargets is what the resolve step found in scope.
type DeletionTargets struct {
	// Metered are the events that produced meter_usage.
	Metered []string
	// Unmetered are the events that produced none. Empty for a feature-narrowed scope.
	Unmetered []string
	// MeterUsageRows counts the meter_usage rows behind Metered, which is what the
	// audit records and the delete removes.
	MeterUsageRows uint64
}

// StrandedMeters is the multi-meter guard's answer: matched events that also
// carry usage on a meter the request did not name.
type StrandedMeters struct {
	EventCount     uint64
	MetersInvolved []string
}

// DeletionRequestRepository is the Postgres state row for a deletion request.
type DeletionRequestRepository interface {
	// CreateBulk inserts one row per customer. Returns ierr.ErrAlreadyExists when
	// a deletion is already in progress for any of them.
	CreateBulk(ctx context.Context, requests []*DeletionRequest) error

	// ListAll returns every row matching the filter, without pagination.
	ListAll(ctx context.Context, filter *types.EventDeletionRequestFilter) ([]*DeletionRequest, error)

	// MarkTerminal closes the given rows. reason is empty for COMPLETED. Keyed on
	// row ids, not request_id, which is audit correlation and need not be unique.
	MarkTerminal(ctx context.Context, ids []string, status types.EventDeletionStatus, reason types.EventDeletionFailureReason) error
}

// DeletedEventsLogRepository is the ClickHouse side of a deletion: what is in
// scope, recording it, then deleting it.
//
// scope is a *MeterUsageQueryParams on every method: it already carries every
// dimension a deletion narrows on, and it builds the WHERE clause for both
// tables. A scope with MeterIDs set is the feature-narrowed case (case 2).
type DeletedEventsLogRepository interface {
	// ResolveEventIDs returns the ids in scope and the meter_usage rows behind them.
	ResolveEventIDs(ctx context.Context, scope *MeterUsageQueryParams) (*DeletionTargets, error)

	// FindStrandedMeters reports matched events carrying meter_usage on a meter
	// outside scope.MeterIDs. Empty for a scope that names no meter.
	FindStrandedMeters(ctx context.Context, scope *MeterUsageQueryParams, eventIDs []string) (*StrandedMeters, error)

	// RecordDeletedEvents writes deleted_events_logs for both id sets before
	// anything is deleted: one row per (event_id, meter_id) for metered, one
	// row with meter_id = '' and qty_total = 0 for unmetered.
	RecordDeletedEvents(ctx context.Context, scope *MeterUsageQueryParams, metered, unmetered []string, stamp DeletionStamp) error

	// DeleteMeterUsage issues one mutation and waits for it.
	DeleteMeterUsage(ctx context.Context, scope *MeterUsageQueryParams, eventIDs []string) error

	// DeleteEvents issues one mutation and waits for it.
	DeleteEvents(ctx context.Context, scope *MeterUsageQueryParams, eventIDs []string) error
}
