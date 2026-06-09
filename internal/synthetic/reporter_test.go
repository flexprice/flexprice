package synthetic

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
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
	for _, want := range []string{"synthetic.check.failed", "wallet-balance-probe", "probe", "customer_id"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q\noutput: %s", want, out)
		}
	}
}

func newCapturedLogger(t *testing.T) (*bytes.Buffer, *logger.Logger) {
	t.Helper()
	buf := &bytes.Buffer{}
	cfg := &config.Configuration{}
	cfg.Logging.Level = "error"
	lg, err := logger.NewLoggerWithWriter(cfg, buf)
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	return buf, lg
}
