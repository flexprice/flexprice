package models

import "time"

// ===================== MarketplaceUsageSnapshot (Cron A, every 6h) =====================

// MarketplaceUsageSnapshotWorkflowInput is the input for MarketplaceUsageSnapshotWorkflow.
// No fields required — period_start/period_end are derived inside the workflow from the run's
// scheduled_time (design doc FLE-981 §8.3).
type MarketplaceUsageSnapshotWorkflowInput struct{}

// MarketplaceUsageSnapshotActivityInput carries the reporting window computed by the workflow.
type MarketplaceUsageSnapshotActivityInput struct {
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
}

// MarketplaceUsageSnapshotWorkflowResult captures outcome metrics.
type MarketplaceUsageSnapshotWorkflowResult struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

// MarketplaceUsageReportWorkflowInput is the input for MarketplaceUsageReportWorkflow. It is
// empty; the activity reads all unsynced usage records itself.
type MarketplaceUsageReportWorkflowInput struct{}

// MarketplaceUsageReportWorkflowResult captures outcome metrics.
type MarketplaceUsageReportWorkflowResult struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}
