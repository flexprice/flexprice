package models

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// StripeEventSyncWorkflowInput represents the input for the Stripe event sync workflow
type StripeEventSyncWorkflowInput struct {
	TenantID       string        `json:"tenant_id"`
	EnvironmentID  string        `json:"environment_id"`
	WindowStart    time.Time     `json:"window_start"`
	WindowEnd      time.Time     `json:"window_end"`
	GracePeriod    time.Duration `json:"grace_period"`
	BatchSizeLimit int           `json:"batch_size_limit"`
	MaxRetries     int           `json:"max_retries"`
	APITimeout     time.Duration `json:"api_timeout"`
}

// StripeEventSyncWorkflowResult represents the result of the Stripe event sync workflow
type StripeEventSyncWorkflowResult struct {
	TotalBatches      int       `json:"total_batches"`
	SuccessfulBatches int       `json:"successful_batches"`
	FailedBatches     int       `json:"failed_batches"`
	TotalEvents       int       `json:"total_events"`
	TotalQuantity     float64   `json:"total_quantity"`
	ProcessedAt       time.Time `json:"processed_at"`
	Errors            []string  `json:"errors,omitempty"`
}

// AggregateEventsActivityInput represents the input for event aggregation activity
type AggregateEventsActivityInput struct {
	TenantID      string    `json:"tenant_id"`
	EnvironmentID string    `json:"environment_id"`
	WindowStart   time.Time `json:"window_start"`
	WindowEnd     time.Time `json:"window_end"`
	BatchSize     int       `json:"batch_size"`
}

// AggregateEventsActivityResult represents the result of event aggregation activity
type AggregateEventsActivityResult struct {
	Aggregations  []EventAggregation `json:"aggregations"`
	TotalEvents   int                `json:"total_events"`
	TotalQuantity float64            `json:"total_quantity"`
}

// EventAggregation represents aggregated event data for a specific customer/meter/event type
type EventAggregation struct {
	CustomerID         string  `json:"customer_id"`
	MeterID            string  `json:"meter_id"`
	EventType          string  `json:"event_type"`
	AggregatedQuantity float64 `json:"aggregated_quantity"`
	EventCount         int     `json:"event_count"`
}

// SyncToStripeActivityInput represents the input for Stripe API sync activity
type SyncToStripeActivityInput struct {
	TenantID      string             `json:"tenant_id"`
	EnvironmentID string             `json:"environment_id"`
	Aggregations  []EventAggregation `json:"aggregations"`
	WindowStart   time.Time          `json:"window_start"`
	WindowEnd     time.Time          `json:"window_end"`
}

// SyncToStripeActivityResult represents the result of Stripe API sync activity
type SyncToStripeActivityResult struct {
	SyncedBatches   []SyncBatchResult `json:"synced_batches"`
	SuccessfulSyncs int               `json:"successful_syncs"`
	FailedSyncs     int               `json:"failed_syncs"`
}

// SyncBatchResult represents the result of syncing a single batch to Stripe
type SyncBatchResult struct {
	BatchID            string  `json:"batch_id"`
	CustomerID         string  `json:"customer_id"`
	MeterID            string  `json:"meter_id"`
	EventType          string  `json:"event_type"`
	StripeEventID      string  `json:"stripe_event_id,omitempty"`
	Success            bool    `json:"success"`
	ErrorMessage       string  `json:"error_message,omitempty"`
	RetryCount         int     `json:"retry_count"`
	AggregatedQuantity float64 `json:"aggregated_quantity"`
	EventCount         int     `json:"event_count"`
}

// TrackSyncBatchActivityInput represents the input for batch tracking activity
type TrackSyncBatchActivityInput struct {
	TenantID      string            `json:"tenant_id"`
	EnvironmentID string            `json:"environment_id"`
	SyncResults   []SyncBatchResult `json:"sync_results"`
	WindowStart   time.Time         `json:"window_start"`
	WindowEnd     time.Time         `json:"window_end"`
}

// TrackSyncBatchActivityResult represents the result of batch tracking activity
type TrackSyncBatchActivityResult struct {
	TrackedBatches   int `json:"tracked_batches"`
	SuccessfulTracks int `json:"successful_tracks"`
	FailedTracks     int `json:"failed_tracks"`
}

// Validation methods

// Validate validates the workflow input
func (input *StripeEventSyncWorkflowInput) Validate() error {
	if input.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if input.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if input.WindowStart.IsZero() {
		return ierr.NewError("window_start is required").
			WithHint("Window start time must be provided").
			Mark(ierr.ErrValidation)
	}

	if input.WindowEnd.IsZero() {
		return ierr.NewError("window_end is required").
			WithHint("Window end time must be provided").
			Mark(ierr.ErrValidation)
	}

	if input.WindowStart.After(input.WindowEnd) {
		return ierr.NewError("window_start cannot be after window_end").
			WithHint("Window start must be before or equal to window end").
			Mark(ierr.ErrValidation)
	}

	if input.GracePeriod < 0 {
		return ierr.NewError("grace_period cannot be negative").
			WithHint("Grace period must be >= 0").
			Mark(ierr.ErrValidation)
	}

	if input.BatchSizeLimit <= 0 {
		return ierr.NewError("batch_size_limit must be positive").
			WithHint("Batch size limit must be > 0").
			Mark(ierr.ErrValidation)
	}

	if input.BatchSizeLimit > 10000 {
		return ierr.NewError("batch_size_limit too large").
			WithHint("Batch size limit must be <= 10000").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Validate validates the aggregation activity input
func (input *AggregateEventsActivityInput) Validate() error {
	if input.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if input.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if input.WindowStart.IsZero() {
		return ierr.NewError("window_start is required").
			WithHint("Window start time must be provided").
			Mark(ierr.ErrValidation)
	}

	if input.WindowEnd.IsZero() {
		return ierr.NewError("window_end is required").
			WithHint("Window end time must be provided").
			Mark(ierr.ErrValidation)
	}

	if input.WindowStart.After(input.WindowEnd) {
		return ierr.NewError("window_start cannot be after window_end").
			WithHint("Window start must be before or equal to window end").
			Mark(ierr.ErrValidation)
	}

	if input.BatchSize <= 0 {
		return ierr.NewError("batch_size must be positive").
			WithHint("Batch size must be > 0").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Validate validates the Stripe sync activity input
func (input *SyncToStripeActivityInput) Validate() error {
	if input.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if input.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if len(input.Aggregations) == 0 {
		return ierr.NewError("aggregations is required").
			WithHint("At least one aggregation must be provided").
			Mark(ierr.ErrValidation)
	}

	for i, agg := range input.Aggregations {
		if err := agg.Validate(); err != nil {
			return ierr.NewError("invalid aggregation").
				WithHint("Aggregation validation failed at index " + string(rune(i+'0'))).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// Validate validates the event aggregation
func (agg *EventAggregation) Validate() error {
	if agg.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("Customer ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if agg.MeterID == "" {
		return ierr.NewError("meter_id is required").
			WithHint("Meter ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if agg.EventType == "" {
		return ierr.NewError("event_type is required").
			WithHint("Event type must not be empty").
			Mark(ierr.ErrValidation)
	}

	if agg.AggregatedQuantity < 0 {
		return ierr.NewError("aggregated_quantity cannot be negative").
			WithHint("Aggregated quantity must be >= 0").
			Mark(ierr.ErrValidation)
	}

	if agg.EventCount < 0 {
		return ierr.NewError("event_count cannot be negative").
			WithHint("Event count must be >= 0").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Validate validates the batch tracking activity input
func (input *TrackSyncBatchActivityInput) Validate() error {
	if input.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if input.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID must not be empty").
			Mark(ierr.ErrValidation)
	}

	if len(input.SyncResults) == 0 {
		return ierr.NewError("sync_results is required").
			WithHint("At least one sync result must be provided").
			Mark(ierr.ErrValidation)
	}

	return nil
}
