package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowPaddleSubscriptionSync      = "PaddleSubscriptionSyncWorkflow"
	ActivitySyncSubscriptionToPaddle    = "SyncSubscriptionToPaddle"
	ActivityCheckSubscriptionSyncStatus = "CheckSubscriptionSyncStatus"
)

// PaddleSubscriptionSyncWorkflow orchestrates the $0 Paddle bootstrap transaction for a
// FlexPrice subscription, enabling card capture via the checkout URL.
//
// Steps:
//  1. Sleep 2s — let subscription commit to DB.
//  2. EnsureCustomerSyncedToPaddle — create Paddle customer + address if absent.
//  3. SyncSubscriptionToPaddle — create $0 bootstrap transaction, store checkout URL in sub metadata.
func PaddleSubscriptionSyncWorkflow(ctx workflow.Context, input models.PaddleSubscriptionSyncWorkflowInput) error {
	logger := workflow.GetLogger(ctx)

	if err := input.Validate(); err != nil {
		return err
	}

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	logger.Info("PaddleSubscriptionSyncWorkflow: step 1 — waiting for subscription to commit",
		"subscription_id", input.SubscriptionID)
	if err := workflow.Sleep(ctx, 2*time.Second); err != nil {
		return err
	}

	logger.Info("PaddleSubscriptionSyncWorkflow: step 2 — ensuring customer synced",
		"subscription_id", input.SubscriptionID)
	customerInput := models.PaddleCustomerSyncWorkflowInput{
		CustomerID:    input.CustomerID,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
	}
	if err := workflow.ExecuteActivity(ctx, ActivityEnsureCustomerSyncedToPaddle, customerInput).Get(ctx, nil); err != nil {
		logger.Error("PaddleSubscriptionSyncWorkflow: customer sync failed",
			"error", err, "subscription_id", input.SubscriptionID)
		return err
	}

	logger.Info("PaddleSubscriptionSyncWorkflow: step 3 — syncing subscription to Paddle",
		"subscription_id", input.SubscriptionID)
	if err := workflow.ExecuteActivity(ctx, ActivitySyncSubscriptionToPaddle, input).Get(ctx, nil); err != nil {
		logger.Error("PaddleSubscriptionSyncWorkflow: subscription sync failed",
			"error", err, "subscription_id", input.SubscriptionID)
		return err
	}

	logger.Info("PaddleSubscriptionSyncWorkflow: completed", "subscription_id", input.SubscriptionID)
	return nil
}
