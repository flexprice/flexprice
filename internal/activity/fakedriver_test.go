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

type call struct {
	Query string
	Args  []any
}

// fakeDriver records statements and answers Query with canned rows.
type fakeDriver struct {
	mu      sync.Mutex
	execs   []call
	queries []call
	columns []string
	rows    []map[string]any
}

func newFakeDriver() *fakeDriver { return &fakeDriver{} }

func (f *fakeDriver) Exec(_ context.Context, query string, args, v any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = append(f.execs, call{query, toArgs(args)})
	if res, ok := v.(*entsql.Result); ok {
		*res = fakeResult(len(f.rows))
	}
	return nil
}

func (f *fakeDriver) Query(_ context.Context, query string, args, v any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, call{query, toArgs(args)})
	rows, ok := v.(*entsql.Rows)
	if !ok {
		return fmt.Errorf("fakeDriver: unexpected %T", v)
	}
	rows.ColumnScanner = &fakeRows{cols: f.columns, data: f.rows, pos: -1}
	return nil
}

func (f *fakeDriver) Tx(context.Context) (dialect.Tx, error) { return fakeTx{f}, nil }
func (f *fakeDriver) Close() error                           { return nil }
func (f *fakeDriver) Dialect() string                        { return dialect.Postgres }

type fakeTx struct{ *fakeDriver }

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

func toArgs(a any) []any {
	if s, ok := a.([]any); ok {
		return s
	}
	return nil
}

type fakeResult int64

func (fakeResult) LastInsertId() (int64, error)   { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return int64(r), nil }

type fakeRows struct {
	cols []string
	data []map[string]any
	pos  int
}

func (r *fakeRows) Close() error                               { return nil }
func (r *fakeRows) ColumnTypes() ([]*stdsql.ColumnType, error) { return nil, nil }
func (r *fakeRows) Columns() ([]string, error)                 { return r.cols, nil }
func (r *fakeRows) Err() error                                 { return nil }
func (r *fakeRows) NextResultSet() bool                        { return false }
func (r *fakeRows) Next() bool                                 { r.pos++; return r.pos < len(r.data) }

func (r *fakeRows) Scan(dest ...any) error {
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
			return fmt.Errorf("fakeRows: unsupported dest %T for %s", dest[i], col)
		}
	}
	return nil
}
