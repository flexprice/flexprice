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

// tracingSvc is wired once at startup via SetTracingService (see
// internal/repository.InitTracing), letting every cache call emit a span
// without threading *tracing.Service through each repository constructor.
var tracingSvc *tracing.Service

// SetTracingService wires the tracing service used by StartRedisCacheSpan.
// Must be called once during app startup before any cache call runs.
func SetTracingService(svc *tracing.Service) {
	tracingSvc = svc
}

// StartRedisCacheSpan creates a span for a Redis cache operation and returns
// a child context carrying the span. Callers should pass the returned ctx to
// the underlying cache call (e.g. redisCache.Get(ctx, key)) so the impl can
// auto-tag cache.hit on the same span via trace.SpanFromContext.
//
// Gated by FLEXPRICE_OTEL_TRACES_STORAGE_SPANS_ENABLED (same flag as the
// DB / ClickHouse storage spans). Emitted as a SpanKindClient span with
// db.system=redis so it is recognized as a database call by trace backends
// (e.g. SigNoz's Database Calls tab).
func StartRedisCacheSpan(ctx context.Context, cacheEntity, operation string, params map[string]interface{}) (*tracing.Span, context.Context) {
	if tracingSvc == nil {
		return nil, ctx
	}
	return tracingSvc.StartCacheSpan(ctx, "redis", cacheEntity, operation, params)
}

// StartInMemoryCacheSpan creates a span for an in-memory cache operation and
// returns a child context carrying the span.
//
// Same gating and span shape as StartRedisCacheSpan, but tagged with
// db.system=in_memory so trace backends can distinguish local hits from
// Redis round-trips (latency profile is completely different).
func StartInMemoryCacheSpan(ctx context.Context, cacheEntity, operation string, params map[string]interface{}) (*tracing.Span, context.Context) {
	if tracingSvc == nil {
		return nil, ctx
	}
	return tracingSvc.StartCacheSpan(ctx, "in_memory", cacheEntity, operation, params)
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

// SetCacheHit tags a cache span with cache.hit=true/false. Call from Get
// paths after the lookup so trace backends can compute per-entity hit rate.
// No-op on nil spans; Set/Delete paths should not call this (the concept
// doesn't apply).
func SetCacheHit(span *tracing.Span, hit bool) {
	// SetCacheHit is nil-safe; it tags the span (if any) and stashes hit/miss
	// for the cache.requests metric emitted on Finish.
	span.SetCacheHit(hit)
}
