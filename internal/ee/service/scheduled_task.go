package service

import (
	"context"
	"net/mail"
	"time"

	cockroachErrors "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/clock"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	domainTask "github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/email"
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
	ListScheduledTasks(ctx context.Context, filter *types.QueryFilter, connectionID string, entityType types.ScheduledTaskEntityType, interval types.ScheduledTaskInterval, enabled string, origin, excludeOrigin types.ExportOrigin) (*dto.ListScheduledTasksResponse, error)
	UpdateScheduledTask(ctx context.Context, id string, req dto.UpdateScheduledTaskRequest) (*dto.ScheduledTaskResponse, error)
	DeleteScheduledTask(ctx context.Context, id string) error
	TriggerForceRun(ctx context.Context, id string, req dto.TriggerForceRunRequest) (*dto.TriggerForceRunResponse, error)
	GetCapabilities(ctx context.Context) *dto.ScheduledTaskCapabilitiesResponse
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
	userRepo        user.Repository
	customerRepo    customer.Repository
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
	userRepo user.Repository,
	customerRepo customer.Repository,
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
		userRepo:        userRepo,
		customerRepo:    customerRepo,
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
	if cfg != nil {
		for _, raw := range cfg.Recipients {
			if types.NormalizeEmail(raw) == "" {
				return ierr.NewError("empty recipient").
					WithHint("Recipient can't be empty").
					Mark(ierr.ErrValidation)
			}
		}
	}
	normalized := emailConfigOrNil(cfg)
	if normalized == nil {
		return nil
	}
	if !s.config.ExportEmailAvailable() {
		return ierr.NewError("email is not configured").
			WithHint("Email is not configured").
			Mark(ierr.ErrValidation)
	}
	if !interval.SupportsEmail() {
		return ierr.NewError("email not supported for this interval").
			WithHint("Email delivery is available only for daily, weekly, monthly and manual exports").
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

// Dry-runs the filter and checks the report is per customer.
func (s *scheduledTaskService) validateCustomerFilter(ctx context.Context, req dto.CreateScheduledTaskRequest) error {
	cf := req.SourceConfig.GetCustomerFilter()
	if cf == nil {
		return nil
	}
	if req.EntityType == types.ScheduledTaskEntityTypeReport && !reportHasCustomerDimension(req.SourceConfig.ReportKey) {
		return ierr.NewError("report has no customer dimension").
			WithHint("This data can't be limited to customers yet.").
			Mark(ierr.ErrValidation)
	}
	if s.customerRepo == nil {
		return ierr.NewError("customer repository not configured").Mark(ierr.ErrInternal)
	}
	if _, err := s.customerRepo.Count(ctx, cf); err != nil {
		if ierr.IsValidation(err) {
			hint := "The customer filter is invalid"
			if hints := cockroachErrors.GetAllHints(err); len(hints) > 0 {
				hint = hints[0]
			}
			return ierr.NewError("invalid customer filter").
				WithHint(hint).
				Mark(ierr.ErrValidation)
		}
		return err
	}
	return nil
}

func reportHasCustomerDimension(key string) bool {
	entry, ok := analytics.LookupReport(key)
	if !ok {
		return false
	}
	for _, d := range entry.Dimensions {
		if d.Key == "customer_id" {
			return true
		}
	}
	return false
}

func (s *scheduledTaskService) validateSource(ctx context.Context, req dto.CreateScheduledTaskRequest) error {
	if err := s.validateCustomerFilter(ctx, req); err != nil {
		return err
	}
	if !req.EntityType.IsReportSource() {
		return nil
	}
	if s.analytics == nil {
		return ierr.NewError("analytics service not configured").Mark(ierr.ErrInternal)
	}
	if req.EntityType == types.ScheduledTaskEntityTypeAnalyticsView {
		grain, err := s.analytics.ValidateViewSource(ctx, req.SourceConfig.ViewID, intervalWindowDays(req.Interval))
		if err != nil {
			return err
		}
		return checkGrain(grain, req.Interval)
	}
	if err := s.analytics.ValidateReportSource(ctx, req.SourceConfig.ReportKey, req.SourceConfig.ReportParams, intervalWindowDays(req.Interval)); err != nil {
		return err
	}
	return checkReportGrain(req.SourceConfig.ReportKey, req.SourceConfig.ReportParams, req.Interval)
}

// Partial buckets would repeat across runs.
func checkReportGrain(key string, params *types.ReportParams, interval types.ScheduledTaskInterval) error {
	if params == nil || params.Shape != string(types.ShapeTimeseries) {
		return nil
	}
	grain := types.Grain(params.Grain)
	if entry, ok := analytics.LookupReport(key); grain == "" && ok && len(entry.Grains) > 0 {
		grain = entry.Grains[0]
	}
	return checkGrain(grain, interval)
}

func checkGrain(grain types.Grain, interval types.ScheduledTaskInterval) error {
	grainDays := map[types.Grain]int{types.GrainWeek: 7, types.GrainMonth: 31}[grain]
	windowDays := intervalWindowDays(interval)
	if windowDays == 0 || grainDays <= windowDays {
		return nil
	}
	return ierr.NewErrorf("grain %s is coarser than the %s interval", grain, interval).
		WithHint("Choose a grain no coarser than the export interval").
		Mark(ierr.ErrValidation)
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

// checkRunWindow applies report window rules.
func (s *scheduledTaskService) checkRunWindow(ctx context.Context, entity types.ScheduledTaskEntityType, source *types.ExportSourceConfig, start, end *time.Time) error {
	if !entity.IsReportSource() {
		return nil
	}
	for _, bound := range []*time.Time{start, end} {
		if bound != nil && !bound.UTC().Equal(bound.UTC().Truncate(24*time.Hour)) {
			return ierr.NewError("report window must be whole UTC days").
				WithHint("start_time and end_time must be midnight UTC for report exports").
				Mark(ierr.ErrValidation)
		}
	}
	if start != nil && end != nil && source != nil {
		days := int(end.Sub(*start).Hours()+23) / 24
		if entity != types.ScheduledTaskEntityTypeAnalyticsView {
			return checkReportRange(source.ReportKey, days)
		}
		if s.analytics == nil {
			return ierr.NewError("analytics service not configured").Mark(ierr.ErrInternal)
		}
		_, err := s.analytics.ValidateViewSource(ctx, source.ViewID, days)
		return err
	}
	return nil
}

func (s *scheduledTaskService) requireCustomerFilterEnabled(req dto.CreateScheduledTaskRequest) error {
	if req.SourceConfig.GetCustomerFilter() == nil && req.EntityType != types.ScheduledTaskEntityTypeCustomers {
		return nil
	}
	if s.config != nil && s.config.Exports.CustomerFilterEnabled {
		return nil
	}
	return ierr.NewError("customer filtered exports are not enabled").
		WithHint("Exports limited to customers are not available yet.").
		Mark(ierr.ErrValidation)
}

// CreateScheduledTask creates a new scheduled task
func (s *scheduledTaskService) CreateScheduledTask(ctx context.Context, req dto.CreateScheduledTaskRequest) (*dto.ScheduledTaskResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := s.requireCustomerFilterEnabled(req); err != nil {
		return nil, err
	}
	if err := requireAnalyticsRead(ctx, s.rbac, req.EntityType); err != nil {
		return nil, err
	}
	if req.Origin == "" {
		req.Origin = types.ExportOriginConsole
	}
	if req.RunWindow != nil {
		window := req.ForceRunRequest()
		if err := s.checkRunWindow(ctx, req.EntityType, req.SourceConfig, window.StartTime, window.EndTime); err != nil {
			return nil, err
		}
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
		EmailConfig:        emailConfigOrNil(req.EmailConfig),
		Origin:             req.Origin,
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

	resp := dto.ToScheduledTaskResponse(task)
	if req.RunNow {
		run, err := s.runNow(ctx, task, req.ForceRunRequest())
		if err != nil {
			s.logger.Error(ctx, "run_now failed", "error", err, "scheduled_task_id", task.ID)
			// Rollback: delete the created task
			_ = s.repo.Delete(ctx, task.ID)
			return nil, err
		}
		resp.Run = run
	}
	return resp, nil
}

func (s *scheduledTaskService) runNow(ctx context.Context, task *scheduledtask.ScheduledTask, req dto.TriggerForceRunRequest) (*dto.TriggerForceRunResponse, error) {
	if s.temporalClient == nil {
		return nil, ierr.NewError("temporal client not configured").
			WithHint("The export could not be started. Try again later.").
			Mark(ierr.ErrInternal)
	}
	workflowID, startTime, endTime, mode, err := s.startForceRun(ctx, task, req)
	if err != nil {
		return nil, err
	}
	return forceRunResponse(workflowID, startTime, endTime, mode), nil
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
	if err := requireAnalyticsRead(ctx, s.rbac, task.EntityType); err != nil {
		return nil, err
	}

	return dto.ToScheduledTaskResponse(task), nil
}

// ListScheduledTasks retrieves a list of scheduled tasks
func (s *scheduledTaskService) ListScheduledTasks(ctx context.Context, filter *types.QueryFilter, connectionID string, entityType types.ScheduledTaskEntityType, interval types.ScheduledTaskInterval, enabled string, origin, excludeOrigin types.ExportOrigin) (*dto.ListScheduledTasksResponse, error) {
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
		Origin:        origin,
		ExcludeOrigin: excludeOrigin,
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

	resp := dto.ToScheduledTaskListResponse(tasks, pagination)
	for _, item := range resp.Items {
		if requireAnalyticsRead(ctx, s.rbac, item.EntityType) != nil {
			item.SourceConfig = nil
		}
	}
	return resp, nil
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

	return forceRunResponse(workflowID, startTime, endTime, mode), nil
}

func forceRunResponse(workflowID string, startTime, endTime time.Time, mode string) *dto.TriggerForceRunResponse {
	resp := &dto.TriggerForceRunResponse{
		WorkflowID: workflowID,
		Message:    "Force run triggered successfully",
		Mode:       mode,
	}
	if !startTime.IsZero() && !endTime.IsZero() {
		resp.StartTime, resp.EndTime = &startTime, &endTime
	}
	return resp
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
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return "", time.Time{}, time.Time{}, "", ierr.WithError(err).
			WithHint("Failed to get scheduled task").
			Mark(ierr.ErrDatabase)
	}
	return s.startForceRun(ctx, task, req)
}

// startForceRun starts the export workflow for a loaded task.
func (s *scheduledTaskService) startForceRun(ctx context.Context, task *scheduledtask.ScheduledTask, req dto.TriggerForceRunRequest) (string, time.Time, time.Time, string, error) {
	taskID := task.ID
	customStart, customEnd := req.StartTime, req.EndTime
	s.logger.Info(ctx, "triggering force run", "task_id", taskID, "custom_start", customStart, "custom_end", customEnd)

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

	if err := s.checkRunWindow(ctx, task.EntityType, task.SourceConfig, customStart, customEnd); err != nil {
		return "", time.Time{}, time.Time{}, "", err
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

func (s *scheduledTaskService) GetCapabilities(_ context.Context) *dto.ScheduledTaskCapabilitiesResponse {
	return &dto.ScheduledTaskCapabilitiesResponse{
		Email: dto.ExportEmailCapability{
			Available:     s.config.ExportEmailAvailable(),
			MaxRecipients: types.MaxExportEmailRecipients,
		},
	}
}
