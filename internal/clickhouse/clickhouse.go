package clickhouse

import (
	"context"
	"fmt"

	clickhouse_go "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/sentry"
)

type ClickHouseStore struct {
	conn   driver.Conn
	sentry *sentry.Service
}

func NewClickHouseStore(config *config.Configuration, sentryService *sentry.Service) (*ClickHouseStore, error) {
	options := config.ClickHouse.GetClientOptions()
	conn, err := clickhouse_go.Open(options)
	if err != nil {
		return nil, fmt.Errorf("init clickhouse client: %w", err)
	}

	return &ClickHouseStore{
		conn:   conn,
		sentry: sentryService,
	}, nil
}

// TracedConn returns a connection that automatically traces all database operations
func (s *ClickHouseStore) GetConn() driver.Conn {
	return &tracedConn{
		conn:   s.conn,
		sentry: s.sentry,
	}
}

// Original connection accessor if needed
func (s *ClickHouseStore) GetRawConn() driver.Conn {
	return s.conn
}

func (s *ClickHouseStore) Close() error {
	return s.conn.Close()
}

// WithSpan creates a new context with a ClickHouse span for monitoring database operations
func (s *ClickHouseStore) WithSpan(ctx context.Context, operation string, params map[string]interface{}) (context.Context, *sentry.SpanFinisher) {
	if s.sentry == nil {
		return ctx, &sentry.SpanFinisher{}
	}

	span, newCtx := s.sentry.StartClickHouseSpan(ctx, operation, params)
	return newCtx, &sentry.SpanFinisher{Span: span}
}

// tracedConn is a wrapper around the ClickHouse Conn interface that adds tracing
type tracedConn struct {
	conn   driver.Conn
	sentry *sentry.Service
}

// Contributors delegates to the underlying connection
func (tc *tracedConn) Contributors() []string {
	return tc.conn.Contributors()
}

// ServerVersion delegates to the underlying connection
func (tc *tracedConn) ServerVersion() (*driver.ServerVersion, error) {
	return tc.conn.ServerVersion()
}

// Select adds tracing and delegates to the underlying connection
func (tc *tracedConn) Select(ctx context.Context, dest any, query string, args ...any) error {
	if tc.sentry == nil {
		return tc.conn.Select(ctx, dest, query, args...)
	}

	span, ctx := tc.sentry.StartClickHouseSpan(ctx, "clickhouse.select", map[string]interface{}{
		"query":      truncateQuery(query),
		"args_count": len(args),
	})
	if span != nil {
		defer span.Finish()
	}

	return tc.conn.Select(ctx, dest, query, args...)
}

// Query adds tracing and delegates to the underlying connection
func (tc *tracedConn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	if tc.sentry == nil {
		return tc.conn.Query(ctx, query, args...)
	}

	span, ctx := tc.sentry.StartClickHouseSpan(ctx, "clickhouse.query", map[string]interface{}{
		"query":      truncateQuery(query),
		"args_count": len(args),
	})
	if span != nil {
		defer span.Finish()
	}

	return tc.conn.Query(ctx, query, args...)
}

// QueryRow adds tracing and delegates to the underlying connection
func (tc *tracedConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	if tc.sentry == nil {
		return tc.conn.QueryRow(ctx, query, args...)
	}

	span, ctx := tc.sentry.StartClickHouseSpan(ctx, "clickhouse.query_row", map[string]interface{}{
		"query":      truncateQuery(query),
		"args_count": len(args),
	})
	if span != nil {
		defer span.Finish()
	}

	return tc.conn.QueryRow(ctx, query, args...)
}

// PrepareBatch adds tracing and delegates to the underlying connection
func (tc *tracedConn) PrepareBatch(ctx context.Context, query string) (driver.Batch, error) {
	if tc.sentry == nil {
		return tc.conn.PrepareBatch(ctx, query)
	}

	span, ctx := tc.sentry.StartClickHouseSpan(ctx, "clickhouse.prepare_batch", map[string]interface{}{
		"query": truncateQuery(query),
	})
	if span != nil {
		defer span.Finish()
	}

	batch, err := tc.conn.PrepareBatch(ctx, query)
	if err != nil {
		return nil, err
	}

	return &tracedBatch{
		batch:  batch,
		sentry: tc.sentry,
	}, nil
}

// Exec adds tracing and delegates to the underlying connection
func (tc *tracedConn) Exec(ctx context.Context, query string, args ...any) error {
	if tc.sentry == nil {
		return tc.conn.Exec(ctx, query, args...)
	}

	span, ctx := tc.sentry.StartClickHouseSpan(ctx, "clickhouse.exec", map[string]interface{}{
		"query":      truncateQuery(query),
		"args_count": len(args),
	})
	if span != nil {
		defer span.Finish()
	}

	return tc.conn.Exec(ctx, query, args...)
}

// AsyncInsert adds tracing and delegates to the underlying connection
func (tc *tracedConn) AsyncInsert(ctx context.Context, query string, wait bool) error {
	if tc.sentry == nil {
		return tc.conn.AsyncInsert(ctx, query, wait)
	}

	span, ctx := tc.sentry.StartClickHouseSpan(ctx, "clickhouse.async_insert", map[string]interface{}{
		"query": truncateQuery(query),
		"wait":  wait,
	})
	if span != nil {
		defer span.Finish()
	}

	return tc.conn.AsyncInsert(ctx, query, wait)
}

// Ping adds tracing and delegates to the underlying connection
func (tc *tracedConn) Ping(ctx context.Context) error {
	if tc.sentry == nil {
		return tc.conn.Ping(ctx)
	}

	span, ctx := tc.sentry.StartClickHouseSpan(ctx, "clickhouse.ping", nil)
	if span != nil {
		defer span.Finish()
	}

	return tc.conn.Ping(ctx)
}

// Stats delegates to the underlying connection
func (tc *tracedConn) Stats() driver.Stats {
	return tc.conn.Stats()
}

// Close delegates to the underlying connection
func (tc *tracedConn) Close() error {
	return tc.conn.Close()
}

// tracedBatch is a wrapper around the ClickHouse Batch interface that adds tracing
type tracedBatch struct {
	batch  driver.Batch
	sentry *sentry.Service
}

// Append delegates to the underlying batch
func (tb *tracedBatch) Append(v ...any) error {
	return tb.batch.Append(v...)
}

// AppendStruct delegates to the underlying batch
func (tb *tracedBatch) AppendStruct(v any) error {
	return tb.batch.AppendStruct(v)
}

// Column delegates to the underlying batch
func (tb *tracedBatch) Column(idx int) driver.BatchColumn {
	return tb.batch.Column(idx)
}

// Abort delegates to the underlying batch
func (tb *tracedBatch) Abort() error {
	return tb.batch.Abort()
}

// Flush delegates to the underlying batch
func (tb *tracedBatch) Flush() error {
	if tb.sentry == nil {
		return tb.batch.Flush()
	}

	ctx := context.Background()
	span, _ := tb.sentry.StartClickHouseSpan(ctx, "clickhouse.batch_flush", nil)
	if span != nil {
		defer span.Finish()
	}

	return tb.batch.Flush()
}

// Send adds tracing and delegates to the underlying batch
func (tb *tracedBatch) Send() error {
	if tb.sentry == nil {
		return tb.batch.Send()
	}

	ctx := context.Background()
	span, _ := tb.sentry.StartClickHouseSpan(ctx, "clickhouse.batch_send", map[string]interface{}{
		"count": tb.batch.IsSent(),
	})
	if span != nil {
		defer span.Finish()
	}

	return tb.batch.Send()
}

// IsSent delegates to the underlying batch
func (tb *tracedBatch) IsSent() bool {
	return tb.batch.IsSent()
}

// Truncate query to avoid sending too much data to Sentry
func truncateQuery(query string) string {
	const maxQueryLength = 1000
	if len(query) > maxQueryLength {
		return query[:maxQueryLength] + "..."
	}
	return query
}
