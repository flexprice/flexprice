package cron

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
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
			// Delivery first keeps the row retryable.
			if err := a.recordFailedDelivery(rowCtx, ref.ID); err != nil {
				a.logger.Error(rowCtx, "stale export sweep: record delivery failed", "error", err, "task_id", ref.ID)
				passErrors++
				continue
			}
			changed, err := a.taskRepo.FailStaleExportTask(rowCtx, ref.ID, cutoff, staleExportReason)
			if err != nil {
				a.logger.Error(rowCtx, "failed to fail stale export run", "error", err, "task_id", ref.ID)
				passErrors++
				continue
			}
			if changed {
				passFailed++
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

// recordFailedDelivery keeps existing deliveries.
func (a *StaleExportSweepActivities) recordFailedDelivery(ctx context.Context, taskID string) error {
	run, err := a.taskRepo.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if existing, ok := run.Metadata["deliveries"]; ok && existing != nil {
		return nil
	}
	var connectionID string
	var cfg *types.S3JobConfig
	if run.ScheduledTaskID != "" {
		st, err := a.scheduledTaskRepo.Get(ctx, run.ScheduledTaskID)
		switch {
		case err == nil:
			connectionID = st.ConnectionID
			cfg, _ = st.GetS3JobConfig()
		case !ierr.IsNotFound(err):
			return err
		}
	}
	d := types.NewFileDelivery(connectionID, cfg, clock.Now())
	d.Status = types.ExportDeliveryStatusFailed
	d.Detail = staleExportReason
	return a.taskRepo.SetTaskMetadataKey(ctx, taskID, "deliveries", []types.ExportDelivery{d})
}
