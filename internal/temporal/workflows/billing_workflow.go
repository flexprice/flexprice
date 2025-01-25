package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// CronBillingWorkflow runs on a schedule and calculates billing
func CronBillingWorkflow(ctx workflow.Context, input BillingWorkflowInput) (*BillingWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting cron billing workflow",
		"customerID", input.CustomerID,
		"subscriptionID", input.SubscriptionID,
		"executionTime", workflow.Now(ctx))

	// Set workflow timeout and retry policy
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 3,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Create unique workflow ID
	info := workflow.GetInfo(ctx)
	childWorkflowID := fmt.Sprintf("calculation-%s-%d", info.WorkflowExecution.RunID, workflow.Now(ctx).Unix())

	// Set child workflow options with proper syntax
	childWorkflowOptions := workflow.ChildWorkflowOptions{
		WorkflowID:         childWorkflowID,
		WorkflowRunTimeout: time.Minute * 5,
		TaskQueue:          "billing-task-queue",
	}
	childCtx := workflow.WithChildOptions(ctx, childWorkflowOptions)

	// Execute child workflow
	var calculationResult CalculationResult
	childWorkflowFuture := workflow.ExecuteChildWorkflow(childCtx, CalculateChargesWorkflow, input)

	// Wait for child workflow to complete
	if err := childWorkflowFuture.Get(ctx, &calculationResult); err != nil {
		logger.Error("Failed to execute calculation workflow", "error", err)
		return &BillingWorkflowResult{
			InvoiceID: "",
			Status:    "failed",
		}, err
	}

	logger.Info("Billing calculation completed",
		"invoiceID", calculationResult.InvoiceID,
		"totalAmount", calculationResult.TotalAmount)

	return &BillingWorkflowResult{
		InvoiceID: calculationResult.InvoiceID,
		Status:    "completed",
	}, nil
}
