package workflows

import (
	"time"

	qbActivities "github.com/flexprice/flexprice/internal/temporal/activities/quickbooks"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowQuickBooksPriceSync = "QuickBooksPriceSyncWorkflow"
	// Activity name - must match the registered method name
	ActivitySyncPriceToQuickBooks = "SyncPriceToQuickBooks"
)

func QuickBooksPriceSyncWorkflow(ctx workflow.Context, in models.QuickBooksPriceSyncWorkflowInput) (*qbActivities.SyncPriceToQuickBooksOutput, error) {

	if err := in.Validate(); err != nil {
		return nil, err
	}

	// Create activity input with context
	activityInput := qbActivities.SyncPriceToQuickBooksInput{
		PriceID:       in.PriceID,
		PlanID:        in.PlanID,
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
		UserID:        in.UserID,
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 10,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 2,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var out qbActivities.SyncPriceToQuickBooksOutput
	if err := workflow.ExecuteActivity(ctx, ActivitySyncPriceToQuickBooks, activityInput).Get(ctx, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
