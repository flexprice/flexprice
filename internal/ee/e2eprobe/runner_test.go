package e2eprobe

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestRunner_ReportsErrors(t *testing.T) {
	rep := &recordingReporter{}
	failing := &checkFn{name: "bad", kind: KindProbe, fn: func(_ context.Context) error { return errors.New("nope") }}
	r := NewRunner(rep, nil, "run-1", nil)
	r.Add(failing, NewTickerScheduler(failing, 20*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go r.Start(ctx)
	time.Sleep(70 * time.Millisecond)
	cancel()
	if got := rep.snapshot(); len(got) == 0 || got[0].CheckName != "bad" || got[0].CheckKind != KindProbe {
		t.Fatalf("missing or wrong failure: %+v", got)
	}
}

// safeBuffer is a bytes.Buffer protected by a mutex, safe for concurrent use.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newInfoLogger returns a safe-buffer-backed logger at Info level for use in tests.
func newInfoLogger(t *testing.T) (*safeBuffer, *logger.Logger) {
	t.Helper()
	buf := &safeBuffer{}
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "time"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.AddSync(buf),
		zapcore.InfoLevel,
	)
	z := zap.New(core)
	return buf, logger.NewFromSugared(z.Sugar())
}

func TestRunner_RecordsSuccessesAndFailures(t *testing.T) {
	var calls int32
	alternating := &checkFn{
		name: "alt",
		kind: KindProbe,
		fn: func(_ context.Context) error {
			n := atomic.AddInt32(&calls, 1)
			if n%2 == 0 {
				return errors.New("even call fails")
			}
			return nil
		},
	}
	r := NewRunner(nil, nil, "run-stats", nil)
	r.Add(alternating, NewTickerScheduler(alternating, 20*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	go r.Start(ctx)
	time.Sleep(150 * time.Millisecond)
	cancel()

	r.statsMu.Lock()
	s := r.stats["alt"]
	r.statsMu.Unlock()

	if s == nil {
		t.Fatal("no stats recorded for check 'alt'")
	}
	total := s.successes + s.failures
	if total == 0 {
		t.Fatal("expected at least one run recorded")
	}
	if s.failures == 0 {
		t.Error("expected at least one failure recorded")
	}
	if s.successes == 0 {
		t.Error("expected at least one success recorded")
	}
}

func TestRunner_HeartbeatEmitsPeriodically(t *testing.T) {
	buf, lg := newInfoLogger(t)
	passing := &checkFn{
		name: "ping",
		kind: KindProbe,
		fn:   func(_ context.Context) error { return nil },
	}
	r := NewRunner(nil, lg, "run-hb", nil)
	r.SetHeartbeatInterval(30 * time.Millisecond)
	r.Add(passing, NewTickerScheduler(passing, 20*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	go r.Start(ctx)
	time.Sleep(150 * time.Millisecond)
	cancel()

	out := buf.String()
	if !strings.Contains(out, "e2eprobe.heartbeat") {
		t.Fatalf("expected heartbeat log; got:\n%s", out)
	}
	if !strings.Contains(out, "check.ping") {
		t.Errorf("expected per-check count for 'ping' in heartbeat; got:\n%s", out)
	}
}

func TestRunner_HeartbeatDisabledWhenIntervalZero(t *testing.T) {
	buf, lg := newInfoLogger(t)
	passing := &checkFn{
		name: "ping",
		kind: KindProbe,
		fn:   func(_ context.Context) error { return nil },
	}
	r := NewRunner(nil, lg, "run-no-hb", nil)
	r.SetHeartbeatInterval(0) // disabled
	r.Add(passing, NewTickerScheduler(passing, 20*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	go r.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	cancel()

	if strings.Contains(buf.String(), "e2eprobe.heartbeat") {
		t.Errorf("heartbeat should be disabled when interval=0; got:\n%s", buf.String())
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
	r := NewRunner(rep, nil, "run-1", nil)
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
