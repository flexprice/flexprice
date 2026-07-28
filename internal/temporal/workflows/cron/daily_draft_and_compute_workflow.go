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

// DailyDraftAndComputeWorkflow creates and recomputes daily draft invoices.
func DailyDraftAndComputeWorkflow(ctx workflow.Context, _ cronModels.DailyDraftAndComputeWorkflowInput) (*cronModels.DailyDraftAndComputeWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting DailyDraftAndComputeWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 24 * time.Hour,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    10 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

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
		"failed", result.FailedCount)

	return &result, nil
}
