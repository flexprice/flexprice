package e2eprobe

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
)

type runnable struct {
	check     Check
	scheduler Scheduler
}

type Runner struct {
	items    []runnable
	reporter Reporter
	logger   *logger.Logger
	runID    string
}

func NewRunner(reporter Reporter, lg *logger.Logger, runID string) *Runner {
	return &Runner{reporter: reporter, logger: lg, runID: runID}
}

func (r *Runner) Add(check Check, sched Scheduler) {
	r.items = append(r.items, runnable{check: check, scheduler: sched})
}

func (r *Runner) Start(ctx context.Context) {
	var wg sync.WaitGroup
	for _, it := range r.items {
		wg.Add(1)
		it := it
		go func() {
			defer wg.Done()
			it.scheduler.Start(ctx, r.execute)
		}()
	}
	wg.Wait()
}

func (r *Runner) execute(ctx context.Context, check Check) {
	defer func() {
		if rec := recover(); rec != nil {
			err := fmt.Errorf("panic: %v", rec)
			if r.logger != nil {
				r.logger.Errorw("check panic", "check_name", check.Name(), "panic", rec)
			}
			if r.reporter != nil {
				r.reporter.Report(ctx, FailureReport{
					CheckName:  check.Name(),
					CheckKind:  check.Kind(),
					Step:       "panic",
					Err:        err,
					RunID:      r.runID,
					OccurredAt: time.Now(),
				})
			}
		}
	}()
	if err := check.Run(ctx); err != nil {
		if r.logger != nil {
			r.logger.Warnw("check failed", "check_name", check.Name(), "kind", check.Kind(), "error", err)
		}
		if r.reporter != nil {
			r.reporter.Report(ctx, FailureReport{
				CheckName:  check.Name(),
				CheckKind:  check.Kind(),
				Step:       "run",
				Err:        err,
				RunID:      r.runID,
				OccurredAt: time.Now(),
			})
		}
	}
}
