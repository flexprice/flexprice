package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowNomodCustomerSync   = "NomodCustomerSyncWorkflow"
	ActivitySyncCustomerToNomod = "SyncCustomerToNomod"
)

func NomodCustomerSyncWorkflow(ctx workflow.Context, input models.NomodCustomerSyncWorkflowInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})
	return workflow.ExecuteActivity(ctx, ActivitySyncCustomerToNomod, input).Get(ctx, nil)
}
