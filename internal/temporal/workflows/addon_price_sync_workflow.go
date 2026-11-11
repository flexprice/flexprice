package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	addonActivities "github.com/flexprice/flexprice/internal/temporal/activities/addon"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowAddonPriceSync  = "AddonPriceSyncWorkflow"
	ActivitySyncAddonPrices = "SyncAddonPrices"
)

// AddonPriceSyncWorkflow runs the addon usage-price sync. Shape mirrors the plan
// PriceSyncV2Workflow; it dispatches the SyncAddonPrices activity.
func AddonPriceSyncWorkflow(ctx workflow.Context, in models.AddonPriceSyncWorkflowInput) (*dto.SyncAddonPricesResponse, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	activityInput := addonActivities.SyncAddonPricesInput{
		AddonID:       in.AddonID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
		UserID:        in.UserID,
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Hour * 6,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var out dto.SyncAddonPricesResponse
	if err := workflow.ExecuteActivity(ctx, ActivitySyncAddonPrices, activityInput).Get(ctx, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
