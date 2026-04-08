// internal/temporal/workflows/price_sync.go
package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	addonActivities "github.com/flexprice/flexprice/internal/temporal/activities/addon"
	planActivities "github.com/flexprice/flexprice/internal/temporal/activities/plan"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// WorkflowPriceSync is the Temporal workflow name.
	WorkflowPriceSync = "PriceSyncWorkflow"
	// ActivitySyncPlanPrices is the registered plan sync activity name.
	ActivitySyncPlanPrices = "SyncPlanPrices"
)

func PriceSyncWorkflow(ctx workflow.Context, in models.PriceSyncWorkflowInput) (*dto.SyncPlanPricesResponse, error) {
	if err := in.Validate(); err != nil {
		return nil, err
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

	switch in.EntityType {
	case types.PRICE_ENTITY_TYPE_PLAN:
		activityInput := planActivities.SyncPlanPricesInput{
			PlanID:        in.EntityID,
			TenantID:      in.TenantID,
			EnvironmentID: in.EnvironmentID,
			UserID:        in.UserID,
		}
		var out dto.SyncPlanPricesResponse
		if err := workflow.ExecuteActivity(ctx, ActivitySyncPlanPrices, activityInput).Get(ctx, &out); err != nil {
			return nil, err
		}
		return &out, nil

	case types.PRICE_ENTITY_TYPE_ADDON:
		activityInput := addonActivities.SyncAddonPricesInput{
			AddonID:       in.EntityID,
			EntityType:    in.EntityType,
			TenantID:      in.TenantID,
			EnvironmentID: in.EnvironmentID,
			UserID:        in.UserID,
		}
		var addonOut dto.SyncAddonPricesResponse
		if err := workflow.ExecuteActivity(ctx, addonActivities.ActivitySyncAddonPrices, activityInput).Get(ctx, &addonOut); err != nil {
			return nil, err
		}
		// Map addon response onto SyncPlanPricesResponse for uniform workflow return type
		return &dto.SyncPlanPricesResponse{
			PlanID:  addonOut.AddonID,
			Message: addonOut.Message,
			Summary: dto.SyncPlanPricesSummary{
				LineItemsFoundForCreation: addonOut.Summary.LineItemsFoundForCreation,
				LineItemsCreated:          addonOut.Summary.LineItemsCreated,
				LineItemsTerminated:       addonOut.Summary.LineItemsTerminated,
			},
		}, nil

	default:
		return nil, ierr.NewError("unsupported entity type for price sync").
			WithHintf("entity_type must be %s or %s, got %s",
				types.PRICE_ENTITY_TYPE_PLAN, types.PRICE_ENTITY_TYPE_ADDON, in.EntityType).
			Mark(ierr.ErrValidation)
	}
}
