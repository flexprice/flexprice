package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	chgo "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func observedLogger() (*logger.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return logger.NewFromSugared(zap.New(core).Sugar()), logs
}

func warnTables(logs *observer.ObservedLogs) []string {
	var out []string
	for _, e := range logs.FilterLevelExact(zapcore.WarnLevel).All() {
		out = append(out, fmt.Sprint(e.ContextMap()["table"]))
	}
	return out
}

func fakeExists(present map[string]bool, errs map[string]error) TableExistsFunc {
	return func(_ context.Context, table string) (bool, error) {
		if err := errs[table]; err != nil {
			return false, err
		}
		return present[table], nil
	}
}

func TestInitRollupTables(t *testing.T) {
	both := map[string]bool{"h": true, "d": true}
	tests := []struct {
		name       string
		hourly     string
		daily      string
		present    map[string]bool
		errs       map[string]error
		wantHourly string
		wantDaily  string
		wantWarns  []string
	}{
		{"both present", "h", "d", both, nil, "h", "d", nil},
		{"hourly only configured", "h", "", both, nil, "h", "", nil},
		{"hourly missing disables both", "h", "d", map[string]bool{"d": true}, nil, "", "", []string{"h", "d"}},
		{"daily missing keeps hourly", "h", "d", map[string]bool{"h": true}, nil, "h", "", []string{"d"}},
		{"hourly check error disables both", "h", "d", both, map[string]error{"h": errors.New("down")}, "", "", []string{"h", "d"}},
		{"daily check error keeps hourly", "h", "d", both, map[string]error{"d": errors.New("down")}, "h", "", []string{"d"}},
		{"daily without hourly", "", "d", both, nil, "", "", []string{"d"}},
		{"nothing configured", "", "", both, nil, "", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, logs := observedLogger()
			h, d := InitRollupTables(context.Background(), fakeExists(tt.present, tt.errs), tt.hourly, tt.daily, log)
			assert.Equal(t, tt.wantHourly, h)
			assert.Equal(t, tt.wantDaily, d)
			assert.Equal(t, tt.wantWarns, warnTables(logs))
		})
	}
}

func TestInitRollupTablesTimeout(t *testing.T) {
	prev := rollupCheckTimeout
	rollupCheckTimeout = 10 * time.Millisecond
	defer func() { rollupCheckTimeout = prev }()

	blocking := func(ctx context.Context, _ string) (bool, error) {
		<-ctx.Done()
		return false, ctx.Err()
	}
	log, logs := observedLogger()
	h, d := InitRollupTables(context.Background(), blocking, "h", "d", log)
	assert.Empty(t, h)
	assert.Empty(t, d)
	assert.Equal(t, []string{"h", "d"}, warnTables(logs))
}

type stubRow struct {
	scan func(dest ...any) error
}

func (r stubRow) Err() error                { return nil }
func (r stubRow) Scan(dest ...any) error    { return r.scan(dest...) }
func (r stubRow) ScanStruct(dest any) error { return nil }

type stubRows struct {
	driver.Rows
	data [][]any
	pos  int
}

func (r *stubRows) Next() bool   { r.pos++; return r.pos <= len(r.data) }
func (r *stubRows) Close() error { return nil }
func (r *stubRows) Err() error   { return nil }
func (r *stubRows) Scan(dest ...any) error {
	for i, d := range dest {
		switch p := d.(type) {
		case *string:
			*p = r.data[r.pos-1][i].(string)
		case *decimal.Decimal:
			*p = r.data[r.pos-1][i].(decimal.Decimal)
		case *uint64:
			*p = r.data[r.pos-1][i].(uint64)
		}
	}
	return nil
}

type rollupStubConn struct {
	driver.Conn
	queries []string
	// errFor injects per-query errors.
	errFor func(query string) error
}

func (c *rollupStubConn) QueryRow(_ context.Context, query string, _ ...any) driver.Row {
	c.queries = append(c.queries, query)
	return stubRow{scan: func(dest ...any) error {
		if err := c.errFor(query); err != nil {
			return err
		}
		for _, d := range dest {
			switch p := d.(type) {
			case *decimal.Decimal:
				*p = decimal.NewFromInt(7)
			case *uint64:
				*p = 3
			case *uint8:
				*p = 1
			}
		}
		return nil
	}}
}

func (c *rollupStubConn) Query(_ context.Context, query string, _ ...any) (driver.Rows, error) {
	c.queries = append(c.queries, query)
	if err := c.errFor(query); err != nil {
		return nil, err
	}
	if strings.Contains(query, "SELECT DISTINCT meter_id") {
		return &stubRows{data: [][]any{{"m1"}, {"m2"}}}, nil
	}
	return &stubRows{data: [][]any{{"m1", decimal.NewFromInt(7), uint64(3)}}}, nil
}

func newStubRepo(conn *rollupStubConn, log *logger.Logger, tables rollupTables) *MeterUsageRepository {
	return &MeterUsageRepository{
		store:  clickhouse.NewClickHouseStoreFromConn(conn),
		logger: log,
		qb:     NewMeterUsageQueryBuilder(),
		rollup: tables,
	}
}

func unknownTable() error {
	return &chgo.Exception{Code: chCodeUnknownTable, Message: "Unknown table meter_usage_hourly"}
}

func TestNewTableExistsFunc(t *testing.T) {
	conn := &rollupStubConn{errFor: func(string) error { return nil }}
	exists := NewTableExistsFunc(clickhouse.NewClickHouseStoreFromConn(conn))
	ok, err := exists(context.Background(), "flexprice.meter_usage_hourly")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, []string{"EXISTS TABLE flexprice.meter_usage_hourly"}, conn.queries)

	_, err = exists(context.Background(), "bad; DROP TABLE x")
	require.Error(t, err)
	assert.Len(t, conn.queries, 1, "invalid names never reach the server")

	_, err = NewTableExistsFunc(nil)(context.Background(), "t")
	require.Error(t, err)

	failing := &rollupStubConn{errFor: func(string) error { return errors.New("down") }}
	ok, err = NewTableExistsFunc(clickhouse.NewClickHouseStoreFromConn(failing))(context.Background(), "t")
	require.Error(t, err)
	assert.False(t, ok)
}

func TestIsMissingRollupErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("boom"), false},
		{"unknown table", &chgo.Exception{Code: 60}, true},
		{"unknown identifier", &chgo.Exception{Code: 47}, true},
		{"other code", &chgo.Exception{Code: 159}, false},
		{"memory limit", &chgo.Exception{Code: 241}, false},
		{"wrapped by fmt", fmt.Errorf("x: %w", &chgo.Exception{Code: 60}), true},
		{"wrapped by ierr", ierr.WithError(&chgo.Exception{Code: 60}).Mark(ierr.ErrDatabase), true},
		{"ierr other code", ierr.WithError(&chgo.Exception{Code: 159}).Mark(ierr.ErrDatabase), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isMissingRollupErr(tt.err))
		})
	}
}

func scalarParams() *events.MeterUsageQueryParams {
	return rollupParams(types.AggregationSum, ts("2026-11-01T00:00:00Z"), ts("2026-11-02T00:00:00Z"))
}

func multiParams() *events.MeterUsageQueryParams {
	p := scalarParams()
	p.MeterID = ""
	p.MeterIDs = []string{"m1", "m2"}
	return p
}

type rollupCall struct {
	name string
	run  func(r *MeterUsageRepository) error
}

func rollupCalls() []rollupCall {
	return []rollupCall{
		{"GetUsage", func(r *MeterUsageRepository) error {
			res, err := r.GetUsage(context.Background(), scalarParams())
			if err == nil {
				if !res.TotalValue.Equal(decimal.NewFromInt(7)) || res.EventCount != 3 {
					return errors.New("unexpected result")
				}
			}
			return err
		}},
		{"GetUsageMultiMeter", func(r *MeterUsageRepository) error {
			res, err := r.GetUsageMultiMeter(context.Background(), multiParams())
			if err == nil && len(res) != 1 {
				return errors.New("unexpected result")
			}
			return err
		}},
		{"GetDistinctMeterIDs", func(r *MeterUsageRepository) error {
			ids, err := r.GetDistinctMeterIDs(context.Background(), multiParams())
			if err == nil && len(ids) != 2 {
				return errors.New("unexpected result")
			}
			return err
		}},
	}
}

func TestRollupCallSites(t *testing.T) {
	for _, c := range rollupCalls() {
		t.Run(c.name+"/uses rollup", func(t *testing.T) {
			conn := &rollupStubConn{errFor: func(string) error { return nil }}
			log, logs := observedLogger()
			require.NoError(t, c.run(newStubRepo(conn, log, rollupTables{hourly: "meter_usage_hourly"})))
			require.Len(t, conn.queries, 1)
			assert.Contains(t, conn.queries[0], "meter_usage_hourly")
			assert.Zero(t, logs.Len())
		})

		t.Run(c.name+"/rollup disabled", func(t *testing.T) {
			conn := &rollupStubConn{errFor: func(string) error { return nil }}
			log, _ := observedLogger()
			require.NoError(t, c.run(newStubRepo(conn, log, rollupTables{})))
			require.Len(t, conn.queries, 1)
			assert.NotContains(t, conn.queries[0], "meter_usage_hourly")
		})

		t.Run(c.name+"/falls back on unknown table and warns once", func(t *testing.T) {
			conn := &rollupStubConn{errFor: func(q string) error {
				if strings.Contains(q, "meter_usage_hourly") {
					return unknownTable()
				}
				return nil
			}}
			log, logs := observedLogger()
			repo := newStubRepo(conn, log, rollupTables{hourly: "meter_usage_hourly"})
			require.NoError(t, c.run(repo))
			require.NoError(t, c.run(repo))
			require.Len(t, conn.queries, 4, "rollup then raw, twice")
			assert.Contains(t, conn.queries[0], "meter_usage_hourly")
			assert.NotContains(t, conn.queries[1], "meter_usage_hourly")
			assert.Equal(t, []string{"meter_usage_hourly"}, warnTables(logs))
		})

		t.Run(c.name+"/falls back on unknown identifier", func(t *testing.T) {
			conn := &rollupStubConn{errFor: func(q string) error {
				if strings.Contains(q, "meter_usage_hourly") {
					return &chgo.Exception{Code: chCodeUnknownIdentifier, Message: "Unknown identifier qty_sum"}
				}
				return nil
			}}
			log, logs := observedLogger()
			require.NoError(t, c.run(newStubRepo(conn, log, rollupTables{hourly: "meter_usage_hourly"})))
			assert.Len(t, conn.queries, 2)
			assert.Len(t, warnTables(logs), 1)
		})

		t.Run(c.name+"/other errors are returned", func(t *testing.T) {
			conn := &rollupStubConn{errFor: func(string) error { return &chgo.Exception{Code: 241, Message: "memory limit"} }}
			log, logs := observedLogger()
			require.Error(t, c.run(newStubRepo(conn, log, rollupTables{hourly: "meter_usage_hourly"})))
			assert.Len(t, conn.queries, 1, "no raw retry")
			assert.Zero(t, logs.Len())
		})

		t.Run(c.name+"/raw failure after fallback is returned", func(t *testing.T) {
			conn := &rollupStubConn{errFor: func(q string) error {
				if strings.Contains(q, "meter_usage_hourly") {
					return unknownTable()
				}
				return errors.New("raw down")
			}}
			log, _ := observedLogger()
			require.Error(t, c.run(newStubRepo(conn, log, rollupTables{hourly: "meter_usage_hourly"})))
			assert.Len(t, conn.queries, 2)
		})
	}
}

func TestRollupFallbackNamesTierFromError(t *testing.T) {
	conn := &rollupStubConn{errFor: func(string) error { return nil }}
	log, logs := observedLogger()
	repo := newStubRepo(conn, log, rollupTables{hourly: "meter_usage_hourly", daily: "meter_usage_daily"})

	err := &chgo.Exception{Code: chCodeUnknownTable, Message: "Unknown table meter_usage_daily"}
	require.True(t, repo.rollupFallback(context.Background(), err))
	assert.Equal(t, []string{"meter_usage_daily"}, warnTables(logs))

	err = &chgo.Exception{Code: chCodeUnknownTable, Message: "Unknown table"}
	require.True(t, repo.rollupFallback(context.Background(), err))
	assert.ElementsMatch(t, []string{"meter_usage_daily", "meter_usage_hourly"}, warnTables(logs))

	assert.False(t, repo.rollupFallback(context.Background(), errors.New("x")))
}
