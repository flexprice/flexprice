package cache

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Cache source labels used in observability events.
const (
	SourceInMemory = "inmemory"
	SourceRedis    = "redis"
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

// entityFromKey derives the domain-entity label from a cache key. Keys are
// generated as "<entity>:v1:<tenant>:<env>:<id>" (see GenerateKey), so the
// first colon-delimited segment identifies the entity.
func entityFromKey(key string) string {
	if i := strings.IndexByte(key, ':'); i >= 0 {
		return key[:i]
	}
	return key
}
