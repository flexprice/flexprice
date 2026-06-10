package spanerr

import (
	"context"
	"errors"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// newRecordingSpan returns a context carrying a live recording span plus the
// recorder that will hold the span once it ends.
func newRecordingSpan(t *testing.T) (context.Context, trace.Span, *tracetest.SpanRecorder) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	return ctx, span, sr
}

func attrMap(t *testing.T, sr *tracetest.SpanRecorder) (events int, byKey map[string]string) {
	t.Helper()
	ended := sr.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(ended))
	}
	byKey = map[string]string{}
	for _, ev := range ended[0].Events() {
		if ev.Name != exceptionEventName {
			continue
		}
		events++
		for _, a := range ev.Attributes {
			byKey[string(a.Key)] = a.Value.Emit() // Emit renders any type (incl. bool) as a string
		}
	}
	return events, byKey
}

func TestRecord_AddsExceptionEvent(t *testing.T) {
	ctx, span, sr := newRecordingSpan(t)

	if !Record(ctx, errors.New("boom")) {
		t.Fatal("Record returned false on a recording span")
	}
	span.End()

	n, attrs := attrMap(t, sr)
	if n != 1 {
		t.Fatalf("expected 1 exception event, got %d", n)
	}
	if attrs[attrExceptionMessage] != "boom" {
		t.Errorf("exception.message = %q, want %q", attrs[attrExceptionMessage], "boom")
	}
	if attrs[attrExceptionType] != "*errors.errorString" {
		t.Errorf("exception.type = %q, want %q", attrs[attrExceptionType], "*errors.errorString")
	}
	if attrs[attrExceptionStack] == "" {
		t.Error("exception.stacktrace is empty; expected a captured stack")
	}
}

func TestRecordException_DefaultsType(t *testing.T) {
	ctx, span, sr := newRecordingSpan(t)
	RecordException(ctx, "", "msg only")
	span.End()

	_, attrs := attrMap(t, sr)
	if attrs[attrExceptionType] != defaultExceptionType {
		t.Errorf("exception.type = %q, want default %q", attrs[attrExceptionType], defaultExceptionType)
	}
}

func TestRecord_DedupWithinScope(t *testing.T) {
	ctx, span, sr := newRecordingSpan(t)
	ctx = WithDedup(ctx)

	err := errors.New("dup")
	Record(ctx, err)
	Record(ctx, err) // same fingerprint, same scope -> suppressed
	Record(ctx, errors.New("other"))
	span.End()

	n, _ := attrMap(t, sr)
	if n != 2 {
		t.Fatalf("expected 2 exception events after dedup, got %d", n)
	}
}

func TestRecord_NoDedupWithoutScope(t *testing.T) {
	ctx, span, sr := newRecordingSpan(t)

	err := errors.New("dup")
	Record(ctx, err)
	Record(ctx, err) // no dedup scope -> both recorded
	span.End()

	n, _ := attrMap(t, sr)
	if n != 2 {
		t.Fatalf("expected 2 exception events without dedup scope, got %d", n)
	}
}

func TestRecord_NoSpanIsNoop(t *testing.T) {
	if Record(context.Background(), errors.New("x")) {
		t.Error("Record returned true with no span in context")
	}
}

func TestRecord_NilError(t *testing.T) {
	ctx, span, sr := newRecordingSpan(t)
	if Record(ctx, nil) {
		t.Error("Record returned true for a nil error")
	}
	span.End()
	if n, _ := attrMap(t, sr); n != 0 {
		t.Errorf("expected 0 exception events for nil error, got %d", n)
	}
}

func TestRecordPanic_MarksEscaped(t *testing.T) {
	ctx, span, sr := newRecordingSpan(t)
	if !RecordPanic(ctx, "kaboom") {
		t.Fatal("RecordPanic returned false on a recording span")
	}
	span.End()

	n, attrs := attrMap(t, sr)
	if n != 1 {
		t.Fatalf("expected 1 exception event, got %d", n)
	}
	if attrs[attrExceptionType] != defaultPanicException {
		t.Errorf("exception.type = %q, want %q", attrs[attrExceptionType], defaultPanicException)
	}
	if attrs[attrExceptionEscaped] != "true" {
		t.Errorf("exception.escaped = %q, want \"true\"", attrs[attrExceptionEscaped])
	}
}
