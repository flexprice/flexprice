package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// WorkflowPaddleInvoicePullSync is the Temporal workflow type name for Paddle invoice pull sync.
	WorkflowPaddleInvoicePullSync = "PaddleInvoicePullSyncWorkflow"
	// ActivityPullAndUpdatePaddleInvoice is the activity type name.
	ActivityPullAndUpdatePaddleInvoice = "PullAndUpdatePaddleInvoice"
)

// PaddleInvoicePullSyncWorkflow polls Paddle for the payment status of a finalized+unpaid invoice
// and updates FlexPrice if the transaction has completed.
func PaddleInvoicePullSyncWorkflow(ctx workflow.Context, input models.PaddleInvoicePullSyncWorkflowInput) error {
	logger := workflow.GetLogger(ctx)

	logger.Info("Starting Paddle invoice pull sync workflow",
		"invoice_id", input.InvoiceID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return err
	}

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})

	if err := workflow.ExecuteActivity(ctx, ActivityPullAndUpdatePaddleInvoice, input).Get(ctx, nil); err != nil {
		logger.Error("Failed to pull and update Paddle invoice",
			"error", err,
			"invoice_id", input.InvoiceID)
		return err
	}

	logger.Info("Successfully completed Paddle invoice pull sync workflow",
		"invoice_id", input.InvoiceID)

	return nil
}
