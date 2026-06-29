package interceptor

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/interceptor"
)

// WriterPinInterceptor installs a per-activity writer pin and routing stats
// counter on the activity context. The first postgres write inside the activity
// flips the pin so all subsequent reads in the same activity go to the writer.
//
// InterceptWorkflow is intentionally not implemented: Temporal workflow
// functions must be deterministic and cannot perform I/O or DB calls.
// All DB access happens in activities, which each get a fresh pin via
// ExecuteActivity. If DB calls are ever added to workflow functions (which
// violates Temporal best practices), those calls will lack a pin holder —
// that is the correct signal that something is wrong.
type WriterPinInterceptor struct {
	interceptor.InterceptorBase
	logger *logger.Logger
}

// NewWriterPinInterceptor constructs the interceptor.
func NewWriterPinInterceptor(log *logger.Logger) *WriterPinInterceptor {
	return &WriterPinInterceptor{logger: log}
}

// InterceptActivity creates an activity inbound interceptor that installs
// a writer pin and routing stats per activity execution.
func (s *WriterPinInterceptor) InterceptActivity(_ context.Context, next interceptor.ActivityInboundInterceptor) interceptor.ActivityInboundInterceptor {
	return &writerPinActivityInterceptor{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
		logger:                         s.logger,
	}
}

type writerPinActivityInterceptor struct {
	interceptor.ActivityInboundInterceptorBase
	logger *logger.Logger
}

// ExecuteActivity wraps the activity context with a writer pin and routing stats.
func (a *writerPinActivityInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	ctx = types.WithWriterPinning(ctx)
	ctx = types.WithRoutingStats(ctx)

	start := time.Now()
	result, err := a.Next.ExecuteActivity(ctx, in)

	if stats := types.GetRoutingStats(ctx); stats != nil && a.logger != nil {
		a.logger.Debug(ctx, "db_routing_summary",
			"entrypoint", "temporal_activity",
			"reader", stats.Reader.Load(),
			"writer_pinned", stats.WriterPinned.Load(),
			"writer_tx", stats.WriterTx.Load(),
			"writer_calls", stats.WriterCalls.Load(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}

	return result, err
}
