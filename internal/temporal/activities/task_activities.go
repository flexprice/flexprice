package activities

import (
	"context"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

const TaskActivityPrefix = "TaskActivities"

// TaskActivities contains all task-related activities
type TaskActivities struct {
	taskService service.TaskService
}

// NewTaskActivities creates a new TaskActivities instance
func NewTaskActivities(taskService service.TaskService) *TaskActivities {
	return &TaskActivities{
		taskService: taskService,
	}
}

// ProcessTask processes a task asynchronously
func (a *TaskActivities) ProcessTask(ctx context.Context, input models.ProcessTaskActivityInput) (*models.ProcessTaskActivityResult, error) {
	// Validate input
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values using centralized utilities
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	// Call the service method to process the task
	// This contains all the business logic
	// Use streaming processing for better performance with large files
	err := a.taskService.ProcessTaskWithStreaming(ctx, input.TaskID)
	if err != nil {
		// Check if it's a timeout error and provide better context
		if isTimeoutError(err) {
			return nil, ierr.WithError(err).
				WithHint("Task processing timed out. This may be due to a large file or network issues. Consider using streaming processing for very large files.").
				WithReportableDetails(map[string]interface{}{
					"task_id":    input.TaskID,
					"error_type": "timeout",
				}).
				Mark(ierr.ErrHTTPClient)
		}

		return nil, ierr.WithError(err).
			WithHint("Failed to process task").
			WithReportableDetails(map[string]interface{}{
				"task_id": input.TaskID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get the updated task to return results
	task, err := a.taskService.GetTask(ctx, input.TaskID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get updated task").
			Mark(ierr.ErrValidation)
	}

	return &models.ProcessTaskActivityResult{
		TaskID:            task.ID,
		ProcessedRecords:  task.ProcessedRecords,
		SuccessfulRecords: task.SuccessfulRecords,
		FailedRecords:     task.FailedRecords,
		ErrorSummary:      task.ErrorSummary,
		Metadata:          task.Metadata,
	}, nil
}

// isTimeoutError checks if the error is related to timeouts
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "deadline exceeded") ||
		strings.Contains(errStr, "context deadline exceeded")
}
