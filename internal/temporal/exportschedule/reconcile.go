package exportschedule

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	sdkclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

const (
	reconcileEnvLimit  = 1000
	reconcilePageLimit = 500
	reconcileAttempts  = 3
	reconcileBackoff   = 5 * time.Second
	reconcileMarkerTTL = 24 * time.Hour
	// Bump when build output changes.
	builderVersion = 2
)

type scheduleHandleGetter interface {
	GetScheduleHandle(ctx context.Context, scheduleID string) models.ScheduleHandle
}

// Reconcile aligns existing schedules with build.
func Reconcile(
	ctx context.Context,
	tc scheduleHandleGetter,
	locker cache.Locker,
	tenants tenant.Repository,
	envs environment.Repository,
	scheduledTasks scheduledtask.Repository,
	log *logger.Logger,
) {
	var marker cache.Lock
	if locker != nil {
		lock, err := locker.AcquireLock(ctx, reconcileMarkerKey(), reconcileMarkerTTL)
		switch {
		case err != nil:
			log.Error(ctx, "export schedule reconcile marker unavailable; running anyway", "error", err)
		case !lock.AcquiredSuccessfully():
			log.Info(ctx, "export schedule reconcile skipped; another pod ran it")
			return
		default:
			marker = lock
		}
	}
	ok := reconcileExportScheduleTimeouts(ctx, tc, tenants, envs, scheduledTasks, log, reconcileEnvLimit, reconcilePageLimit, reconcileBackoff)
	if !ok && marker != nil {
		if err := marker.Release(ctx); err != nil {
			log.Error(ctx, "export schedule reconcile marker release failed", "error", err)
		}
	}
}

// Held while running and after success.
func reconcileMarkerKey() string {
	return fmt.Sprintf("export_schedule_reconcile:v%d:%s", builderVersion, types.ExportWorkflowTimeout)
}

func reconcileExportScheduleTimeouts(
	ctx context.Context,
	tc scheduleHandleGetter,
	tenants tenant.Repository,
	envs environment.Repository,
	scheduledTasks scheduledtask.Repository,
	log *logger.Logger,
	envPageSize, taskPageSize int,
	backoff time.Duration,
) bool {
	for attempt := 1; attempt <= reconcileAttempts; attempt++ {
		err := reconcilePass(ctx, tc, tenants, envs, scheduledTasks, log, envPageSize, taskPageSize)
		if err == nil {
			return true
		}
		log.Error(ctx, "export schedule reconcile pass failed", "error", err, "attempt", attempt)
		if attempt == reconcileAttempts {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff << (attempt - 1)):
		}
	}
	return false
}

// reconcilePass errors on any failure or cancellation.
func reconcilePass(
	ctx context.Context,
	tc scheduleHandleGetter,
	tenants tenant.Repository,
	envs environment.Repository,
	scheduledTasks scheduledtask.Repository,
	log *logger.Logger,
	envPageSize, taskPageSize int,
) error {
	updated, current, failed, drifted := 0, 0, 0, 0
	var listErr error

	tenantIDs, err := tenants.ListIDs(ctx)
	if err != nil {
		return fmt.Errorf("list tenant ids: %w", err)
	}

	for _, tenantID := range tenantIDs {
		tenantCtx := types.SetTenantID(ctx, tenantID)
		var envList []*environment.Environment
		for offset := 0; ; offset += envPageSize {
			envFilter := types.GetDefaultFilter()
			envFilter.Limit = envPageSize
			envFilter.Offset = offset
			envPage, err := envs.List(tenantCtx, envFilter)
			if err != nil {
				log.Error(tenantCtx, "export schedule reconcile: list environments failed", "error", err)
				listErr = err
				break
			}
			envList = append(envList, envPage...)
			if len(envPage) < envPageSize {
				break
			}
		}

		for _, env := range envList {
			envCtx := types.SetEnvironmentID(tenantCtx, env.ID)
			afterID := ""
			for {
				page, err := scheduledTasks.List(envCtx, &scheduledtask.ListFilters{Limit: taskPageSize, KeysetByID: true, AfterID: afterID})
				if err != nil {
					log.Error(envCtx, "export schedule reconcile: list scheduled tasks failed", "error", err)
					listErr = err
					break
				}
				for _, st := range page {
					if st.Interval == types.ScheduledTaskIntervalManual {
						continue
					}
					changed, drift, err := reconcileExportSchedule(ctx, tc, st)
					if drift != "" {
						log.Info(envCtx, "export schedule drift left unchanged", "scheduled_task_id", st.ID, "drift", drift)
						drifted++
					}
					switch {
					case err != nil:
						log.Error(envCtx, "export schedule reconcile failed", "error", err, "scheduled_task_id", st.ID)
						failed++
					case changed:
						updated++
					default:
						current++
					}
				}
				if len(page) < taskPageSize {
					break
				}
				afterID = page[len(page)-1].ID
			}
		}
	}

	log.Info(ctx, "export_schedule.timeouts_reconciled", "updated", updated, "already_current", current, "failed", failed, "drift", drifted)
	switch {
	case listErr != nil:
		return fmt.Errorf("listing failed: %w", listErr)
	case ctx.Err() != nil:
		return fmt.Errorf("reconcile interrupted: %w", ctx.Err())
	case failed > 0:
		return fmt.Errorf("%d export schedules failed to reconcile", failed)
	}
	return nil
}

// Schedule drift kinds, logged and never fixed.
const (
	driftMissing  = "missing_schedule"
	driftPaused   = "paused_schedule"
	driftUnpaused = "unpaused_schedule"
)

// reconcileExportSchedule updates only existing schedules.
func reconcileExportSchedule(ctx context.Context, tc scheduleHandleGetter, task *scheduledtask.ScheduledTask) (bool, string, error) {
	d, err := build(task)
	if err != nil {
		return false, "", fmt.Errorf("build schedule %q: %w", task.ID, err)
	}
	active := task.Enabled && task.Status == types.StatusPublished

	handle := tc.GetScheduleHandle(ctx, task.ID)
	desc, err := handle.Describe(ctx)
	if isNotFound(err) {
		if active {
			return false, driftMissing, nil
		}
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("describe schedule %q: %w", task.ID, err)
	}

	drift := ""
	paused := desc.Schedule.State != nil && desc.Schedule.State.Paused
	switch {
	case active && paused:
		drift = driftPaused
	case !task.Enabled && !paused:
		drift = driftUnpaused
	}
	if !d.needsUpdate(desc.Schedule) {
		return false, drift, nil
	}

	changed := false
	err = handle.Update(ctx, sdkclient.ScheduleUpdateOptions{
		DoUpdate: func(in sdkclient.ScheduleUpdateInput) (*sdkclient.ScheduleUpdate, error) {
			if !d.needsUpdate(in.Description.Schedule) {
				return nil, temporal.ErrSkipScheduleUpdate
			}
			action := in.Description.Schedule.Action.(*sdkclient.ScheduleWorkflowAction)
			action.WorkflowExecutionTimeout = d.action.WorkflowExecutionTimeout
			action.WorkflowRunTimeout = d.action.WorkflowRunTimeout
			if d.policy != nil {
				in.Description.Schedule.Policy = d.applyPolicy(in.Description.Schedule.Policy)
			}
			changed = true
			return &sdkclient.ScheduleUpdate{Schedule: &in.Description.Schedule}, nil
		},
	})
	if err != nil {
		return false, drift, fmt.Errorf("update schedule %q: %w", task.ID, err)
	}
	return changed, drift, nil
}

// needsUpdate compares timeouts and timezone policies.
func (d desired) needsUpdate(current sdkclient.Schedule) bool {
	wf, ok := current.Action.(*sdkclient.ScheduleWorkflowAction)
	if !ok {
		return false
	}
	if wf.WorkflowExecutionTimeout != d.action.WorkflowExecutionTimeout ||
		wf.WorkflowRunTimeout != d.action.WorkflowRunTimeout {
		return true
	}
	if d.policy == nil {
		return false
	}
	return current.Policy == nil ||
		current.Policy.Overlap != d.policy.Overlap ||
		current.Policy.CatchupWindow != d.policy.CatchupWindow
}
