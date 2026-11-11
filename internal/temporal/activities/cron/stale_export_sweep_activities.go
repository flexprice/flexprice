package cron

import (
	"context"
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
	total := 0
	for pass := 0; pass < staleExportMaxPasses; pass++ {
		refs, err := a.taskRepo.ListStaleExportTasks(ctx, cutoff, a.batchSize)
		if err != nil {
			a.logger.Error(ctx, "stale export sweep failed", "error", err, "failed_so_far", total)
			return total, err
		}

		passFailed := 0
		for _, ref := range refs {
			rowCtx := types.SetEnvironmentID(types.SetTenantID(ctx, ref.TenantID), ref.EnvironmentID)
			changed, err := a.taskRepo.FailStaleExportTask(rowCtx, ref.ID, cutoff, staleExportReason)
			if err != nil {
				a.logger.Error(ctx, "failed to fail stale export run", "error", err, "task_id", ref.ID)
				continue
			}
			if changed {
				passFailed++
			}
		}
		total += passFailed

		if len(refs) < a.batchSize || passFailed == 0 {
			break
		}
	}
	if total > 0 {
		a.logger.Info(ctx, "failed stale export runs", "count", total)
	}
	return total, nil
}
