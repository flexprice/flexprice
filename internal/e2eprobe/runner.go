package e2eprobe

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
)

type runnable struct {
	check     Check
	scheduler Scheduler
}

type runStats struct {
	successes int64
	failures  int64
}

type Runner struct {
	items             []runnable
	reporter          Reporter
	logger            *logger.Logger
	runID             string
	globalAttrs       map[string]string
	statsMu           sync.Mutex
	stats             map[string]*runStats // check_name → counters
	started           time.Time
	heartbeatInterval time.Duration
}

// NewRunner creates a Runner. globalAttrs (tenant_id, environment_id, etc.) are
// merged into every FailureReport so that Slack/OTEL alerts are immediately
// actionable without having to look up context from logs.
func NewRunner(reporter Reporter, lg *logger.Logger, runID string, globalAttrs map[string]string) *Runner {
	merged := make(map[string]string, len(globalAttrs))
	for k, v := range globalAttrs {
		merged[k] = v
	}
	return &Runner{
		reporter:    reporter,
		logger:      lg,
		runID:       runID,
		globalAttrs: merged,
		stats:       make(map[string]*runStats),
	}
}

// SetHeartbeatInterval configures how often a summary heartbeat line is logged.
// Pass 0 to disable heartbeat logging. Returns the receiver for chaining.
func (r *Runner) SetHeartbeatInterval(d time.Duration) *Runner {
	r.heartbeatInterval = d
	return r
}

func (r *Runner) Add(check Check, sched Scheduler) {
	r.items = append(r.items, runnable{check: check, scheduler: sched})
}

func (r *Runner) Start(ctx context.Context) {
	r.started = time.Now()
	var wg sync.WaitGroup
	if r.heartbeatInterval > 0 && r.logger != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.runHeartbeat(ctx)
		}()
	}
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

func (r *Runner) runHeartbeat(ctx context.Context) {
	t := time.NewTicker(r.heartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.logHeartbeat()
		}
	}
}

func (r *Runner) logHeartbeat() {
	r.statsMu.Lock()
	defer r.statsMu.Unlock()

	var totalRuns, totalFails int64
	perCheck := make(map[string]string, len(r.stats))
	for name, s := range r.stats {
		runs := s.successes + s.failures
		totalRuns += runs
		totalFails += s.failures
		perCheck[name] = fmt.Sprintf("%d/%d", s.successes, runs)
	}
	var successRate string
	if totalRuns > 0 {
		successRate = fmt.Sprintf("%.2f%%", float64(totalRuns-totalFails)*100/float64(totalRuns))
	} else {
		successRate = "n/a"
	}
	uptime := time.Since(r.started).Truncate(time.Second).String()

	fields := []any{
		"event", "e2eprobe.heartbeat",
		"run_id", r.runID,
		"uptime", uptime,
		"total_runs", totalRuns,
		"total_failures", totalFails,
		"success_rate", successRate,
	}
	// Add per-check counts (sorted for stable output).
	keys := make([]string, 0, len(perCheck))
	for k := range perCheck {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fields = append(fields, "check."+k, perCheck[k])
	}
	r.logger.Info(context.Background(), "e2eprobe heartbeat", fields...)
}

// recordResult increments the success or failure counter for the named check.
func (r *Runner) recordResult(name string, success bool) {
	r.statsMu.Lock()
	defer r.statsMu.Unlock()
	s, ok := r.stats[name]
	if !ok {
		s = &runStats{}
		r.stats[name] = s
	}
	if success {
		s.successes++
	} else {
		s.failures++
	}
}

func (r *Runner) execute(ctx context.Context, check Check) {
	defer func() {
		if rec := recover(); rec != nil {
			err := fmt.Errorf("panic: %v", rec)
			r.recordResult(check.Name(), false)
			if r.logger != nil {
				r.logger.Error(ctx, "check panic", "error", err.Error(), "check_name", check.Name(), "panic", fmt.Sprint(rec))
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
		r.recordResult(check.Name(), false)
		if r.logger != nil {
			// Use Info because the failure is reported through the Reporter
			// (Slack + OTEL); this is the "recovered" path per LL003 guidance.
			r.logger.Info(ctx, "check completed with failure (reported)", "check_name", check.Name(), "kind", check.Kind(), "error", err.Error())
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
	} else {
		r.recordResult(check.Name(), true)
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
