package invoice

import (
	"time"

	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// WorkflowComputeInvoice is the workflow name - must match the function name
	WorkflowComputeInvoice = "ComputeInvoiceWorkflow"
)

// ComputeInvoiceWorkflow computes a draft invoice (line items, coupons, taxes)
// via a single activity. The heavy work runs in the activity with a long timeout.
func ComputeInvoiceWorkflow(
	ctx workflow.Context,
	input invoiceModels.ComputeInvoiceWorkflowInput,
) (*invoiceModels.ComputeInvoiceWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting compute invoice workflow",
		"invoice_id", input.InvoiceID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return nil, err
	}

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	activityInput := invoiceModels.ComputeInvoiceActivityInput{
		InvoiceID:     input.InvoiceID,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		UserID:        input.UserID,
	}

	var activityOutput invoiceModels.ComputeInvoiceActivityOutput
	err := workflow.ExecuteActivity(ctx, ActivityComputeInvoice, activityInput).Get(ctx, &activityOutput)
	if err != nil {
		logger.Error("Failed to compute invoice",
			"error", err,
			"invoice_id", input.InvoiceID)
		return nil, err
	}

	logger.Info("Successfully computed invoice",
		"invoice_id", input.InvoiceID,
		"skipped", activityOutput.Skipped)

	return &invoiceModels.ComputeInvoiceWorkflowResult{
		Success:     true,
		Skipped:     activityOutput.Skipped,
		CompletedAt: workflow.Now(ctx),
	}, nil
}
