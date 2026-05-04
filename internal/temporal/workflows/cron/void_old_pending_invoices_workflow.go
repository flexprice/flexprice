package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityVoidOldPendingInvoices = "VoidOldPendingInvoicesActivity"
)

// VoidOldPendingInvoicesWorkflow voids old pending invoices for incomplete subscriptions
// across all tenant/environment pairs. Run via Temporal Schedule every hour.
func VoidOldPendingInvoicesWorkflow(ctx workflow.Context, _ cronModels.VoidOldPendingInvoicesWorkflowInput) (*cronModels.VoidOldPendingInvoicesWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting VoidOldPendingInvoicesWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result cronModels.VoidOldPendingInvoicesWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityVoidOldPendingInvoices).Get(ctx, &result); err != nil {
		log.Error("VoidOldPendingInvoicesWorkflow activity failed", "error", err)
		return nil, err
	}
	return &result, nil
}
