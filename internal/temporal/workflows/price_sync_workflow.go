// internal/temporal/workflows/price_sync.go
package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	planActivities "github.com/flexprice/flexprice/internal/temporal/activities/plan"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowPriceSync = "PriceSyncWorkflow"
	// Activity name - must match the registered method name (just "SyncPlanPrices")
	ActivitySyncPlanPrices = "SyncPlanPrices"
)

func PriceSyncWorkflow(ctx workflow.Context, in models.PriceSyncWorkflowInput) (*dto.SyncPlanPricesResponse, error) {

	if err := in.Validate(); err != nil {
		return nil, err
	}

	// Create activity input with context
	activityInput := planActivities.SyncPlanPricesInput{
		PlanID:        in.PlanID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
		UserID:        in.UserID,
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Hour * 1,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)
	var out dto.SyncPlanPricesResponse
	if err := workflow.ExecuteActivity(ctx, ActivitySyncPlanPrices, activityInput).Get(ctx, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
