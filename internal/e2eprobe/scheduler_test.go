package e2eprobe

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type fakeCheck struct {
	name string
	kind Kind
	runs int32
}

func (f *fakeCheck) Name() string                { return f.name }
func (f *fakeCheck) Kind() Kind                  { return f.kind }
func (f *fakeCheck) Run(_ context.Context) error { atomic.AddInt32(&f.runs, 1); return nil }

func TestTickerScheduler_TicksImmediatelyThenAtInterval(t *testing.T) {
	c := &fakeCheck{name: "x", kind: KindProbe}
	s := NewTickerScheduler(c, 30*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Start(ctx, func(ctx context.Context, k Check) { _ = k.Run(ctx) }); close(done) }()
	time.Sleep(110 * time.Millisecond)
	cancel()
	<-done
	if got := atomic.LoadInt32(&c.runs); got < 3 {
		t.Errorf("runs=%d, want >=3 (immediate + 3 ticks)", got)
	}
}

func TestOneShotScheduler_RunsOnce(t *testing.T) {
	c := &fakeCheck{name: "x"}
	s := NewOneShotScheduler(c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { s.Start(ctx, func(ctx context.Context, k Check) { _ = k.Run(ctx) }); close(done) }()
	<-done
	if atomic.LoadInt32(&c.runs) != 1 {
		t.Errorf("runs=%d, want 1", c.runs)
	}
}

func TestTickerScheduler_StopsOnContext(t *testing.T) {
	c := &fakeCheck{name: "x"}
	s := NewTickerScheduler(c, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Start(ctx, func(ctx context.Context, k Check) { _ = k.Run(ctx) }); close(done) }()
	time.Sleep(25 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("scheduler did not stop on cancel")
	}
}

func TestRateScheduler_RunsAtApproximatelyRate(t *testing.T) {
	c := &fakeCheck{name: "rate"}
	s := NewRateScheduler(c, 50) // 50/sec
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Start(ctx, func(ctx context.Context, k Check) { _ = k.Run(ctx) }); close(done) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
	runs := atomic.LoadInt32(&c.runs)
	// At 50/sec over ~200ms, expect roughly 10 runs. Allow wide bounds.
	if runs < 5 || runs > 30 {
		t.Errorf("runs=%d, want 5..30", runs)
	}
}

func TestRateScheduler_ZeroRateNoOps(t *testing.T) {
	c := &fakeCheck{name: "zero"}
	s := NewRateScheduler(c, 0)
	ctx, cancel := context.WithCancel(context.Background())
	go s.Start(ctx, func(ctx context.Context, k Check) { _ = k.Run(ctx) })
	time.Sleep(20 * time.Millisecond)
	cancel()
	if atomic.LoadInt32(&c.runs) != 0 {
		t.Errorf("runs=%d, want 0", c.runs)
	}
}
