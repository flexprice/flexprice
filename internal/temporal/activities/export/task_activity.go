package export

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// TaskActivity handles task-related operations
type TaskActivity struct {
	taskRepo task.Repository
	logger   *logger.Logger
}

// NewTaskActivity creates a new task activity
func NewTaskActivity(taskRepo task.Repository, logger *logger.Logger) *TaskActivity {
	return &TaskActivity{
		taskRepo: taskRepo,
		logger:   logger,
	}
}

// CreateTaskInput represents input for creating a task
type CreateTaskInput struct {
	TaskID          string // Pre-generated task ID
	WorkflowID      string // Temporal workflow ID
	ScheduledTaskID string
	TenantID        string
	EnvID           string
	UserID          string // User who triggered the export (empty for scheduled runs)
	EntityType      types.ScheduledTaskEntityType
	StartTime       time.Time
	EndTime         time.Time
}

// CreateTaskOutput represents output from creating a task
type CreateTaskOutput struct {
	TaskID string
}

// CreateTask creates a new export task
func (a *TaskActivity) CreateTask(ctx context.Context, input CreateTaskInput) (*CreateTaskOutput, error) {
	a.logger.Infow("creating export task",
		"task_id", input.TaskID,
		"workflow_id", input.WorkflowID,
		"scheduled_task_id", input.ScheduledTaskID,
		"entity_type", input.EntityType,
		"start_time", input.StartTime,
		"end_time", input.EndTime)

	// Set user ID in context for proper BaseModel creation
	// For scheduled runs, use system user ID; for manual runs, use the provided user ID
	userID := input.UserID
	if userID == "" {
		userID = types.DefaultUserID // Use system user ID for scheduled runs
	}
	ctx = types.SetUserID(ctx, userID)
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvID)

	// Create task with pre-generated ID
	now := time.Now()
	baseModel := types.GetDefaultBaseModel(ctx)

	a.logger.Infow("creating task with base model",
		"task_id", input.TaskID,
		"created_by", baseModel.CreatedBy,
		"updated_by", baseModel.UpdatedBy,
		"tenant_id", baseModel.TenantID,
		"user_id_from_input", input.UserID)

	newTask := &task.Task{
		ID:              input.TaskID,      // Use pre-generated ID
		WorkflowID:      &input.WorkflowID, // Store Temporal workflow ID
		EnvironmentID:   input.EnvID,       // Set environment ID
		TaskType:        types.TaskTypeExport,
		EntityType:      types.EntityType(input.EntityType),
		ScheduledTaskID: input.ScheduledTaskID, // Always store scheduled_task_id
		FileURL:         "",                    // Will be set after upload
		FileType:        types.FileTypeCSV,
		TaskStatus:      types.TaskStatusPending,
		Metadata: map[string]interface{}{
			"start_time": input.StartTime.Format(time.RFC3339),
			"end_time":   input.EndTime.Format(time.RFC3339),
		},
		StartedAt: &now,
		BaseModel: baseModel, // This will properly set CreatedBy and UpdatedBy, and TenantID and EnvironmentID
	}

	err := a.taskRepo.Create(ctx, newTask)
	if err != nil {
		a.logger.Errorw("failed to create task", "error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to create export task").
			Mark(ierr.ErrDatabase)
	}

	a.logger.Infow("export task created", "task_id", newTask.ID)

	return &CreateTaskOutput{
		TaskID: newTask.ID,
	}, nil
}

// UpdateTaskStatusInput represents input for updating task status
type UpdateTaskStatusInput struct {
	TaskID     string
	TenantID   string
	EnvID      string
	UserID     string
	Status     types.TaskStatus
	RecordInfo *RecordInfo // Optional: for tracking progress
	Error      string      // Optional: for failures
}

// RecordInfo holds record count information
type RecordInfo struct {
	TotalRecords      int
	ProcessedRecords  int
	SuccessfulRecords int
	FailedRecords     int
}

// UpdateTaskStatus updates the status of a task
func (a *TaskActivity) UpdateTaskStatus(ctx context.Context, input UpdateTaskStatusInput) error {
	a.logger.Infow("updating task status",
		"task_id", input.TaskID,
		"status", input.Status)

	// Set tenant and env context for repository queries
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvID)

	existingTask, err := a.taskRepo.Get(ctx, input.TaskID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get task").
			Mark(ierr.ErrDatabase)
	}

	// Update status
	existingTask.TaskStatus = input.Status

	// Update record counts if provided
	if input.RecordInfo != nil {
		existingTask.TotalRecords = &input.RecordInfo.TotalRecords
		existingTask.ProcessedRecords = input.RecordInfo.ProcessedRecords
		existingTask.SuccessfulRecords = input.RecordInfo.SuccessfulRecords
		existingTask.FailedRecords = input.RecordInfo.FailedRecords
	}

	// Set user ID in context for proper update tracking
	// Use the UserID from input, fallback to context if not provided
	userID := input.UserID
	if userID == "" {
		userID = types.GetUserID(ctx)
	}
	if userID == "" {
		userID = types.DefaultUserID // Ensure we have a valid user ID
	}
	ctx = types.SetUserID(ctx, userID)

	// Update timestamps based on status
	now := time.Now()
	existingTask.UpdatedAt = now
	existingTask.UpdatedBy = userID

	switch input.Status {
	case types.TaskStatusProcessing:
		if existingTask.StartedAt == nil {
			existingTask.StartedAt = &now
		}
	case types.TaskStatusCompleted:
		existingTask.CompletedAt = &now
	case types.TaskStatusFailed:
		existingTask.FailedAt = &now
		if input.Error != "" {
			existingTask.ErrorSummary = &input.Error
		}
	}

	err = a.taskRepo.Update(ctx, existingTask)
	if err != nil {
		a.logger.Errorw("failed to update task status", "error", err)
		return ierr.WithError(err).
			WithHint("Failed to update task status").
			Mark(ierr.ErrDatabase)
	}

	a.logger.Infow("task status updated", "task_id", input.TaskID, "status", input.Status)
	return nil
}

// CompleteTaskInput represents input for completing a task
type CompleteTaskInput struct {
	TaskID      string
	TenantID    string
	EnvID       string
	UserID      string
	FileURL     string
	RecordCount int
	FileSize    int64
}

// CompleteTask marks a task as completed with final details
func (a *TaskActivity) CompleteTask(ctx context.Context, input CompleteTaskInput) error {
	a.logger.Infow("completing task",
		"task_id", input.TaskID,
		"record_count", input.RecordCount)

	// Set tenant and env context for repository queries
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvID)

	existingTask, err := a.taskRepo.Get(ctx, input.TaskID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get task").
			Mark(ierr.ErrDatabase)
	}

	// Update task with completion details
	existingTask.TaskStatus = types.TaskStatusCompleted
	existingTask.FileURL = input.FileURL
	existingTask.TotalRecords = &input.RecordCount
	existingTask.SuccessfulRecords = input.RecordCount
	existingTask.ProcessedRecords = input.RecordCount

	// Set user ID in context for proper update tracking
	// Use the UserID from input, fallback to context if not provided
	userID := input.UserID
	if userID == "" {
		userID = types.GetUserID(ctx)
	}
	if userID == "" {
		userID = types.DefaultUserID // Ensure we have a valid user ID
	}
	ctx = types.SetUserID(ctx, userID)

	now := time.Now()
	existingTask.CompletedAt = &now
	existingTask.UpdatedAt = now
	existingTask.UpdatedBy = userID

	// Add file details to metadata
	if existingTask.Metadata == nil {
		existingTask.Metadata = make(map[string]interface{})
	}
	existingTask.Metadata["file_size_bytes"] = input.FileSize
	existingTask.Metadata["completed_at"] = now.Format(time.RFC3339)

	err = a.taskRepo.Update(ctx, existingTask)
	if err != nil {
		a.logger.Errorw("failed to complete task", "error", err)
		return ierr.WithError(err).
			WithHint("Failed to complete task").
			Mark(ierr.ErrDatabase)
	}

	a.logger.Infow("task completed successfully", "task_id", input.TaskID)
	return nil
}
