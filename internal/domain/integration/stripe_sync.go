package integration

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// SyncStatus represents the status of a sync batch
type SyncStatus string

const (
	SyncStatusPending    SyncStatus = "pending"
	SyncStatusProcessing SyncStatus = "processing"
	SyncStatusCompleted  SyncStatus = "completed"
	SyncStatusFailed     SyncStatus = "failed"
	SyncStatusRetrying   SyncStatus = "retrying"
)

// StripeSyncBatch represents a batch of events for Stripe synchronization
type StripeSyncBatch struct {
	// ID is the unique identifier for the sync batch
	ID string `db:"id" json:"id"`

	// EntityID is the FlexPrice entity identifier (customer ID, subscription ID, etc.)
	EntityID string `db:"entity_id" json:"entity_id"`

	// EntityType is the type of entity being synced (customer, subscription, etc.)
	EntityType EntityType `db:"entity_type" json:"entity_type"`

	// MeterID is the FlexPrice meter identifier
	MeterID string `db:"meter_id" json:"meter_id"`

	// EventType is the type of events being synced
	EventType string `db:"event_type" json:"event_type"`

	// AggregatedQuantity is the sum of billable quantities for the time window
	AggregatedQuantity float64 `db:"aggregated_quantity" json:"aggregated_quantity"`

	// EventCount is the number of events aggregated in this batch
	EventCount int `db:"event_count" json:"event_count"`

	// StripeEventID is the ID returned by Stripe API for tracking
	StripeEventID string `db:"stripe_event_id" json:"stripe_event_id"`

	// SyncStatus represents the current status of the sync
	SyncStatus SyncStatus `db:"sync_status" json:"sync_status"`

	// RetryCount tracks the number of retry attempts
	RetryCount int `db:"retry_count" json:"retry_count"`

	// ErrorMessage stores the last error message if sync failed
	ErrorMessage string `db:"error_message" json:"error_message"`

	// WindowStart is the start time of the aggregation window
	WindowStart time.Time `db:"window_start" json:"window_start"`

	// WindowEnd is the end time of the aggregation window
	WindowEnd time.Time `db:"window_end" json:"window_end"`

	// SyncedAt is the timestamp when the sync was completed
	SyncedAt *time.Time `db:"synced_at" json:"synced_at"`

	// EnvironmentID is the environment identifier for the batch
	EnvironmentID string `db:"environment_id" json:"environment_id"`

	types.BaseModel
}

// TODO: FromEnt and FromEntList will be implemented after Ent code generation

// ValidateSyncStatus validates the sync status
func ValidateSyncStatus(status SyncStatus) bool {
	switch status {
	case SyncStatusPending, SyncStatusProcessing, SyncStatusCompleted, SyncStatusFailed, SyncStatusRetrying:
		return true
	default:
		return false
	}
}

// IsRetryable returns true if the sync batch can be retried
func (s *StripeSyncBatch) IsRetryable() bool {
	return s.SyncStatus == SyncStatusFailed && s.RetryCount < MaxRetryCount
}

// MaxRetryCount defines the maximum number of retry attempts
const MaxRetryCount = 5

// CanTransitionTo checks if the sync batch can transition to the given status
func (s *StripeSyncBatch) CanTransitionTo(newStatus SyncStatus) bool {
	switch s.SyncStatus {
	case SyncStatusPending:
		return newStatus == SyncStatusProcessing || newStatus == SyncStatusFailed
	case SyncStatusProcessing:
		return newStatus == SyncStatusCompleted || newStatus == SyncStatusFailed
	case SyncStatusFailed:
		return newStatus == SyncStatusRetrying || newStatus == SyncStatusCompleted
	case SyncStatusRetrying:
		return newStatus == SyncStatusProcessing || newStatus == SyncStatusFailed
	case SyncStatusCompleted:
		return false // Completed is a final state
	default:
		return false
	}
}

// MarkAsProcessing transitions the batch to processing status
func (s *StripeSyncBatch) MarkAsProcessing() error {
	if !s.CanTransitionTo(SyncStatusProcessing) {
		return ierr.NewError("cannot transition to processing status").
			WithHint("Batch must be in pending or retrying status").
			Mark(ierr.ErrValidation)
	}
	s.SyncStatus = SyncStatusProcessing
	return nil
}

// MarkAsCompleted transitions the batch to completed status
func (s *StripeSyncBatch) MarkAsCompleted(stripeEventID string) error {
	if !s.CanTransitionTo(SyncStatusCompleted) {
		return ierr.NewError("cannot transition to completed status").
			WithHint("Batch must be in processing, failed, or retrying status").
			Mark(ierr.ErrValidation)
	}
	s.SyncStatus = SyncStatusCompleted
	s.StripeEventID = stripeEventID
	now := time.Now().UTC()
	s.SyncedAt = &now
	return nil
}

// MarkAsFailed transitions the batch to failed status
func (s *StripeSyncBatch) MarkAsFailed(errorMessage string) error {
	if !s.CanTransitionTo(SyncStatusFailed) {
		return ierr.NewError("cannot transition to failed status").
			WithHint("Batch must be in pending, processing, or retrying status").
			Mark(ierr.ErrValidation)
	}
	s.SyncStatus = SyncStatusFailed
	s.ErrorMessage = errorMessage
	return nil
}

// MarkAsRetrying transitions the batch to retrying status
func (s *StripeSyncBatch) MarkAsRetrying() error {
	if !s.CanTransitionTo(SyncStatusRetrying) {
		return ierr.NewError("cannot transition to retrying status").
			WithHint("Batch must be in failed status").
			Mark(ierr.ErrValidation)
	}
	if !s.IsRetryable() {
		return ierr.NewError("batch cannot be retried").
			WithHint("Maximum retry count exceeded").
			Mark(ierr.ErrValidation)
	}
	s.SyncStatus = SyncStatusRetrying
	s.RetryCount++
	return nil
}

// Validate validates all fields of the stripe sync batch
func (s *StripeSyncBatch) Validate() error {
	if s.EntityID == "" {
		return ierr.NewError("entity_id is required").
			WithHint("Entity ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if !ValidateEntityType(s.EntityType) {
		return ierr.NewError("invalid entity_type").
			WithHint("Entity type must be one of: customer").
			Mark(ierr.ErrValidation)
	}

	if s.MeterID == "" {
		return ierr.NewError("meter_id is required").
			WithHint("Meter ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if s.EventType == "" {
		return ierr.NewError("event_type is required").
			WithHint("Event type must not be empty").
			Mark(ierr.ErrValidation)
	}

	if !ValidateSyncStatus(s.SyncStatus) {
		return ierr.NewError("invalid sync_status").
			WithHint("Sync status must be one of: pending, processing, completed, failed, retrying").
			Mark(ierr.ErrValidation)
	}

	if s.AggregatedQuantity < 0 {
		return ierr.NewError("aggregated_quantity cannot be negative").
			WithHint("Aggregated quantity must be >= 0").
			Mark(ierr.ErrValidation)
	}

	if s.EventCount < 0 {
		return ierr.NewError("event_count cannot be negative").
			WithHint("Event count must be >= 0").
			Mark(ierr.ErrValidation)
	}

	if s.RetryCount < 0 {
		return ierr.NewError("retry_count cannot be negative").
			WithHint("Retry count must be >= 0").
			Mark(ierr.ErrValidation)
	}

	if s.RetryCount > MaxRetryCount {
		return ierr.NewError("retry_count exceeds maximum").
			WithHint("Retry count must be <= 5").
			Mark(ierr.ErrValidation)
	}

	if s.WindowStart.After(s.WindowEnd) {
		return ierr.NewError("window_start cannot be after window_end").
			WithHint("Window start must be before or equal to window end").
			Mark(ierr.ErrValidation)
	}

	// Field length validations
	if len(s.EntityID) > 50 {
		return ierr.NewError("entity_id too long").
			WithHint("Entity ID must be less than 50 characters").
			Mark(ierr.ErrValidation)
	}

	if len(string(s.EntityType)) > 50 {
		return ierr.NewError("entity_type too long").
			WithHint("Entity type must be less than 50 characters").
			Mark(ierr.ErrValidation)
	}

	if len(s.MeterID) > 50 {
		return ierr.NewError("meter_id too long").
			WithHint("Meter ID must be less than 50 characters").
			Mark(ierr.ErrValidation)
	}

	if len(s.EventType) > 100 {
		return ierr.NewError("event_type too long").
			WithHint("Event type must be less than 100 characters").
			Mark(ierr.ErrValidation)
	}

	if len(s.StripeEventID) > 255 {
		return ierr.NewError("stripe_event_id too long").
			WithHint("Stripe event ID must be less than 255 characters").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Helper methods for backward compatibility and common use cases

// NewCustomerSyncBatch creates a new sync batch for a customer entity
func NewCustomerSyncBatch(customerID, meterID, eventType string, windowStart, windowEnd time.Time, environmentID string) *StripeSyncBatch {
	return &StripeSyncBatch{
		EntityID:      customerID,
		EntityType:    EntityTypeCustomer,
		MeterID:       meterID,
		EventType:     eventType,
		SyncStatus:    SyncStatusPending,
		WindowStart:   windowStart,
		WindowEnd:     windowEnd,
		EnvironmentID: environmentID,
	}
}

// IsCustomerBatch returns true if this sync batch is for a customer entity
func (s *StripeSyncBatch) IsCustomerBatch() bool {
	return s.EntityType == EntityTypeCustomer
}

// GetCustomerID returns the customer ID if this is a customer batch, empty string otherwise
func (s *StripeSyncBatch) GetCustomerID() string {
	if s.IsCustomerBatch() {
		return s.EntityID
	}
	return ""
}
