package kafka

import (
	"context"

	"github.com/flexprice/flexprice/internal/tracing"
)

// tracingSvc is wired once at startup via SetTracingService (see
// internal/repository.InitTracing), mirroring the ent/clickhouse/cache
// repository helpers. It lets the Kafka consumer message handlers — which
// build a fresh context.Background() per message and don't take
// *tracing.Service through their constructors — start a root span without
// threading the service as a dependency.
var tracingSvc *tracing.Service

// SetTracingService wires the tracing service used by StartConsumerSpan.
// Must be called once during app startup before any consumer handler runs.
func SetTracingService(svc *tracing.Service) {
	tracingSvc = svc
}

// StartConsumerSpan starts a root span for a single consumed message and
// returns it along with a context carrying that span.
//
// This is what makes DB routing observable on the consumer workloads: the
// reader/writer router records db.resolved_target on the span found in the
// context (GetSpanFromContext). Consumer handlers process messages on a bare
// context.Background(), which has no span, so without this the tag is silently
// dropped. Call it once at the top of each message's processing, mirroring the
// otelgin server span the HTTP API gets per request.
//
// Safe when tracing is disabled or unwired: returns (nil, ctx) unchanged, and
// the returned *tracing.Span's methods (Finish, ...) are all nil-safe.
func StartConsumerSpan(ctx context.Context, name string) (*tracing.Span, context.Context) {
	if tracingSvc == nil {
		return nil, ctx
	}
	return tracingSvc.StartKafkaConsumerSpan(ctx, name)
}
