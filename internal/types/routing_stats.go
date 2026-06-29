package types

import (
	"context"
	"sync/atomic"
)

const CtxRoutingStats ContextKey = "ctx_routing_stats"

// RoutingStats tracks DB routing decisions for one unit of work.
// All fields are atomic so concurrent goroutines in the same request are safe.
type RoutingStats struct {
	Reader       atomic.Int64
	WriterPinned atomic.Int64
	WriterTx     atomic.Int64
	WriterForced atomic.Int64
	WriterCalls  atomic.Int64
}

// WithRoutingStats installs a fresh RoutingStats counter on the context.
func WithRoutingStats(ctx context.Context) context.Context {
	return context.WithValue(ctx, CtxRoutingStats, &RoutingStats{})
}

// GetRoutingStats returns the RoutingStats from context, or nil if not installed.
func GetRoutingStats(ctx context.Context) *RoutingStats {
	if s, ok := ctx.Value(CtxRoutingStats).(*RoutingStats); ok {
		return s
	}
	return nil
}
