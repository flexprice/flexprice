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
	WorkflowHubSpotDealSync = "HubSpotDealSyncWorkflow"
	// Activity names - must match the registered method names
	ActivityCreateLineItems  = "CreateLineItems"
	ActivityUpdateDealAmount = "UpdateDealAmount"
)

// HubSpotDealSyncWorkflow orchestrates the HubSpot deal synchronization process
// Steps:
// 1. Create line items in HubSpot deal
// 2. Sleep for 10 seconds to allow HubSpot to recalculate ACV
// 3. Update deal amount with the calculated ACV
func HubSpotDealSyncWorkflow(ctx workflow.Context, input models.HubSpotDealSyncWorkflowInput) error {
	logger := workflow.GetLogger(ctx)

	logger.Info("Starting HubSpot deal sync workflow",
		"subscription_id", input.SubscriptionID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	// Validate input
	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return err
	}

	// Track workflow execution start
	tracking.ExecuteTrackWorkflowStart(ctx, tracking.TrackWorkflowStartInput{
		WorkflowType:  WorkflowHubSpotDealSync,
		TaskQueue:     string(types.TemporalTaskQueueTask),
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		UserID:        "", // System workflow, no specific user
		Metadata: map[string]interface{}{
			"subscription_id": input.SubscriptionID,
		},
	})

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Step 1: Create HubSpot line items
	logger.Info("Step 1: Creating line items in HubSpot", "subscription_id", input.SubscriptionID)

	err := workflow.ExecuteActivity(ctx, ActivityCreateLineItems, input).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to create line items",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return err
	}

	logger.Info("Line items created successfully", "subscription_id", input.SubscriptionID)

	// Step 2: Sleep for 10 seconds to allow HubSpot to recalculate ACV
	logger.Info("Step 2: Waiting for HubSpot to recalculate ACV",
		"subscription_id", input.SubscriptionID,
		"wait_seconds", 10)

	err = workflow.Sleep(ctx, 10*time.Second)
	if err != nil {
		logger.Error("Sleep was interrupted", "error", err)
		return err
	}

	logger.Info("Wait completed, proceeding to update deal amount", "subscription_id", input.SubscriptionID)

	// Step 3: Update deal amount with ACV
	logger.Info("Step 3: Updating deal amount with ACV", "subscription_id", input.SubscriptionID)

	err = workflow.ExecuteActivity(ctx, ActivityUpdateDealAmount, input).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to update deal amount",
			"error", err,
			"subscription_id", input.SubscriptionID)
		// Don't fail the entire workflow if deal amount update fails
		// Line items were created successfully
		logger.Warn("Continuing despite deal amount update failure")
	}

	logger.Info("Successfully completed HubSpot deal sync workflow",
		"subscription_id", input.SubscriptionID)

	return nil
}
