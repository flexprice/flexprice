package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowPaddleInvoiceSync = "PaddleInvoiceSyncWorkflow"
	// Activity names - must match the registered method names
	ActivitySyncInvoiceToPaddle = "SyncInvoiceToPaddle"
)

// PaddleInvoiceSyncWorkflow orchestrates the Paddle invoice synchronization process
// Steps:
// 1. Sleep for 5 seconds to allow invoice to be committed to database
// 2. Sync invoice to Paddle (create transaction, sync customer, save checkout URL to metadata)
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

	// Step 1: Sleep for 5 seconds to allow invoice to be committed to database
	logger.Info("Step 1: Waiting for invoice to be committed to database",
		"invoice_id", input.InvoiceID,
		"wait_seconds", 5)

	err := workflow.Sleep(ctx, 5*time.Second)
	if err != nil {
		logger.Error("Sleep was interrupted", "error", err)
		return err
	}

	logger.Info("Wait completed, proceeding to sync invoice to Paddle", "invoice_id", input.InvoiceID)

	// Step 2: Sync invoice to Paddle
	logger.Info("Step 2: Syncing invoice to Paddle", "invoice_id", input.InvoiceID)

	err = workflow.ExecuteActivity(ctx, ActivitySyncInvoiceToPaddle, input).Get(ctx, nil)
	if err != nil {
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
