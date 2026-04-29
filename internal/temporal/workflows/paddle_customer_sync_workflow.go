package workflows

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowPaddleCustomerSync   = "PaddleCustomerSyncWorkflow"
	ActivitySyncCustomerToPaddle = "SyncCustomerToPaddle"
)

func PaddleCustomerSyncWorkflow(ctx workflow.Context, input models.PaddleCustomerSyncWorkflowInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	if input.CustomerID == "" {
		return ierr.NewError("customer_id is required for Paddle customer sync workflow").
			WithHint("This workflow syncs one customer id; InvoiceID fallback is only for Paddle invoice sync").
			Mark(ierr.ErrValidation)
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
	return workflow.ExecuteActivity(ctx, ActivitySyncCustomerToPaddle, input).Get(ctx, nil)
}
