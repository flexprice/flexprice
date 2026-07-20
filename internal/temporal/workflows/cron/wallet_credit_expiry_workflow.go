package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityExpireCredits = "ExpireCreditsActivity"
)

// WalletCreditExpiryWorkflow expires wallet credits that have passed their expiry date.
// It is triggered by a Temporal Schedule every 15 minutes.
func WalletCreditExpiryWorkflow(ctx workflow.Context, _ cronModels.WalletCreditExpiryWorkflowInput) (*cronModels.WalletCreditExpiryWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting WalletCreditExpiryWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result cronModels.WalletCreditExpiryWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityExpireCredits).Get(ctx, &result); err != nil {
		log.Error("WalletCreditExpiryWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("WalletCreditExpiryWorkflow completed",
		"total", result.Total,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
	)
	return &result, nil
}
