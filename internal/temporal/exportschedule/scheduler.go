package exportschedule

import (
	"context"
	"errors"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	temporalClient "github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	exportWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/export"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	// CatchupWindow bounds missed-run replay.
	CatchupWindow = 24 * time.Hour

	workflowTimeout     = 60 * time.Minute
	workflowTaskTimeout = 60 * time.Minute
)

// Scheduler manages per-export Temporal schedules.
type Scheduler interface {
	// Create creates the export schedule.
	Create(ctx context.Context, task *scheduledtask.ScheduledTask) error
	// UpdateSpec reports whether the schedule exists.
	UpdateSpec(ctx context.Context, task *scheduledtask.ScheduledTask) (bool, error)
	// Pause ignores a missing schedule.
	Pause(ctx context.Context, task *scheduledtask.ScheduledTask) error
	// Resume creates a missing schedule.
	Resume(ctx context.Context, task *scheduledtask.ScheduledTask) error
	// Delete ignores a missing schedule.
	Delete(ctx context.Context, task *scheduledtask.ScheduledTask) error
	// StartRun returns the started workflow ID.
	StartRun(ctx context.Context, task *scheduledtask.ScheduledTask, run Run) (string, error)
}

// Run describes one on-demand export run.
type Run struct {
	WorkflowID          string
	UserID              string
	Start               *time.Time
	End                 *time.Time
	CustomerExternalIDs []string
}

type scheduler struct {
	client temporalClient.TemporalClient
	logger *logger.Logger
}

// NewScheduler returns nil without Temporal.
func NewScheduler(c temporalClient.TemporalClient, log *logger.Logger) Scheduler {
	if c == nil {
		return nil
	}
	return &scheduler{client: c, logger: log}
}

func (s *scheduler) Create(ctx context.Context, task *scheduledtask.ScheduledTask) error {
	if task.Interval == types.ScheduledTaskIntervalManual {
		return nil
	}
	s.logger.Info(ctx, "creating new temporal schedule", "task_id", task.ID)

	d, err := build(task)
	if err != nil {
		return err
	}
	opts := models.CreateScheduleOptions{
		ID:     task.ID,
		Spec:   d.spec,
		Action: d.action,
		Paused: false,
	}
	if d.policy != nil {
		opts.Overlap = d.policy.Overlap
		opts.CatchupWindow = d.policy.CatchupWindow
	}

	if _, err := s.client.CreateSchedule(ctx, opts); err != nil {
		s.logger.Error(ctx, "failed to create temporal schedule", "error", err)
		return ierr.WithError(err).
			WithHint("Failed to create Temporal schedule").
			Mark(ierr.ErrInternal)
	}

	s.logger.Info(ctx, "temporal schedule created successfully",
		"task_id", task.ID,
		"schedule_id", task.ID,
		"cron", d.spec.CronExpressions)
	return nil
}

func (s *scheduler) UpdateSpec(ctx context.Context, task *scheduledtask.ScheduledTask) (bool, error) {
	if task.Interval == types.ScheduledTaskIntervalManual {
		return false, nil
	}
	d, err := build(task)
	if err != nil {
		return false, err
	}
	err = s.client.GetScheduleHandle(ctx, task.ID).Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(in client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			spec := d.spec
			in.Description.Schedule.Spec = &spec
			if d.policy != nil {
				in.Description.Schedule.Policy = d.applyPolicy(in.Description.Schedule.Policy)
			}
			return &client.ScheduleUpdate{Schedule: &in.Description.Schedule}, nil
		},
	})
	if isNotFound(err) {
		s.logger.Info(ctx, "temporal schedule not found; skipping spec sync", "schedule_id", task.ID)
		return false, nil
	}
	if err != nil {
		return false, ierr.WithError(err).
			WithHint("Failed to update Temporal schedule").
			Mark(ierr.ErrInternal)
	}
	return true, nil
}

func (s *scheduler) Pause(ctx context.Context, task *scheduledtask.ScheduledTask) error {
	if task.Interval == types.ScheduledTaskIntervalManual {
		return nil
	}
	err := s.client.GetScheduleHandle(ctx, task.ID).Pause(ctx, client.SchedulePauseOptions{})
	if isNotFound(err) {
		s.logger.Info(ctx, "no temporal schedule to pause", "task_id", task.ID)
		return nil
	}
	if err != nil {
		s.logger.Error(ctx, "failed to pause schedule", "error", err)
		return ierr.WithError(err).
			WithHint("Failed to pause Temporal schedule").
			Mark(ierr.ErrInternal)
	}
	s.logger.Info(ctx, "temporal schedule paused successfully", "task_id", task.ID)
	return nil
}

func (s *scheduler) Resume(ctx context.Context, task *scheduledtask.ScheduledTask) error {
	if task.Interval == types.ScheduledTaskIntervalManual {
		return nil
	}
	handle := s.client.GetScheduleHandle(ctx, task.ID)
	desc, err := handle.Describe(ctx)
	if isNotFound(err) {
		s.logger.Info(ctx, "schedule not found, creating new one", "task_id", task.ID, "schedule_id", task.ID)
		return s.Create(ctx, task)
	}
	if err != nil {
		s.logger.Error(ctx, "failed to describe schedule", "error", err, "schedule_id", task.ID)
		return ierr.WithError(err).
			WithHint("Failed to resume Temporal schedule").
			Mark(ierr.ErrInternal)
	}

	if !desc.Schedule.State.Paused {
		s.logger.Info(ctx, "temporal schedule already running", "task_id", task.ID, "schedule_id", task.ID)
		return nil
	}
	s.logger.Info(ctx, "resuming paused schedule", "task_id", task.ID, "schedule_id", task.ID)
	if err := handle.Unpause(ctx, client.ScheduleUnpauseOptions{}); err != nil {
		s.logger.Error(ctx, "failed to unpause schedule", "error", err)
		return ierr.WithError(err).
			WithHint("Failed to resume Temporal schedule").
			Mark(ierr.ErrInternal)
	}
	s.logger.Info(ctx, "temporal schedule resumed successfully", "task_id", task.ID)
	return nil
}

func (s *scheduler) Delete(ctx context.Context, task *scheduledtask.ScheduledTask) error {
	if task.Interval == types.ScheduledTaskIntervalManual {
		return nil
	}
	err := s.client.GetScheduleHandle(ctx, task.ID).Delete(ctx)
	if isNotFound(err) {
		s.logger.Info(ctx, "temporal schedule not found; treating as deleted", "task_id", task.ID, "schedule_id", task.ID)
		return nil
	}
	if err != nil {
		s.logger.Error(ctx, "failed to delete temporal schedule", "schedule_id", task.ID, "error", err)
		return ierr.WithError(err).
			WithHint("Failed to delete Temporal schedule").
			Mark(ierr.ErrInternal)
	}
	s.logger.Info(ctx, "temporal schedule deleted successfully", "task_id", task.ID, "schedule_id", task.ID)
	return nil
}

func (s *scheduler) StartRun(ctx context.Context, task *scheduledtask.ScheduledTask, run Run) (string, error) {
	opts := models.StartWorkflowOptions{
		ID:                       run.WorkflowID,
		TaskQueue:                string(types.TemporalTaskQueueExport),
		WorkflowExecutionTimeout: workflowTimeout,
		WorkflowRunTimeout:       workflowTimeout,
		WorkflowTaskTimeout:      workflowTaskTimeout,
	}
	input := exportWorkflows.ExecuteExportWorkflowInput{
		ScheduledTaskID:     task.ID,
		TenantID:            task.TenantID,
		EnvID:               task.EnvironmentID,
		UserID:              run.UserID,
		CustomStartTime:     run.Start,
		CustomEndTime:       run.End,
		CustomerExternalIDs: run.CustomerExternalIDs,
	}

	wf, err := s.client.StartWorkflow(ctx, opts, exportWorkflows.ExecuteExportWorkflow, input)
	if err != nil {
		s.logger.Error(ctx, "failed to start export workflow", "error", err)
		return "", ierr.WithError(err).
			WithHint("Failed to start export workflow").
			Mark(ierr.ErrInternal)
	}
	s.logger.Info(ctx, "export workflow started",
		"scheduled_task_id", task.ID,
		"workflow_id", wf.GetID(),
		"run_id", wf.GetRunID())
	return wf.GetID(), nil
}

// desired is an export's target schedule.
type desired struct {
	spec   client.ScheduleSpec
	action *client.ScheduleWorkflowAction
	// nil keeps Temporal's default policies.
	policy *client.SchedulePolicies
}

// build derives every schedule setting.
func build(task *scheduledtask.ScheduledTask) (desired, error) {
	cron, err := scheduledtask.Cron(task.Interval, task.Schedule)
	if err != nil {
		return desired{}, err
	}
	d := desired{
		spec: client.ScheduleSpec{CronExpressions: []string{cron}},
		action: &client.ScheduleWorkflowAction{
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
			WorkflowExecutionTimeout: workflowTimeout,
			WorkflowRunTimeout:       workflowTimeout,
			WorkflowTaskTimeout:      workflowTaskTimeout,
		},
	}
	if task.Schedule != nil {
		d.spec.TimeZoneName = task.Schedule.Timezone
		d.policy = &client.SchedulePolicies{
			Overlap:       enums.SCHEDULE_OVERLAP_POLICY_BUFFER_ALL,
			CatchupWindow: CatchupWindow,
		}
	}
	return d, nil
}

// applyPolicy keeps unrelated policy fields.
func (d desired) applyPolicy(current *client.SchedulePolicies) *client.SchedulePolicies {
	policy := client.SchedulePolicies{}
	if current != nil {
		policy = *current
	}
	policy.Overlap = d.policy.Overlap
	policy.CatchupWindow = d.policy.CatchupWindow
	return &policy
}

func isNotFound(err error) bool {
	var notFound *serviceerror.NotFound
	return errors.As(err, &notFound)
}
