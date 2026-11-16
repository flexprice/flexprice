package cron

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	staleExportAge       = types.ExportWorkflowTimeout + 30*time.Minute
	staleExportReason    = "The run stopped before finishing. Run it again."
	staleExportBatchSize = 500
	staleExportMaxPasses = 20
)

// StaleExportSweepActivities fails export runs that never reached a final status.
type StaleExportSweepActivities struct {
	taskRepo          task.Repository
	scheduledTaskRepo scheduledtask.Repository
	logger            *logger.Logger
	batchSize         int
}

func NewStaleExportSweepActivities(taskRepo task.Repository, scheduledTaskRepo scheduledtask.Repository, log *logger.Logger) *StaleExportSweepActivities {
	return &StaleExportSweepActivities{taskRepo: taskRepo, scheduledTaskRepo: scheduledTaskRepo, logger: log, batchSize: staleExportBatchSize}
}

// SweepStaleExportRunsActivity returns how many runs it failed.
func (a *StaleExportSweepActivities) SweepStaleExportRunsActivity(ctx context.Context) (int, error) {
	cutoff := clock.Now().Add(-staleExportAge)
	total, rowErrors := 0, 0
	for pass := 0; pass < staleExportMaxPasses; pass++ {
		refs, err := a.taskRepo.ListStaleExportTasks(ctx, cutoff, a.batchSize)
		if err != nil {
			a.logger.Error(ctx, "stale export sweep failed", "error", err, "failed_so_far", total)
			return total, err
		}

		passFailed, passErrors := 0, 0
		for _, ref := range refs {
			rowCtx := types.SetEnvironmentID(types.SetTenantID(ctx, ref.TenantID), ref.EnvironmentID)
			rowCtx = types.SetUserID(rowCtx, types.DefaultUserID)
			changed, err := a.taskRepo.FailStaleExportTask(rowCtx, ref.ID, cutoff, staleExportReason)
			if err != nil {
				a.logger.Error(ctx, "failed to fail stale export run", "error", err, "task_id", ref.ID)
				passErrors++
				continue
			}
			if changed {
				passFailed++
				a.recordFailedDelivery(rowCtx, ref.ID)
			}
		}
		total += passFailed
		rowErrors += passErrors

		if len(refs) < a.batchSize {
			break
		}
		if passFailed == 0 {
			if passErrors == 0 {
				a.logger.Info(ctx, "stale export sweep made no progress on a full page", "batch_size", a.batchSize)
			}
			break
		}
	}
	if total > 0 {
		a.logger.Info(ctx, "failed stale export runs", "count", total)
	}
	if rowErrors > 0 {
		return total, fmt.Errorf("failed to fail %d stale export runs", rowErrors)
	}
	return total, nil
}

// recordFailedDelivery logs and drops its own errors.
func (a *StaleExportSweepActivities) recordFailedDelivery(ctx context.Context, taskID string) {
	run, err := a.taskRepo.Get(ctx, taskID)
	if err != nil {
		a.logger.Error(ctx, "stale export sweep: get run failed", "error", err, "task_id", taskID)
		return
	}
	var connectionID string
	var cfg *types.S3JobConfig
	if run.ScheduledTaskID != "" {
		st, err := a.scheduledTaskRepo.Get(ctx, run.ScheduledTaskID)
		if err != nil {
			a.logger.Error(ctx, "stale export sweep: get export failed", "error", err, "task_id", taskID)
			return
		}
		connectionID = st.ConnectionID
		cfg, _ = st.GetS3JobConfig()
	}
	d := types.NewFileDelivery(connectionID, cfg, clock.Now())
	d.Status = types.ExportDeliveryStatusFailed
	d.Detail = staleExportReason
	if err := a.taskRepo.SetTaskMetadataKey(ctx, taskID, "deliveries", []types.ExportDelivery{d}); err != nil {
		a.logger.Error(ctx, "stale export sweep: record delivery failed", "error", err, "task_id", taskID)
	}
}
