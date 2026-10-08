package models

import "time"

// RevenueRollupInput is the input for RevenueRollupWorkflow. Interval sizes
// the scan window (zero means 1h); an explicit Since wins over the derived
// window — the handle for manual/backfill runs.
type RevenueRollupInput struct {
	Interval time.Duration `json:"interval"`
	Since    *time.Time    `json:"since,omitempty"`
	// ResumeAfterSubscriptionID restarts a scan after a given subscription,
	// within ResumeEnvironmentID. Heartbeat details only survive within one
	// activity execution, so a run whose attempts are exhausted cannot resume
	// itself -- this carries the cursor from its last heartbeat into a new run.
	ResumeEnvironmentID       string `json:"resume_environment_id,omitempty"`
	ResumeAfterSubscriptionID string `json:"resume_after_subscription_id,omitempty"`
}

// RevenueRollupWorkflowResult mirrors the counts from RevenueRollupService.RollupDirty.
type RevenueRollupWorkflowResult struct {
	Rolled  int `json:"rolled"`
	Skipped int `json:"skipped"`
}

// RevenueSweepResult mirrors the counts from RevenueService.ReconcileBookedInvoices.
type RevenueSweepResult struct {
	Checked   int `json:"checked"`
	Drifted   int `json:"drifted"`
	Corrected int `json:"corrected"`
}

// RollupDirtyActivityInput carries the scan window and any manual resume point.
type RollupDirtyActivityInput struct {
	Since                     time.Time `json:"since"`
	ResumeEnvironmentID       string    `json:"resume_environment_id,omitempty"`
	ResumeAfterSubscriptionID string    `json:"resume_after_subscription_id,omitempty"`
}
