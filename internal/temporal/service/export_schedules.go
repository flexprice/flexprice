package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/api/serviceerror"
	sdkclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

const (
	reconcileEnvLimit  = 1000
	reconcilePageLimit = 500
	reconcileAttempts  = 3
	reconcileBackoff   = 5 * time.Second
)

type scheduleHandleGetter interface {
	GetScheduleHandle(ctx context.Context, scheduleID string) models.ScheduleHandle
}

// ReconcileExportScheduleTimeouts raises older export schedules to the current workflow timeout.
// Failures are logged and never returned.
func ReconcileExportScheduleTimeouts(
	ctx context.Context,
	tc scheduleHandleGetter,
	tenants tenant.Repository,
	envs environment.Repository,
	scheduledTasks scheduledtask.Repository,
	log *logger.Logger,
) {
	reconcileExportScheduleTimeouts(ctx, tc, tenants, envs, scheduledTasks, log, reconcileEnvLimit, reconcilePageLimit, reconcileBackoff)
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
) {
	for attempt := 1; attempt <= reconcileAttempts; attempt++ {
		err := reconcilePass(ctx, tc, tenants, envs, scheduledTasks, log, envPageSize, taskPageSize)
		if err == nil {
			return
		}
		log.Error(ctx, "export schedule reconcile pass failed", "error", err, "attempt", attempt)
		if attempt == reconcileAttempts {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff << (attempt - 1)):
		}
	}
}

// reconcilePass returns an error when any listing failed so the caller can retry.
func reconcilePass(
	ctx context.Context,
	tc scheduleHandleGetter,
	tenants tenant.Repository,
	envs environment.Repository,
	scheduledTasks scheduledtask.Repository,
	log *logger.Logger,
	envPageSize, taskPageSize int,
) error {
	updated, current, failed := 0, 0, 0
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
				log.Error(ctx, "export schedule reconcile: list environments failed", "error", err, "tenant_id", tenantID)
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
					log.Error(ctx, "export schedule reconcile: list scheduled tasks failed", "error", err, "tenant_id", tenantID, "env_id", env.ID)
					listErr = err
					break
				}
				for _, st := range page {
					if st.TemporalScheduleID == "" || st.Interval == types.ScheduledTaskIntervalManual {
						continue
					}
					changed, err := reconcileExportSchedule(ctx, tc, st.TemporalScheduleID)
					switch {
					case err != nil:
						log.Error(ctx, "export schedule reconcile failed", "error", err, "scheduled_task_id", st.ID)
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

	log.Info(ctx, "export schedule timeouts reconciled", "updated", updated, "already_current", current, "failed", failed)
	if listErr != nil {
		return fmt.Errorf("listing failed: %w", listErr)
	}
	return nil
}

func reconcileExportSchedule(ctx context.Context, tc scheduleHandleGetter, scheduleID string) (bool, error) {
	handle := tc.GetScheduleHandle(ctx, scheduleID)
	desc, err := handle.Describe(ctx)
	if err != nil {
		var notFound *serviceerror.NotFound
		if errors.As(err, &notFound) {
			return false, nil
		}
		return false, fmt.Errorf("describe schedule %q: %w", scheduleID, err)
	}
	if !needsTimeoutUpdate(desc.Schedule.Action) {
		return false, nil
	}

	changed := false
	err = handle.Update(ctx, sdkclient.ScheduleUpdateOptions{
		DoUpdate: func(in sdkclient.ScheduleUpdateInput) (*sdkclient.ScheduleUpdate, error) {
			if !needsTimeoutUpdate(in.Description.Schedule.Action) {
				return nil, temporal.ErrSkipScheduleUpdate
			}
			action := in.Description.Schedule.Action.(*sdkclient.ScheduleWorkflowAction)
			action.WorkflowExecutionTimeout = types.ExportWorkflowTimeout
			action.WorkflowRunTimeout = types.ExportWorkflowTimeout
			changed = true
			return &sdkclient.ScheduleUpdate{Schedule: &in.Description.Schedule}, nil
		},
	})
	if err != nil {
		return false, fmt.Errorf("update schedule %q: %w", scheduleID, err)
	}
	return changed, nil
}

func needsTimeoutUpdate(action sdkclient.ScheduleAction) bool {
	wf, ok := action.(*sdkclient.ScheduleWorkflowAction)
	if !ok {
		return false
	}
	return wf.WorkflowExecutionTimeout != types.ExportWorkflowTimeout ||
		wf.WorkflowRunTimeout != types.ExportWorkflowTimeout
}
