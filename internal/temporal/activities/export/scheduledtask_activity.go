package export

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// IntervalBoundaryCalculator defines the interface for calculating interval boundaries
type IntervalBoundaryCalculator interface {
	CalculateIntervalBoundaries(currentTime time.Time, interval types.ScheduledTaskInterval) (startTime, endTime time.Time)
}

// ScheduledTaskActivity handles scheduled task operations
type ScheduledTaskActivity struct {
	scheduledTaskRepo  scheduledtask.Repository
	taskRepo           task.Repository
	logger             *logger.Logger
	boundaryCalculator IntervalBoundaryCalculator
}

// NewScheduledTaskActivity creates a new scheduled task activity
func NewScheduledTaskActivity(
	scheduledTaskRepo scheduledtask.Repository,
	taskRepo task.Repository,
	logger *logger.Logger,
	boundaryCalculator IntervalBoundaryCalculator,
) *ScheduledTaskActivity {
	return &ScheduledTaskActivity{
		scheduledTaskRepo:  scheduledTaskRepo,
		taskRepo:           taskRepo,
		logger:             logger,
		boundaryCalculator: boundaryCalculator,
	}
}

// GetScheduledTaskDetailsInput represents input for getting task details
type GetScheduledTaskDetailsInput struct {
	ScheduledTaskID       string
	TenantID              string
	EnvID                 string
	WorkflowExecutionTime time.Time // Actual workflow execution time
	// Optional custom time range for force runs
	CustomStartTime *time.Time
	CustomEndTime   *time.Time
}

// ScheduledTaskDetails contains task details needed for export
type ScheduledTaskDetails struct {
	ScheduledTaskID string
	TenantID        string
	EnvID           string
	EntityType      types.ScheduledTaskEntityType
	Enabled         bool
	ConnectionID    string
	StartTime       time.Time
	EndTime         time.Time
	JobConfig       *types.S3JobConfig
}

// GetScheduledTaskDetails fetches scheduled task and calculates time range
func (a *ScheduledTaskActivity) GetScheduledTaskDetails(ctx context.Context, input GetScheduledTaskDetailsInput) (*ScheduledTaskDetails, error) {
	a.logger.Infow("fetching scheduled task details",
		"scheduled_task_id", input.ScheduledTaskID,
		"tenant_id", input.TenantID,
		"env_id", input.EnvID)

	// Add tenant and env to context for repository query
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvID)

	// Get scheduled task
	task, err := a.scheduledTaskRepo.Get(ctx, input.ScheduledTaskID)
	if err != nil {
		a.logger.Errorw("failed to get scheduled task", "error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch scheduled task").
			Mark(ierr.ErrDatabase)
	}

	// Parse job config
	jobConfig, err := task.GetS3JobConfig()
	if err != nil {
		a.logger.Errorw("failed to parse job config", "error", err)
		return nil, ierr.WithError(err).
			WithHint("Invalid job configuration").
			Mark(ierr.ErrValidation)
	}

	// Use workflow execution time for calculating interval boundaries
	// This ensures retries use the same time boundaries as the original execution
	executionTime := input.WorkflowExecutionTime
	a.logger.Infow("execution time", "execution_time", executionTime)
	if executionTime.IsZero() {
		// Fallback to current time if not provided (for backward compatibility)
		executionTime = time.Now()
	}

	// Calculate start and end times
	var startTime, endTime time.Time

	// Check if custom time range is provided (for force runs)
	if input.CustomStartTime != nil && input.CustomEndTime != nil {
		// Use custom time range for force runs
		startTime = *input.CustomStartTime
		endTime = *input.CustomEndTime
		a.logger.Infow("using custom time range for force run",
			"scheduled_task_id", input.ScheduledTaskID,
			"start_time", startTime,
			"end_time", endTime)
	} else {
		// For scheduled runs: uses interval boundaries or incremental sync
		startTime, endTime = a.calculateTimeRange(ctx, task, executionTime)
	}

	a.logger.Infow("scheduled task details retrieved",
		"scheduled_task_id", input.ScheduledTaskID,
		"entity_type", task.EntityType,
		"interval", task.Interval,
		"execution_time", executionTime,
		"start_time", startTime,
		"end_time", endTime)

	return &ScheduledTaskDetails{
		ScheduledTaskID: task.ID,
		TenantID:        task.TenantID,
		EnvID:           task.EnvironmentID,
		EntityType:      task.EntityType,
		Enabled:         task.Enabled,
		ConnectionID:    task.ConnectionID,
		StartTime:       startTime,
		EndTime:         endTime,
		JobConfig:       jobConfig,
	}, nil
}

// calculateTimeRange determines the start and end time for the export
// Always exports the previous completed interval based on execution time
// Each run is independent and deterministic - no catch-up for missed runs
// Example: Run at 12:15 with hourly interval always exports 11:00-12:00
func (a *ScheduledTaskActivity) calculateTimeRange(ctx context.Context, task *scheduledtask.ScheduledTask, currentTime time.Time) (time.Time, time.Time) {
	interval := types.ScheduledTaskInterval(task.Interval)

	// Get current interval boundaries
	// Example: currentTime = 12:15 → returns 12:00-13:00 (current interval)
	currentIntervalStart, _ := a.boundaryCalculator.CalculateIntervalBoundaries(currentTime, interval)

	// We want to export the PREVIOUS completed interval
	// So use current interval start as the end time
	endTime := currentIntervalStart

	// Calculate start of previous interval based on interval type
	var startTime time.Time
	switch interval {
	case types.ScheduledTaskIntervalCustom:
		startTime = endTime.Add(-10 * time.Minute)
	case types.ScheduledTaskIntervalHourly:
		startTime = endTime.Add(-1 * time.Hour)
	case types.ScheduledTaskIntervalDaily:
		startTime = endTime.AddDate(0, 0, -1)
	default:
		startTime = endTime.AddDate(0, 0, -1)
	}

	a.logger.Infow("calculated export time range",
		"scheduled_task_id", task.ID,
		"interval", interval,
		"current_time", currentTime,
		"start_time", startTime,
		"end_time", endTime,
		"duration", endTime.Sub(startTime))

	return startTime, endTime
}
