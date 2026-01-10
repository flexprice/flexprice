package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateScheduledTaskRequest represents a request to create a scheduled task
type CreateScheduledTaskRequest struct {
	ConnectionID string                        `json:"connection_id" binding:"required" validate:"required"`
	EntityType   types.ScheduledTaskEntityType `json:"entity_type" binding:"required" validate:"required"`
	Interval     types.ScheduledTaskInterval   `json:"interval" binding:"required" validate:"required" enums:"hourly,daily"` // Note: "custom" is excluded from API (internal testing only)
	Enabled      bool                          `json:"enabled"`
	JobConfig    *types.S3JobConfig            `json:"job_config" binding:"required" validate:"required"`
}

// Validate validates the create request
func (r *CreateScheduledTaskRequest) Validate() error {
	// run struct-tag validations first
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Reject "custom" interval from API requests (internal testing only)
	if r.Interval == types.ScheduledTaskIntervalCustom {
		return ierr.NewError("interval 'custom' is not available for API usage").
			WithHint("Please use 'hourly' or 'daily' intervals.").
			Mark(ierr.ErrValidation)
	}

	// Validate entity type
	if err := r.EntityType.Validate(); err != nil {
		return err
	}

	// Validate interval
	if err := r.Interval.Validate(); err != nil {
		return err
	}

	// Note: S3JobConfig validation is done in service layer after checking
	// if connection is Flexprice-managed

	return nil
}

// UpdateScheduledTaskRequest represents a request to update a scheduled task
// Only enabled field is allowed to be updated
type UpdateScheduledTaskRequest struct {
	Enabled *bool `json:"enabled" validate:"required"`
}

// Validate validates the update request
func (r *UpdateScheduledTaskRequest) Validate() error {
	if r.Enabled == nil {
		return ierr.NewError("enabled field is required").
			WithHint("Please provide enabled field (true or false)").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ScheduledTaskResponse represents a scheduled task response
type ScheduledTaskResponse struct {
	ID            string                        `json:"id"`
	TenantID      string                        `json:"tenant_id"`
	EnvironmentID string                        `json:"environment_id"`
	ConnectionID  string                        `json:"connection_id"`
	EntityType    types.ScheduledTaskEntityType `json:"entity_type"`
	Interval      types.ScheduledTaskInterval   `json:"interval" enums:"hourly,daily"` // Note: "custom" is excluded from API docs
	Enabled       bool                          `json:"enabled"`
	JobConfig     *types.S3JobConfig            `json:"job_config"`
	Status        string                        `json:"status"`
	CreatedAt     time.Time                     `json:"created_at"`
	UpdatedAt     time.Time                     `json:"updated_at"`
}

// ListScheduledTasksResponse represents a list of scheduled tasks
type ListScheduledTasksResponse = types.ListResponse[*ScheduledTaskResponse]

// ToScheduledTaskResponse converts a domain scheduled task to a response
func ToScheduledTaskResponse(task *scheduledtask.ScheduledTask) *ScheduledTaskResponse {
	return &ScheduledTaskResponse{
		ID:            task.ID,
		TenantID:      task.TenantID,
		EnvironmentID: task.EnvironmentID,
		ConnectionID:  task.ConnectionID,
		EntityType:    task.EntityType,
		Interval:      task.Interval,
		Enabled:       task.Enabled,
		JobConfig:     task.JobConfig,
		Status:        string(task.Status),
		CreatedAt:     task.CreatedAt,
		UpdatedAt:     task.UpdatedAt,
	}
}

// ToScheduledTaskListResponse converts domain scheduled tasks to a list response
func ToScheduledTaskListResponse(tasks []*scheduledtask.ScheduledTask, pagination types.PaginationResponse) *ListScheduledTasksResponse {
	responses := make([]*ScheduledTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		responses = append(responses, ToScheduledTaskResponse(task))
	}

	return &ListScheduledTasksResponse{
		Items:      responses,
		Pagination: pagination,
	}
}

// ToCreateScheduledTaskInput converts request to domain input
func (r *CreateScheduledTaskRequest) ToCreateInput() *types.CreateScheduledTaskInput {
	enabled := r.Enabled // default will be handled by service if needed
	return &types.CreateScheduledTaskInput{
		ConnectionID: r.ConnectionID,
		EntityType:   r.EntityType,
		Interval:     r.Interval,
		Enabled:      enabled,
		JobConfig:    r.JobConfig,
	}
}

// TriggerForceRunRequest represents the request to trigger a force run
type TriggerForceRunRequest struct {
	StartTime *time.Time `json:"start_time,omitempty" validate:"omitempty"`
	EndTime   *time.Time `json:"end_time,omitempty" validate:"omitempty"`
}

// Validate validates the force run request
func (r *TriggerForceRunRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// If only one is provided, return error
	if (r.StartTime != nil && r.EndTime == nil) || (r.StartTime == nil && r.EndTime != nil) {
		return ierr.NewError("both start_time and end_time must be provided together").
			WithHint("Please provide both start_time and end_time, or neither for automatic calculation").
			Mark(ierr.ErrValidation)
	}

	// If both are provided, validate the time range
	if r.StartTime != nil && r.EndTime != nil {
		if r.EndTime.Before(*r.StartTime) || r.EndTime.Equal(*r.StartTime) {
			return ierr.NewError("end_time must be after start_time").
				WithHint("End time must be after start time").
				WithReportableDetails(map[string]interface{}{
					"start_time": r.StartTime.Format(time.RFC3339),
					"end_time":   r.EndTime.Format(time.RFC3339),
				}).
				Mark(ierr.ErrValidation)
		}

		// Validate that end time is not in the future
		now := time.Now()
		if r.EndTime.After(now) {
			return ierr.NewError("end_time cannot be in the future").
				WithHint("End time must be in the past or present").
				WithReportableDetails(map[string]interface{}{
					"end_time": r.EndTime.Format(time.RFC3339),
					"now":      now.Format(time.RFC3339),
				}).
				Mark(ierr.ErrValidation)
		}

		// Optional: Add maximum time range validation (e.g., max 1 year)
		duration := r.EndTime.Sub(*r.StartTime)
		maxDuration := 365 * 24 * time.Hour // 1 year
		if duration > maxDuration {
			return ierr.NewError("time range cannot exceed 1 year").
				WithHint("Maximum allowed time range is 1 year").
				WithReportableDetails(map[string]interface{}{
					"duration":     duration.String(),
					"max_duration": maxDuration.String(),
				}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// TriggerForceRunResponse represents the response from force run trigger
type TriggerForceRunResponse struct {
	WorkflowID string    `json:"workflow_id"`
	Message    string    `json:"message"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	Mode       string    `json:"mode"` // "custom" or "automatic"
}

// ExportRequest represents an export request
type ExportRequest struct {
	EntityType   types.ScheduledTaskEntityType `json:"entity_type"`
	ConnectionID string                        `json:"connection_id"` // Connection ID for S3 credentials
	TenantID     string                        `json:"tenant_id"`
	EnvID        string                        `json:"env_id"`
	StartTime    time.Time                     `json:"start_time"`
	EndTime      time.Time                     `json:"end_time"`
	JobConfig    *types.S3JobConfig            `json:"job_config"` // S3 job configuration from scheduled_tasks
}

// ExportResponse represents the result of an export operation
type ExportResponse struct {
	EntityType    types.ScheduledTaskEntityType `json:"entity_type"`
	RecordCount   int                           `json:"record_count"`
	FileURL       string                        `json:"file_url"`
	FileSizeBytes int64                         `json:"file_size_bytes"`
	ExportedAt    time.Time                     `json:"exported_at"`
}
