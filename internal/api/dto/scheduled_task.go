package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateScheduledTaskRequest represents a request to create a scheduled task
type CreateScheduledTaskRequest struct {
	ConnectionID string                        `json:"connection_id,omitempty"`
	Name         string                        `json:"name,omitempty"`
	EntityType   types.ScheduledTaskEntityType `json:"entity_type" binding:"required" validate:"required"`
	// "custom" is excluded from API.
	Interval  types.ScheduledTaskInterval `json:"interval" binding:"required" validate:"required" enums:"hourly,daily,weekly,monthly,manual"`
	Enabled   bool                        `json:"enabled"`
	JobConfig *types.S3JobConfig          `json:"job_config,omitempty"`
	Schedule  *types.ExportSchedule       `json:"schedule,omitempty"`
	// Required for report and analytics_view exports.
	SourceConfig *types.ExportSourceConfig `json:"source_config,omitempty"`
}

const maxScheduledTaskNameLength = 120

// Validate validates the create request
func (r *CreateScheduledTaskRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Reject "custom" interval from API requests (internal testing only)
	if r.Interval == types.ScheduledTaskIntervalCustom {
		return ierr.NewError("interval 'custom' is not available for API usage").
			WithHint("Please use 'hourly' or 'daily' intervals.").
			Mark(ierr.ErrValidation)
	}

	if err := r.EntityType.Validate(); err != nil {
		return err
	}

	if err := r.Interval.Validate(); err != nil {
		return err
	}

	if len(r.Name) > maxScheduledTaskNameLength {
		return ierr.NewError("name is too long").
			WithHint("Name must be at most 120 characters").
			Mark(ierr.ErrValidation)
	}

	if err := types.ValidateExportSchedule(r.Interval, r.Schedule); err != nil {
		return err
	}

	if err := r.SourceConfig.Validate(r.EntityType); err != nil {
		return err
	}

	if r.EntityType.IsReportSource() {
		if err := r.validateReportSource(); err != nil {
			return err
		}
	}

	if r.ConnectionID != "" {
		if r.JobConfig == nil {
			return ierr.NewError("job_config is required").
				WithHint("Provide job_config when connection_id is set").
				Mark(ierr.ErrValidation)
		}
		return nil
	}

	switch r.Interval {
	case types.ScheduledTaskIntervalEvery15Minutes, types.ScheduledTaskIntervalEvery30Minutes:
		return ierr.NewError("interval is not available without a connection").
			WithHint("Use hourly, daily, weekly, monthly or manual").
			Mark(ierr.ErrValidation)
	}

	if jc := r.JobConfig; jc != nil {
		if jc.Bucket != "" || jc.Region != "" || jc.KeyPrefix != "" || jc.EndpointURL != "" ||
			jc.UsePathStyle || jc.Encryption != "" || jc.Provider != "" {
			return ierr.NewError("job_config storage fields are set by Flexprice when no connection_id is given").
				WithHint("Only export_metadata_fields and compression may be set").
				Mark(ierr.ErrValidation)
		}
		if err := jc.Compression.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (r *CreateScheduledTaskRequest) validateReportSource() error {
	switch r.Interval {
	case types.ScheduledTaskIntervalDaily, types.ScheduledTaskIntervalWeekly, types.ScheduledTaskIntervalMonthly:
		if r.Schedule == nil {
			return ierr.NewError("schedule is required").
				WithHint("Scheduled report exports require a schedule").
				Mark(ierr.ErrValidation)
		}
	case types.ScheduledTaskIntervalManual:
	default:
		return ierr.NewError("invalid interval for report export").
			WithHint("Use daily, weekly, monthly or manual").
			Mark(ierr.ErrValidation)
	}
	if r.JobConfig != nil && len(r.JobConfig.ExportMetadataFields) > 0 {
		return ierr.NewError("export_metadata_fields not supported").
			WithHint("Report exports do not take metadata fields").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// UpdateScheduledTaskRequest represents a request to update a scheduled task
type UpdateScheduledTaskRequest struct {
	Enabled  *bool                 `json:"enabled,omitempty"`
	Name     *string               `json:"name,omitempty"`
	Schedule *types.ExportSchedule `json:"schedule,omitempty"`
}

// Validate validates the update request
func (r *UpdateScheduledTaskRequest) Validate() error {
	if r.Enabled == nil && r.Name == nil && r.Schedule == nil {
		return ierr.NewError("no fields to update").
			WithHint("Provide enabled, name or schedule").
			Mark(ierr.ErrValidation)
	}
	if r.Name != nil && len(*r.Name) > maxScheduledTaskNameLength {
		return ierr.NewError("name is too long").
			WithHint("Name must be at most 120 characters").
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
	// "custom" is excluded from API.
	Interval     types.ScheduledTaskInterval `json:"interval" enums:"hourly,daily,weekly,monthly,manual"`
	Enabled      bool                        `json:"enabled"`
	JobConfig    *types.S3JobConfig          `json:"job_config"`
	Name         string                      `json:"name,omitempty"`
	ManagedCopy  bool                        `json:"managed_copy,omitempty"`
	Schedule     *types.ExportSchedule       `json:"schedule,omitempty"`
	SourceConfig *types.ExportSourceConfig   `json:"source_config,omitempty"`
	Status       string                      `json:"status"`
	CreatedAt    time.Time                   `json:"created_at"`
	UpdatedAt    time.Time                   `json:"updated_at"`
}

// ListScheduledTasksResponse represents a list of scheduled tasks
type ListScheduledTasksResponse = types.ListResponse[*ScheduledTaskResponse] // @name ListScheduledTasksResponse

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
		Name:          task.Name,
		ManagedCopy:   task.ManagedCopy,
		Schedule:      task.Schedule,
		SourceConfig:  task.SourceConfig,
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
	// Limits a wallet_ledger export to these customers; empty means every customer.
	CustomerExternalIDs []string `json:"customer_external_ids,omitempty" validate:"omitempty,max=100,dive,required"`
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
		now := clock.Now()
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
	// Only exports that support it read this; empty means every customer.
	CustomerExternalIDs []string `json:"customer_external_ids,omitempty"`
	// Writes to the Flexprice bucket.
	ManagedCopy bool `json:"managed_copy,omitempty"`
	// Run identifier; makes each run write a distinct object.
	TaskID       string                    `json:"task_id,omitempty"`
	SourceConfig *types.ExportSourceConfig `json:"source_config,omitempty"`
}

// ExportResponse represents the result of an export operation
type ExportResponse struct {
	EntityType    types.ScheduledTaskEntityType `json:"entity_type"`
	RecordCount   int                           `json:"record_count"`
	FileURL       string                        `json:"file_url"`
	FileSizeBytes int64                         `json:"file_size_bytes"`
	ExportedAt    time.Time                     `json:"exported_at"`
}
