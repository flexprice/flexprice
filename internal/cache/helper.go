package cache

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/tracing"
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

// StartCacheSpan creates a new span for a cache operation.
// Currently a no-op; preserved as a hook for selective re-enablement.
func StartCacheSpan(ctx context.Context, cache, operation string, params map[string]interface{}) *tracing.Span {
	_ = ctx
	_ = cache
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
