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
	items       []runnable
	reporter    Reporter
	logger      *logger.Logger
	runID       string
	globalAttrs map[string]string
}

// NewRunner creates a Runner. globalAttrs (tenant_id, environment_id, etc.) are
// merged into every FailureReport so that Slack/OTEL alerts are immediately
// actionable without having to look up context from logs.
func NewRunner(reporter Reporter, lg *logger.Logger, runID string, globalAttrs map[string]string) *Runner {
	merged := make(map[string]string, len(globalAttrs))
	for k, v := range globalAttrs {
		merged[k] = v
	}
	return &Runner{reporter: reporter, logger: lg, runID: runID, globalAttrs: merged}
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
				attrs := r.mergeAttrs(nil)
				attrs["step"] = "panic"
				r.reporter.Report(ctx, FailureReport{
					CheckName:  check.Name(),
					CheckKind:  check.Kind(),
					Step:       "panic",
					Err:        err,
					RunID:      r.runID,
					Attributes: attrs,
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
				Attributes: r.mergeAttrs(AttributesFrom(err)),
				OccurredAt: time.Now(),
			})
		}
	}
}

// mergeAttrs produces a fresh map combining globalAttrs with any per-error
// attrs. Per-error attrs win on conflict.
func (r *Runner) mergeAttrs(errAttrs map[string]string) map[string]string {
	out := make(map[string]string, len(r.globalAttrs)+len(errAttrs))
	for k, v := range r.globalAttrs {
		out[k] = v
	}
	for k, v := range errAttrs {
		out[k] = v
	}
	return out
}
