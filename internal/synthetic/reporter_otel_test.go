package synthetic

import (
	"context"
	"errors"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOTELReporter_RecordsErrorSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	r := NewOTELReporter(tp.Tracer("synthetic"))
	r.Report(context.Background(), FailureReport{
		CheckName:  "wallet-debit-verification",
		CheckKind:  KindProbe,
		Step:       "assert-debit",
		Err:        errors.New("short"),
		RunID:      "abc",
		Attributes: map[string]string{"wallet_id": "wal_1"},
		OccurredAt: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
	})
	spans := rec.Ended()
	if len(spans) != 1 || spans[0].Name() != "synthetic.check.run" {
		t.Fatalf("spans=%+v", spans)
	}
	if spans[0].Status().Code.String() != "Error" {
		t.Errorf("status=%s", spans[0].Status().Code.String())
	}
	got := map[string]string{}
	for _, kv := range spans[0].Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	for k, want := range map[string]string{
		"synthetic.check.name": "wallet-debit-verification",
		"synthetic.check.kind": "probe",
		"synthetic.step":       "assert-debit",
		"synthetic.run_id":     "abc",
		"wallet_id":            "wal_1",
	} {
		if got[k] != want {
			t.Errorf("attr[%s]=%q want %q", k, got[k], want)
		}
	}
}
