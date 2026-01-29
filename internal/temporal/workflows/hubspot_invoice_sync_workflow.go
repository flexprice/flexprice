package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/temporal/tracking"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowHubSpotInvoiceSync = "HubSpotInvoiceSyncWorkflow"
	// Activity names - must match the registered method names
	ActivitySyncInvoiceToHubSpot = "SyncInvoiceToHubSpot"
)

// HubSpotInvoiceSyncWorkflow orchestrates the HubSpot invoice synchronization process
// Steps:
// 1. Sleep for 5 seconds to allow invoice to be committed to database
// 2. Sync invoice to HubSpot (create invoice, line items, associate to contact, update properties, set status)
func HubSpotInvoiceSyncWorkflow(ctx workflow.Context, input models.HubSpotInvoiceSyncWorkflowInput) error {
	logger := workflow.GetLogger(ctx)

	logger.Info("Starting HubSpot invoice sync workflow",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return err
	}

	// Track workflow execution start
	tracking.ExecuteTrackWorkflowStart(ctx, tracking.TrackWorkflowStartInput{
		WorkflowType:  WorkflowHubSpotInvoiceSync,
		TaskQueue:     string(types.TemporalTaskQueueTask),
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		UserID:        "", // System workflow, no specific user
		Metadata: map[string]interface{}{
			"invoice_id":  input.InvoiceID,
			"customer_id": input.CustomerID,
		},
	})

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

	// Step 2: Sync invoice to HubSpot
	logger.Info("Step 2: Syncing invoice to HubSpot", "invoice_id", input.InvoiceID)

	err = workflow.ExecuteActivity(ctx, ActivitySyncInvoiceToHubSpot, input).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to sync invoice to HubSpot",
			"error", err,
			"invoice_id", input.InvoiceID)
		// Log error but don't fail the workflow - invoice sync is not critical
		logger.Warn("Continuing despite invoice sync failure")
		return nil // Return nil to not fail the workflow
	}

	logger.Info("Successfully completed HubSpot invoice sync workflow",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID)

	return nil
}
