package models

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// TaskProcessingWorkflowInput represents the input for task processing workflow
type TaskProcessingWorkflowInput struct {
	TaskID        string `json:"task_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

// Validate validates the task processing workflow input
func (i *TaskProcessingWorkflowInput) Validate() error {
	if i.TaskID == "" {
		return ierr.NewError("task_id is required").
			WithHint("Task ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// TaskProcessingWorkflowResult represents the result of task processing workflow
type TaskProcessingWorkflowResult struct {
	TaskID            string                 `json:"task_id"`
	Status            string                 `json:"status"`
	ProcessedRecords  int                    `json:"processed_records"`
	SuccessfulRecords int                    `json:"successful_records"`
	FailedRecords     int                    `json:"failed_records"`
	ErrorSummary      *string                `json:"error_summary,omitempty"`
	CompletedAt       time.Time              `json:"completed_at"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

// ProcessTaskActivityInput represents the input for process task activity
type ProcessTaskActivityInput struct {
	TaskID        string `json:"task_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
}

// Validate validates the process task activity input
func (i *ProcessTaskActivityInput) Validate() error {
	if i.TaskID == "" {
		return ierr.NewError("task_id is required").
			WithHint("Task ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ProcessTaskActivityResult represents the result of process task activity
type ProcessTaskActivityResult struct {
	TaskID            string                 `json:"task_id"`
	ProcessedRecords  int                    `json:"processed_records"`
	SuccessfulRecords int                    `json:"successful_records"`
	FailedRecords     int                    `json:"failed_records"`
	ErrorSummary      *string                `json:"error_summary,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateTaskProgressActivityInput represents the input for update task progress activity
type UpdateTaskProgressActivityInput struct {
	TaskID            string  `json:"task_id"`
	ProcessedRecords  int     `json:"processed_records"`
	SuccessfulRecords int     `json:"successful_records"`
	FailedRecords     int     `json:"failed_records"`
	ErrorSummary      *string `json:"error_summary,omitempty"`
}

// Validate validates the update task progress activity input
func (i *UpdateTaskProgressActivityInput) Validate() error {
	if i.TaskID == "" {
		return ierr.NewError("task_id is required").
			WithHint("Task ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ProcessFileChunkActivityInput represents the input for processing a file chunk
type ProcessFileChunkActivityInput struct {
	TaskID        string     `json:"task_id"`
	ChunkData     [][]string `json:"chunk_data"`
	ChunkIndex    int        `json:"chunk_index"`
	TotalChunks   int        `json:"total_chunks"`
	Headers       []string   `json:"headers"`
	EntityType    string     `json:"entity_type"`
	TenantID      string     `json:"tenant_id"`
	EnvironmentID string     `json:"environment_id"`
}

// Validate validates the process file chunk activity input
func (i *ProcessFileChunkActivityInput) Validate() error {
	if i.TaskID == "" {
		return ierr.NewError("task_id is required").
			WithHint("Task ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EntityType == "" {
		return ierr.NewError("entity_type is required").
			WithHint("Entity type is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ProcessFileChunkActivityResult represents the result of processing a file chunk
type ProcessFileChunkActivityResult struct {
	ChunkIndex        int     `json:"chunk_index"`
	ProcessedRecords  int     `json:"processed_records"`
	SuccessfulRecords int     `json:"successful_records"`
	FailedRecords     int     `json:"failed_records"`
	ErrorSummary      *string `json:"error_summary,omitempty"`
}
