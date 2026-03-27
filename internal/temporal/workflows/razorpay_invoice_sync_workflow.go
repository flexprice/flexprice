package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowRazorpayInvoiceSync = "RazorpayInvoiceSyncWorkflow"
	// Activity names - must match the registered method names
	ActivitySyncInvoiceToRazorpay = "SyncInvoiceToRazorpay"
)

// RazorpayInvoiceSyncWorkflow orchestrates the Razorpay invoice synchronization process.
func RazorpayInvoiceSyncWorkflow(ctx workflow.Context, input models.RazorpayInvoiceSyncWorkflowInput) error {
	logger := workflow.GetLogger(ctx)

	logger.Info("Starting Razorpay invoice sync workflow",
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

	err := workflow.ExecuteActivity(ctx, ActivitySyncInvoiceToRazorpay, input).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to sync invoice to Razorpay",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	logger.Info("Successfully completed Razorpay invoice sync workflow",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID)

	return nil
}
