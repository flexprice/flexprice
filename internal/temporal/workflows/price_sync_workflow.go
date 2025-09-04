// internal/temporal/workflows/price_sync.go
package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Use the full method name if you register the whole struct:
	ActivitySyncPlanPrices = "PlanActivities.SyncPlanPrices"
)

func PriceSyncWorkflow(ctx workflow.Context, in models.PriceSyncWorkflowInput) (*dto.SyncPlanPricesResponse, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting price sync workflow", "planID", in.PlanID)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var out dto.SyncPlanPricesResponse
	if err := workflow.ExecuteActivity(ctx, ActivitySyncPlanPrices, in.PlanID).Get(ctx, &out); err != nil {
		log.Error("Price sync failed", "planID", in.PlanID, "error", err)
		return nil, err
	}
	return &out, nil
}
