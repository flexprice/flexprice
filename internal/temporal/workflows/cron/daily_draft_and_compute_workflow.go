package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityDailyDraftAndCompute = "DailyDraftAndComputeActivity"
)

// DailyDraftAndComputeWorkflow ensures every active subscription in an enabled tenant×environment
// has a draft invoice for its current period and recomputes it — never finalizes. Triggered daily
// by a Temporal Schedule (see internal/temporal/service/schedules.go).
func DailyDraftAndComputeWorkflow(ctx workflow.Context, _ cronModels.DailyDraftAndComputeWorkflowInput) (*cronModels.DailyDraftAndComputeWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting DailyDraftAndComputeWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 24 * time.Hour,
		// HeartbeatTimeout matches ExpireCreditsActivity's cross-tenant fan-out pattern
		// (WalletCreditExpiryWorkflow), not FinalizeDueDraftsActivity's (which doesn't
		// heartbeat) — a worker crash mid-run must be caught in minutes, not up to 24h, since
		// this job's whole premise is same-day freshness.
		HeartbeatTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    10 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Anchor the day-stamp used for child workflow IDs to this run's scheduled start time, not
	// wall-clock time read inside a retried activity — mirrors MarketplaceUsageSnapshotWorkflow's
	// scheduledStartTime helper in this same package.
	referenceTime, ok := scheduledStartTime(ctx)
	if !ok {
		log.Warn("scheduled start time unavailable; falling back to current time for the day-stamp")
		referenceTime = workflow.Now(ctx)
	}

	activityInput := cronModels.DailyDraftAndComputeActivityInput{
		ReferenceTime: referenceTime,
	}

	var result cronModels.DailyDraftAndComputeWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityDailyDraftAndCompute, activityInput).Get(ctx, &result); err != nil {
		log.Error("DailyDraftAndComputeWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("DailyDraftAndComputeWorkflow completed",
		"tenant_envs_processed", result.TenantEnvsProcessed,
		"total_due_subscriptions", result.TotalDueSubscriptions,
		"triggered", result.TriggeredCount,
		"skipped", result.SkippedCount,
		"failed", result.FailedCount)

	return &result, nil
}
