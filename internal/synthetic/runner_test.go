package synthetic

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunner_ReportsErrors(t *testing.T) {
	rep := &recordingReporter{}
	failing := &checkFn{name: "bad", kind: KindProbe, fn: func(_ context.Context) error { return errors.New("nope") }}
	r := NewRunner(rep, nil, "run-1")
	r.Add(failing, NewTickerScheduler(failing, 20*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go r.Start(ctx)
	time.Sleep(70 * time.Millisecond)
	cancel()
	if got := rep.snapshot(); len(got) == 0 || got[0].CheckName != "bad" || got[0].CheckKind != KindProbe {
		t.Fatalf("missing or wrong failure: %+v", got)
	}
}

func TestRunner_RecoversPanics(t *testing.T) {
	rep := &recordingReporter{}
	var calls int32
	panicker := &checkFn{name: "p", kind: KindProbe, fn: func(_ context.Context) error {
		atomic.AddInt32(&calls, 1)
		if atomic.LoadInt32(&calls) == 1 {
			panic("boom")
		}
		return nil
	}}
	r := NewRunner(rep, nil, "run-1")
	r.Add(panicker, NewTickerScheduler(panicker, 20*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go r.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	if atomic.LoadInt32(&calls) < 2 {
		t.Errorf("expected runner to recover and tick again, calls=%d", calls)
	}
	if got := rep.snapshot(); len(got) == 0 || got[0].Step != "panic" {
		t.Errorf("missing panic report: %+v", got)
	}
}
