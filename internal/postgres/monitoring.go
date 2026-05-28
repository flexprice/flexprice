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
// We deliberately do NOT start a separate "postgres.transaction" span here.
// Under the old sentry-go SDK, child spans were embedded inside the parent
// transaction's payload and never appeared as standalone rows. With OTel +
// Sentry's OTLP integration, every span becomes its own searchable row, which
// clutters the trace list with one extra row per HTTP request. The timing
// information for the transaction is already captured implicitly by the parent
// HTTP span (otelgin); per-statement detail will come from otelpgx when added.
//
// We still tag the parent span in Writer/Reader below so observers can see
// which endpoint each query hit.
func (c *TracingClient) WithTx(ctx context.Context, fn func(context.Context) error) error {
	return c.client.WithTx(ctx, fn)
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
