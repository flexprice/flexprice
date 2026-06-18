package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const ActivityReconcilePendingMoyasarPayments = "ReconcilePendingMoyasarPaymentsActivity"

// MoyasarReconcilePendingWorkflow polls all PENDING Moyasar payments, re-fetches their
// status from Moyasar, and advances them to SUCCEEDED or FAILED. For AUTH payments that
// succeed it also activates the associated payment method token.
// Runs every 15 minutes via a Temporal Schedule.
func MoyasarReconcilePendingWorkflow(ctx workflow.Context, _ cronModels.MoyasarReconcilePendingWorkflowInput) (*cronModels.MoyasarReconcilePendingWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting MoyasarReconcilePendingWorkflow")

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

	var result cronModels.MoyasarReconcilePendingWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityReconcilePendingMoyasarPayments).Get(ctx, &result); err != nil {
		log.Error("MoyasarReconcilePendingWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("MoyasarReconcilePendingWorkflow completed",
		"total", result.Total,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
		"skipped", result.Skipped,
	)
	return &result, nil
}
