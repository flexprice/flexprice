package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowBillingEntityEnable          = "BillingEntityEnableWorkflow"
	ActivityPrepareBillingEntityEnable   = "PrepareBillingEntityEnable"
	ActivityAssignBillingEntityCustomers = "AssignBillingEntityCustomers"
	ActivityCompleteBillingEntityEnable  = "CompleteBillingEntityEnable"
)

// BillingEntityEnableWorkflow creates the default entities, assigns existing customers to them in
// batches and moves the tenant to enabled. Every step is safe to rerun.
func BillingEntityEnableWorkflow(ctx workflow.Context, in models.BillingEntityEnableWorkflowInput) error {
	if err := in.Validate(); err != nil {
		return err
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

	if err := workflow.ExecuteActivity(ctx, ActivityPrepareBillingEntityEnable, in).Get(ctx, nil); err != nil {
		return err
	}
	if err := workflow.ExecuteActivity(ctx, ActivityAssignBillingEntityCustomers, in).Get(ctx, nil); err != nil {
		return err
	}
	return workflow.ExecuteActivity(ctx, ActivityCompleteBillingEntityEnable, in).Get(ctx, nil)
}
