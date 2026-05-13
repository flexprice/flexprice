// internal/temporal/workflows/price_sync_v2_workflow.go
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
	// V2 workflow + activity names. Coexists with V1 (PriceSyncWorkflow /
	// SyncPlanPrices); the API handler picks one based on
	// PlanPriceSyncConfig.UseV2ForPlan.
	WorkflowPriceSyncV2     = "PriceSyncV2Workflow"
	ActivitySyncPlanPricesV2 = "SyncPlanPricesV2"
)

// PriceSyncV2Workflow runs the sequence-driven plan-price sync. Shape mirrors
// the V1 workflow; the only difference is which activity it dispatches.
func PriceSyncV2Workflow(ctx workflow.Context, in models.PriceSyncWorkflowInput) (*dto.SyncPlanPricesResponse, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

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
	if err := workflow.ExecuteActivity(ctx, ActivitySyncPlanPricesV2, activityInput).Get(ctx, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
