package clickhouse

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"maps"
	"slices"
	"time"

	clickhouse_go "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

const (
	defaultReportsMaxExecutionSeconds = 10
	defaultReportsMaxResultRows       = 10000
	maxCachedReportBytes              = 1 << 20
)

var _ analytics.ReportsRepository = (*ReportsRepository)(nil)

type ReportsRepository struct {
	conn   driver.Conn
	cfg    config.ReportsConfig
	logger *logger.Logger
	cache  cache.RedisCache
}

func NewReportsRepository(cfg *config.Configuration, log *logger.Logger, rc cache.RedisCache) (*ReportsRepository, error) {
	r := &ReportsRepository{cfg: cfg.Reports, logger: log, cache: rc}
	if r.cfg.MaxExecutionSeconds <= 0 {
		r.cfg.MaxExecutionSeconds = defaultReportsMaxExecutionSeconds
	}
	if r.cfg.MaxResultRows <= 0 {
		r.cfg.MaxResultRows = defaultReportsMaxResultRows
	}
	if !r.cfg.Enabled() {
		return r, nil
	}
	opts := cfg.ClickHouse.GetClientOptions()
	opts.Auth = clickhouse_go.Auth{
		Database: r.cfg.Database,
		Username: r.cfg.Username,
		Password: r.cfg.Password,
	}
	conn, err := clickhouse_go.Open(opts)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("open reports clickhouse connection").Mark(ierr.ErrSystem)
	}
	r.conn = conn
	return r, nil
}

func (r *ReportsRepository) Enabled() bool { return r.conn != nil }

func (r *ReportsRepository) MaxRows() int { return r.cfg.MaxResultRows }

const (
	chCodeTooManyRows         = 158
	chCodeTimeoutExceeded     = 159
	chCodeMemoryLimitExceeded = 241
)

func reportError(err error, hint string) error {
	var ex *proto.Exception
	if errors.As(err, &ex) && (ex.Code == chCodeTooManyRows || ex.Code == chCodeTimeoutExceeded || ex.Code == chCodeMemoryLimitExceeded) {
		return ierr.WithError(err).
			WithHint("Query too large; narrow the date range").
			Mark(ierr.ErrValidation)
	}
	return ierr.WithError(err).WithHint(hint).Mark(ierr.ErrDatabase)
}

func (r *ReportsRepository) RunReport(ctx context.Context, rv *analytics.ResolvedView, tenantID, environmentID string) (*analytics.ReportResult, error) {
	if r.conn == nil {
		return nil, ierr.NewError("financial reports are not enabled").Mark(ierr.ErrPermissionDenied)
	}
	q, err := r.buildReport(rv, tenantID, environmentID)
	if err != nil {
		return nil, err
	}
	rows, err := r.run(ctx, q)
	if err != nil {
		return nil, err
	}
	return reportResult(q, rows, r.cfg.MaxResultRows)
}

func (r *ReportsRepository) buildReport(rv *analytics.ResolvedView, tenantID, environmentID string) (*reportQuery, error) {
	if rv == nil {
		return buildBillingQueryWithUsage(rv, r.cfg.Database, r.cfg.UsageDatabase, tenantID, environmentID, r.cfg.MaxResultRows)
	}
	w := *rv
	w.Time.From = r.stableTime(rv.Time.From)
	w.Time.To = r.stableTime(rv.Time.To)
	return buildBillingQueryWithUsage(&w, r.cfg.Database, r.cfg.UsageDatabase, tenantID, environmentID, r.cfg.MaxResultRows)
}

// stableTime lets relative ranges share cache keys.
func (r *ReportsRepository) stableTime(t time.Time) time.Time {
	if !r.resultCacheOn() {
		return t
	}
	return t.Truncate(time.Minute)
}

func (r *ReportsRepository) resultCacheOn() bool {
	return r.cache != nil && r.cache.IsEnabled() && r.cfg.QueryCacheTTLSeconds > 0
}

func reportResult(q *reportQuery, rows [][]string, maxRows int) (*analytics.ReportResult, error) {
	if !q.TopN && maxRows > 0 && len(rows) > maxRows {
		return nil, ierr.NewErrorf("result exceeds %d rows", maxRows).
			WithHint("Narrow the time range or remove dimensions").
			Mark(ierr.ErrValidation)
	}
	return &analytics.ReportResult{Columns: q.Columns, Rows: rows}, nil
}

func (r *ReportsRepository) run(ctx context.Context, q *reportQuery) ([][]string, error) {
	return r.cached(ctx, q, func() ([][]string, error) { return r.query(ctx, q) })
}

func (r *ReportsRepository) cached(ctx context.Context, q *reportQuery, fetch func() ([][]string, error)) ([][]string, error) {
	if !r.resultCacheOn() {
		return fetch()
	}
	key := reportCacheKey(ctx, q)
	if v, ok := r.cache.Get(ctx, key); ok {
		if rows, ok := cache.UnmarshalCacheValue[[][]string](v); ok {
			return *rows, nil
		}
	}
	rows, err := fetch()
	if err != nil {
		return nil, err
	}
	if b, err := json.Marshal(rows); err == nil && len(b) <= maxCachedReportBytes {
		r.cache.Set(ctx, key, string(b), time.Duration(r.cfg.QueryCacheTTLSeconds)*time.Second)
	}
	return rows, nil
}

// Params carry tenant and environment.
func reportCacheKey(ctx context.Context, q *reportQuery) string {
	h := sha256.New()
	writeLengthPrefixed(h, q.SQL)
	for _, k := range slices.Sorted(maps.Keys(q.Params)) {
		writeLengthPrefixed(h, k)
		writeLengthPrefixed(h, q.Params[k])
	}
	return cache.GenerateKey(ctx, cache.PrefixReportResult, hex.EncodeToString(h.Sum(nil)))
}

func writeLengthPrefixed(h hash.Hash, s string) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(s)))
	h.Write(n[:])
	h.Write([]byte(s))
}

func (r *ReportsRepository) query(ctx context.Context, q *reportQuery) ([][]string, error) {
	settings := clickhouse_go.Settings{
		"max_execution_time":   r.cfg.MaxExecutionSeconds,
		"max_result_rows":      r.cfg.MaxResultRows + 1,
		"result_overflow_mode": "throw",
		// Blocks writes; still allows settings.
		"readonly": 2,
	}
	if r.cfg.MaxRowsToRead > 0 {
		settings["max_rows_to_read"] = r.cfg.MaxRowsToRead
	}
	if r.cfg.QueryCacheTTLSeconds > 0 {
		settings["use_query_cache"] = 1
		settings["query_cache_ttl"] = r.cfg.QueryCacheTTLSeconds
		// Cache, not throw, on now().
		settings["query_cache_nondeterministic_function_handling"] = "save"
	} else {
		settings["use_query_cache"] = 0
	}
	ctx = clickhouse_go.Context(ctx, clickhouse_go.WithParameters(q.Params), clickhouse_go.WithSettings(settings))

	rows, err := r.conn.Query(ctx, q.SQL)
	if err != nil {
		return nil, reportError(err, "run report query")
	}
	defer rows.Close()

	out := [][]string{}
	for rows.Next() {
		vals := make([]string, len(q.Columns))
		ptrs := make([]any, len(vals))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, ierr.WithError(err).WithHint("scan report row").Mark(ierr.ErrDatabase)
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, reportError(err, "read report rows")
	}
	return out, nil
}
