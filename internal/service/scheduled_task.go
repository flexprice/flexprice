package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	temporalClient "github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	exportWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/export"
	subscriptionWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/client"
)

// ScheduledTaskService handles scheduled task operations
type ScheduledTaskService interface {
	CreateScheduledTask(ctx context.Context, req dto.CreateScheduledTaskRequest) (*dto.ScheduledTaskResponse, error)
	GetScheduledTask(ctx context.Context, id string) (*dto.ScheduledTaskResponse, error)
	ListScheduledTasks(ctx context.Context, filter *types.QueryFilter, connectionID string, entityType types.ScheduledTaskEntityType, interval types.ScheduledTaskInterval, enabled string) (*dto.ListScheduledTasksResponse, error)
	UpdateScheduledTask(ctx context.Context, id string, req dto.UpdateScheduledTaskRequest) (*dto.ScheduledTaskResponse, error)
	DeleteScheduledTask(ctx context.Context, id string) error
	TriggerForceRun(ctx context.Context, id string, req dto.TriggerForceRunRequest) (*dto.TriggerForceRunResponse, error)

	CalculateIntervalBoundaries(currentTime time.Time, interval types.ScheduledTaskInterval) (startTime, endTime time.Time)
	ScheduleUpdateBillingPeriod(ctx context.Context) (string, error)
}

type scheduledTaskService struct {
	repo           scheduledtask.Repository
	temporalClient temporalClient.TemporalClient
	logger         *logger.Logger
}

// NewScheduledTaskService creates a new scheduled task service
func NewScheduledTaskService(
	repo scheduledtask.Repository,
	temporalClient temporalClient.TemporalClient,
	logger *logger.Logger,
) ScheduledTaskService {
	return &scheduledTaskService{
		repo:           repo,
		temporalClient: temporalClient,
		logger:         logger,
	}
}

// CreateScheduledTask creates a new scheduled task
func (s *scheduledTaskService) CreateScheduledTask(ctx context.Context, req dto.CreateScheduledTaskRequest) (*dto.ScheduledTaskResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Create scheduled task
	now := time.Now()

	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	s.logger.Infow("creating scheduled task",
		"tenant_id", tenantID,
		"environment_id", envID,
		"entity_type", req.EntityType)

	// Generate task ID upfront
	taskID := types.GenerateUUIDWithPrefix("schtask")

	// Set temporal schedule ID upfront (same as task ID)
	var temporalScheduleID string
	if req.Enabled && s.temporalClient != nil {
		temporalScheduleID = taskID
	}

	task := &scheduledtask.ScheduledTask{
		ID:                 taskID,
		TenantID:           tenantID,
		EnvironmentID:      envID,
		ConnectionID:       req.ConnectionID,
		EntityType:         req.EntityType,
		Interval:           req.Interval,
		Enabled:            req.Enabled,
		JobConfig:          req.JobConfig,
		TemporalScheduleID: temporalScheduleID, // Set upfront!
		Status:             types.StatusPublished,
		CreatedAt:          now,
		UpdatedAt:          now,
		CreatedBy:          types.GetUserID(ctx),
		UpdatedBy:          types.GetUserID(ctx),
	}

	// Save to database
	err := s.repo.Create(ctx, task)
	if err != nil {
		s.logger.Errorw("failed to create scheduled task", "error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to create scheduled task").
			Mark(ierr.ErrDatabase)
	}

	s.logger.Infow("scheduled task created successfully",
		"id", task.ID,
		"entity_type", task.EntityType,
		"interval", task.Interval)

	// Start Temporal schedule if enabled
	if task.Enabled && s.temporalClient != nil {
		err = s.startScheduledTask(ctx, task)
		if err != nil {
			s.logger.Errorw("failed to start temporal schedule", "error", err)
			// Rollback: delete the created task
			_ = s.repo.Delete(ctx, task.ID)
			return nil, ierr.WithError(err).
				WithHint("Failed to create Temporal schedule").
				Mark(ierr.ErrInternal)
		}
	}

	return dto.ToScheduledTaskResponse(task), nil
}

// GetScheduledTask retrieves a scheduled task by ID
func (s *scheduledTaskService) GetScheduledTask(ctx context.Context, id string) (*dto.ScheduledTaskResponse, error) {
	task, err := s.repo.Get(ctx, id)
	if err != nil {
		s.logger.Errorw("failed to get scheduled task", "id", id, "error", err)
		return nil, ierr.WithError(err).
			WithHint("Scheduled task not found").
			Mark(ierr.ErrNotFound)
	}

	return dto.ToScheduledTaskResponse(task), nil
}

// ListScheduledTasks retrieves a list of scheduled tasks
func (s *scheduledTaskService) ListScheduledTasks(ctx context.Context, filter *types.QueryFilter, connectionID string, entityType types.ScheduledTaskEntityType, interval types.ScheduledTaskInterval, enabled string) (*dto.ListScheduledTasksResponse, error) {
	if filter == nil {
		filter = types.NewDefaultQueryFilter()
	}

	// Convert QueryFilter to ListFilters
	listFilters := &scheduledtask.ListFilters{
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		ConnectionID:  connectionID,
		EntityType:    entityType,
		Interval:      interval,
		Limit:         filter.GetLimit(),
		Offset:        filter.GetOffset(),
	}

	// Parse enabled filter
	if enabled != "" {
		if enabled == "true" {
			enabledBool := true
			listFilters.Enabled = &enabledBool
		} else if enabled == "false" {
			enabledBool := false
			listFilters.Enabled = &enabledBool
		}
	}

	tasks, err := s.repo.List(ctx, listFilters)
	if err != nil {
		s.logger.Errorw("failed to list scheduled tasks", "error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve scheduled tasks").
			Mark(ierr.ErrDatabase)
	}

	// Get total count (for pagination)
	totalCount := len(tasks) // TODO: implement proper count query if needed

	// Create pagination response
	pagination := types.PaginationResponse{
		Total:  totalCount,
		Limit:  filter.GetLimit(),
		Offset: filter.GetOffset(),
	}

	return dto.ToScheduledTaskListResponse(tasks, pagination), nil
}

// UpdateScheduledTask updates a scheduled task (only enabled field can be changed)
// When enabled=true: resumes paused schedules or creates new ones if they don't exist
// When enabled=false: pauses existing schedules
func (s *scheduledTaskService) UpdateScheduledTask(ctx context.Context, id string, req dto.UpdateScheduledTaskRequest) (*dto.ScheduledTaskResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get existing task
	task, err := s.repo.Get(ctx, id)
	if err != nil {
		s.logger.Errorw("failed to get scheduled task for update", "id", id, "error", err)
		return nil, ierr.WithError(err).
			WithHint("Scheduled task not found").
			Mark(ierr.ErrNotFound)
	}

	// Check if task is archived
	if task.Status == types.StatusArchived {
		return nil, ierr.NewError("cannot update archived scheduled task").
			WithHint("This scheduled task has been archived and cannot be modified").
			WithReportableDetails(map[string]interface{}{
				"task_id": id,
				"status":  task.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	// Track if enabled status changed
	wasEnabled := task.Enabled
	newEnabled := *req.Enabled

	// Check if there's actually a change
	if wasEnabled == newEnabled {
		s.logger.Infow("no change in enabled status", "id", id, "enabled", newEnabled)
		return dto.ToScheduledTaskResponse(task), nil
	}

	// Update enabled status
	task.Enabled = newEnabled
	task.UpdatedAt = time.Now()
	task.UpdatedBy = types.GetUserID(ctx)

	// Save updated task
	err = s.repo.Update(ctx, task)
	if err != nil {
		s.logger.Errorw("failed to update scheduled task", "id", id, "error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to update scheduled task").
			Mark(ierr.ErrDatabase)
	}

	// Handle Temporal schedule pause/resume
	if s.temporalClient != nil {
		if newEnabled {
			// Resume/Start the schedule
			s.logger.Infow("resuming temporal schedule", "task_id", id)
			err = s.resumeScheduledTask(ctx, task)
			if err != nil {
				s.logger.Errorw("failed to resume temporal schedule", "task_id", id, "error", err)
				// Rollback the database change
				task.Enabled = wasEnabled
				_ = s.repo.Update(ctx, task)
				return nil, ierr.WithError(err).
					WithHint("Failed to resume schedule in Temporal").
					Mark(ierr.ErrInternal)
			}
			s.logger.Infow("temporal schedule resumed successfully", "task_id", id)
		} else {
			// Pause the schedule
			s.logger.Infow("pausing temporal schedule", "task_id", id)
			err = s.pauseScheduledTask(ctx, task)
			if err != nil {
				s.logger.Errorw("failed to pause temporal schedule", "task_id", id, "error", err)
				// Rollback the database change
				task.Enabled = wasEnabled
				_ = s.repo.Update(ctx, task)
				return nil, ierr.WithError(err).
					WithHint("Failed to pause schedule in Temporal").
					Mark(ierr.ErrInternal)
			}
			s.logger.Infow("temporal schedule paused successfully", "task_id", id)
		}
	}

	action := "paused"
	if newEnabled {
		action = "resumed"
	}
	s.logger.Infow("scheduled task updated successfully",
		"id", task.ID,
		"action", action,
		"enabled", newEnabled)

	return dto.ToScheduledTaskResponse(task), nil
}

// DeleteScheduledTask archives a scheduled task (soft delete)
func (s *scheduledTaskService) DeleteScheduledTask(ctx context.Context, id string) error {
	// Get existing task
	task, err := s.repo.Get(ctx, id)
	if err != nil {
		s.logger.Errorw("failed to get scheduled task for deletion", "id", id, "error", err)
		return ierr.WithError(err).
			WithHint("Scheduled task not found").
			Mark(ierr.ErrNotFound)
	}

	// Check if already archived
	if task.Status == types.StatusArchived {
		s.logger.Infow("scheduled task already archived", "id", id)
		return ierr.NewError("scheduled task is already archived").
			WithHint("This scheduled task has already been deleted").
			WithReportableDetails(map[string]interface{}{
				"task_id": id,
				"status":  task.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	// Delete the Temporal schedule first
	if s.temporalClient != nil {
		s.logger.Infow("deleting temporal schedule", "task_id", id)
		err = s.deleteTemporalSchedule(ctx, task)
		if err != nil {
			s.logger.Errorw("failed to delete temporal schedule", "task_id", id, "error", err)
			return ierr.WithError(err).
				WithHint("Failed to delete Temporal schedule").
				Mark(ierr.ErrInternal)
		}
		s.logger.Infow("temporal schedule deleted successfully", "task_id", id)
	}

	// Mark the task as deleted
	task.Status = types.StatusDeleted
	task.Enabled = false // Disable when deleting
	task.UpdatedAt = time.Now()
	task.UpdatedBy = types.GetUserID(ctx)

	// Save the deleted task
	err = s.repo.Update(ctx, task)
	if err != nil {
		s.logger.Errorw("failed to mark scheduled task as deleted", "id", id, "error", err)
		return ierr.WithError(err).
			WithHint("Failed to delete scheduled task").
			Mark(ierr.ErrDatabase)
	}

	s.logger.Infow("scheduled task deleted successfully",
		"id", id,
		"status", task.Status)
	return nil
}

// TriggerForceRun triggers a force run export immediately with optional custom time range
func (s *scheduledTaskService) TriggerForceRun(ctx context.Context, id string, req dto.TriggerForceRunRequest) (*dto.TriggerForceRunResponse, error) {
	if s.temporalClient == nil {
		return nil, ierr.NewError("temporal client not configured").
			WithHint("Temporal client is not available").
			Mark(ierr.ErrInternal)
	}

	workflowID, startTime, endTime, mode, err := s.triggerForceRun(ctx, id, req.StartTime, req.EndTime)
	if err != nil {
		s.logger.Errorw("failed to trigger force run", "id", id, "error", err)
		return nil, err
	}

	s.logger.Infow("force run triggered",
		"id", id,
		"workflow_id", workflowID,
		"start_time", startTime,
		"end_time", endTime,
		"mode", mode)

	return &dto.TriggerForceRunResponse{
		WorkflowID: workflowID,
		Message:    "Force run triggered successfully",
		StartTime:  startTime,
		EndTime:    endTime,
		Mode:       mode,
	}, nil
}

// ===== PRIVATE HELPER METHODS =====

// startScheduledTask creates a new Temporal schedule for the task (used during creation)
// This function should only be called when creating a new scheduled task
func (s *scheduledTaskService) startScheduledTask(ctx context.Context, task *scheduledtask.ScheduledTask) error {
	s.logger.Infow("creating new temporal schedule", "task_id", task.ID)

	scheduleID := task.ID
	cronExpr := s.getCronExpression(types.ScheduledTaskInterval(task.Interval))

	scheduleSpec := client.ScheduleSpec{
		CronExpressions: []string{cronExpr},
	}

	action := &client.ScheduleWorkflowAction{
		Workflow: exportWorkflows.ExecuteExportWorkflow,
		Args: []interface{}{
			exportWorkflows.ExecuteExportWorkflowInput{
				ScheduledTaskID: task.ID,
				TenantID:        task.TenantID,
				EnvID:           task.EnvironmentID,
				UserID:          task.CreatedBy,
			},
		},
		TaskQueue:                string(types.TemporalTaskQueueExport),
		WorkflowExecutionTimeout: 15 * time.Minute,
		WorkflowRunTimeout:       15 * time.Minute,
		WorkflowTaskTimeout:      15 * time.Minute,
	}

	scheduleOptions := models.CreateScheduleOptions{
		ID:     scheduleID,
		Spec:   scheduleSpec,
		Action: action,
		Paused: false,
	}

	_, err := s.temporalClient.CreateSchedule(ctx, scheduleOptions)
	if err != nil {
		s.logger.Errorw("failed to create temporal schedule", "error", err)
		return ierr.WithError(err).
			WithHint("Failed to create Temporal schedule").
			Mark(ierr.ErrInternal)
	}

	s.logger.Infow("temporal schedule created successfully",
		"task_id", task.ID,
		"schedule_id", scheduleID,
		"cron", cronExpr)

	return nil
}

// pauseScheduledTask pauses the Temporal schedule for the task
// This function safely handles cases where the schedule doesn't exist
func (s *scheduledTaskService) pauseScheduledTask(ctx context.Context, task *scheduledtask.ScheduledTask) error {
	if task.TemporalScheduleID == "" {
		s.logger.Infow("no temporal schedule to pause", "task_id", task.ID)
		return nil
	}

	handle := s.temporalClient.GetScheduleHandle(ctx, task.TemporalScheduleID)
	err := handle.Pause(ctx, client.SchedulePauseOptions{})
	if err != nil {
		s.logger.Errorw("failed to pause schedule", "error", err)
		return ierr.WithError(err).
			WithHint("Failed to pause Temporal schedule").
			Mark(ierr.ErrInternal)
	}

	s.logger.Infow("temporal schedule paused successfully", "task_id", task.ID)
	return nil
}

// resumeScheduledTask resumes or creates a Temporal schedule for the task
// This function intelligently handles:
// - Resuming paused schedules
// - Creating new schedules if they don't exist
// - Handling schedules that are already running
func (s *scheduledTaskService) resumeScheduledTask(ctx context.Context, task *scheduledtask.ScheduledTask) error {
	if task.TemporalScheduleID == "" {
		s.logger.Infow("no temporal schedule ID, creating new schedule", "task_id", task.ID)
		err := s.startScheduledTask(ctx, task)
		if err != nil {
			return err
		}
		task.TemporalScheduleID = task.ID
		err = s.repo.Update(ctx, task)
		if err != nil {
			return err
		}
		return nil
	}

	scheduleID := task.TemporalScheduleID
	handle := s.temporalClient.GetScheduleHandle(ctx, scheduleID)

	// Try to get existing schedule to check its state
	schedule, err := handle.Describe(ctx)
	if err != nil {
		// Schedule doesn't exist, create a new one
		s.logger.Infow("schedule not found, creating new one", "task_id", task.ID, "schedule_id", scheduleID)
		return s.startScheduledTask(ctx, task)
	}

	// Check if schedule is paused
	if schedule.Schedule.State.Paused {
		s.logger.Infow("resuming paused schedule", "task_id", task.ID, "schedule_id", scheduleID)
		err = handle.Unpause(ctx, client.ScheduleUnpauseOptions{})
		if err != nil {
			s.logger.Errorw("failed to unpause schedule", "error", err)
			return ierr.WithError(err).
				WithHint("Failed to resume Temporal schedule").
				Mark(ierr.ErrInternal)
		}
		s.logger.Infow("temporal schedule resumed successfully", "task_id", task.ID)
		return nil
	}

	// Schedule exists and is not paused
	s.logger.Infow("temporal schedule already running", "task_id", task.ID, "schedule_id", scheduleID)
	return nil
}

// deleteTemporalSchedule deletes the Temporal schedule for the task
func (s *scheduledTaskService) deleteTemporalSchedule(ctx context.Context, task *scheduledtask.ScheduledTask) error {
	if task.TemporalScheduleID == "" {
		s.logger.Infow("no temporal schedule to delete", "task_id", task.ID)
		return nil
	}

	handle := s.temporalClient.GetScheduleHandle(ctx, task.TemporalScheduleID)
	err := handle.Delete(ctx)
	if err != nil {
		s.logger.Errorw("failed to delete temporal schedule", "schedule_id", task.TemporalScheduleID, "error", err)
		return ierr.WithError(err).
			WithHint("Failed to delete Temporal schedule").
			Mark(ierr.ErrInternal)
	}

	s.logger.Infow("temporal schedule deleted successfully",
		"task_id", task.ID,
		"schedule_id", task.TemporalScheduleID)
	return nil
}

// triggerForceRun executes the export workflow immediately (bypasses schedule)
// If customStart and customEnd are provided, uses those times. Otherwise, calculates automatically.
// Returns: workflowID, startTime, endTime, mode, error
func (s *scheduledTaskService) triggerForceRun(ctx context.Context, taskID string, customStart, customEnd *time.Time) (string, time.Time, time.Time, string, error) {
	s.logger.Infow("triggering force run", "task_id", taskID, "custom_start", customStart, "custom_end", customEnd)

	// Get the task
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return "", time.Time{}, time.Time{}, "", ierr.WithError(err).
			WithHint("Failed to get scheduled task").
			Mark(ierr.ErrDatabase)
	}

	// Determine time range and mode
	var startTime, endTime time.Time
	var mode string

	if customStart != nil && customEnd != nil {
		// User provided custom time range
		startTime = *customStart
		endTime = *customEnd
		mode = "custom"
		s.logger.Infow("using custom time range",
			"start_time", startTime,
			"end_time", endTime,
			"duration", endTime.Sub(startTime))
	} else {
		// Calculate automatically based on interval boundaries
		interval := types.ScheduledTaskInterval(task.Interval)
		currentTime := time.Now()

		// Calculate interval boundaries for force run
		startTime, endTime = s.CalculateIntervalBoundaries(currentTime, interval)
		mode = "force run"

		s.logger.Infow("using automatic time range based on interval boundaries",
			"start_time", startTime,
			"end_time", endTime,
			"current_time", currentTime,
			"interval", interval,
			"duration", endTime.Sub(startTime))
	}

	// Generate workflow ID for force run
	workflowID := fmt.Sprintf("%s-export", types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TASK))

	s.logger.Infow("triggering force run export",
		"workflow_id", workflowID,
		"scheduled_task_id", taskID,
		"mode", mode)

	workflowOptions := models.StartWorkflowOptions{
		ID:                       workflowID,
		TaskQueue:                string(types.TemporalTaskQueueExport),
		WorkflowExecutionTimeout: 15 * time.Minute, // 15 minutes for export tasks
		WorkflowRunTimeout:       15 * time.Minute, // 15 minutes for single workflow run
		WorkflowTaskTimeout:      15 * time.Minute, // 15 minute for workflow task processing
	}

	input := exportWorkflows.ExecuteExportWorkflowInput{
		ScheduledTaskID: taskID,
		TenantID:        task.TenantID,
		EnvID:           task.EnvironmentID,
		UserID:          types.GetUserID(ctx), // Get user ID from context for force runs
		CustomStartTime: &startTime,           // Always pass calculated time range for force runs
		CustomEndTime:   &endTime,
	}

	workflowRun, err := s.temporalClient.StartWorkflow(ctx, workflowOptions, exportWorkflows.ExecuteExportWorkflow, input)
	if err != nil {
		s.logger.Errorw("failed to start export workflow", "error", err)
		return "", time.Time{}, time.Time{}, "", ierr.WithError(err).
			WithHint("Failed to start export workflow").
			Mark(ierr.ErrInternal)
	}

	s.logger.Infow("force run triggered",
		"scheduled_task_id", taskID,
		"workflow_id", workflowRun.GetID(),
		"run_id", workflowRun.GetRunID(),
		"start_time", startTime,
		"end_time", endTime,
		"mode", mode)

	return workflowRun.GetID(), startTime, endTime, mode, nil
}

// getCronExpression converts interval to cron expression
// All schedules (except testing) include a 15-minute buffer after the interval boundary
// to allow time for data ingestion into the feature_usage table
func (s *scheduledTaskService) getCronExpression(interval types.ScheduledTaskInterval) string {
	switch interval {
	case types.ScheduledTaskIntervalCustom:
		return "*/10 * * * *" // Every 10 minutes (for testing, no buffer)
	case types.ScheduledTaskIntervalEvery15Minutes:
		return "*/15 * * * *" // Every 15 minutes
	case types.ScheduledTaskIntervalHourly:
		return "15 * * * *" // Every hour at 15 minutes past (e.g., 10:15, 11:15, 12:15)
	case types.ScheduledTaskIntervalDaily:
		return "15 0 * * *" // Every day at 00:15 AM
	default:
		return "15 0 * * *" // Default to daily at 00:15
	}
}

// CalculateIntervalBoundaries calculates interval boundaries based on current time
//
// Returns the interval that aligns with the current time:
//   - Run at 10:30 → 10:00-11:00 (current interval)
//   - Run at 11:15 (cron) → 11:00-12:00 (current interval)
//   - Run at 12:15 (cron) → 12:00-13:00 (current interval)
//
// The activity layer uses the start boundary as the end time for exports,
// then subtracts one interval to calculate the previous completed interval
func (s *scheduledTaskService) CalculateIntervalBoundaries(currentTime time.Time, interval types.ScheduledTaskInterval) (startTime, endTime time.Time) {
	switch interval {
	case types.ScheduledTaskIntervalCustom:
		// For testing: align to 10-minute intervals
		// Return the CURRENT 10-minute interval
		// Example: If current time is 2:07 PM, return 2:00 PM - 2:10 PM
		minutes := currentTime.Minute() / 10 * 10
		startTime = time.Date(
			currentTime.Year(), currentTime.Month(), currentTime.Day(),
			currentTime.Hour(), minutes, 0, 0, currentTime.Location(),
		)
		endTime = startTime.Add(10 * time.Minute)

	case types.ScheduledTaskIntervalHourly:
		// Return the CURRENT hour
		// Example: If current time is 10:30 AM, return 10:00 AM → 11:00 AM
		startTime = time.Date(
			currentTime.Year(), currentTime.Month(), currentTime.Day(),
			currentTime.Hour(), 0, 0, 0, currentTime.Location(),
		)
		endTime = startTime.Add(1 * time.Hour)

	case types.ScheduledTaskIntervalDaily:
		// Return the CURRENT day
		// Example: If run anytime on 16 Oct 2025, return 16 Oct 00:00 → 17 Oct 00:00
		startTime = time.Date(
			currentTime.Year(), currentTime.Month(), currentTime.Day(),
			0, 0, 0, 0, currentTime.Location(),
		)
		endTime = startTime.AddDate(0, 0, 1) // Next day
	case types.ScheduledTaskIntervalEvery15Minutes:
		// Return the CURRENT 15-minute interval
		// Example: If current time is 2:07 PM, return 2:00 PM - 2:15 PM
		minutes := currentTime.Minute() / 15 * 15
		startTime = time.Date(
			currentTime.Year(), currentTime.Month(), currentTime.Day(),
			currentTime.Hour(), minutes, 0, 0, currentTime.Location(),
		)
		endTime = startTime.Add(15 * time.Minute)
	default:
		// Default to current day
		startTime = time.Date(
			currentTime.Year(), currentTime.Month(), currentTime.Day(),
			0, 0, 0, 0, currentTime.Location(),
		)
		endTime = startTime.AddDate(0, 0, 1)
	}

	return startTime, endTime
}

func (s *scheduledTaskService) ScheduleUpdateBillingPeriod(ctx context.Context) (string, error) {

	scheduleID := types.GenerateUUIDWithPrefix("schtask_billing")
	cronExpr := s.getCronExpression(types.ScheduledTaskIntervalEvery15Minutes)

	scheduleSpec := client.ScheduleSpec{
		CronExpressions: []string{cronExpr},
	}

	action := &client.ScheduleWorkflowAction{
		Workflow: subscriptionWorkflows.ScheduleSubscriptionBillingWorkflow,
		Args: []interface{}{
			subscriptionModels.ScheduleSubscriptionBillingWorkflowInput{
				BatchSize: types.DEFAULT_BATCH_SIZE,
			},
		},
		TaskQueue:                string(types.TemporalTaskQueueSubscription),
		WorkflowExecutionTimeout: 1 * time.Hour,
		WorkflowRunTimeout:       1 * time.Hour,
		WorkflowTaskTimeout:      1 * time.Hour,
	}

	scheduleOptions := models.CreateScheduleOptions{
		ID:     scheduleID,
		Spec:   scheduleSpec,
		Action: action,
		Paused: false,
	}

	_, err := s.temporalClient.CreateSchedule(ctx, scheduleOptions)
	if err != nil {
		s.logger.Errorw("failed to create temporal schedule", "error", err)
		return "", ierr.WithError(err).
			WithHint("Failed to create Temporal schedule").
			Mark(ierr.ErrInternal)
	}

	return "Triggered update billing period workflow successfully", nil
}
