package clickhouse

import (
	"context"
	"fmt"
	"time"

	clickhouse_go "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/getsentry/sentry-go"
)

type ClickHouseStore struct {
	conn   driver.Conn
	config *config.Configuration
	logger *logger.Logger
}

func NewClickHouseStore(config *config.Configuration, logger *logger.Logger) (*ClickHouseStore, error) {
	options := config.ClickHouse.GetClientOptions()
	conn, err := clickhouse_go.Open(options)
	if err != nil {
		return nil, fmt.Errorf("init clickhouse client: %w", err)
	}

	return &ClickHouseStore{
		conn:   conn,
		config: config,
		logger: logger,
	}, nil
}

func (s *ClickHouseStore) GetConn() driver.Conn {
	return s.conn
}

// Close closes the ClickHouse connection
func (s *ClickHouseStore) Close() error {
	return s.conn.Close()
}

// Query executes a query with Sentry monitoring
func (s *ClickHouseStore) Query(ctx context.Context, query string, args ...interface{}) (driver.Rows, error) {
	if s.config.Sentry.Enabled {
		span := sentry.StartSpan(ctx, "clickhouse.query")
		span.Description = query
		span.Op = "db.clickhouse.query"
		span.SetData("query", query)

		defer span.Finish()

		start := time.Now()
		rows, err := s.conn.Query(ctx, query, args...)

		span.SetData("duration_ms", time.Since(start).Milliseconds())
		if err != nil {
			span.Status = sentry.SpanStatusInternalError
			span.SetData("error", err.Error())
		} else {
			span.Status = sentry.SpanStatusOK
		}

		return rows, err
	}

	return s.conn.Query(ctx, query, args...)
}

// QueryRow executes a query that returns a single row with Sentry monitoring
func (s *ClickHouseStore) QueryRow(ctx context.Context, query string, args ...interface{}) driver.Row {
	if s.config.Sentry.Enabled {
		span := sentry.StartSpan(ctx, "clickhouse.query_row")
		span.Description = query
		span.Op = "db.clickhouse.query_row"
		span.SetData("query", query)

		defer span.Finish()

		start := time.Now()
		row := s.conn.QueryRow(ctx, query, args...)

		span.SetData("duration_ms", time.Since(start).Milliseconds())
		span.Status = sentry.SpanStatusOK

		return row
	}

	return s.conn.QueryRow(ctx, query, args...)
}

// Exec executes a query without returning any rows with Sentry monitoring
func (s *ClickHouseStore) Exec(ctx context.Context, query string, args ...interface{}) error {
	if s.config.Sentry.Enabled {
		span := sentry.StartSpan(ctx, "clickhouse.exec")
		span.Description = query
		span.Op = "db.clickhouse.exec"
		span.SetData("query", query)

		defer span.Finish()

		start := time.Now()
		err := s.conn.Exec(ctx, query, args...)

		span.SetData("duration_ms", time.Since(start).Milliseconds())
		if err != nil {
			span.Status = sentry.SpanStatusInternalError
			span.SetData("error", err.Error())
		} else {
			span.Status = sentry.SpanStatusOK
		}

		return err
	}

	return s.conn.Exec(ctx, query, args...)
}
