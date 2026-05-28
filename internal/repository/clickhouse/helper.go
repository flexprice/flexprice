package clickhouse

import (
	"context"

	"github.com/flexprice/flexprice/internal/tracing"
)

// StartRepositorySpan creates a new span for a repository operation.
// Currently a no-op; preserved as a hook for selective re-enablement.
func StartRepositorySpan(ctx context.Context, repository, operation string, params map[string]interface{}) *tracing.Span {
	_ = ctx
	_ = repository
	_ = operation
	_ = params
	return nil
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
