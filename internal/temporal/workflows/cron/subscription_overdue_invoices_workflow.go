package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityProcessOverdueSubscriptionInvoices = "ProcessOverdueSubscriptionInvoicesActivity"
)

// SubscriptionOverdueInvoicesWorkflow marks payment-gated subscriptions incomplete once a renewal
// invoice is past due.
func SubscriptionOverdueInvoicesWorkflow(ctx workflow.Context, _ cronModels.SubscriptionOverdueInvoicesWorkflowInput) error {
	log := workflow.GetLogger(ctx)
	log.Info("Starting SubscriptionOverdueInvoicesWorkflow")

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

	if err := workflow.ExecuteActivity(ctx, ActivityProcessOverdueSubscriptionInvoices).Get(ctx, nil); err != nil {
		log.Error("SubscriptionOverdueInvoicesWorkflow activity failed", "error", err)
		return err
	}

	log.Info("SubscriptionOverdueInvoicesWorkflow completed")
	return nil
}
