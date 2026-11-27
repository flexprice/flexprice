package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/flexprice/flexprice/internal/spanerr"
)

// StartSpan is a no-op without tracing.
func StartSpan(ctx context.Context, name string, attrs ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.Tracer(tracerName).Start(ctx, name, attrs...)
}

// EndSpan ends the span in ctx.
func EndSpan(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	// Deduped errors still mark status.
	if err != nil && !spanerr.Record(ctx, err) {
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
