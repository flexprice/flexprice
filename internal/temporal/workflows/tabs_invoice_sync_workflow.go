package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowTabsInvoiceSync   = "TabsInvoiceSyncWorkflow"
	ActivitySyncInvoiceToTabs = "SyncInvoiceToTabs"
)

// TabsInvoiceSyncWorkflow syncs finalized invoices to Tabs.
func TabsInvoiceSyncWorkflow(ctx workflow.Context, input models.TabsInvoiceSyncWorkflowInput) error {
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
	return workflow.ExecuteActivity(ctx, ActivitySyncInvoiceToTabs, input).Get(ctx, nil)
}
