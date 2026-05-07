package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowRazorpayCustomerSync   = "RazorpayCustomerSyncWorkflow"
	ActivitySyncCustomerToRazorpay = "SyncCustomerToRazorpay"
)

func RazorpayCustomerSyncWorkflow(ctx workflow.Context, input models.RazorpayCustomerSyncWorkflowInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})
	if err := workflow.Sleep(ctx, 2*time.Second); err != nil {
		return err
	}
	return workflow.ExecuteActivity(ctx, ActivitySyncCustomerToRazorpay, input).Get(ctx, nil)
}
