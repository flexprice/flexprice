package cron

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowMarketplaceUsageSnapshot = "MarketplaceUsageSnapshotWorkflow"
	ActivityMarketplaceUsageSnapshot = "MarketplaceUsageSnapshotActivity"
)

// MarketplaceUsageSnapshotWorkflow computes the reporting window and runs the snapshot activity,
// which writes usage records for every published aws_marketplace connection. It is triggered by a
// Temporal Schedule every 6 hours (see internal/temporal/service/schedules.go).
func MarketplaceUsageSnapshotWorkflow(ctx workflow.Context, _ models.MarketplaceUsageSnapshotWorkflowInput) (*models.MarketplaceUsageSnapshotWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting MarketplaceUsageSnapshotWorkflow")

	// The window is anchored to the run's scheduled time, not its actual execution time. The
	// window is 6 hours wide (equal to the schedule interval) and ends 4 hours before the
	// scheduled time; the 4-hour lag lets usage ingestion settle before a period is treated as
	// final. Anchoring to the scheduled time keeps consecutive runs contiguous and non-overlapping,
	// and makes a manual re-run recompute the same window (and therefore the same AWS timestamp),
	// so AWS de-duplicates the resend instead of double-billing.
	//
	// The scheduled time is read from the TemporalScheduledStartTime search attribute. This has not
	// yet been verified against the deployed Temporal version, so if the attribute is absent the
	// code falls back to the current time and logs a warning.
	scheduledTime, ok := scheduledStartTime(ctx)
	if !ok {
		log.Warn("scheduled start time unavailable; falling back to current time for the reporting window")
		scheduledTime = workflow.Now(ctx)
	}

	periodStart := scheduledTime.Add(-10 * time.Hour)
	periodEnd := scheduledTime.Add(-4 * time.Hour)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	activityInput := models.MarketplaceUsageSnapshotActivityInput{
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
	}

	var result models.MarketplaceUsageSnapshotWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityMarketplaceUsageSnapshot, activityInput).Get(ctx, &result); err != nil {
		log.Error("MarketplaceUsageSnapshotWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("MarketplaceUsageSnapshotWorkflow completed",
		"period_start", periodStart,
		"period_end", periodEnd,
		"total", result.Total,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
	)
	return &result, nil
}

// scheduledStartTime reads the schedule-provided start time for this workflow execution from the
// TemporalScheduledStartTime search attribute, returning false if it is not set.
func scheduledStartTime(ctx workflow.Context) (time.Time, bool) {
	attrs := workflow.GetTypedSearchAttributes(ctx)
	key := temporal.NewSearchAttributeKeyTime("TemporalScheduledStartTime")
	return attrs.GetTime(key)
}
