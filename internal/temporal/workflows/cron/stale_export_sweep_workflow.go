package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const ActivitySweepStaleExportRuns = "SweepStaleExportRunsActivity"

// StaleExportSweepWorkflow fails export runs stuck without a final status.
func StaleExportSweepWorkflow(ctx workflow.Context, _ cronModels.StaleExportSweepWorkflowInput) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    3,
		},
	})
	return workflow.ExecuteActivity(ctx, ActivitySweepStaleExportRuns).Get(ctx, nil)
}
