package clickhouse

import (
	"context"

	"github.com/flexprice/flexprice/internal/tracing"
)

// tracingSvc is wired once at startup via SetTracingService (see
// internal/repository.InitTracing), letting every repository method emit a
// span without threading *tracing.Service through each constructor.
var tracingSvc *tracing.Service

// SetTracingService wires the tracing service used by StartRepositorySpan.
// Must be called once during app startup before any repository method runs.
func SetTracingService(svc *tracing.Service) {
	tracingSvc = svc
}

// StartRepositorySpan creates a span for a ClickHouse repository operation.
//
// Gated by FLEXPRICE_OTEL_TRACES_STORAGE_SPANS_ENABLED (same flag as the
// lower-level tracedConn query spans). Emitted as a SpanKindClient span with
// db.system=clickhouse so it is recognized as a database call by trace
// backends (e.g. SigNoz's Database Calls tab).
func StartRepositorySpan(ctx context.Context, repository, operation string, params map[string]interface{}) *tracing.Span {
	if tracingSvc == nil {
		return nil
	}
	span, _ := tracingSvc.StartRepositorySpan(ctx, "clickhouse", repository, operation, params)
	return span
}

// FinishSpan safely finishes a span, handling nil spans.
func FinishSpan(span *tracing.Span) {
	span.Finish()
}

// SetSpanError marks a span as failed and adds error information.
func SetSpanError(span *tracing.Span, err error) {
	span.SetStatusError(err)
}

// SetSpanSuccess marks a span as successful.
func SetSpanSuccess(span *tracing.Span) {
	span.SetStatusOK()
}
