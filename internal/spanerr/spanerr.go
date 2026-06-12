// Package spanerr records Go errors and panics as OpenTelemetry "exception"
// span events so they surface in SigNoz's Exceptions tab.
//
// SigNoz (like any OTLP backend) builds its Exceptions view from span events
// named "exception" carrying the semantic-convention attributes exception.type,
// exception.message and exception.stacktrace. The OTel SDK's span.RecordError
// emits exactly such an event, but (a) it does not capture a stacktrace unless
// asked and (b) it derives exception.type from the Go dynamic type, which loses
// the richer error_type our structured logging already produces. This package
// emits the event directly so we control all three attributes and always attach
// a stacktrace.
//
// It is a leaf package: it depends only on the OTel API (not on internal/logger
// or internal/tracing), so both of those packages can import it without an
// import cycle.
package spanerr

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// OTel semantic-convention names for exception span events. Hard-coded as string
// literals (rather than pulled from a semconv/vX.Y.Z package) so a semconv
// version bump can never silently change what SigNoz reads.
const (
	exceptionEventName    = "exception"
	attrExceptionType     = "exception.type"
	attrExceptionMessage  = "exception.message"
	attrExceptionStack    = "exception.stacktrace"
	attrExceptionEscaped  = "exception.escaped"
	defaultExceptionType  = "error"
	defaultPanicException = "panic"
)

// dedupCtxKey marks a context that carries a per-scope dedup set.
type dedupCtxKey struct{}

// WithDedup returns a context carrying a fresh fingerprint set scoped to the
// current unit of work (an HTTP request, a Temporal activity, a pubsub handler).
// While that context is in effect, Record/RecordException will record any given
// (type, message) pair at most once — so a handler that both logs an error and
// explicitly captures it does not produce two identical exception events.
//
// When a context has no dedup set (the common case for ad-hoc/background work),
// recording proceeds without deduplication. Seeding is therefore an optimization,
// never a correctness requirement.
func WithDedup(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Value(dedupCtxKey{}).(*sync.Map); ok {
		return ctx // already seeded; don't reset the set mid-scope
	}
	return context.WithValue(ctx, dedupCtxKey{}, &sync.Map{})
}

// markSeen reports whether (errType, message) should be recorded now. It returns
// true the first time a fingerprint is seen within a dedup scope and false on
// repeats. With no dedup set in ctx it always returns true.
func markSeen(ctx context.Context, errType, message string) bool {
	set, ok := ctx.Value(dedupCtxKey{}).(*sync.Map)
	if !ok {
		return true
	}
	_, loaded := set.LoadOrStore(errType+"\x00"+message, struct{}{})
	return !loaded
}

// Record records err as an exception event on the active span in ctx and flips
// the span status to Error. It is a no-op (returning false) when ctx carries no
// recording span — callers that need the error captured even without a span
// should use tracing.Service.CaptureException, which synthesizes one.
func Record(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	return RecordException(ctx, fmt.Sprintf("%T", err), err.Error())
}

// RecordException records an exception event from an already-extracted type and
// message. internal/logger uses this for auto-capture, where the error has often
// already been reduced to its structured "error"/"error_type" fields.
func RecordException(ctx context.Context, errType, message string) bool {
	span := trace.SpanFromContext(ctx)
	if span == nil || !span.SpanContext().IsValid() || !span.IsRecording() {
		return false
	}
	if errType == "" {
		errType = defaultExceptionType
	}
	if !markSeen(ctx, errType, message) {
		return false
	}
	span.AddEvent(exceptionEventName, trace.WithAttributes(
		attribute.String(attrExceptionType, errType),
		attribute.String(attrExceptionMessage, message),
		attribute.String(attrExceptionStack, currentStack()),
	))
	span.SetStatus(codes.Error, message)
	return true
}

// RecordPanic records a recovered panic value as an exception event with the
// unwinding stacktrace and marks it escaped. Intended for recovery middleware.
func RecordPanic(ctx context.Context, recovered any) bool {
	span := trace.SpanFromContext(ctx)
	if span == nil || !span.SpanContext().IsValid() || !span.IsRecording() {
		return false
	}
	message := fmt.Sprintf("%v", recovered)
	span.AddEvent(exceptionEventName, trace.WithAttributes(
		attribute.String(attrExceptionType, defaultPanicException),
		attribute.String(attrExceptionMessage, message),
		attribute.String(attrExceptionStack, string(debug.Stack())),
		attribute.Bool(attrExceptionEscaped, true),
	))
	span.SetStatus(codes.Error, message)
	return true
}

// currentStack captures the calling goroutine's stack. Equivalent in spirit to
// trace.WithStackTrace(true) on RecordError, but lets us emit the event directly.
func currentStack() string {
	buf := make([]byte, 8192)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}
