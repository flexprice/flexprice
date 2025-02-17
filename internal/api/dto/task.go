package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
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
		return errors.New(errors.ErrCodeValidation, "file_url cannot be empty")
	}
	if err := r.FileType.Validate(); err != nil {
		return err
	}

	validator := validator.New()
	err := validator.Struct(r)
	if err != nil {
		return errors.New(errors.ErrCodeValidation, err.Error())
	}
	return nil
}

// ToTask converts the request to a domain task
func (r *CreateTaskRequest) ToTask(ctx context.Context) *task.Task {
	return &task.Task{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TASK),
		TaskType:   r.TaskType,
		EntityType: r.EntityType,
		FileURL:    r.FileURL,
		FileName:   r.FileName,
		FileType:   r.FileType,
		TaskStatus: types.TaskStatusPending,
		Metadata:   r.Metadata,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
}

// TaskResponse represents a task in responses
type TaskResponse struct {
	ID                string                 `json:"id"`
	TaskType          types.TaskType         `json:"task_type"`
	EntityType        types.EntityType       `json:"entity_type"`
	FileURL           string                 `json:"file_url"`
	FileName          *string                `json:"file_name,omitempty"`
	FileType          types.FileType         `json:"file_type"`
	TaskStatus        types.TaskStatus       `json:"task_status"`
	TotalRecords      *int                   `json:"total_records,omitempty"`
	ProcessedRecords  int                    `json:"processed_records"`
	SuccessfulRecords int                    `json:"successful_records"`
	FailedRecords     int                    `json:"failed_records"`
	ErrorSummary      *string                `json:"error_summary,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
	StartedAt         *time.Time             `json:"started_at,omitempty"`
	CompletedAt       *time.Time             `json:"completed_at,omitempty"`
	FailedAt          *time.Time             `json:"failed_at,omitempty"`
	TenantID          string                 `json:"tenant_id"`
	Status            string                 `json:"status"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
	CreatedBy         string                 `json:"created_by,omitempty"`
	UpdatedBy         string                 `json:"updated_by,omitempty"`
}

// NewTaskResponse creates a new task response from a domain task
func NewTaskResponse(t *task.Task) *TaskResponse {
	if t == nil {
		return nil
	}

	return &TaskResponse{
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
		TenantID:          t.TenantID,
		Status:            string(t.Status),
		CreatedAt:         t.CreatedAt,
		UpdatedAt:         t.UpdatedAt,
		CreatedBy:         t.CreatedBy,
		UpdatedBy:         t.UpdatedBy,
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
		return errors.New(errors.ErrCodeValidation, "task_status cannot be empty")
	}
	return nil
}
