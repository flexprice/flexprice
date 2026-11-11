package cron

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/logger"
)

const (
	staleExportAge       = 2 * time.Hour
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
		n, err := a.taskRepo.FailStaleExportTasks(ctx, cutoff, staleExportReason, a.batchSize)
		total += n
		if err != nil {
			a.logger.Error(ctx, "stale export sweep failed", "error", err, "failed_so_far", total)
			return total, err
		}
		if n < a.batchSize {
			break
		}
	}
	if total > 0 {
		a.logger.Info(ctx, "failed stale export runs", "count", total)
	}
	return total, nil
}
