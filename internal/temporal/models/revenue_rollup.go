package models

import "time"

// RevenueRollupInput is the input for RevenueRollupWorkflow. Interval sizes
// the scan window (zero means 1h); an explicit Since wins over the derived
// window — the handle for manual/backfill runs.
type RevenueRollupInput struct {
	Interval time.Duration `json:"interval"`
	Since    *time.Time    `json:"since,omitempty"`
}

// RevenueRollupWorkflowResult mirrors the counts from RevenueRollupService.RollupDirty.
type RevenueRollupWorkflowResult struct {
	Rolled  int `json:"rolled"`
	Skipped int `json:"skipped"`
}

// RevenueSweepResult mirrors the counts from RevenueService.SweepDrift.
type RevenueSweepResult struct {
	Checked   int `json:"checked"`
	Drifted   int `json:"drifted"`
	Corrected int `json:"corrected"`
}
