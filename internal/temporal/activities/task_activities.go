package activities

import (
	"context"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
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

	// Start a goroutine to send heartbeats
	heartbeatCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go a.sendHeartbeats(heartbeatCtx, input.TaskID)

	// Call the service method to process the task
	// This contains all the business logic
	// For now, use regular processing - streaming can be added later if needed
	err := a.taskService.ProcessTask(ctx, input.TaskID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to process task").
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

// sendHeartbeats sends periodic heartbeats to Temporal
func (a *TaskActivities) sendHeartbeats(ctx context.Context, taskID string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Send heartbeat with task progress
			activity.RecordHeartbeat(ctx, map[string]interface{}{
				"task_id": taskID,
				"status":  "processing",
				"time":    time.Now().UTC(),
			})
		}
	}
}
