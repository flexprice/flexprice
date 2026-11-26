package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const ActivityOptimizeMeterUsage = "OptimizeMeterUsageActivity"

// MeterUsageOptimizeWorkflow merges recently closed meter_usage day-partitions.
// A failed run changes nothing about query results.
func MeterUsageOptimizeWorkflow(ctx workflow.Context, _ cronModels.MeterUsageOptimizeInput) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 4 * time.Hour,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval: time.Minute,
			MaximumAttempts: 2,
		},
	})

	var result cronModels.MeterUsageOptimizeResult
	if err := workflow.ExecuteActivity(ctx, ActivityOptimizeMeterUsage).Get(ctx, &result); err != nil {
		workflow.GetLogger(ctx).Error("MeterUsageOptimizeWorkflow failed", "error", err)
		return err
	}
	workflow.GetLogger(ctx).Info("MeterUsageOptimizeWorkflow completed",
		"optimized", result.Optimized, "failed", result.Failed)
	return nil
}
