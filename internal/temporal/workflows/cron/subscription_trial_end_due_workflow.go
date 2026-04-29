package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityProcessTrialEndDue = "ProcessTrialEndDueActivity"
)

// SubscriptionTrialEndDueWorkflow processes trialing subscriptions whose trial has ended.
// Same work as POST /v1/cron/subscriptions/process-trial-end-due; run via Temporal Schedule.
func SubscriptionTrialEndDueWorkflow(ctx workflow.Context, _ cronModels.SubscriptionTrialEndDueWorkflowInput) (*cronModels.SubscriptionTrialEndDueWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting SubscriptionTrialEndDueWorkflow")

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

	var result cronModels.SubscriptionTrialEndDueWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityProcessTrialEndDue).Get(ctx, &result); err != nil {
		log.Error("SubscriptionTrialEndDueWorkflow activity failed", "error", err)
		return nil, err
	}
	log.Info("SubscriptionTrialEndDueWorkflow completed",
		"total_success", result.TotalSuccess,
		"total_failed", result.TotalFailed,
	)
	return &result, nil
}
