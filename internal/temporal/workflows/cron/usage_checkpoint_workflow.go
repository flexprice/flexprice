package cron

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/aggregatedusage"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityListUsageCheckpointEnvironments = "ListUsageCheckpointEnvironmentsActivity"
	ActivityUsageCheckpointEnvironment      = "UsageCheckpointEnvironmentActivity"

	// usageCheckpointEnvironmentTimeout stays below the hourly interval; the schedule skips overlapping runs.
	usageCheckpointEnvironmentTimeout = 50 * time.Minute
	usageCheckpointListTimeout        = 5 * time.Minute
)

var usageCheckpointRetryPolicy = &temporal.RetryPolicy{
	InitialInterval:    30 * time.Second,
	BackoffCoefficient: 2.0,
	MaximumInterval:    5 * time.Minute,
	MaximumAttempts:    2,
}

// UsageCheckpointWorkflow brings per-period meter totals up to the aggregated usage cutoff, one activity
// per environment so a slow environment cannot time out the others. The cutoff comes from the
// scheduled time, so a retry or manual re-run covers the same window.
func UsageCheckpointWorkflow(ctx workflow.Context, _ cronModels.UsageCheckpointWorkflowInput) error {
	scheduledTime, ok := scheduledStartTime(ctx)
	if !ok {
		scheduledTime = workflow.Now(ctx)
	}
	cutoff := aggregatedusage.Cutoff(scheduledTime)

	listCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: usageCheckpointListTimeout,
		RetryPolicy:         usageCheckpointRetryPolicy,
	})
	var environments []cronModels.UsageCheckpointEnvironment
	if err := workflow.ExecuteActivity(listCtx, ActivityListUsageCheckpointEnvironments, cronModels.UsageCheckpointListInput{Cutoff: cutoff}).Get(ctx, &environments); err != nil {
		return err
	}

	envCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: usageCheckpointEnvironmentTimeout,
		RetryPolicy:         usageCheckpointRetryPolicy,
	})
	futures := make([]workflow.Future, 0, len(environments))
	for _, environment := range environments {
		futures = append(futures, workflow.ExecuteActivity(envCtx, ActivityUsageCheckpointEnvironment, cronModels.UsageCheckpointEnvironmentInput{
			TenantID:      environment.TenantID,
			EnvironmentID: environment.EnvironmentID,
			Cutoff:        cutoff,
		}))
	}
	var firstErr error
	for _, future := range futures {
		if err := future.Get(ctx, nil); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
