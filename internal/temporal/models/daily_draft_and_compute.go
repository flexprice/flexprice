package models

import "time"

// DailyDraftAndComputeWorkflowInput is input for DailyDraftAndComputeWorkflow. Empty today —
// present for symmetry with every other scheduled cron workflow input in this package, and so a
// future knob (e.g. a manual override reference time) has somewhere to go without breaking the
// schedule registration's Input type.
type DailyDraftAndComputeWorkflowInput struct{}

// DailyDraftAndComputeActivityInput is input for DailyDraftAndComputeActivity.
type DailyDraftAndComputeActivityInput struct {
	// ReferenceTime anchors the day-stamp used in each triggered subscription's deterministic
	// workflow ID. Read once by the parent workflow (from the TemporalScheduledStartTime search
	// attribute, falling back to workflow.Now(ctx)) so a retry hours later — even across a UTC
	// midnight boundary — still stamps with the original run's date, keeping the dedupe ID
	// stable across the whole activity attempt.
	ReferenceTime time.Time `json:"reference_time"`
}

// DailyDraftAndComputeWorkflowResult is the result of DailyDraftAndComputeWorkflow /
// DailyDraftAndComputeActivity.
type DailyDraftAndComputeWorkflowResult struct {
	TenantEnvsProcessed   int `json:"tenant_envs_processed"`
	TenantEnvsFailed      int `json:"tenant_envs_failed"`
	TotalDueSubscriptions int `json:"total_due_subscriptions"`
	TriggeredCount        int `json:"triggered_count"`
	SkippedCount          int `json:"skipped_count"`
	FailedCount           int `json:"failed_count"`
}
