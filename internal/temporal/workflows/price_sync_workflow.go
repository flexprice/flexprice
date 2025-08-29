package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/temporal/models"
	temporalsdk "go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// PriceSyncWorkflow represents a workflow that syncs plan prices
func PriceSyncWorkflow(ctx workflow.Context, input models.PriceSyncWorkflowInput) (*dto.SyncPlanPricesResponse, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting price sync workflow", "planID", input.PlanID)

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 5,
		RetryPolicy: &temporalsdk.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Execute the sync activity using the correct activity name
	var result dto.SyncPlanPricesResponse
	if err := workflow.ExecuteActivity(ctx, "SyncPlanPrices", input.PlanID).Get(ctx, &result); err != nil {
		logger.Error("Price sync failed", "planID", input.PlanID, "error", err)
		return nil, err
	}

	return &result, nil
}
