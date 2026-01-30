package models

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ReprocessEventsWorkflowInput represents the input for reprocess events workflow
type ReprocessEventsWorkflowInput struct {
	ExternalCustomerID string    `json:"external_customer_id"`
	EventName          string    `json:"event_name"`
	StartDate          time.Time `json:"start_date"`
	EndDate            time.Time `json:"end_date"`
	BatchSize          int       `json:"batch_size"`
	TenantID           string    `json:"tenant_id"`
	EnvironmentID      string    `json:"environment_id"`
	UserID             string    `json:"user_id"`
}

// Validate validates the reprocess events workflow input
func (i *ReprocessEventsWorkflowInput) Validate() error {
	// external_customer_id is optional - allows reprocessing all events
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
	// Validate batch size (default to 100 if not provided or invalid)
	if i.BatchSize <= 0 {
		i.BatchSize = 100 // Default batch size
	}
	return nil
}

// ReprocessEventsWorkflowResult represents the result of reprocess events workflow
type ReprocessEventsWorkflowResult struct {
	TotalEventsFound     int       `json:"total_events_found"`
	TotalEventsPublished int       `json:"total_events_published"`
	ProcessedBatches     int       `json:"processed_batches"`
	CompletedAt          time.Time `json:"completed_at"`
}
