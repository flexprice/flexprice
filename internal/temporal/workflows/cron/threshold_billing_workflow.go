package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityProcessThresholdBilling = "ProcessThresholdBillingActivity"
)

// ThresholdBillingWorkflow checks all subscriptions with an effective auto_invoice_threshold
// and creates mid-period invoices for those whose current-period usage has crossed the threshold.
// Intended to run every 5 minutes via a Temporal Schedule.
func ThresholdBillingWorkflow(ctx workflow.Context, _ cronModels.ThresholdBillingWorkflowInput) (*cronModels.ThresholdBillingWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting ThresholdBillingWorkflow")

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

	var result cronModels.ThresholdBillingWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityProcessThresholdBilling).Get(ctx, &result); err != nil {
		log.Error("ThresholdBillingWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("ThresholdBillingWorkflow completed",
		"total_checked", result.TotalChecked,
		"total_invoiced", result.TotalInvoiced,
		"total_failed", result.TotalFailed)

	return &result, nil
}
