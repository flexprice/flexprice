package models

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ReprocessRawEventsWorkflowInput represents the input for reprocess raw events workflow
type ReprocessRawEventsWorkflowInput struct {
	ExternalCustomerIDs []string  `json:"external_customer_ids"`
	EventNames          []string  `json:"event_names"`
	StartDate           time.Time `json:"start_date"`
	EndDate             time.Time `json:"end_date"`
	BatchSize           int       `json:"batch_size"`
	TenantID            string    `json:"tenant_id"`
	EnvironmentID       string    `json:"environment_id"`
	UserID              string    `json:"user_id"`
	EventIDs            []string  `json:"event_ids"`
	UseUnprocessed      bool      `json:"use_unprocessed"`
}

// Validate validates the reprocess raw events workflow input
func (i *ReprocessRawEventsWorkflowInput) Validate() error {
	if i.StartDate.IsZero() {
		return ierr.NewError("start_date is required").
			WithHint("Start date is required").
			Mark(ierr.ErrValidation)
	}
	if i.EndDate.IsZero() {
		return ierr.NewError("end_date is required").
			WithHint("End date is required").
			Mark(ierr.ErrValidation)
	}
	if i.StartDate.After(i.EndDate) {
		return ierr.NewError("start_date must be before end_date").
			WithHint("Start date must be before end date").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	// Validate batch size (default to 1000 if not provided or invalid)
	if i.BatchSize <= 0 {
		i.BatchSize = 1000 // Default batch size
	}
	return nil
}

// ReprocessRawEventsWorkflowResult represents the result of reprocess raw events workflow
type ReprocessRawEventsWorkflowResult struct {
	TotalEventsFound          int       `json:"total_events_found"`
	TotalEventsPublished      int       `json:"total_events_published"`
	TotalEventsFailed         int       `json:"total_events_failed"`
	TotalEventsDropped        int       `json:"total_events_dropped"`        // Events that failed validation
	TotalTransformationErrors int       `json:"total_transformation_errors"` // Events that errored during transformation
	ProcessedBatches          int       `json:"processed_batches"`
	CompletedAt               time.Time `json:"completed_at"`
}
