package cache

import (
	"context"
	"encoding/json"

	"github.com/getsentry/sentry-go"
)

// UnmarshalCacheValue attempts to convert a cache value to the specified type.
// It handles both in-memory cache (which stores actual objects) and Redis cache (which stores JSON strings).
// Returns the typed value and true if successful, nil and false otherwise.
func UnmarshalCacheValue[T any](value interface{}) (*T, bool) {
	if value == nil {
		return nil, false
	}

	// Try direct type assertion first (for in-memory cache)
	if typed, ok := value.(*T); ok {
		return typed, true
	}

	// Try type assertion for non-pointer type T (some caches may store values directly)
	if typed, ok := value.(T); ok {
		return &typed, true
	}

	// Try unmarshalling from JSON string (for Redis cache)
	if str, ok := value.(string); ok {
		var result T
		if err := json.Unmarshal([]byte(str), &result); err == nil {
			return &result, true
		}
	}

	return nil, false
}

// StartCache	Span creates a new span for a cache operation
// Returns nil if Sentry is not available in the context
func StartCacheSpan(ctx context.Context, cache, operation string, params map[string]interface{}) *sentry.Span {
	// Get the hub from the context
	// hub := sentry.GetHubFromContext(ctx)
	// if hub == nil {
	// 	return nil
	// }

	// // Create a new span for this operation
	// span := sentry.StartSpan(ctx, "cache."+cache+"."+operation)
	// if span != nil {
	// 	span.Description = "cache." + cache + "." + operation
	// 	span.Op = "db.cache"

	// 	// Add repository data
	// 	span.SetData("cache", cache)
	// 	span.SetData("operation", operation)

	// 	// Add additional parameters
	// 	for k, v := range params {
	// 		span.SetData(k, v)
	// 	}
	// }

	// return span
	return nil
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
