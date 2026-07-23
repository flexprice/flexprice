package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowZohoBooksInvoiceSync     = "ZohoBooksInvoiceSyncWorkflow"
	ActivitySyncInvoiceToZoho        = "SyncInvoiceToZoho"
	WorkflowZohoBooksInvoiceMarkPaid = "ZohoBooksInvoiceMarkPaidWorkflow"
	ActivityMarkZohoBooksInvoicePaid = "MarkZohoBooksInvoicePaid"
)

// ZohoBooksInvoiceSyncWorkflow syncs finalized invoices to Zoho Books.
func ZohoBooksInvoiceSyncWorkflow(ctx workflow.Context, input models.ZohoBooksInvoiceSyncWorkflowInput) error {
	logger := workflow.GetLogger(ctx)
	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return err
	}

	opts := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, opts)

	if err := workflow.Sleep(ctx, 5*time.Second); err != nil {
		return err
	}
	return workflow.ExecuteActivity(ctx, ActivitySyncInvoiceToZoho, input).Get(ctx, nil)
}

// ZohoBooksInvoiceMarkPaidWorkflow marks the corresponding Zoho Books invoice as paid
// when FlexPrice marks the invoice paid.
func ZohoBooksInvoiceMarkPaidWorkflow(ctx workflow.Context, input models.ZohoBooksInvoiceMarkPaidWorkflowInput) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting Zoho Books mark-paid workflow",
		"invoice_id", input.InvoiceID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return err
	}

	opts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, opts)

	if err := workflow.ExecuteActivity(ctx, ActivityMarkZohoBooksInvoicePaid, input).Get(ctx, nil); err != nil {
		logger.Error("Failed to mark Zoho Books invoice as paid", "error", err, "invoice_id", input.InvoiceID)
		return err
	}

	logger.Info("Successfully marked Zoho Books invoice as paid", "invoice_id", input.InvoiceID)
	return nil
}
