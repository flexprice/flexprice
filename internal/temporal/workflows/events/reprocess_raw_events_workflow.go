package events

import (
	"time"

	models "github.com/flexprice/flexprice/internal/temporal/models/events"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowReprocessRawEvents = "ReprocessRawEventsWorkflow"
	// Activity names - must match the registered method names
	ActivityReprocessRawEvents = "ReprocessRawEvents"
)

// ReprocessRawEventsWorkflow reprocesses raw events
func ReprocessRawEventsWorkflow(ctx workflow.Context, input models.ReprocessRawEventsWorkflowInput) (*models.ReprocessRawEventsWorkflowResult, error) {
	// Validate input
	if err := input.Validate(); err != nil {
		return nil, err
	}

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting reprocess raw events workflow",
		"external_customer_ids", input.ExternalCustomerIDs,
		"event_names", input.EventNames,
		"start_date", input.StartDate,
		"end_date", input.EndDate)

	// Define activity options with 1 hour timeout as requested
	// Activity timeout set to 55 minutes to leave buffer for workflow
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 55,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 10,
			MaximumAttempts:    3, // 3 retry attempts as requested
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Execute the reprocess raw events activity
	var result models.ReprocessRawEventsWorkflowResult
	err := workflow.ExecuteActivity(ctx, ActivityReprocessRawEvents, input).Get(ctx, &result)

	if err != nil {
		logger.Error("Reprocess raw events workflow failed",
			"external_customer_ids", input.ExternalCustomerIDs,
			"event_names", input.EventNames,
			"error", err)
		return &models.ReprocessRawEventsWorkflowResult{
			TotalEventsFound:          0,
			TotalEventsPublished:      0,
			TotalEventsFailed:         0,
			TotalEventsDropped:        0,
			TotalTransformationErrors: 0,
			ProcessedBatches:          0,
			CompletedAt:               workflow.Now(ctx),
		}, err
	}

	logger.Info("Reprocess raw events workflow completed successfully",
		"external_customer_ids", input.ExternalCustomerIDs,
		"event_names", input.EventNames,
		"total_events_found", result.TotalEventsFound,
		"total_events_published", result.TotalEventsPublished,
		"total_events_dropped", result.TotalEventsDropped,
		"total_transformation_errors", result.TotalTransformationErrors,
		"total_events_failed", result.TotalEventsFailed,
		"processed_batches", result.ProcessedBatches)

	result.CompletedAt = workflow.Now(ctx)
	return &result, nil
}
