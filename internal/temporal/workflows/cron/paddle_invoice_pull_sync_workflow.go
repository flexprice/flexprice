package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityFetchAndTriggerPaddleInvoicePullSync = "FetchAndTriggerPaddleInvoicePullSyncActivity"
)

// PaddleInvoicePullSyncCronWorkflow is the Temporal-scheduled cron workflow that fans out
// PaddleInvoicePullSyncWorkflow for every finalized+unpaid invoice with a Paddle connection.
func PaddleInvoicePullSyncCronWorkflow(ctx workflow.Context, _ cronModels.PaddleInvoicePullSyncCronInput) (*cronModels.PaddleInvoicePullSyncCronResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting PaddleInvoicePullSyncCronWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result cronModels.PaddleInvoicePullSyncCronResult
	if err := workflow.ExecuteActivity(ctx, ActivityFetchAndTriggerPaddleInvoicePullSync).Get(ctx, &result); err != nil {
		log.Error("PaddleInvoicePullSyncCronWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("PaddleInvoicePullSyncCronWorkflow completed",
		"total", result.Total,
		"triggered", result.Triggered,
		"failed", result.Failed,
	)
	return &result, nil
}
