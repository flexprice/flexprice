package activity

import (
	"context"
	stdsql "database/sql"
	"database/sql/driver"
	"fmt"
	"sync"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

type statement struct {
	Query string
	Args  []any
}

// recordingDriver records the SQL ent sends and answers queries with canned rows.
type recordingDriver struct {
	mu      sync.Mutex
	execs   []statement
	queries []statement
	columns []string
	rows    []map[string]any
}

func newRecordingDriver() *recordingDriver { return &recordingDriver{} }

func (d *recordingDriver) Exec(_ context.Context, query string, args, v any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.execs = append(d.execs, statement{query, toArgs(args)})
	if res, ok := v.(*entsql.Result); ok {
		*res = rowsAffected(len(d.rows))
	}
	return nil
}

func (d *recordingDriver) Query(_ context.Context, query string, args, v any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.queries = append(d.queries, statement{query, toArgs(args)})
	rows, ok := v.(*entsql.Rows)
	if !ok {
		return fmt.Errorf("recordingDriver: unexpected %T", v)
	}
	rows.ColumnScanner = &cannedRows{cols: d.columns, data: d.rows, pos: -1}
	return nil
}

func (d *recordingDriver) Tx(context.Context) (dialect.Tx, error) { return recordingTx{d}, nil }
func (d *recordingDriver) Close() error                           { return nil }
func (d *recordingDriver) Dialect() string                        { return dialect.Postgres }

type recordingTx struct{ *recordingDriver }

func (recordingTx) Commit() error   { return nil }
func (recordingTx) Rollback() error { return nil }

func toArgs(a any) []any {
	if s, ok := a.([]any); ok {
		return s
	}
	return nil
}

type rowsAffected int64

func (rowsAffected) LastInsertId() (int64, error)   { return 0, nil }
func (r rowsAffected) RowsAffected() (int64, error) { return int64(r), nil }

type cannedRows struct {
	cols []string
	data []map[string]any
	pos  int
}

func (r *cannedRows) Close() error                               { return nil }
func (r *cannedRows) ColumnTypes() ([]*stdsql.ColumnType, error) { return nil, nil }
func (r *cannedRows) Columns() ([]string, error)                 { return r.cols, nil }
func (r *cannedRows) Err() error                                 { return nil }
func (r *cannedRows) NextResultSet() bool                        { return false }
func (r *cannedRows) Next() bool                                 { r.pos++; return r.pos < len(r.data) }

func (r *cannedRows) Scan(dest ...any) error {
	for i, col := range r.cols {
		val := r.data[r.pos][col]
		switch d := dest[i].(type) {
		case *[]byte:
			if b, ok := val.([]byte); ok {
				*d = b
			}
		case stdsql.Scanner:
			if err := d.Scan(driver.Value(val)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("cannedRows: unsupported dest %T for %s", dest[i], col)
		}
	}
	return nil
}
