package models

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// UsageAlertWorkflowInput is the input to UsageAlertWorkflow. The workflow runs
// once per (tenant, environment, customer) per debounce window; the first trigger
// for a customer schedules the workflow with StartDelay, subsequent triggers
// collide on the stable workflow ID and are absorbed by Temporal.
//
// The workflow itself is trigger-agnostic. Today the only caller is meter-usage
// post-insert, but any source (wallet transactions, subscription cron, etc.) can
// schedule it — the workflow just runs "evaluate spend + wallet alerts for
// customer".
type UsageAlertWorkflowInput struct {
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	CustomerID    string `json:"customer_id"`

	// ActivityStaleAfter caps queue wait per activity (ScheduleToStartTimeout).
	// Zero disables staleness handling. Stamped from config by the scheduler so
	// workflow code stays deterministic.
	ActivityStaleAfter time.Duration `json:"activity_stale_after,omitempty"`
	// StaleRescheduleDelay is the pause before re-enqueueing a stale activity.
	StaleRescheduleDelay time.Duration `json:"stale_reschedule_delay,omitempty"`
}

func (i UsageAlertWorkflowInput) Validate() error {
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").Mark(ierr.ErrValidation)
	}
	if i.CustomerID == "" {
		return ierr.NewError("customer_id is required").Mark(ierr.ErrValidation)
	}
	return nil
}

// UsageAlertActivityInput is the input to the usage-alert activities.
type UsageAlertActivityInput struct {
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	CustomerID    string `json:"customer_id"`
}
