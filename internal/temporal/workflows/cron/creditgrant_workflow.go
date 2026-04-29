package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityProcessScheduledCreditGrantApplications = "ProcessScheduledCreditGrantApplicationsActivity"
)

// CreditGrantProcessingWorkflow processes all pending scheduled credit grant applications.
// It is triggered by a Temporal Schedule every 15 minutes.
func CreditGrantProcessingWorkflow(ctx workflow.Context, _ cronModels.CreditGrantProcessingWorkflowInput) (*cronModels.CreditGrantProcessingWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting CreditGrantProcessingWorkflow")

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

	var result cronModels.CreditGrantProcessingWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityProcessScheduledCreditGrantApplications).Get(ctx, &result); err != nil {
		log.Error("CreditGrantProcessingWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("CreditGrantProcessingWorkflow completed",
		"processed", result.Processed,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
	)
	return &result, nil
}
