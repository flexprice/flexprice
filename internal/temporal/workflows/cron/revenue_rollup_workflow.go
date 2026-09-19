package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityRollupDirty = "RollupDirtyActivity"
	ActivitySweepDrift  = "SweepDriftActivity"

	// defaultRevenueRollupInterval is used when the schedule doesn't set Interval.
	defaultRevenueRollupInterval = 24 * time.Hour

	// driftSweepLookback windows the sweep on invoice created_at; deep
	// backfills pass an explicit Since instead.
	driftSweepLookback = 7 * 24 * time.Hour
)

// RevenueRollupWorkflow drives the periodic revenue_facts dirty-rollup: it asks
// RollupDirtyActivity to re-upsert every subscription with activity since
// (scheduled fire time - Interval). The scan is coarse and safe to over-run
// because RevenueRollupService.RollupDirty's upsert is idempotent.
// Triggered by a Temporal Schedule that is off by default
// (see analytics.revenue_rollup.enabled).
func RevenueRollupWorkflow(ctx workflow.Context, in cronModels.RevenueRollupInput) error {
	log := workflow.GetLogger(ctx)

	interval := in.Interval
	if interval <= 0 {
		interval = defaultRevenueRollupInterval
	}

	referenceTime, ok := scheduledStartTime(ctx)
	if !ok {
		referenceTime = workflow.Now(ctx)
	}
	since := referenceTime.Add(-interval)
	// An explicit Since (manual/backfill run) wins over the schedule-derived window.
	if in.Since != nil && !in.Since.IsZero() {
		since = *in.Since
	}

	log.Info("Starting RevenueRollupWorkflow", "since", since, "interval", interval)

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

	var result cronModels.RevenueRollupWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityRollupDirty, since).Get(ctx, &result); err != nil {
		log.Error("RevenueRollupWorkflow activity failed", "error", err)
		return err
	}

	// Sweep after the rollup so freshly re-rolled facts are compared. The
	// sweep windows wider than the rollup: an invoice can finalize days after
	// its usage last moved.
	sweepSince := referenceTime.Add(-driftSweepLookback)
	if in.Since != nil && !in.Since.IsZero() {
		sweepSince = *in.Since
	}
	var sweep cronModels.RevenueSweepResult
	if err := workflow.ExecuteActivity(ctx, ActivitySweepDrift, sweepSince).Get(ctx, &sweep); err != nil {
		log.Error("RevenueRollupWorkflow sweep activity failed", "error", err)
		return err
	}

	log.Info("RevenueRollupWorkflow completed",
		"rolled", result.Rolled, "skipped", result.Skipped,
		"checked", sweep.Checked, "drifted", sweep.Drifted, "corrected", sweep.Corrected)
	return nil
}
