package workflows

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"
)

func BillingWorkflow(ctx workflow.Context, input BillingWorkflowInput) (*BillingWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting billing workflow",
		"customerID", input.CustomerID,
		"subscriptionID", input.SubscriptionID)

	// Set workflow timeout
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 10,
	})

	// Execute child workflow
	var calculationResult *CalculationResult
	childCtx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID: fmt.Sprintf("calculation-%s-%s", input.CustomerID, input.SubscriptionID),
	})

	err := workflow.ExecuteChildWorkflow(childCtx, DummyChildWorkflow, input).Get(ctx, &calculationResult)
	if err != nil {
		logger.Error("Failed to execute child workflow", "error", err)
		return nil, err
	}

	logger.Info("Child workflow completed",
		"totalAmount", calculationResult.TotalAmount,
		"itemCount", len(calculationResult.Items))

	// Return result
	return &BillingWorkflowResult{
		InvoiceID: calculationResult.InvoiceID,
		Status:    "completed",
	}, nil
}

func DummyChildWorkflow(ctx workflow.Context, input BillingWorkflowInput) (*CalculationResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting dummy child workflow",
		"customerID", input.CustomerID,
		"subscriptionID", input.SubscriptionID)

	// Log some random information
	logger.Info("Performing mock calculations in child workflow")

	// Just return a mock result for now
	return &CalculationResult{
		InvoiceID:   fmt.Sprintf("INV-%s-%d", input.CustomerID, time.Now().Unix()),
		TotalAmount: 100.0,
		Items: []InvoiceItem{
			{
				Description: "Mock charge",
				Amount:      100.0,
			},
		},
	}, nil
}
