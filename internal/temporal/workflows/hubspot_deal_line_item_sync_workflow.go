package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowHubSpotDealLineItemSync = "HubSpotDealLineItemSyncWorkflow"
	// Activity names - must match the registered method names
	ActivityCreateLineItem              = "CreateLineItem"
	ActivityDeleteLineItem              = "DeleteLineItem"
	ActivityUpdateDealAmountForLineItem = "UpdateDealAmountForLineItem"
)

// HubSpotDealLineItemSyncWorkflow syncs a single subscription line item's create or
// delete to the corresponding HubSpot deal line item, then resyncs the deal's amount
// from HubSpot's recalculated ACV. A dedicated workflow type, input model, and
// activity set from the older bulk HubSpotDealSyncWorkflow — see that file's sibling
// design doc note on why they're kept fully separate rather than sharing code.
// Steps:
// 1. Create or delete the HubSpot line item, depending on input.Operation
// 2. Sleep for 10 seconds to allow HubSpot to recalculate ACV
// 3. Update deal amount with the calculated ACV
func HubSpotDealLineItemSyncWorkflow(ctx workflow.Context, input models.HubSpotDealLineItemSyncWorkflowInput) error {
	logger := workflow.GetLogger(ctx)

	logger.Info("Starting HubSpot deal line item sync workflow",
		"subscription_id", input.SubscriptionID,
		"line_item_id", input.LineItemID,
		"operation", input.Operation,
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

	activityName := ActivityCreateLineItem
	if input.Operation == models.HubSpotLineItemSyncOperationDeleted {
		activityName = ActivityDeleteLineItem
	}

	logger.Info("Step 1: syncing HubSpot line item", "line_item_id", input.LineItemID, "operation", input.Operation)

	if err := workflow.ExecuteActivity(ctx, activityName, input).Get(ctx, nil); err != nil {
		logger.Error("HubSpot line item sync failed", "error", err, "line_item_id", input.LineItemID)
		return err
	}

	logger.Info("Step 2: waiting for HubSpot to recalculate ACV", "wait_seconds", 10)

	if err := workflow.Sleep(ctx, 10*time.Second); err != nil {
		logger.Error("Sleep was interrupted", "error", err)
		return err
	}

	logger.Info("Step 3: updating deal amount with ACV", "deal_id", input.DealID)

	if err := workflow.ExecuteActivity(ctx, ActivityUpdateDealAmountForLineItem, input).Get(ctx, nil); err != nil {
		logger.Error("Failed to update deal amount", "error", err, "deal_id", input.DealID)
		// Don't fail the entire workflow if deal amount update fails — the line item
		// sync itself already succeeded.
		logger.Warn("Continuing despite deal amount update failure")
	}

	logger.Info("Successfully completed HubSpot deal line item sync workflow",
		"line_item_id", input.LineItemID)

	return nil
}
