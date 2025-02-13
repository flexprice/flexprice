package workflows

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/workflow"
)

func CalculateChargesWorkflow(ctx workflow.Context, input models.BillingWorkflowInput) (*models.CalculationResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting calculation workflow",
		"customerID", input.CustomerID,
		"subscriptionID", input.SubscriptionID)

	// Just return a mock result for now
	return &models.CalculationResult{
		InvoiceID:   fmt.Sprintf("INV-%s-%d", input.CustomerID, time.Now().Unix()),
		TotalAmount: 100.0,
		Items: []models.InvoiceItem{
			{
				Description: "Mock charge",
				Amount:      100.0,
			},
		},
	}, nil
}
