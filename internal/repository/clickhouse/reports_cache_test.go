package clickhouse

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportsCache(t *testing.T) {
	ctx := context.Background()
	cfg := config.ReportsConfig{QueryCacheTTLSeconds: 60}
	q := &reportQuery{SQL: "SELECT 1", Params: map[string]string{"tenant_id": "t1", "environment_id": "e1"}}
	want := [][]string{{"USD", "10"}}

	counting := func(calls *int, rows [][]string, err error) func() ([][]string, error) {
		return func() ([][]string, error) {
			*calls++
			return rows, err
		}
	}

	t.Run("no cache queries every time", func(t *testing.T) {
		r := &ReportsRepository{cfg: cfg}
		calls := 0
		for range 2 {
			got, err := r.cached(ctx, q, counting(&calls, want, nil))
			require.NoError(t, err)
			assert.Equal(t, want, got)
		}
		assert.Equal(t, 2, calls)
	})

	t.Run("zero ttl skips redis", func(t *testing.T) {
		r := &ReportsRepository{cache: testutil.NewInMemoryRedis()}
		calls := 0
		for range 2 {
			_, err := r.cached(ctx, q, counting(&calls, want, nil))
			require.NoError(t, err)
		}
		assert.Equal(t, 2, calls)
	})

	t.Run("second call hits cache", func(t *testing.T) {
		r := &ReportsRepository{cfg: cfg, cache: testutil.NewInMemoryRedis()}
		calls := 0
		for range 2 {
			got, err := r.cached(ctx, q, counting(&calls, want, nil))
			require.NoError(t, err)
			assert.Equal(t, want, got)
		}
		assert.Equal(t, 1, calls)
	})

	t.Run("errors are not cached", func(t *testing.T) {
		r := &ReportsRepository{cfg: cfg, cache: testutil.NewInMemoryRedis()}
		calls := 0
		_, err := r.cached(ctx, q, counting(&calls, nil, errors.New("boom")))
		require.Error(t, err)
		got, err := r.cached(ctx, q, counting(&calls, want, nil))
		require.NoError(t, err)
		assert.Equal(t, want, got)
		assert.Equal(t, 2, calls)
	})

	t.Run("other tenant misses", func(t *testing.T) {
		r := &ReportsRepository{cfg: cfg, cache: testutil.NewInMemoryRedis()}
		other := &reportQuery{SQL: q.SQL, Params: map[string]string{"tenant_id": "t2", "environment_id": "e1"}}
		calls := 0
		_, err := r.cached(ctx, q, counting(&calls, want, nil))
		require.NoError(t, err)
		got, err := r.cached(ctx, other, counting(&calls, [][]string{{"EUR", "5"}}, nil))
		require.NoError(t, err)
		assert.Equal(t, [][]string{{"EUR", "5"}}, got)
		assert.Equal(t, 2, calls)
		assert.NotEqual(t, reportCacheKey(ctx, q), reportCacheKey(ctx, other))
	})
}

func TestReportCacheKey_NoDelimiterCollision(t *testing.T) {
	ctx := context.Background()
	a := &reportQuery{SQL: "q", Params: map[string]string{"f0": "a\x00f1\x00b"}}
	b := &reportQuery{SQL: "q", Params: map[string]string{"f0": "a", "f1": "b"}}
	assert.NotEqual(t, reportCacheKey(ctx, a), reportCacheKey(ctx, b))
}

func TestReportsRepository_RelativeRangesShareKey(t *testing.T) {
	ctx := context.Background()
	r := &ReportsRepository{cfg: config.ReportsConfig{Database: "db", QueryCacheTTLSeconds: 60}, cache: testutil.NewInMemoryRedis()}
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	key := func(now time.Time) string {
		q, err := r.buildReport(&analytics.ResolvedView{
			Family: types.FamilyBilling, Shape: types.ShapeScalar, Metrics: []types.Metric{types.MetricInvoiceTotal},
			Time: analytics.TimeSpec{From: now.AddDate(0, 0, -7), To: now},
		}, "t1", "e1")
		require.NoError(t, err)
		return reportCacheKey(ctx, q)
	}
	assert.Equal(t, key(base.Add(123456*time.Microsecond)), key(base.Add(41*time.Second)))
	assert.NotEqual(t, key(base), key(base.Add(time.Minute)))
}

func TestReportsRepository_NoCacheKeepsExactWindow(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 45, 0, time.UTC)
	for name, r := range map[string]*ReportsRepository{
		"no redis": {cfg: config.ReportsConfig{Database: "db", QueryCacheTTLSeconds: 60}},
		"zero ttl": {cfg: config.ReportsConfig{Database: "db"}, cache: testutil.NewInMemoryRedis()},
	} {
		assert.Equal(t, now, r.stableTime(now), name)
	}
}

func TestReportsRepository_BuildReportNilView(t *testing.T) {
	r := &ReportsRepository{cfg: config.ReportsConfig{Database: "db"}}
	_, err := r.buildReport(nil, "t1", "e1")
	require.Error(t, err)
}
