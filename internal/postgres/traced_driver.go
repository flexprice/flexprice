package postgres

import (
	"context"
	"database/sql"
	"strings"

	"entgo.io/ent/dialect"
	"github.com/flexprice/flexprice/internal/tracing"
)

// tracedDriver wraps an Ent dialect.Driver so every SQL statement — reads,
// writes, and statements inside transactions — emits a SpanKindClient span
// with the OTel db.system attribute (see tracing.StartDBSpan), which is what
// SigNoz's "Database Calls" tab keys on.
//
// Span creation is gated per call on otel.traces.storage_spans_enabled
// (env: FLEXPRICE_OTEL_TRACES_STORAGE_SPANS_ENABLED); at default (false) the
// wrapper is a pure passthrough.
type tracedDriver struct {
	dialect.Driver
	tracing  *tracing.Service
	endpoint string // "writer" or "reader"
}

func newTracedDriver(drv dialect.Driver, tracingSvc *tracing.Service, endpoint string) dialect.Driver {
	return &tracedDriver{Driver: drv, tracing: tracingSvc, endpoint: endpoint}
}

// Exec executes a query that does not return records (INSERT, UPDATE, DELETE).
func (d *tracedDriver) Exec(ctx context.Context, query string, args, v any) error {
	return d.traced(ctx, query, func(ctx context.Context) error {
		return d.Driver.Exec(ctx, query, args, v)
	})
}

// Query executes a query that returns rows (SELECT).
func (d *tracedDriver) Query(ctx context.Context, query string, args, v any) error {
	return d.traced(ctx, query, func(ctx context.Context) error {
		return d.Driver.Query(ctx, query, args, v)
	})
}

// Tx starts a transaction whose statements are also traced.
func (d *tracedDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	tx, err := d.Driver.Tx(ctx)
	if err != nil {
		return nil, err
	}
	return &tracedTx{Tx: tx, driver: d}, nil
}

// BeginTx satisfies the optional interface ent's client checks for when
// transaction options are supplied.
func (d *tracedDriver) BeginTx(ctx context.Context, opts *sql.TxOptions) (dialect.Tx, error) {
	drv, ok := d.Driver.(interface {
		BeginTx(context.Context, *sql.TxOptions) (dialect.Tx, error)
	})
	if !ok {
		return d.Tx(ctx)
	}
	tx, err := drv.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &tracedTx{Tx: tx, driver: d}, nil
}

func (d *tracedDriver) traced(ctx context.Context, query string, fn func(context.Context) error) error {
	if d.tracing == nil || !d.tracing.IsStorageSpansEnabled() {
		return fn(ctx)
	}
	span, spanCtx := d.tracing.StartDBSpan(ctx, spanNameForStatement(query), map[string]interface{}{
		"db.statement": truncateStatement(query),
		"db.endpoint":  d.endpoint,
	})
	if span == nil {
		return fn(ctx)
	}
	defer span.Finish()
	if err := fn(spanCtx); err != nil {
		span.SetStatusError(err)
		return err
	}
	span.SetStatusOK()
	return nil
}

// tracedTx traces statements executed inside a transaction.
type tracedTx struct {
	dialect.Tx
	driver *tracedDriver
}

func (t *tracedTx) Exec(ctx context.Context, query string, args, v any) error {
	return t.driver.traced(ctx, query, func(ctx context.Context) error {
		return t.Tx.Exec(ctx, query, args, v)
	})
}

func (t *tracedTx) Query(ctx context.Context, query string, args, v any) error {
	return t.driver.traced(ctx, query, func(ctx context.Context) error {
		return t.Tx.Query(ctx, query, args, v)
	})
}

// spanNameForStatement derives a low-cardinality span name from the SQL verb,
// e.g. "postgres.select". Full statements go in the db.statement attribute.
func spanNameForStatement(query string) string {
	verb, _, _ := strings.Cut(strings.TrimSpace(query), " ")
	verb = strings.ToLower(verb)
	switch verb {
	case "select", "insert", "update", "delete", "with":
		return "postgres." + verb
	case "":
		return "postgres.query"
	default:
		return "postgres.exec"
	}
}

// truncateStatement limits statement length to keep OTel span attributes small.
func truncateStatement(query string) string {
	const maxStatementLength = 1000
	if len(query) > maxStatementLength {
		return query[:maxStatementLength] + "..."
	}
	return query
}
