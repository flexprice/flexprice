package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// WorkflowPaddleInvoiceSync is the Temporal workflow type name used to start this workflow.
	WorkflowPaddleInvoiceSync = "PaddleInvoiceSyncWorkflow"
	// Activity type names — must match the method names registered with the Temporal worker.
	ActivitySyncInvoiceToPaddle          = "SyncInvoiceToPaddle"
	ActivityEnsureCustomerSyncedToPaddle = "EnsureCustomerSyncedToPaddle"
)

// PaddleInvoiceSyncWorkflow orchestrates the Paddle invoice synchronization process.
//
// Steps:
//  1. Sleep 5 s — allow invoice to commit to the database.
//  2. Ensure customer exists in Paddle (create if missing). Fails immediately on validation
//     errors (e.g. customer has no email or no address country) so operators see a clear
//     failure reason in the Temporal UI without waiting for retries to be exhausted.
//  3. Sync invoice to Paddle — create the Paddle transaction and persist the checkout URL.
func PaddleInvoiceSyncWorkflow(ctx workflow.Context, input models.PaddleInvoiceSyncWorkflowInput) error {
	logger := workflow.GetLogger(ctx)

	logger.Info("Starting Paddle invoice sync workflow",
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

	// invoiceCommitDelay is the time the workflow waits for the invoice row to be
	// fully committed to the database before attempting Paddle sync.
	var invoiceCommitDelay = 5 * time.Second

	// Step 1: Sleep to allow invoice to be committed to the database.
	logger.Info("Step 1: Waiting for invoice to be committed to database",
		"invoice_id", input.InvoiceID,
		"wait_duration", invoiceCommitDelay.String())

	if err := workflow.Sleep(ctx, invoiceCommitDelay); err != nil {
		logger.Error("Sleep was interrupted", "error", err)
		return err
	}

	logger.Info("Wait completed, proceeding to customer pre-check", "invoice_id", input.InvoiceID)

	// Step 2: Ensure the customer exists in Paddle before attempting invoice sync.
	// If the customer's initial sync workflow failed (e.g. missing email), this step
	// retries the creation now. Validation errors fail immediately (non-retryable).
	logger.Info("Step 2: Ensuring customer is synced to Paddle", "customer_id", input.CustomerID)

	customerInput := models.PaddleCustomerSyncWorkflowInput{
		CustomerID:    input.CustomerID,
		InvoiceID:     input.InvoiceID,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
	}

	if err := workflow.ExecuteActivity(ctx, ActivityEnsureCustomerSyncedToPaddle, customerInput).Get(ctx, nil); err != nil {
		logger.Error("Customer pre-check failed, aborting invoice sync",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	logger.Info("Customer pre-check passed, proceeding to invoice sync", "invoice_id", input.InvoiceID)

	// Step 3: Sync invoice to Paddle.
	logger.Info("Step 3: Syncing invoice to Paddle", "invoice_id", input.InvoiceID)

	if err := workflow.ExecuteActivity(ctx, ActivitySyncInvoiceToPaddle, input).Get(ctx, nil); err != nil {
		logger.Error("Failed to sync invoice to Paddle",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	logger.Info("Successfully completed Paddle invoice sync workflow",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID)

	return nil
}
