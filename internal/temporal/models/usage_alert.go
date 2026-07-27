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

	// ScheduledFor is the intended fire time (schedule time + StartDelay),
	// stamped by the scheduler. The workflow compares it against workflow.Now
	// to detect runs that sat in a backlogged workflow task queue.
	ScheduledFor time.Time `json:"scheduled_for,omitempty"`
	// StaleAfter is the staleness bound: a run firing more than this past
	// ScheduledFor yields once via ContinueAsNew; also each activity's
	// ScheduleToStartTimeout. Zero disables staleness handling. Stamped from
	// config by the scheduler so workflow code stays deterministic.
	StaleAfter time.Duration `json:"stale_after,omitempty"`
	// AlreadyRescheduled marks a run created by the staleness re-schedule — at
	// most one re-schedule per chain, so a sustained backlog can't livelock on
	// ContinueAsNew.
	AlreadyRescheduled bool `json:"already_rescheduled,omitempty"`
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
