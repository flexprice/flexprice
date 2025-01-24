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
		"executionTime", workflow.Now(ctx))

	// Set workflow timeout and retry policy
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 3,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	})

	// Execute a child workflow
	var calculationResult CalculationResult
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID: fmt.Sprintf("calculation-5min-%d", workflow.Now(ctx).Unix()),
	})

	err := workflow.ExecuteChildWorkflow(childCtx, CalculateChargesWorkflow, input).Get(ctx, &calculationResult)
	if err != nil {
		logger.Error("Failed to execute calculation workflow", "error", err)
		return nil, err
	}

	logger.Info("5-minute billing calculation completed",
		"totalAmount", calculationResult.TotalAmount,
		"itemCount", len(calculationResult.Items))

	return &BillingWorkflowResult{
		InvoiceID: calculationResult.InvoiceID,
		Status:    "completed",
	}, nil
}
