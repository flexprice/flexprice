package models

import "time"

// ===================== MarketplaceUsageSnapshot (Cron A, every 6h) =====================

// MarketplaceUsageSnapshotWorkflowInput is the input for the snapshot cron. It is empty: the
// reporting window is derived inside the workflow from the run's scheduled time.
type MarketplaceUsageSnapshotWorkflowInput struct{}

// MarketplaceUsageSnapshotActivityInput carries the reporting window computed by the workflow.
type MarketplaceUsageSnapshotActivityInput struct {
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
}

// MarketplaceUsageSnapshotWorkflowResult captures outcome metrics. Total counts distinct
// subscriptions walked; Succeeded counts those that now have a usage record for the window (whether
// this run wrote it or found one already there); Failed counts those that could not be snapshotted.
// The window is echoed back so a run's output is self-describing in the Temporal UI without having
// to cross-reference the schedule's fire time.
//
// Counts only, no id lists: a run walks every marketplace subscription in the deployment, so naming
// them would write an unbounded list into Temporal's workflow history. The per-subscription detail
// lives in the logs, keyed by subscription_id.
type MarketplaceUsageSnapshotWorkflowResult struct {
	Total       int       `json:"total"`
	Succeeded   int       `json:"succeeded"`
	Failed      int       `json:"failed"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
}

// MarketplaceUsageReportWorkflowInput is the input for MarketplaceUsageReportWorkflow. It is
// empty; the activity reads all unsynced usage records itself.
type MarketplaceUsageReportWorkflowInput struct{}

// MarketplaceUsageReportWorkflowResult captures outcome metrics. Total counts records that reached
// at least one relevant connection this run; Succeeded counts those now fully synced to every
// relevant marketplace; Failed counts those still awaiting at least one. Skipped counts sync entries
// resolved without anything being posted (today only Azure's zero-quantity case), which would
// otherwise be indistinguishable from a real acceptance in Succeeded.
//
// Counts only, never ids: a run covers every record across every tenant, so any id list here would be
// unbounded in Temporal's workflow history. Every id is already on the per-record log lines, which is
// where debugging starts.
type MarketplaceUsageReportWorkflowResult struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

// ===================== MarketplaceSubscriptionFinalUsageFlush (triggered by CancelSubscription) =====================

// MarketplaceSubscriptionFinalUsageFlushWorkflowInput is the input for the cancellation flush.
//
// CancelAt must be the marketplace's own cancellation instant, which the tenant supplies on the
// cancellation request. It is not the time Flexprice processed that request: that is always later,
// and reporting against it would put the final usage after the cancellation the provider recorded.
//
// TenantID and EnvironmentID are stamped on from the triggering context when the workflow starts.
// They are required, not informational: the activity scopes every query by them, and workflow
// tracking reads them from this struct.
type MarketplaceSubscriptionFinalUsageFlushWorkflowInput struct {
	SubscriptionID string    `json:"subscription_id"`
	CancelAt       time.Time `json:"cancel_at"`
	TenantID       string    `json:"tenant_id"`
	EnvironmentID  string    `json:"environment_id"`
}

// MarketplaceSubscriptionFinalUsageFlushActivityInput mirrors the workflow input; kept as a separate type so
// the activity signature doesn't change if the workflow input grows fields the activity doesn't need.
type MarketplaceSubscriptionFinalUsageFlushActivityInput struct {
	SubscriptionID string    `json:"subscription_id"`
	CancelAt       time.Time `json:"cancel_at"`
	TenantID       string    `json:"tenant_id"`
	EnvironmentID  string    `json:"environment_id"`
}

// MarketplaceSubscriptionFinalUsageFlushWorkflowResult captures what one flush did.
type MarketplaceSubscriptionFinalUsageFlushWorkflowResult struct {
	SubscriptionID string `json:"subscription_id"`

	// The final usage record covering the span up to cancellation, whether this run created it or
	// found one an earlier attempt had already written. PeriodEnd is the true cancellation instant;
	// the reporting margin is applied only to the value sent to the providers. All three are empty
	// when no such record was needed, which is what a backdated cancellation looks like.
	PeriodStart   time.Time `json:"period_start,omitempty"`
	PeriodEnd     time.Time `json:"period_end,omitempty"`
	FinalRecordID string    `json:"final_record_id,omitempty"`

	// Every record this run reported, the backlog and the final one alike, counted by outcome. A
	// record is succeeded once every marketplace mapped to it has accepted or skipped it and at least
	// one accepted; skipped when they all skipped; failed while any is still outstanding. Counted
	// rather than named: a cancellation can carry a full day of backlog behind it.
	RecordsSucceeded int `json:"records_succeeded"`
	RecordsFailed    int `json:"records_failed"`
	RecordsSkipped   int `json:"records_skipped"`

	// How many marketplace mappings this run archived. Zero unless every record above synced and every
	// mapped connection resolved: the mappings stay published on any failure so the reporting cron can
	// still find the outstanding records.
	MappingsDelinked int `json:"mappings_delinked"`
}
