package service

import (
	"context"
	"errors"
	"net/mail"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/email"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/storage"
	temporalClient "github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	exportWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/export"
	invoiceWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/invoice"
	subscriptionWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/subscription"
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
	GetCapabilities(ctx context.Context) *dto.ScheduledTaskCapabilitiesResponse

	CalculateIntervalBoundaries(currentTime time.Time, interval types.ScheduledTaskInterval) (startTime, endTime time.Time)
	ScheduleUpdateBillingPeriod(ctx context.Context) (string, error)
	ScheduleDraftFinalization(ctx context.Context) (string, error)
}

type scheduledTaskService struct {
	repo            scheduledtask.Repository
	connectionRepo  connection.Repository
	temporalClient  temporalClient.TemporalClient
	logger          *logger.Logger
	config          *config.Configuration
	storageResolver storage.Resolver
	rbac            *rbac.RBACService
	analytics       AnalyticsService
	userRepo        user.Repository
}

func NewScheduledTaskService(
	repo scheduledtask.Repository,
	connectionRepo connection.Repository,
	temporalClient temporalClient.TemporalClient,
	logger *logger.Logger,
	config *config.Configuration,
	storageResolver storage.Resolver,
	rbacService *rbac.RBACService,
	analytics AnalyticsService,
	userRepo user.Repository,
) ScheduledTaskService {
	return &scheduledTaskService{
		repo:            repo,
		connectionRepo:  connectionRepo,
		temporalClient:  temporalClient,
		logger:          logger,
		config:          config,
		storageResolver: storageResolver,
		rbac:            rbacService,
		analytics:       analytics,
		userRepo:        userRepo,
	}
}

// emailConfigOrNil normalizes recipients; empty becomes nil.
func emailConfigOrNil(cfg *types.ExportEmailConfig) *types.ExportEmailConfig {
	if cfg == nil {
		return nil
	}
	recipients := types.NormalizeRecipients(cfg.Recipients)
	if len(recipients) == 0 {
		return nil
	}
	return &types.ExportEmailConfig{Recipients: recipients}
}

// Requires recipients be tenant users.
func (s *scheduledTaskService) validateRecipients(ctx context.Context, interval types.ScheduledTaskInterval, cfg *types.ExportEmailConfig) error {
	normalized := emailConfigOrNil(cfg)
	if normalized == nil {
		return nil
	}
	if !s.config.ExportEmailAvailable() {
		return ierr.NewError("email is not configured").
			WithHint("Email is not configured").
			Mark(ierr.ErrValidation)
	}
	if interval == types.ScheduledTaskIntervalHourly {
		return ierr.NewError("email not supported for hourly exports").
			WithHint("Email delivery is not available for hourly exports").
			Mark(ierr.ErrValidation)
	}
	if len(normalized.Recipients) > types.MaxExportEmailRecipients {
		return ierr.NewError("too many recipients").
			WithHintf("At most %d recipients are allowed", types.MaxExportEmailRecipients).
			Mark(ierr.ErrValidation)
	}
	if types.GetTenantID(ctx) == "" {
		return ierr.NewError("tenant missing from context").Mark(ierr.ErrInternal)
	}
	if s.userRepo == nil {
		return ierr.NewError("user repository not configured").Mark(ierr.ErrInternal)
	}
	for _, addr := range normalized.Recipients {
		masked := email.MaskAddress(addr)
		if parsed, err := mail.ParseAddress(addr); err != nil || parsed.Address != addr {
			return ierr.NewError("invalid recipient").
				WithHintf("%s is not a valid email address", masked).
				Mark(ierr.ErrValidation)
		}
		if _, err := s.userRepo.GetByEmail(ctx, addr); err != nil {
			if !ierr.IsNotFound(err) {
				return ierr.NewError("failed to verify recipient").
					WithHint("Could not verify email recipients").
					Mark(ierr.ErrDatabase)
			}
			return ierr.NewError("recipient is not a user").
				WithHintf("%s is not a user in this account", masked).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
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
	if err := s.validateRecipients(ctx, req.Interval, req.EmailConfig); err != nil {
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
	managedCopy := req.ConnectionID == ""
	if managedCopy {
		jobConfig, err = s.flexpriceJobConfig(ctx, req, taskID)
	} else {
		jobConfig, err = s.connectionJobConfig(ctx, req)
	}
	if err != nil {
		return nil, err
	}

	// Manual exports skip Temporal
	var temporalScheduleID string
	if req.Enabled && s.temporalClient != nil && req.Interval != types.ScheduledTaskIntervalManual {
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
		ManagedCopy:        managedCopy,
		Schedule:           req.Schedule,
		SourceConfig:       req.SourceConfig,
		EmailConfig:        emailConfigOrNil(req.EmailConfig),
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
		err = s.startScheduledTask(ctx, task)
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

	// Get existing task
	task, err := s.repo.Get(ctx, id)
	if err != nil {
		s.logger.Error(ctx, "failed to get scheduled task for update", "id", id, "error", err)
		return nil, ierr.WithError(err).
			WithHint("Scheduled task not found").
			Mark(ierr.ErrNotFound)
	}

	if err := requireAnalyticsRead(ctx, s.rbac, task.EntityType); err != nil {
		return nil, err
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

	if req.Name != nil || req.Schedule != nil || req.EmailConfig != nil {
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
	if s.temporalClient != nil {
		if newEnabled {
			// Resume/Start the schedule
			s.logger.Info(ctx, "resuming temporal schedule", "task_id", id)
			err = s.resumeScheduledTask(ctx, task)
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
			err = s.pauseScheduledTask(ctx, task)
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
	if s.temporalClient != nil {
		s.logger.Info(ctx, "deleting temporal schedule", "task_id", id)
		err = s.deleteTemporalSchedule(ctx, task)
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
	if s.temporalClient == nil {
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
	s.logger.Info(ctx, "creating new temporal schedule", "task_id", task.ID)

	scheduleID := task.ID
	if task.Interval == types.ScheduledTaskIntervalManual {
		return nil
	}

	scheduleSpec, err := s.scheduleSpec(task)
	if err != nil {
		return err
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
		WorkflowExecutionTimeout: 60 * time.Minute,
		WorkflowRunTimeout:       60 * time.Minute,
		WorkflowTaskTimeout:      60 * time.Minute,
	}

	scheduleOptions := models.CreateScheduleOptions{
		ID:     scheduleID,
		Spec:   scheduleSpec,
		Action: action,
		Paused: false,
	}
	timezoneScheduleOptions(task, &scheduleOptions)

	_, err = s.temporalClient.CreateSchedule(ctx, scheduleOptions)
	if err != nil {
		s.logger.Error(ctx, "failed to create temporal schedule", "error", err)
		return ierr.WithError(err).
			WithHint("Failed to create Temporal schedule").
			Mark(ierr.ErrInternal)
	}

	s.logger.Info(ctx, "temporal schedule created successfully",
		"task_id", task.ID,
		"schedule_id", scheduleID,
		"cron", scheduleSpec.CronExpressions)

	return nil
}

// pauseScheduledTask pauses the Temporal schedule for the task
// This function safely handles cases where the schedule doesn't exist
func (s *scheduledTaskService) pauseScheduledTask(ctx context.Context, task *scheduledtask.ScheduledTask) error {
	if task.TemporalScheduleID == "" {
		s.logger.Info(ctx, "no temporal schedule to pause", "task_id", task.ID)
		return nil
	}

	handle := s.temporalClient.GetScheduleHandle(ctx, task.TemporalScheduleID)
	err := handle.Pause(ctx, client.SchedulePauseOptions{})
	if err != nil {
		s.logger.Error(ctx, "failed to pause schedule", "error", err)
		return ierr.WithError(err).
			WithHint("Failed to pause Temporal schedule").
			Mark(ierr.ErrInternal)
	}

	s.logger.Info(ctx, "temporal schedule paused successfully", "task_id", task.ID)
	return nil
}

// resumeScheduledTask resumes or creates a Temporal schedule for the task
// This function intelligently handles:
// - Resuming paused schedules
// - Creating new schedules if they don't exist
// - Handling schedules that are already running
func (s *scheduledTaskService) resumeScheduledTask(ctx context.Context, task *scheduledtask.ScheduledTask) error {
	if task.Interval == types.ScheduledTaskIntervalManual {
		return nil
	}
	if task.TemporalScheduleID == "" {
		s.logger.Info(ctx, "no temporal schedule ID, creating new schedule", "task_id", task.ID)
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
		s.logger.Info(ctx, "schedule not found, creating new one", "task_id", task.ID, "schedule_id", scheduleID)
		return s.startScheduledTask(ctx, task)
	}

	// Check if schedule is paused
	if schedule.Schedule.State.Paused {
		s.logger.Info(ctx, "resuming paused schedule", "task_id", task.ID, "schedule_id", scheduleID)
		err = handle.Unpause(ctx, client.ScheduleUnpauseOptions{})
		if err != nil {
			s.logger.Error(ctx, "failed to unpause schedule", "error", err)
			return ierr.WithError(err).
				WithHint("Failed to resume Temporal schedule").
				Mark(ierr.ErrInternal)
		}
		s.logger.Info(ctx, "temporal schedule resumed successfully", "task_id", task.ID)
		return nil
	}

	// Schedule exists and is not paused
	s.logger.Info(ctx, "temporal schedule already running", "task_id", task.ID, "schedule_id", scheduleID)
	return nil
}

// deleteTemporalSchedule deletes the Temporal schedule for the task
func (s *scheduledTaskService) deleteTemporalSchedule(ctx context.Context, task *scheduledtask.ScheduledTask) error {
	if task.TemporalScheduleID == "" {
		s.logger.Info(ctx, "no temporal schedule to delete", "task_id", task.ID)
		return nil
	}

	handle := s.temporalClient.GetScheduleHandle(ctx, task.TemporalScheduleID)
	err := handle.Delete(ctx)
	if err != nil {
		// Deleting a schedule is allowed to be idempotent. If it's already gone, treat it as success.
		var notFound *serviceerror.NotFound
		if errors.As(err, &notFound) {
			s.logger.Info(ctx, "temporal schedule not found; treating as deleted", "task_id", task.ID, "schedule_id", task.TemporalScheduleID)
			return nil
		}

		s.logger.Error(ctx, "failed to delete temporal schedule", "schedule_id", task.TemporalScheduleID, "error", err)
		return ierr.WithError(err).
			WithHint("Failed to delete Temporal schedule").
			Mark(ierr.ErrInternal)
	}

	s.logger.Info(ctx, "temporal schedule deleted successfully",
		"task_id", task.ID,
		"schedule_id", task.TemporalScheduleID)
	return nil
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

	if err := requireAnalyticsRead(ctx, s.rbac, task.EntityType); err != nil {
		return "", time.Time{}, time.Time{}, "", err
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
		startTime, endTime = s.CalculateIntervalBoundaries(currentTime, interval)
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

	workflowOptions := models.StartWorkflowOptions{
		ID:                       workflowID,
		TaskQueue:                string(types.TemporalTaskQueueExport),
		WorkflowExecutionTimeout: 60 * time.Minute, // 60 minutes for export tasks
		WorkflowRunTimeout:       60 * time.Minute, // 60 minutes for single workflow run
		WorkflowTaskTimeout:      60 * time.Minute, // 60 minutes for workflow task processing
	}

	input := exportWorkflows.ExecuteExportWorkflowInput{
		ScheduledTaskID:     taskID,
		TenantID:            task.TenantID,
		EnvID:               task.EnvironmentID,
		UserID:              types.GetUserID(ctx), // Get user ID from context for force runs
		CustomerExternalIDs: req.CustomerExternalIDs,
	}
	if !noWindow {
		input.CustomStartTime = &startTime
		input.CustomEndTime = &endTime
	}

	workflowRun, err := s.temporalClient.StartWorkflow(ctx, workflowOptions, exportWorkflows.ExecuteExportWorkflow, input)
	if err != nil {
		s.logger.Error(ctx, "failed to start export workflow", "error", err)
		return "", time.Time{}, time.Time{}, "", ierr.WithError(err).
			WithHint("Failed to start export workflow").
			Mark(ierr.ErrInternal)
	}

	s.logger.Info(ctx, "force run triggered",
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
	case types.ScheduledTaskIntervalEvery30Minutes:
		return "*/30 * * * *" // Every 30 minutes
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
		s.logger.Error(ctx, "failed to create temporal schedule", "error", err)
		return "", ierr.WithError(err).
			WithHint("Failed to create Temporal schedule").
			Mark(ierr.ErrInternal)
	}

	return "Triggered update billing period workflow successfully", nil
}

// ScheduleDraftFinalization creates a Temporal schedule that runs every 30 minutes
// to finalize draft invoices whose finalization delay has elapsed.
func (s *scheduledTaskService) ScheduleDraftFinalization(ctx context.Context) (string, error) {
	scheduleID := types.GenerateUUIDWithPrefix("schtask_draft_finalize")

	cronExpr := s.getCronExpression(types.ScheduledTaskIntervalEvery30Minutes)

	scheduleSpec := client.ScheduleSpec{
		CronExpressions: []string{cronExpr}, // Every 30 minutes
	}

	action := &client.ScheduleWorkflowAction{
		Workflow: invoiceWorkflows.ScheduleDraftFinalizationWorkflow,
		Args: []interface{}{
			invoiceModels.ScheduleDraftFinalizationWorkflowInput{
				BatchSize: 100,
			},
		},
		TaskQueue:                string(types.TemporalTaskQueueInvoice),
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
		s.logger.Error(ctx, "failed to create draft finalization schedule", "error", err)
		return "", ierr.WithError(err).
			WithHint("Failed to create draft finalization schedule").
			Mark(ierr.ErrInternal)
	}

	return "Triggered draft finalization schedule successfully", nil
}

func (s *scheduledTaskService) GetCapabilities(_ context.Context) *dto.ScheduledTaskCapabilitiesResponse {
	return &dto.ScheduledTaskCapabilitiesResponse{
		Email: dto.ExportEmailCapability{
			Available:     s.config.ExportEmailAvailable(),
			MaxRecipients: types.MaxExportEmailRecipients,
		},
	}
}
