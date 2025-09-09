package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateTaskRequest represents the request to create a new task
type CreateTaskRequest struct {
	TaskType   types.TaskType         `json:"task_type" binding:"required"`
	EntityType types.EntityType       `json:"entity_type" binding:"required"`
	FileURL    string                 `json:"file_url" binding:"required"`
	FileName   *string                `json:"file_name,omitempty"`
	FileType   types.FileType         `json:"file_type" binding:"required"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

func (r *CreateTaskRequest) Validate() error {
	if err := r.TaskType.Validate(); err != nil {
		return err
	}
	if err := r.EntityType.Validate(); err != nil {
		return err
	}
	if r.FileURL == "" {
		return ierr.NewError("file_url cannot be empty").
			WithHint("File URL cannot be empty").
			Mark(ierr.ErrValidation)
	}
	if err := r.FileType.Validate(); err != nil {
		return err
	}

	return validator.ValidateRequest(r)
}

// ToTask converts the request to a domain task
func (r *CreateTaskRequest) ToTask(ctx context.Context) *task.Task {
	return &task.Task{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TASK),
		TaskType:      r.TaskType,
		EntityType:    r.EntityType,
		FileURL:       r.FileURL,
		FileName:      r.FileName,
		FileType:      r.FileType,
		TaskStatus:    types.TaskStatusPending,
		Metadata:      r.Metadata,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
}

// TaskResponse represents a task in responses
type TaskResponse struct {
	task.Task
}

// NewTaskResponse creates a new task response from a domain task
func NewTaskResponse(t *task.Task) *TaskResponse {
	if t == nil {
		return nil
	}

	return &TaskResponse{
		Task: task.Task{
			ID:                t.ID,
			TaskType:          t.TaskType,
			EntityType:        t.EntityType,
			FileURL:           t.FileURL,
			FileName:          t.FileName,
			FileType:          t.FileType,
			TaskStatus:        t.TaskStatus,
			TotalRecords:      t.TotalRecords,
			ProcessedRecords:  t.ProcessedRecords,
			SuccessfulRecords: t.SuccessfulRecords,
			FailedRecords:     t.FailedRecords,
			ErrorSummary:      t.ErrorSummary,
			Metadata:          t.Metadata,
			StartedAt:         t.StartedAt,
			CompletedAt:       t.CompletedAt,
			FailedAt:          t.FailedAt,
			BaseModel:         t.BaseModel,
		},
	}
}

// ListTasksResponse represents the response for listing tasks
type ListTasksResponse struct {
	Items      []*TaskResponse          `json:"items"`
	Pagination types.PaginationResponse `json:"pagination"`
}

// UpdateTaskStatusRequest represents a request to update a task's status
type UpdateTaskStatusRequest struct {
	TaskStatus types.TaskStatus `json:"task_status" binding:"required"`
}

func (r *UpdateTaskStatusRequest) Validate() error {
	if r.TaskStatus == "" {
		return ierr.NewError("task_status cannot be empty").
			WithHint("Task status cannot be empty").
			Mark(ierr.ErrValidation)
	}
	return nil
}
