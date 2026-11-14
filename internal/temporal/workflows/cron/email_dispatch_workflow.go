package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityDispatchEmails = "DispatchEmailsActivity"
)

// EmailDispatchWorkflow sends queued customer emails. Triggered by a Temporal schedule every minute.
func EmailDispatchWorkflow(ctx workflow.Context, _ cronModels.EmailDispatchWorkflowInput) (*cronModels.EmailDispatchWorkflowResult, error) {
	log := workflow.GetLogger(ctx)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result cronModels.EmailDispatchWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityDispatchEmails).Get(ctx, &result); err != nil {
		log.Error("EmailDispatchWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("EmailDispatchWorkflow completed", "sent", result.Sent, "failed", result.Failed, "skipped", result.Skipped, "deferred", result.Deferred)
	return &result, nil
}
