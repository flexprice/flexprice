package e2eprobe

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type recordingReporter struct {
	mu  sync.Mutex
	got []FailureReport
}

func (r *recordingReporter) Report(_ context.Context, f FailureReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, f)
}

func (r *recordingReporter) snapshot() []FailureReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]FailureReport, len(r.got))
	copy(out, r.got)
	return out
}

func TestCompositeReporter_FansOut(t *testing.T) {
	a := &recordingReporter{}
	b := &recordingReporter{}
	c := NewCompositeReporter(a, nil, b) // nil sinks must be skipped
	c.Report(context.Background(), FailureReport{CheckName: "x", Err: errors.New("boom")})
	if len(a.snapshot()) != 1 || len(b.snapshot()) != 1 {
		t.Fatalf("fan-out broken")
	}
}

func TestLogReporter(t *testing.T) {
	buf, lg := newCapturedLogger(t)
	r := NewLogReporter(lg)
	r.Report(context.Background(), FailureReport{
		CheckName:  "wallet-balance-probe",
		CheckKind:  KindProbe,
		Step:       "fetch",
		Err:        errors.New("503"),
		RunID:      "abc",
		Attributes: map[string]string{"customer_id": "c_1"},
		OccurredAt: time.Now(),
	})
	out := buf.String()
	for _, want := range []string{"e2eprobe.check.failed", "wallet-balance-probe", "probe", "customer_id", "503"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q\noutput: %s", want, out)
		}
	}
}

func newCapturedLogger(t *testing.T) (*bytes.Buffer, *logger.Logger) {
	t.Helper()
	buf := &bytes.Buffer{}
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "time"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.AddSync(buf),
		zapcore.ErrorLevel,
	)
	z := zap.New(core)
	return buf, logger.NewFromSugared(z.Sugar())
}
