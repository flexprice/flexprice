package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowWhopInvoiceSync     = "WhopInvoiceSyncWorkflow"
	WorkflowWhopInvoiceMarkPaid = "WhopInvoiceMarkPaidWorkflow"
	ActivitySyncInvoiceToWhop   = "SyncInvoiceToWhop"
	ActivityMarkWhopInvoicePaid = "MarkWhopInvoicePaid"
)

// WhopInvoiceSyncWorkflow syncs a Flexprice invoice to Whop.
// Sleeps 5s first to let the invoice commit, then calls SyncInvoiceToWhop activity.
func WhopInvoiceSyncWorkflow(ctx workflow.Context, input models.WhopInvoiceSyncWorkflowInput) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting Whop invoice sync workflow",
		"invoice_id", input.InvoiceID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	if err := input.Validate(); err != nil {
		return err
	}

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})

	if err := workflow.Sleep(ctx, 5*time.Second); err != nil {
		return err
	}

	if err := workflow.ExecuteActivity(ctx, ActivitySyncInvoiceToWhop, input).Get(ctx, nil); err != nil {
		logger.Error("Failed to sync invoice to Whop", "error", err, "invoice_id", input.InvoiceID)
		return err
	}

	logger.Info("Successfully synced invoice to Whop", "invoice_id", input.InvoiceID)
	return nil
}

// WhopInvoiceMarkPaidWorkflow marks the Whop invoice as paid when Flexprice marks it paid.
func WhopInvoiceMarkPaidWorkflow(ctx workflow.Context, input models.WhopInvoiceMarkPaidWorkflowInput) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting Whop mark-paid workflow",
		"invoice_id", input.InvoiceID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	if err := input.Validate(); err != nil {
		return err
	}

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})

	if err := workflow.ExecuteActivity(ctx, ActivityMarkWhopInvoicePaid, input).Get(ctx, nil); err != nil {
		logger.Error("Failed to mark Whop invoice as paid", "error", err, "invoice_id", input.InvoiceID)
		return err
	}

	logger.Info("Successfully marked Whop invoice as paid", "invoice_id", input.InvoiceID)
	return nil
}
