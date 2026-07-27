package models

import "time"

// DailyDraftAndComputeWorkflowInput is the workflow input.
type DailyDraftAndComputeWorkflowInput struct{}

// DailyDraftAndComputeActivityInput is input for DailyDraftAndComputeActivity.
type DailyDraftAndComputeActivityInput struct {
	// ReferenceTime is the scheduled run's start time, for log correlation.
	ReferenceTime time.Time `json:"reference_time"`
}

// DailyDraftAndComputeWorkflowResult is the workflow result.
type DailyDraftAndComputeWorkflowResult struct {
	TenantEnvsProcessed   int `json:"tenant_envs_processed"`
	TenantEnvsFailed      int `json:"tenant_envs_failed"`
	TotalDueSubscriptions int `json:"total_due_subscriptions"`
	TriggeredCount        int `json:"triggered_count"`
	FailedCount           int `json:"failed_count"`
}
