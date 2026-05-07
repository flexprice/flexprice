package invoice

import (
	"time"

	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowScheduleDraftFinalization = "ScheduleDraftFinalizationWorkflow"
	ActivityFinalizeDueDrafts         = "FinalizeDueDraftsActivity"
)

// ScheduleDraftFinalizationWorkflow is a thin wrapper that delegates to FinalizeDueDraftsActivity.
// The activity scans for due draft invoices and fires FinalizeDraftInvoiceWorkflow for each.
// Same pattern as ScheduleSubscriptionBillingWorkflow.
func ScheduleDraftFinalizationWorkflow(
	ctx workflow.Context,
	input invoiceModels.ScheduleDraftFinalizationWorkflowInput,
) (*invoiceModels.ScheduleDraftFinalizationWorkflowResult, error) {
	batchSize := input.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	logger := workflow.GetLogger(ctx)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 24 * time.Hour,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    10 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result invoiceModels.ScheduleDraftFinalizationWorkflowResult
	err := workflow.ExecuteActivity(ctx, ActivityFinalizeDueDrafts, invoiceModels.FinalizeDueDraftsActivityInput{
		BatchSize: batchSize,
	}).Get(ctx, &result)
	if err != nil {
		logger.Error("FinalizeDueDraftsActivity failed", "error", err)
		return nil, err
	}

	return &result, nil
}
