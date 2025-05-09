package cache

import (
	"context"

	"github.com/getsentry/sentry-go"
)

// StartCache	Span creates a new span for a cache operation
// Returns nil if Sentry is not available in the context
func StartCacheSpan(ctx context.Context, cache, operation string, params map[string]interface{}) *sentry.Span {
	// Get the hub from the context
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		return nil
	}

	// Create a new span for this operation
	span := sentry.StartSpan(ctx, "cache."+cache+"."+operation)
	if span != nil {
		span.Description = "cache." + cache + "." + operation
		span.Op = "db.cache"

		// Add repository data
		span.SetData("cache", cache)
		span.SetData("operation", operation)

		// Add additional parameters
		for k, v := range params {
			span.SetData(k, v)
		}
	}

	return span
}

// FinishSpan safely finishes a span, handling nil spans
func FinishSpan(span *sentry.Span) {
	if span != nil {
		span.Finish()
	}
}

// SetSpanError marks a span as failed and adds error information
func SetSpanError(span *sentry.Span, err error) {
	if span == nil || err == nil {
		return
	}

	span.Status = sentry.SpanStatusInternalError
	span.SetData("error", err.Error())
}

// SetSpanSuccess marks a span as successful
func SetSpanSuccess(span *sentry.Span) {
	if span != nil {
		span.Status = sentry.SpanStatusOK
	}
}
