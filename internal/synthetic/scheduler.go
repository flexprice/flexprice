package synthetic

import (
	"context"
	"fmt"
	"time"
)

// RunFunc is the callback Schedulers invoke to actually execute a Check. The
// concrete RunFunc is provided by Runner and wraps the call in span / panic
// recovery / failure reporting.
type RunFunc func(ctx context.Context, check Check)

type Scheduler interface {
	Schedule() string
	Start(ctx context.Context, run RunFunc)
}

// ---------- Ticker ----------

func NewTickerScheduler(check Check, interval time.Duration) Scheduler {
	if interval <= 0 {
		interval = 1 * time.Minute
	}
	return &tickerScheduler{check: check, interval: interval}
}

type tickerScheduler struct {
	check    Check
	interval time.Duration
}

func (s *tickerScheduler) Schedule() string { return fmt.Sprintf("ticker:%s", s.interval) }

func (s *tickerScheduler) Start(ctx context.Context, run RunFunc) {
	// Fire once immediately so process restarts don't lose a tick.
	run(ctx, s.check)
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run(ctx, s.check)
		}
	}
}

// ---------- OneShot ----------

func NewOneShotScheduler(check Check) Scheduler {
	return &oneShotScheduler{check: check}
}

type oneShotScheduler struct{ check Check }

func (s *oneShotScheduler) Schedule() string { return "oneshot" }

func (s *oneShotScheduler) Start(ctx context.Context, run RunFunc) {
	run(ctx, s.check)
}

// ---------- Rate ----------

func NewRateScheduler(check Check, ratePerSecond int) Scheduler {
	return &rateScheduler{check: check, rate: ratePerSecond}
}

type rateScheduler struct {
	check Check
	rate  int
}

func (s *rateScheduler) Schedule() string { return fmt.Sprintf("rate:%d/s", s.rate) }

func (s *rateScheduler) Start(ctx context.Context, run RunFunc) {
	if s.rate <= 0 {
		<-ctx.Done()
		return
	}
	interval := time.Second / time.Duration(s.rate)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run(ctx, s.check)
		}
	}
}
