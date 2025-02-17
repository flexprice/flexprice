package workflows

import (
	"fmt"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/workflow"
)

func CalculateChargesWorkflow(ctx workflow.Context, input models.BillingWorkflowInput) (*models.CalculationResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting calculation workflow",
		"customerID", input.CustomerID,
		"subscriptionID", input.SubscriptionID)

	return &models.CalculationResult{
		InvoiceID:   fmt.Sprintf("INV-%s-%d", input.CustomerID, workflow.Now(ctx).Unix()),
		TotalAmount: 100.0,
		Items: []models.InvoiceItem{
			{
				Description: "Mock charge",
				Amount:      100.0,
			},
		},
	}, nil
}
