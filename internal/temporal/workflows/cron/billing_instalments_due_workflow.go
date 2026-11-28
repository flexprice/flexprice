package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityBillDueInstalments = "BillDueInstalmentsActivity"
)

// BillingInstalmentsDueWorkflow bills each due separate-invoice instalment on its own one-off invoice.
func BillingInstalmentsDueWorkflow(ctx workflow.Context, _ cronModels.BillingInstalmentsDueWorkflowInput) error {
	log := workflow.GetLogger(ctx)
	log.Info("Starting BillingInstalmentsDueWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	if err := workflow.ExecuteActivity(ctx, ActivityBillDueInstalments).Get(ctx, nil); err != nil {
		log.Error("BillingInstalmentsDueWorkflow activity failed", "error", err)
		return err
	}

	log.Info("BillingInstalmentsDueWorkflow completed")
	return nil
}
