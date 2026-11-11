package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	domainTask "github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/temporal/exportschedule"
	"github.com/flexprice/flexprice/internal/types"
)

// ScheduledTaskService handles scheduled task operations
type ScheduledTaskService interface {
	CreateScheduledTask(ctx context.Context, req dto.CreateScheduledTaskRequest) (*dto.ScheduledTaskResponse, error)
	GetScheduledTask(ctx context.Context, id string) (*dto.ScheduledTaskResponse, error)
	ListScheduledTasks(ctx context.Context, filter *types.QueryFilter, connectionID string, entityType types.ScheduledTaskEntityType, interval types.ScheduledTaskInterval, enabled string) (*dto.ListScheduledTasksResponse, error)
	UpdateScheduledTask(ctx context.Context, id string, req dto.UpdateScheduledTaskRequest) (*dto.ScheduledTaskResponse, error)
	DeleteScheduledTask(ctx context.Context, id string) error
	TriggerForceRun(ctx context.Context, id string, req dto.TriggerForceRunRequest) (*dto.TriggerForceRunResponse, error)
}

type scheduledTaskService struct {
	repo            scheduledtask.Repository
	taskRepo        domainTask.Repository
	connectionRepo  connection.Repository
	scheduler       exportschedule.Scheduler
	logger          *logger.Logger
	config          *config.Configuration
	storageResolver storage.Resolver
	rbac            *rbac.RBACService
	analytics       AnalyticsService
}

// NewScheduledTaskService accepts a nil scheduler.
func NewScheduledTaskService(
	repo scheduledtask.Repository,
	taskRepo domainTask.Repository,
	connectionRepo connection.Repository,
	scheduler exportschedule.Scheduler,
	logger *logger.Logger,
	config *config.Configuration,
	storageResolver storage.Resolver,
	rbacService *rbac.RBACService,
	analytics AnalyticsService,
) ScheduledTaskService {
	return &scheduledTaskService{
		repo:            repo,
		taskRepo:        taskRepo,
		connectionRepo:  connectionRepo,
		scheduler:       scheduler,
		logger:          logger,
		config:          config,
		storageResolver: storageResolver,
		rbac:            rbacService,
		analytics:       analytics,
	}
}

// Denies report exports without analytics read.
func requireAnalyticsRead(ctx context.Context, rbacService *rbac.RBACService, entity types.ScheduledTaskEntityType) error {
	if !entity.IsReportSource() {
		return nil
	}
	if rbacService != nil && rbacService.HasPermission(types.GetRoles(ctx), string(types.EntityAnalytics), string(types.ActionRead)) {
		return nil
	}
	return ierr.NewError("analytics read permission required").
		WithHint("Report exports require analytics read access").
		Mark(ierr.ErrPermissionDenied)
}

func (s *scheduledTaskService) validateSource(ctx context.Context, req dto.CreateScheduledTaskRequest) error {
	if !req.EntityType.IsReportSource() {
		return nil
	}
	if s.analytics == nil {
		return ierr.NewError("analytics service not configured").Mark(ierr.ErrInternal)
	}
	if req.EntityType == types.ScheduledTaskEntityTypeAnalyticsView {
		return s.analytics.ValidateViewSource(ctx, req.SourceConfig.ViewID, intervalWindowDays(req.Interval))
	}
	if err := s.analytics.ValidateReportSource(ctx, req.SourceConfig.ReportKey, req.SourceConfig.ReportParams); err != nil {
		return err
	}
	return checkReportRange(req.SourceConfig.ReportKey, intervalWindowDays(req.Interval))
}

// Longest window per interval.
func intervalWindowDays(interval types.ScheduledTaskInterval) int {
	switch interval {
	case types.ScheduledTaskIntervalDaily:
		return 1
	case types.ScheduledTaskIntervalWeekly:
		return 7
	case types.ScheduledTaskIntervalMonthly:
		return 31
	}
	return 0
}

func checkReportRange(key string, days int) error {
	entry, ok := analytics.LookupReport(key)
	if !ok || entry.MaxRangeDays <= 0 || days <= entry.MaxRangeDays {
		return nil
	}
	return ierr.NewErrorf("report covers at most %d days", entry.MaxRangeDays).
		WithHintf("This report covers at most %d days; choose a shorter interval or window", entry.MaxRangeDays).
		Mark(ierr.ErrValidation)
}

// CreateScheduledTask creates a new scheduled task
func (s *scheduledTaskService) CreateScheduledTask(ctx context.Context, req dto.CreateScheduledTaskRequest) (*dto.ScheduledTaskResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := requireAnalyticsRead(ctx, s.rbac, req.EntityType); err != nil {
		return nil, err
	}
	if err := s.validateSource(ctx, req); err != nil {
		return nil, err
	}

	// Create scheduled task
	now := clock.Now()

	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	s.logger.Info(ctx, "creating scheduled task",
		"tenant_id", tenantID,
		"environment_id", envID,
		"entity_type", req.EntityType,
		"connection_id", req.ConnectionID)

	taskID := types.GenerateUUIDWithPrefix("schtask")

	var jobConfig *types.S3JobConfig
	var err error
	if req.ConnectionID == "" {
		jobConfig, err = s.flexpriceJobConfig(ctx, req, taskID)
	} else {
		jobConfig, err = s.connectionJobConfig(ctx, req)
	}
	if err != nil {
		return nil, err
	}

	// Manual exports skip Temporal
	var temporalScheduleID string
	if req.Enabled && s.scheduler != nil && req.Interval != types.ScheduledTaskIntervalManual {
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
		JobConfig:          jobConfig,
		TemporalScheduleID: temporalScheduleID,
		Name:               req.Name,
		Schedule:           req.Schedule,
		SourceConfig:       req.SourceConfig,
		Status:             types.StatusPublished,
		CreatedAt:          now,
		UpdatedAt:          now,
		CreatedBy:          types.GetUserID(ctx),
		UpdatedBy:          types.GetUserID(ctx),
	}

	// Save to database
	err = s.repo.Create(ctx, task)
	if err != nil {
		s.logger.Error(ctx, "failed to create scheduled task", "error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to create scheduled task").
			Mark(ierr.ErrDatabase)
	}

	s.logger.Info(ctx, "scheduled task created successfully",
		"id", task.ID,
		"entity_type", task.EntityType,
		"interval", task.Interval)

	// Start Temporal schedule if enabled
	if temporalScheduleID != "" {
		err = s.scheduler.Create(ctx, task)
		if err != nil {
			s.logger.Error(ctx, "failed to start temporal schedule", "error", err)
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
		s.logger.Error(ctx, "failed to get scheduled task", "id", id, "error", err)
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
		s.logger.Error(ctx, "failed to list scheduled tasks", "error", err)
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

	// Writer read keeps updated_at fresh.
	task, err := s.repo.Get(types.WithForceWriter(ctx), id)
	if err != nil {
		s.logger.Error(ctx, "failed to get scheduled task for update", "id", id, "error", err)
		return nil, ierr.WithError(err).
			WithHint("Scheduled task not found").
			Mark(ierr.ErrNotFound)
	}

	if err := requireAnalyticsRead(ctx, s.rbac, task.EntityType); err != nil {
		return nil, err
	}

	if task.Status == types.StatusArchived || task.Status == types.StatusDeleted {
		return nil, ierr.NewError("cannot update archived or deleted scheduled task").
			WithHint("This scheduled task has been archived or deleted and cannot be modified").
			WithReportableDetails(map[string]interface{}{
				"task_id": id,
				"status":  task.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	if req.Name != nil || req.Schedule != nil {
		if err := s.updateExportSettings(ctx, task, req); err != nil {
			return nil, err
		}
	}

	if req.Enabled == nil {
		return dto.ToScheduledTaskResponse(task), nil
	}

	wasEnabled := task.Enabled
	newEnabled := *req.Enabled

	// Check if there's actually a change
	if wasEnabled == newEnabled {
		s.logger.Info(ctx, "no change in enabled status", "id", id, "enabled", newEnabled)
		return dto.ToScheduledTaskResponse(task), nil
	}

	// Update enabled status
	task.Enabled = newEnabled
	task.UpdatedAt = clock.Now()
	task.UpdatedBy = types.GetUserID(ctx)

	// Save updated task
	err = s.repo.Update(ctx, task)
	if err != nil {
		s.logger.Error(ctx, "failed to update scheduled task", "id", id, "error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to update scheduled task").
			Mark(ierr.ErrDatabase)
	}

	// Handle Temporal schedule pause/resume
	if s.scheduler != nil {
		if newEnabled {
			// Resume/Start the schedule
			s.logger.Info(ctx, "resuming temporal schedule", "task_id", id)
			err = s.resumeSchedule(ctx, task)
			if err != nil {
				s.logger.Error(ctx, "failed to resume temporal schedule", "task_id", id, "error", err)
				// Rollback the database change
				task.Enabled = wasEnabled
				_ = s.repo.Update(ctx, task)
				return nil, ierr.WithError(err).
					WithHint("Failed to resume schedule in Temporal").
					Mark(ierr.ErrInternal)
			}
			s.logger.Info(ctx, "temporal schedule resumed successfully", "task_id", id)
		} else {
			// Pause the schedule
			s.logger.Info(ctx, "pausing temporal schedule", "task_id", id)
			err = s.scheduler.Pause(ctx, task)
			if err != nil {
				s.logger.Error(ctx, "failed to pause temporal schedule", "task_id", id, "error", err)
				// Rollback the database change
				task.Enabled = wasEnabled
				_ = s.repo.Update(ctx, task)
				return nil, ierr.WithError(err).
					WithHint("Failed to pause schedule in Temporal").
					Mark(ierr.ErrInternal)
			}
			s.logger.Info(ctx, "temporal schedule paused successfully", "task_id", id)
		}
	}

	action := "paused"
	if newEnabled {
		action = "resumed"
	}
	s.logger.Info(ctx, "scheduled task updated successfully",
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
		s.logger.Error(ctx, "failed to get scheduled task for deletion", "id", id, "error", err)
		return ierr.WithError(err).
			WithHint("Scheduled task not found").
			Mark(ierr.ErrNotFound)
	}

	// Check if already archived
	if task.Status == types.StatusArchived {
		s.logger.Info(ctx, "scheduled task already archived", "id", id)
		return ierr.NewError("scheduled task is already archived").
			WithHint("This scheduled task has already been deleted").
			WithReportableDetails(map[string]interface{}{
				"task_id": id,
				"status":  task.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	// Delete the Temporal schedule first
	if s.scheduler != nil {
		s.logger.Info(ctx, "deleting temporal schedule", "task_id", id)
		err = s.scheduler.Delete(ctx, task)
		if err != nil {
			s.logger.Error(ctx, "failed to delete temporal schedule", "task_id", id, "error", err)
			return ierr.WithError(err).
				WithHint("Failed to delete Temporal schedule").
				Mark(ierr.ErrInternal)
		}
		s.logger.Info(ctx, "temporal schedule deleted successfully", "task_id", id)
	}

	// Mark the task as deleted
	task.Status = types.StatusDeleted
	task.Enabled = false // Disable when deleting
	task.UpdatedAt = clock.Now()
	task.UpdatedBy = types.GetUserID(ctx)

	// Save the deleted task
	err = s.repo.Update(ctx, task)
	if err != nil {
		s.logger.Error(ctx, "failed to mark scheduled task as deleted", "id", id, "error", err)
		return ierr.WithError(err).
			WithHint("Failed to delete scheduled task").
			Mark(ierr.ErrDatabase)
	}

	s.logger.Info(ctx, "scheduled task deleted successfully",
		"id", id,
		"status", task.Status)
	return nil
}

// TriggerForceRun triggers a force run export immediately with optional custom time range
func (s *scheduledTaskService) TriggerForceRun(ctx context.Context, id string, req dto.TriggerForceRunRequest) (*dto.TriggerForceRunResponse, error) {
	if s.scheduler == nil {
		return nil, ierr.NewError("temporal client not configured").
			WithHint("Temporal client is not available").
			Mark(ierr.ErrInternal)
	}

	workflowID, startTime, endTime, mode, err := s.triggerForceRun(ctx, id, req)
	if err != nil {
		s.logger.Error(ctx, "failed to trigger force run", "id", id, "error", err)
		return nil, err
	}

	s.logger.Info(ctx, "force run triggered",
		"id", id,
		"workflow_id", workflowID,
		"start_time", startTime,
		"end_time", endTime,
		"mode", mode)

	resp := &dto.TriggerForceRunResponse{
		WorkflowID: workflowID,
		Message:    "Force run triggered successfully",
		Mode:       mode,
	}
	if !startTime.IsZero() && !endTime.IsZero() {
		resp.StartTime, resp.EndTime = &startTime, &endTime
	}
	return resp, nil
}

// ===== PRIVATE HELPER METHODS =====

// resumeSchedule also records the schedule ID.
func (s *scheduledTaskService) resumeSchedule(ctx context.Context, task *scheduledtask.ScheduledTask) error {
	if err := s.scheduler.Resume(ctx, task); err != nil {
		return err
	}
	if task.Interval == types.ScheduledTaskIntervalManual || task.TemporalScheduleID != "" {
		return nil
	}
	task.TemporalScheduleID = task.ID
	return s.repo.Update(ctx, task)
}

// triggerForceRun executes the export workflow immediately (bypasses schedule)
// If customStart and customEnd are provided, uses those times. Otherwise, calculates automatically.
// Returns: workflowID, startTime, endTime, mode, error
func (s *scheduledTaskService) triggerForceRun(ctx context.Context, taskID string, req dto.TriggerForceRunRequest) (string, time.Time, time.Time, string, error) {
	customStart, customEnd := req.StartTime, req.EndTime
	s.logger.Info(ctx, "triggering force run", "task_id", taskID, "custom_start", customStart, "custom_end", customEnd)

	// Get the task
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return "", time.Time{}, time.Time{}, "", ierr.WithError(err).
			WithHint("Failed to get scheduled task").
			Mark(ierr.ErrDatabase)
	}

	if task.Status == types.StatusArchived || task.Status == types.StatusDeleted {
		return "", time.Time{}, time.Time{}, "", ierr.NewError("cannot run archived or deleted scheduled task").
			WithHint("This scheduled task has been archived or deleted and cannot be run").
			WithReportableDetails(map[string]interface{}{
				"task_id": taskID,
				"status":  task.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	if err := requireAnalyticsRead(ctx, s.rbac, task.EntityType); err != nil {
		return "", time.Time{}, time.Time{}, "", err
	}

	if !task.Enabled {
		return "", time.Time{}, time.Time{}, "", ierr.NewError("export is paused").
			WithHint("Turn the export on to run it.").
			WithReportableDetails(map[string]interface{}{"task_id": taskID}).
			Mark(ierr.ErrValidation)
	}

	if len(req.CustomerExternalIDs) > 0 && task.EntityType != types.ScheduledTaskEntityTypeWalletLedger {
		return "", time.Time{}, time.Time{}, "", ierr.NewError("customer filter is not supported for this export").
			WithHintf("customer_external_ids can only be used with %s exports", types.ScheduledTaskEntityTypeWalletLedger).
			Mark(ierr.ErrValidation)
	}

	if task.EntityType.IsReportSource() {
		for _, bound := range []*time.Time{customStart, customEnd} {
			if bound != nil && !bound.UTC().Equal(bound.UTC().Truncate(24*time.Hour)) {
				return "", time.Time{}, time.Time{}, "", ierr.NewError("report window must be whole UTC days").
					WithHint("start_time and end_time must be midnight UTC for report exports").
					Mark(ierr.ErrValidation)
			}
		}
		if customStart != nil && customEnd != nil && task.SourceConfig != nil {
			days := int(customEnd.Sub(*customStart).Hours()+23) / 24
			if err := checkReportRange(task.SourceConfig.ReportKey, days); err != nil {
				return "", time.Time{}, time.Time{}, "", err
			}
		}
	}

	// Determine time range and mode
	var startTime, endTime time.Time
	var mode string

	noWindow := customStart == nil && customEnd == nil &&
		(task.Schedule != nil || task.Interval == types.ScheduledTaskIntervalManual)

	if noWindow {
		mode = "force run"
	} else if customStart != nil && customEnd != nil {
		// User provided custom time range
		startTime = *customStart
		endTime = *customEnd
		mode = "custom"
		s.logger.Info(ctx, "using custom time range",
			"start_time", startTime,
			"end_time", endTime,
			"duration", endTime.Sub(startTime))
	} else {
		// Calculate automatically based on interval boundaries
		interval := types.ScheduledTaskInterval(task.Interval)
		currentTime := clock.Now()

		// Calculate interval boundaries for force run
		if startTime, endTime, err = scheduledtask.Bounds(interval, nil, currentTime); err != nil {
			return "", time.Time{}, time.Time{}, "", err
		}
		mode = "force run"

		s.logger.Info(ctx, "using automatic time range based on interval boundaries",
			"start_time", startTime,
			"end_time", endTime,
			"current_time", currentTime,
			"interval", interval,
			"duration", endTime.Sub(startTime))
	}

	// Generate task ID and use it as workflow ID (they must be the same)
	// This ensures the task record ID matches the Temporal workflow ID
	exportTaskID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TASK)
	workflowID := exportTaskID

	s.logger.Info(ctx, "triggering force run export",
		"workflow_id", workflowID,
		"export_task_id", exportTaskID,
		"scheduled_task_id", taskID,
		"mode", mode)

	run := exportschedule.Run{
		WorkflowID:          workflowID,
		UserID:              types.GetUserID(ctx),
		CustomerExternalIDs: req.CustomerExternalIDs,
	}
	if !noWindow {
		run.Start = &startTime
		run.End = &endTime
	}

	startedID, err := s.scheduler.StartRun(ctx, task, run)
	if err != nil {
		return "", time.Time{}, time.Time{}, "", err
	}

	s.logger.Info(ctx, "force run triggered",
		"scheduled_task_id", taskID,
		"workflow_id", startedID,
		"start_time", startTime,
		"end_time", endTime,
		"mode", mode)

	return startedID, startTime, endTime, mode, nil
}
