package workflows

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	temporalsdk "go.temporal.io/sdk/temporal"

	"go.temporal.io/sdk/workflow"
)

// CronBillingWorkflow represents a recurring billing workflow.
func CronBillingWorkflow(ctx workflow.Context, input models.BillingWorkflowInput) (*models.BillingWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting cron billing workflow", "customerID", input.CustomerID, "subscriptionID", input.SubscriptionID)

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 3,
		RetryPolicy: &temporalsdk.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	childWorkflowID := fmt.Sprintf("calculation-%s-%d", workflow.GetInfo(ctx).WorkflowExecution.RunID, workflow.Now(ctx).Unix())
	childWorkflowOptions := workflow.ChildWorkflowOptions{
		WorkflowID:         childWorkflowID,
		WorkflowRunTimeout: time.Minute * 5,
		TaskQueue:          workflow.GetInfo(ctx).TaskQueueName,
	}
	childCtx := workflow.WithChildOptions(ctx, childWorkflowOptions)

	var result models.BillingWorkflowResult
	future := workflow.ExecuteChildWorkflow(childCtx, CalculateChargesWorkflow, input)
	if err := future.Get(ctx, &result); err != nil {
		logger.Error("Child workflow failed", "error", err)
		return nil, err
	}

	return &models.BillingWorkflowResult{
		InvoiceID: result.InvoiceID,
		Status:    "completed",
	}, nil
}
