package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowTaskProcessing = "TaskProcessingWorkflow"
	// Activity names - must match the registered method names
	ActivityProcessTask = "ProcessTask"
)

// TaskProcessingWorkflow processes a task asynchronously
func TaskProcessingWorkflow(ctx workflow.Context, input models.TaskProcessingWorkflowInput) (*models.TaskProcessingWorkflowResult, error) {
	// Validate input
	if err := input.Validate(); err != nil {
		return nil, err
	}

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting task processing workflow", "task_id", input.TaskID)

	// Define activity options with extended timeouts for large file processing
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Hour * 4, // Extended to 4 hours for very large files
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 10,
			MaximumAttempts:    5, // Increased retry attempts
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Execute the main task processing activity
	var result models.ProcessTaskActivityResult
	activityInput := models.ProcessTaskActivityInput{
		TaskID:        input.TaskID,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
	}
	err := workflow.ExecuteActivity(ctx, ActivityProcessTask, activityInput).Get(ctx, &result)

	if err != nil {
		logger.Error("Task processing failed", "task_id", input.TaskID, "error", err)
		errorMsg := err.Error()
		return &models.TaskProcessingWorkflowResult{
			TaskID:       input.TaskID,
			Status:       "failed",
			CompletedAt:  workflow.Now(ctx),
			ErrorSummary: &errorMsg,
		}, nil
	}

	logger.Info("Task processing completed successfully",
		"task_id", input.TaskID,
		"processed_records", result.ProcessedRecords,
		"successful_records", result.SuccessfulRecords,
		"failed_records", result.FailedRecords)

	return &models.TaskProcessingWorkflowResult{
		TaskID:            input.TaskID,
		Status:            "completed",
		ProcessedRecords:  result.ProcessedRecords,
		SuccessfulRecords: result.SuccessfulRecords,
		FailedRecords:     result.FailedRecords,
		ErrorSummary:      result.ErrorSummary,
		CompletedAt:       workflow.Now(ctx),
		Metadata:          result.Metadata,
	}, nil
}
