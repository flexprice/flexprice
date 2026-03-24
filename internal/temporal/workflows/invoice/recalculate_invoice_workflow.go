package invoice

import (
	"time"

	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// WorkflowRecalculateInvoice is the workflow name - must match the function name
	WorkflowRecalculateInvoice = "RecalculateInvoiceWorkflow"
	// ActivityRecalculateInvoice is the activity name - must match the registered method name
	ActivityRecalculateInvoice = "RecalculateInvoiceActivity"
)

// RecalculateInvoiceWorkflow recalculates a voided subscription invoice by creating a replacement invoice via a single activity.
// Heavy work (CreateSubscriptionInvoice, etc.) runs in the activity with a long timeout to avoid blocking the API.
func RecalculateInvoiceWorkflow(
	ctx workflow.Context,
	input invoiceModels.RecalculateInvoiceWorkflowInput,
) (*invoiceModels.RecalculateInvoiceWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting recalculate invoice workflow (voided)",
		"invoice_id", input.InvoiceID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return nil, err
	}

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    2,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	activityInput := invoiceModels.RecalculateInvoiceActivityInput{
		InvoiceID:     input.InvoiceID,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		UserID:        input.UserID,
	}

	var activityOutput invoiceModels.RecalculateInvoiceActivityOutput
	err := workflow.ExecuteActivity(ctx, ActivityRecalculateInvoice, activityInput).Get(ctx, &activityOutput)
	if err != nil {
		logger.Error("Failed to recalculate invoice",
			"error", err,
			"invoice_id", input.InvoiceID)
		return nil, err
	}

	logger.Info("Successfully recalculated invoice",
		"invoice_id", input.InvoiceID)

	return &invoiceModels.RecalculateInvoiceWorkflowResult{
		Success:     activityOutput.Success,
		CompletedAt: workflow.Now(ctx),
		InvoiceID:   activityOutput.InvoiceID,
	}, nil
}
