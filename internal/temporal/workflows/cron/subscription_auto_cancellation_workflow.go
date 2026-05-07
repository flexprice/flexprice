package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityProcessAutoCancellation = "ProcessAutoCancellationActivity"
)

// SubscriptionAutoCancellationWorkflow cancels subscriptions that are past their grace period.
// It is triggered by a Temporal Schedule every 15 minutes.
func SubscriptionAutoCancellationWorkflow(ctx workflow.Context, _ cronModels.SubscriptionAutoCancellationWorkflowInput) (*cronModels.SubscriptionAutoCancellationWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting SubscriptionAutoCancellationWorkflow")

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

	var result cronModels.SubscriptionAutoCancellationWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityProcessAutoCancellation).Get(ctx, &result); err != nil {
		log.Error("SubscriptionAutoCancellationWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("SubscriptionAutoCancellationWorkflow completed")
	return &result, nil
}
