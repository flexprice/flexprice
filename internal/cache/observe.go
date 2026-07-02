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

// RecordLookup records a cache lookup outcome as an OTel span event, emitting a
// hit event when hit is true and a miss event otherwise. entity is the domain
// entity name (e.g. "customer"), source is "inmemory" or "redis".
func RecordLookup(ctx context.Context, entity, source string, hit bool) {
	name := "cache.miss"
	if hit {
		name = "cache.hit"
	}
	recordEvent(ctx, name, entity, source)
}

// RecordSet records a cache write as an OTel span event.
func RecordSet(ctx context.Context, entity, source string) {
	recordEvent(ctx, "cache.set", entity, source)
}

// RecordDelete records a cache invalidation as an OTel span event.
func RecordDelete(ctx context.Context, entity, source string) {
	recordEvent(ctx, "cache.delete", entity, source)
}

// RecordError records a cache backend failure (e.g. a Redis transport/timeout
// error) as an OTel span event. It is deliberately distinct from a lookup miss
// so genuine "cold cache" misses stay separable from "cache is degraded" in
// miss-rate metrics.
func RecordError(ctx context.Context, entity, source string) {
	recordEvent(ctx, "cache.error", entity, source)
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
