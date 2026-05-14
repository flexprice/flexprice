package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityProcessAutoInvoiceThresholdBilling = "ProcessAutoInvoiceThresholdBillingActivity"
)

// AutoInvoiceThresholdBillingWorkflow checks subscriptions with subscription-level
// auto_invoice_threshold set and creates mid-period invoices when current-period usage
// has crossed that threshold.
// Intended to run every 5 minutes via a Temporal Schedule.
func AutoInvoiceThresholdBillingWorkflow(ctx workflow.Context, _ cronModels.AutoInvoiceThresholdBillingWorkflowInput) (*cronModels.AutoInvoiceThresholdBillingWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting auto invoice threshold billing workflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result cronModels.AutoInvoiceThresholdBillingWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityProcessAutoInvoiceThresholdBilling).Get(ctx, &result); err != nil {
		log.Error("Auto invoice threshold billing workflow activity failed", "error", err)
		return nil, err
	}

	log.Info("Auto invoice threshold billing workflow completed",
		"total_checked", result.TotalChecked,
		"total_invoiced", result.TotalInvoiced,
		"total_skipped", result.TotalSkipped,
		"total_failed", result.TotalFailed)

	return &result, nil
}
