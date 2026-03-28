package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowStripeCustomerSync   = "StripeCustomerSyncWorkflow"
	ActivitySyncCustomerToStripe = "SyncCustomerToStripe"
)

func StripeCustomerSyncWorkflow(ctx workflow.Context, input models.StripeCustomerSyncWorkflowInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})
	return workflow.ExecuteActivity(ctx, ActivitySyncCustomerToStripe, input).Get(ctx, nil)
}
