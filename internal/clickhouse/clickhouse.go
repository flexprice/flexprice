package clickhouse

import (
	"context"
	"fmt"

	clickhouse_go "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/tracing"
)

type ClickHouseStore struct {
	conn    driver.Conn
	tracing *tracing.Service
}

func NewClickHouseStore(config *config.Configuration, tracingService *tracing.Service) (*ClickHouseStore, error) {
	options := config.ClickHouse.GetClientOptions()
	conn, err := clickhouse_go.Open(options)
	if err != nil {
		return nil, fmt.Errorf("init clickhouse client: %w", err)
	}

	return &ClickHouseStore{
		conn:    conn,
		tracing: tracingService,
	}, nil
}

// GetConn returns the ClickHouse driver connection.
//
// When otel.traces.storage_spans_enabled is true (env:
// FLEXPRICE_OTEL_TRACES_STORAGE_SPANS_ENABLED=true), the connection is wrapped
// in tracedConn so every Select/QueryRow/Ping/Batch call emits a child span.
// The flag defaults to false to avoid span volume explosion before operators
// have a feel for the cost at scale.
func (s *ClickHouseStore) GetConn() driver.Conn {
	if s.tracing != nil && s.tracing.IsStorageSpansEnabled() {
		return &tracedConn{Conn: s.conn, tracing: s.tracing}
	}
	return s.conn
}

// Original connection accessor if needed
func (s *ClickHouseStore) GetRawConn() driver.Conn {
	return s.conn
}

func (s *ClickHouseStore) Close() error {
	return s.conn.Close()
}

// WithSpan creates a new context with a ClickHouse span for monitoring database operations
func (s *ClickHouseStore) WithSpan(ctx context.Context, operation string, params map[string]interface{}) (context.Context, *tracing.SpanFinisher) {
	if s.tracing == nil {
		return ctx, &tracing.SpanFinisher{}
	}

	span, newCtx := s.tracing.StartClickHouseSpan(ctx, operation, params)
	return newCtx, &tracing.SpanFinisher{Span: span}
}

// tracedConn is a wrapper around the ClickHouse Conn interface that adds tracing.
//
// driver.Conn is embedded rather than implemented: the driver adds methods to it
// in minor releases, which breaks any type that spells out every method.
type tracedConn struct {
	driver.Conn
	tracing *tracing.Service
}

// Select adds tracing and delegates to the underlying connection
func (tc *tracedConn) Select(ctx context.Context, dest any, query string, args ...any) error {
	if tc.tracing == nil {
		return tc.Conn.Select(ctx, dest, query, args...)
	}

	span, ctx := tc.tracing.StartClickHouseSpan(ctx, "clickhouse.select", map[string]interface{}{
		"db.statement": truncateQuery(query),
		"args_count":   len(args),
	})
	defer span.Finish()

	err := tc.Conn.Select(ctx, dest, query, args...)
	span.SetStatusError(err)
	return err
}

// Query adds tracing and delegates to the underlying connection
func (tc *tracedConn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	if tc.tracing == nil {
		return tc.Conn.Query(ctx, query, args...)
	}

	// Note: the span covers the query call, not consumption of the returned
	// rows, so it measures time-to-first-response rather than full read time.
	span, ctx := tc.tracing.StartClickHouseSpan(ctx, "clickhouse.query", map[string]interface{}{
		"db.statement": truncateQuery(query),
		"args_count":   len(args),
	})
	defer span.Finish()

	rows, err := tc.Conn.Query(ctx, query, args...)
	span.SetStatusError(err)
	return rows, err
}

// QueryRow adds tracing and delegates to the underlying connection
func (tc *tracedConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	if tc.tracing == nil {
		return tc.Conn.QueryRow(ctx, query, args...)
	}

	span, ctx := tc.tracing.StartClickHouseSpan(ctx, "clickhouse.query_row", map[string]interface{}{
		"db.statement": truncateQuery(query),
		"args_count":   len(args),
	})
	defer span.Finish()

	return tc.Conn.QueryRow(ctx, query, args...)
}

// PrepareBatch adds tracing and delegates to the underlying connection
func (tc *tracedConn) PrepareBatch(ctx context.Context, query string, options ...driver.PrepareBatchOption) (driver.Batch, error) {
	if tc.tracing == nil {
		return tc.Conn.PrepareBatch(ctx, query, options...)
	}

	span, ctx := tc.tracing.StartClickHouseSpan(ctx, "clickhouse.prepare_batch", map[string]interface{}{
		"db.statement": truncateQuery(query),
	})
	defer span.Finish()

	batch, err := tc.Conn.PrepareBatch(ctx, query, options...)
	if err != nil {
		span.SetStatusError(err)
		return nil, err
	}

	return &tracedBatch{
		Batch:   batch,
		tracing: tc.tracing,
		ctx:     ctx,
	}, nil
}

// Exec adds tracing and delegates to the underlying connection
func (tc *tracedConn) Exec(ctx context.Context, query string, args ...any) error {
	if tc.tracing == nil {
		return tc.Conn.Exec(ctx, query, args...)
	}

	span, ctx := tc.tracing.StartClickHouseSpan(ctx, "clickhouse.exec", map[string]interface{}{
		"db.statement": truncateQuery(query),
		"args_count":   len(args),
	})
	defer span.Finish()

	err := tc.Conn.Exec(ctx, query, args...)
	span.SetStatusError(err)
	return err
}

// AsyncInsert adds tracing and delegates to the underlying connection
func (tc *tracedConn) AsyncInsert(ctx context.Context, query string, wait bool, args ...any) error {
	if tc.tracing == nil {
		return tc.Conn.AsyncInsert(ctx, query, wait, args...)
	}

	span, ctx := tc.tracing.StartClickHouseSpan(ctx, "clickhouse.async_insert", map[string]interface{}{
		"db.statement": truncateQuery(query),
		"wait":         wait,
	})
	defer span.Finish()

	err := tc.Conn.AsyncInsert(ctx, query, wait, args...)
	span.SetStatusError(err)
	return err
}

// Ping adds tracing and delegates to the underlying connection
func (tc *tracedConn) Ping(ctx context.Context) error {
	if tc.tracing == nil {
		return tc.Conn.Ping(ctx)
	}

	span, ctx := tc.tracing.StartClickHouseSpan(ctx, "clickhouse.ping", nil)
	defer span.Finish()

	err := tc.Conn.Ping(ctx)
	span.SetStatusError(err)
	return err
}

// tracedBatch is a wrapper around the ClickHouse Batch interface that adds tracing.
//
// driver.Batch is embedded for the same reason as driver.Conn in tracedConn.
type tracedBatch struct {
	driver.Batch
	tracing *tracing.Service
	ctx     context.Context // context captured at PrepareBatch time for child spans
}

// Flush delegates to the underlying batch
func (tb *tracedBatch) Flush() error {
	if tb.tracing == nil {
		return tb.Batch.Flush()
	}

	// Use the context captured at PrepareBatch time so the flush span is a
	// child of the originating request trace rather than a detached root span.
	span, _ := tb.tracing.StartClickHouseSpan(tb.ctx, "clickhouse.batch_flush", nil)
	defer span.Finish()

	err := tb.Batch.Flush()
	span.SetStatusError(err)
	return err
}

// Send adds tracing and delegates to the underlying batch
func (tb *tracedBatch) Send() error {
	if tb.tracing == nil {
		return tb.Batch.Send()
	}

	// Use the context captured at PrepareBatch time so the send span is a
	// child of the originating request trace rather than a detached root span.
	span, _ := tb.tracing.StartClickHouseSpan(tb.ctx, "clickhouse.batch_send", map[string]interface{}{
		"rows": tb.Batch.Rows(),
	})
	defer span.Finish()

	err := tb.Batch.Send()
	span.SetStatusError(err)
	return err
}

// truncateQuery limits query length to keep OTel span attributes small.
func truncateQuery(query string) string {
	const maxQueryLength = 1000
	if len(query) > maxQueryLength {
		return query[:maxQueryLength] + "..."
	}
	return query
}
