package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowZohoBooksInvoiceSync = "ZohoBooksInvoiceSyncWorkflow"
	ActivitySyncInvoiceToZoho    = "SyncInvoiceToZoho"
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
