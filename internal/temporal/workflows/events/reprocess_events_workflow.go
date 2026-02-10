package events

import (
	"time"

	models "github.com/flexprice/flexprice/internal/temporal/models/events"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowReprocessEvents = "ReprocessEventsWorkflow"
	// Activity names - must match the registered method names
	ActivityReprocessEvents = "ReprocessEvents"
)

// ReprocessEventsWorkflow reprocesses events for feature usage tracking
func ReprocessEventsWorkflow(ctx workflow.Context, input models.ReprocessEventsWorkflowInput) (*models.ReprocessEventsWorkflowResult, error) {
	// Validate input
	if err := input.Validate(); err != nil {
		return nil, err
	}

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting reprocess events workflow",
		"external_customer_id", input.ExternalCustomerID,
		"event_name", input.EventName,
		"start_date", input.StartDate,
		"end_date", input.EndDate)

	// Define activity options with extended timeouts for large batch processing
	// Activity timeout set to 10 hours to allow for long-running reprocessing
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Hour * 10,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 10,
			MaximumAttempts:    3, // Moderate retry attempts
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Execute the reprocess events activity
	var result models.ReprocessEventsWorkflowResult
	err := workflow.ExecuteActivity(ctx, ActivityReprocessEvents, input).Get(ctx, &result)

	if err != nil {
		logger.Error("Reprocess events workflow failed",
			"external_customer_id", input.ExternalCustomerID,
			"event_name", input.EventName,
			"error", err)
		return &models.ReprocessEventsWorkflowResult{
			TotalEventsFound:     0,
			TotalEventsPublished: 0,
			ProcessedBatches:     0,
			CompletedAt:          workflow.Now(ctx),
		}, err
	}

	logger.Info("Reprocess events workflow completed successfully",
		"external_customer_id", input.ExternalCustomerID,
		"event_name", input.EventName,
		"total_events_found", result.TotalEventsFound,
		"total_events_published", result.TotalEventsPublished,
		"processed_batches", result.ProcessedBatches)

	result.CompletedAt = workflow.Now(ctx)
	return &result, nil
}
