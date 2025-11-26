package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowHubSpotQuoteSync = "HubSpotQuoteSyncWorkflow"
	// Activity name - must match the registered method name
	ActivityCreateQuoteAndLineItems = "CreateQuoteAndLineItems"
)

// HubSpotQuoteSyncWorkflow orchestrates the HubSpot quote synchronization process
// Steps:
// 1. Create quote in HubSpot and attach line items
func HubSpotQuoteSyncWorkflow(ctx workflow.Context, input models.HubSpotQuoteSyncWorkflowInput) error {
	logger := workflow.GetLogger(ctx)

	logger.Info("Starting HubSpot quote sync workflow",
		"subscription_id", input.SubscriptionID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	// Validate input
	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return err
	}

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Create quote and line items in HubSpot
	logger.Info("Creating quote and line items in HubSpot", "subscription_id", input.SubscriptionID)

	err := workflow.ExecuteActivity(ctx, ActivityCreateQuoteAndLineItems, input).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to create quote and line items",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return err
	}

	logger.Info("Successfully completed HubSpot quote sync workflow",
		"subscription_id", input.SubscriptionID)

	return nil
}

