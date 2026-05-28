package postgres

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/tracing"
	"github.com/flexprice/flexprice/internal/types"
)

// TracingClient wraps the standard postgres client with OTel span tracking.
type TracingClient struct {
	client  IClient
	tracing *tracing.Service
	logger  *logger.Logger
}

// NewTracingClient creates a new OTel-instrumented Postgres client.
func NewTracingClient(client IClient, tracingSvc *tracing.Service, logger *logger.Logger) IClient {
	return &TracingClient{
		client:  client,
		tracing: tracingSvc,
		logger:  logger,
	}
}

// WithTx wraps the given function in a transaction.
//
// A "postgres.transaction" child span is created only when
// otel.traces.storage_spans_enabled is true. At default (false) the span is
// skipped to avoid one extra row per HTTP request in Sentry/SigNoz; the parent
// HTTP span from otelgin already captures total request latency.
func (c *TracingClient) WithTx(ctx context.Context, fn func(context.Context) error) error {
	if !c.tracing.IsStorageSpansEnabled() {
		return c.client.WithTx(ctx, fn)
	}
	span, spanCtx := c.tracing.StartDBSpan(ctx, "postgres.transaction", nil)
	defer span.Finish()
	err := c.client.WithTx(spanCtx, fn)
	if err != nil {
		span.SetStatusError(err)
	} else {
		span.SetStatusOK()
	}
	return err
}

// TxFromContext returns the transaction from context if it exists.
func (c *TracingClient) TxFromContext(ctx context.Context) *ent.Tx {
	return c.client.TxFromContext(ctx)
}

// Writer returns the writer client for write operations.
func (c *TracingClient) Writer(ctx context.Context) *ent.Client {
	if span := c.tracing.GetSpanFromContext(ctx); span != nil {
		span.SetTag("db.endpoint", "writer")
		span.SetTag("db.resolved_target", "writer")
	}
	return c.client.Writer(ctx)
}

// Reader returns the appropriate client for read operations.
func (c *TracingClient) Reader(ctx context.Context) *ent.Client {
	actualTarget := "reader"
	if c.client.TxFromContext(ctx) != nil {
		actualTarget = "writer_via_tx"
	} else if types.ShouldForceWriter(ctx) {
		actualTarget = "writer_forced"
	}

	if span := c.tracing.GetSpanFromContext(ctx); span != nil {
		span.SetTag("db.endpoint", "reader")
		span.SetTag("db.resolved_target", actualTarget)
	}

	return c.client.Reader(ctx)
}

// LockWithWait acquires an advisory lock with wait.
func (c *TracingClient) LockWithWait(ctx context.Context, req LockRequest) error {
	return c.client.LockWithWait(ctx, req)
}

// Close closes the database connection.
func (c *TracingClient) Close() error {
	return c.client.Close()
}
