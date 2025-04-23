package postgres

import (
	"context"
	"database/sql"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

// OtelDriver wraps a SQL driver with OpenTelemetry instrumentation
type OtelDriver struct {
	dialect.Driver
	tracer trace.Tracer
	cfg    *config.Configuration
	logger *logger.Logger
}

// NewOtelDriver creates a new driver with OpenTelemetry instrumentation
func NewOtelDriver(driver dialect.Driver, cfg *config.Configuration, logger *logger.Logger) *OtelDriver {
	return &OtelDriver{
		Driver: driver,
		tracer: otel.GetTracerProvider().Tracer("github.com/flexprice/flexprice/internal/postgres"),
		cfg:    cfg,
		logger: logger,
	}
}

// Query implements the dialect.Driver Query method with tracing
func (d *OtelDriver) Query(ctx context.Context, query string, args, v interface{}) error {
	if !d.cfg.Telemetry.Enabled {
		return d.Driver.Query(ctx, query, args, v)
	}

	ctx, span := d.tracer.Start(ctx, "sql.query",
		trace.WithAttributes(
			semconv.DBSystemPostgreSQL,
			semconv.DBStatementKey.String(query),
			attribute.String("db.name", d.cfg.Postgres.DBName),
		),
	)
	defer span.End()

	// Track latency for this query
	start := time.Now()
	err := d.Driver.Query(ctx, query, args, v)
	duration := time.Since(start)

	// Also add Sentry tracing if enabled
	if d.cfg.Sentry.Enabled {
		sentrySpan := sentry.StartSpan(ctx, "db.query")
		sentrySpan.Description = query
		sentrySpan.Op = "db.postgres.query"
		sentrySpan.SetData("query", query)
		sentrySpan.SetData("duration_ms", duration.Milliseconds())
		defer sentrySpan.Finish()

		if err != nil {
			sentrySpan.Status = sentry.SpanStatusInternalError
			sentrySpan.SetData("error", err.Error())
		} else {
			sentrySpan.Status = sentry.SpanStatusOK
		}
	}

	// Add debug logging
	fields := []interface{}{
		"duration_ms", duration.Milliseconds(),
		"query", query,
	}
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		fields = append(fields, "error", err.Error())
		d.logger.Debugw("database query failed", fields...)
	} else {
		span.SetStatus(codes.Ok, "")
		d.logger.Debugw("database query completed", fields...)
	}

	return err
}

// Exec implements the dialect.Driver Exec method with tracing
func (d *OtelDriver) Exec(ctx context.Context, query string, args, v interface{}) error {
	if !d.cfg.Telemetry.Enabled {
		return d.Driver.Exec(ctx, query, args, v)
	}

	ctx, span := d.tracer.Start(ctx, "sql.exec",
		trace.WithAttributes(
			semconv.DBSystemPostgreSQL,
			semconv.DBStatementKey.String(query),
			attribute.String("db.name", d.cfg.Postgres.DBName),
		),
	)
	defer span.End()

	// Track latency for this query
	start := time.Now()
	err := d.Driver.Exec(ctx, query, args, v)
	duration := time.Since(start)

	// Also add Sentry tracing if enabled
	if d.cfg.Sentry.Enabled {
		sentrySpan := sentry.StartSpan(ctx, "db.exec")
		sentrySpan.Description = query
		sentrySpan.Op = "db.postgres.exec"
		sentrySpan.SetData("query", query)
		sentrySpan.SetData("duration_ms", duration.Milliseconds())
		defer sentrySpan.Finish()

		if err != nil {
			sentrySpan.Status = sentry.SpanStatusInternalError
			sentrySpan.SetData("error", err.Error())
		} else {
			sentrySpan.Status = sentry.SpanStatusOK
		}
	}

	// Add debug logging
	fields := []interface{}{
		"duration_ms", duration.Milliseconds(),
		"query", query,
	}
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		fields = append(fields, "error", err.Error())
		d.logger.Debugw("database exec failed", fields...)
	} else {
		span.SetStatus(codes.Ok, "")
		d.logger.Debugw("database exec completed", fields...)
	}

	return err
}

// OpenOtelDB opens a database connection with OpenTelemetry instrumentation
func OpenOtelDB(driverName, dataSourceName string, cfg *config.Configuration, logger *logger.Logger) (*sql.DB, error) {
	// Open standard SQL DB
	db, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		return nil, err
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.Postgres.MaxOpenConns)
	db.SetMaxIdleConns(cfg.Postgres.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.Postgres.ConnMaxLifetimeMinutes) * time.Minute)

	// For now we're using our custom wrapper instead of otelsql
	// In a future version, we could switch to using otelsql

	return db, nil
}

// NewOtelEntDriver creates a new Ent driver with OpenTelemetry instrumentation
func NewOtelEntDriver(driverName, dataSourceName string, cfg *config.Configuration, logger *logger.Logger) (dialect.Driver, error) {
	// Open database with OpenTelemetry instrumentation
	db, err := OpenOtelDB(driverName, dataSourceName, cfg, logger)
	if err != nil {
		return nil, err
	}

	// Create the SQL driver
	drv := entsql.OpenDB(dialect.Postgres, db)

	// Wrap with OpenTelemetry instrumentation if enabled
	if cfg.Telemetry.Enabled {
		return NewOtelDriver(drv, cfg, logger), nil
	}

	return drv, nil
}
