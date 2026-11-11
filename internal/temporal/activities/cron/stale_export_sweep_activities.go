package cron

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	staleExportAge       = types.ExportWorkflowTimeout + 30*time.Minute
	staleExportReason    = "Run did not finish."
	staleExportBatchSize = 500
	staleExportMaxPasses = 20
)

// StaleExportSweepActivities fails export runs that never reached a final status.
type StaleExportSweepActivities struct {
	taskRepo  task.Repository
	logger    *logger.Logger
	batchSize int
}

func NewStaleExportSweepActivities(taskRepo task.Repository, log *logger.Logger) *StaleExportSweepActivities {
	return &StaleExportSweepActivities{taskRepo: taskRepo, logger: log, batchSize: staleExportBatchSize}
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
