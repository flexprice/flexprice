package task

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

type Task struct {
	ID                string                 `json:"id"`
	TaskType          types.TaskType         `json:"task_type"`
	EntityType        types.EntityType       `json:"entity_type"`
	FileURL           string                 `json:"file_url"`
	FileName          *string                `json:"file_name,omitempty"`
	FileType          types.FileType         `json:"file_type"`
	TaskStatus        types.TaskStatus       `json:"task_status"`
	TotalRecords      *int                   `json:"total_records"`
	ProcessedRecords  int                    `json:"processed_records"`
	SuccessfulRecords int                    `json:"successful_records"`
	FailedRecords     int                    `json:"failed_records"`
	ErrorSummary      *string                `json:"error_summary"`
	Metadata          map[string]interface{} `json:"metadata"`
	StartedAt         *time.Time             `json:"started_at"`
	CompletedAt       *time.Time             `json:"completed_at"`
	FailedAt          *time.Time             `json:"failed_at"`
	types.BaseModel
}

// FromEnt converts ent.Task to domain Task
func FromEnt(e *ent.Task) *Task {
	if e == nil {
		return nil
	}

	return &Task{
		ID:                e.ID,
		TaskType:          types.TaskType(e.TaskType),
		EntityType:        types.EntityType(e.EntityType),
		FileURL:           e.FileURL,
		FileName:          e.FileName,
		FileType:          types.FileType(e.FileType),
		TaskStatus:        types.TaskStatus(e.TaskStatus),
		TotalRecords:      e.TotalRecords,
		ProcessedRecords:  e.ProcessedRecords,
		SuccessfulRecords: e.SuccessfulRecords,
		FailedRecords:     e.FailedRecords,
		ErrorSummary:      e.ErrorSummary,
		Metadata:          e.Metadata,
		StartedAt:         e.StartedAt,
		CompletedAt:       e.CompletedAt,
		FailedAt:          e.FailedAt,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		},
	}
}

// FromEntList converts a list of ent.Task to domain Tasks
func FromEntList(list []*ent.Task) []*Task {
	if list == nil {
		return nil
	}
	tasks := make([]*Task, len(list))
	for i, item := range list {
		tasks[i] = FromEnt(item)
	}
	return tasks
}

// Validate validates the task
func (t *Task) Validate() error {
	return nil
}
