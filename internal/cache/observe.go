package cache

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Cache source labels used in observability events and spans.
const (
	SourceInMemory = "inmemory"
	SourceRedis    = "redis"

	// cacheTracerName is the instrumentation scope for cache spans.
	cacheTracerName = "github.com/flexprice/flexprice/internal/cache"
)

// recordEvent adds a cache observability event to the active span, if any.
func recordEvent(ctx context.Context, name, entity, source string) {
	sp := trace.SpanFromContext(ctx)
	if !sp.IsRecording() {
		return
	}
	sp.AddEvent(name, trace.WithAttributes(
		attribute.String("cache.entity", entity),
		attribute.String("cache.source", source),
	))
}

// RecordHit records a cache hit as an OTel span event.
// entity is the domain entity name (e.g. "customer"), source is "inmemory" or "redis".
func RecordHit(ctx context.Context, entity, source string) {
	recordEvent(ctx, "cache.hit", entity, source)
}

// RecordMiss records a cache miss as an OTel span event.
func RecordMiss(ctx context.Context, entity, source string) {
	recordEvent(ctx, "cache.miss", entity, source)
}

// RecordSet records a cache write as an OTel span event.
func RecordSet(ctx context.Context, entity, source string) {
	recordEvent(ctx, "cache.set", entity, source)
}

// RecordDelete records a cache invalidation as an OTel span event.
func RecordDelete(ctx context.Context, entity, source string) {
	recordEvent(ctx, "cache.delete", entity, source)
}

// ---------------------------------------------------------------------------
// Per-operation cache spans
//
// These create real child spans (cache.get / cache.set / cache.delete ...) so
// individual cache calls show up with their own latency in the trace, nested
// under the active request/repository span. Following the Postgres and
// ClickHouse convention (see postgres.TracingClient.WithTx), they are gated by
// otel.traces.storage_spans_enabled and default OFF to avoid span-volume
// explosion; the always-on Record* events above remain the lightweight signal.
// ---------------------------------------------------------------------------

// storageSpansEnabled reports whether per-operation storage spans should be
// created for cache calls. It mirrors tracing.Service.IsStorageSpansEnabled but
// reads the config directly so the cache layer does not depend on the tracing
// Service (the cache clients are global singletons initialized outside fx).
func storageSpansEnabled(cfg *config.Configuration) bool {
	return cfg != nil &&
		cfg.Otel.Enabled &&
		cfg.Otel.Traces.Enabled &&
		cfg.Otel.Traces.StorageSpansEnabled
}

// entityFromKey derives the domain-entity label from a cache key. Keys are
// generated as "<entity>:v1:<tenant>:<env>:<id>" (see GenerateKey), so the
// first colon-delimited segment identifies the entity.
func entityFromKey(key string) string {
	if i := strings.IndexByte(key, ':'); i >= 0 {
		return key[:i]
	}
	return key
}

// startCacheSpan starts a child span for a single cache operation (op is
// "get"/"set"/"delete"/...). When enabled is false it is a no-op that returns
// the original context and a nil span, so callers can pass the config gate
// without branching. The returned context carries the span for downstream
// clients (e.g. go-redis) that accept a context.
func startCacheSpan(ctx context.Context, enabled bool, op, source, key string) (context.Context, trace.Span) {
	if !enabled {
		return ctx, nil
	}
	ctx, span := otel.Tracer(cacheTracerName).Start(ctx, "cache."+op, trace.WithSpanKind(trace.SpanKindClient))
	span.SetAttributes(
		attribute.String("cache.operation", op),
		attribute.String("cache.source", source),
		attribute.String("cache.entity", entityFromKey(key)),
	)
	return ctx, span
}

// setCacheHit records whether a lookup hit or missed on the given span.
func setCacheHit(span trace.Span, hit bool) {
	if span == nil {
		return
	}
	span.SetAttributes(attribute.Bool("cache.hit", hit))
}

// failCacheSpan marks the span as failed with the given error.
func failCacheSpan(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// endCacheSpan ends the span, tolerating a nil span.
func endCacheSpan(span trace.Span) {
	if span != nil {
		span.End()
	}
}
