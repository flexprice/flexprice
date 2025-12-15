package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowNomodInvoiceSync = "NomodInvoiceSyncWorkflow"
	// Activity names - must match the registered method names
	ActivitySyncInvoiceToNomod = "SyncInvoiceToNomod"
)

// NomodInvoiceSyncWorkflow orchestrates the Nomod invoice synchronization process
// Steps:
// 1. Sleep for 5 seconds to allow invoice to be committed to database
// 2. Sync invoice to Nomod (create invoice, line items, associate to customer)
func NomodInvoiceSyncWorkflow(ctx workflow.Context, input models.NomodInvoiceSyncWorkflowInput) error {
	logger := workflow.GetLogger(ctx)

	logger.Info("Starting Nomod invoice sync workflow",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return err
	}

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Step 1: Sleep for 5 seconds to allow invoice to be committed to database
	// This ensures the invoice data is fully persisted before we try to sync it
	logger.Info("Step 1: Waiting for invoice to be committed to database",
		"invoice_id", input.InvoiceID,
		"wait_seconds", 5)

	err := workflow.Sleep(ctx, 5*time.Second)
	if err != nil {
		logger.Error("Sleep was interrupted", "error", err)
		return err
	}

	logger.Info("Wait completed, proceeding to sync invoice", "invoice_id", input.InvoiceID)

	// Step 2: Sync invoice to Nomod
	logger.Info("Step 2: Syncing invoice to Nomod", "invoice_id", input.InvoiceID)

	err = workflow.ExecuteActivity(ctx, ActivitySyncInvoiceToNomod, input).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to sync invoice to Nomod",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	logger.Info("Successfully completed Nomod invoice sync workflow",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID)

	return nil
}

