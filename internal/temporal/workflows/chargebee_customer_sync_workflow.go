package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowChargebeeCustomerSync   = "ChargebeeCustomerSyncWorkflow"
	ActivitySyncCustomerToChargebee = "SyncCustomerToChargebee"
)

func ChargebeeCustomerSyncWorkflow(ctx workflow.Context, input models.ChargebeeCustomerSyncWorkflowInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})
	return workflow.ExecuteActivity(ctx, ActivitySyncCustomerToChargebee, input).Get(ctx, nil)
}
