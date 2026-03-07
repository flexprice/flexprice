package invoice

import (
	"time"

	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowRegenerateDraftInvoicesScheduled = "RegenerateDraftInvoicesScheduledWorkflow"
	WorkflowRegenerateDraftInvoicesBatch    = "RegenerateDraftInvoicesBatchWorkflow"
	ActivityTriggerRegenerateDraftInvoices  = "TriggerRegenerateDraftInvoicesActivity"
	ActivityRegenerateDraftInvoicesBatch    = "RegenerateDraftInvoicesBatchActivity"
)

// RegenerateDraftInvoicesScheduledWorkflow runs every 6 hours (via schedule). It executes a single activity that triggers the service to list draft subscription invoices in batches and start a batch workflow per batch.
func RegenerateDraftInvoicesScheduledWorkflow(ctx workflow.Context) (*invoiceModels.TriggerRegenerateDraftInvoicesActivityOutput, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting regenerate draft invoices scheduled workflow")

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 30,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    1,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	var output invoiceModels.TriggerRegenerateDraftInvoicesActivityOutput
	err := workflow.ExecuteActivity(ctx, ActivityTriggerRegenerateDraftInvoices).Get(ctx, &output)
	if err != nil {
		logger.Error("Trigger regenerate draft invoices activity failed", "error", err)
		return nil, err
	}

	logger.Info("Regenerate draft invoices scheduled workflow completed",
		"batches_started", output.BatchesStarted,
		"total_invoices", output.TotalInvoices)
	return &output, nil
}

// RegenerateDraftInvoicesBatchWorkflow processes a batch of draft subscription invoices (up to 500), regenerating each in place via the invoice service.
func RegenerateDraftInvoicesBatchWorkflow(
	ctx workflow.Context,
	input invoiceModels.RegenerateDraftInvoicesBatchWorkflowInput,
) (*invoiceModels.RegenerateDraftInvoicesBatchWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting regenerate draft invoices batch workflow", "invoice_count", len(input.Invoices))

	if err := input.Validate(); err != nil {
		logger.Error("Invalid batch workflow input", "error", err)
		return nil, err
	}

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 30,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    2,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	var result invoiceModels.RegenerateDraftInvoicesBatchWorkflowResult
	err := workflow.ExecuteActivity(ctx, ActivityRegenerateDraftInvoicesBatch, input).Get(ctx, &result)
	if err != nil {
		logger.Error("Regenerate draft invoices batch activity failed", "error", err)
		return nil, err
	}

	logger.Info("Regenerate draft invoices batch workflow completed",
		"processed", result.Processed,
		"succeeded", result.Succeeded,
		"failed", result.Failed)
	return &result, nil
}
