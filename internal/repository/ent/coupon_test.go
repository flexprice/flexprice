package ent

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	domainCoupon "github.com/flexprice/flexprice/internal/domain/coupon"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	_ "github.com/lib/pq"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// noopRedisCache is a minimal no-op cache.RedisCache implementation used only
// to satisfy NewCouponRepository's constructor signature in this test file.
// Cannot use testutil.NewInMemoryRedis() here: internal/testutil imports
// internal/repository/ent (via webhook/publisher), so importing testutil from
// a _test.go file in this package would create an import cycle.
type noopRedisCache struct{}

func (noopRedisCache) IsEnabled() bool                                      { return false }
func (noopRedisCache) IsRedisCache() bool                                   { return true }
func (noopRedisCache) Get(_ context.Context, _ string) (interface{}, bool)  { return nil, false }
func (noopRedisCache) Set(_ context.Context, _ string, _ interface{}, _ time.Duration) {
}
func (noopRedisCache) Delete(_ context.Context, _ string)         {}
func (noopRedisCache) DeleteByPrefix(_ context.Context, _ string) {}
func (noopRedisCache) Flush(_ context.Context)                    {}
func (noopRedisCache) ForceCacheGet(_ context.Context, _ string) (interface{}, bool) {
	return nil, false
}
func (noopRedisCache) ForceCacheSet(_ context.Context, _ string, _ interface{}, _ time.Duration) {
}
func (noopRedisCache) ForceCacheDelete(_ context.Context, _ string) {}
func (noopRedisCache) ForceCacheGetWithTTL(_ context.Context, _ string) (interface{}, time.Duration, bool) {
	return nil, 0, false
}

var _ cache.RedisCache = noopRedisCache{}

// newRealPostgresTestClient builds a real postgres.IClient backed by an actual
// Postgres instance (as opposed to testutil.MockPostgresClient, which never
// hits a real database and therefore cannot exercise a WHERE-clause-level
// atomic guard or real concurrent transaction/connection behavior).
//
// Configuration comes from FLEXPRICE_TEST_POSTGRES_* env vars, defaulting to
// localhost:55432 (an ad-hoc local Postgres instance with Ent-generated
// schema migrated, used to develop/verify this fix — the flexprice
// docker-compose "postgres" service normally binds host port 5432, which was
// occupied by an unrelated project's container in this environment).
//
// This repo has no pre-existing real-DB test harness under
// internal/repository/ent/ (no *_test.go files existed there before this
// change) — this is the harness this task introduces. The test skips
// (instead of failing) when no reachable Postgres is configured, so it does
// not break `make test` / CI runs without a live database.
func newRealPostgresTestClient(t *testing.T) postgres.IClient {
	t.Helper()

	host := envOrDefault("FLEXPRICE_TEST_POSTGRES_HOST", "localhost")
	port := envOrDefault("FLEXPRICE_TEST_POSTGRES_PORT", "55432")
	user := envOrDefault("FLEXPRICE_TEST_POSTGRES_USER", "flexprice")
	password := envOrDefault("FLEXPRICE_TEST_POSTGRES_PASSWORD", "flexprice123")
	dbname := envOrDefault("FLEXPRICE_TEST_POSTGRES_DBNAME", "flexprice")
	sslmode := envOrDefault("FLEXPRICE_TEST_POSTGRES_SSLMODE", "disable")

	dsn := "host=" + host + " port=" + port + " user=" + user +
		" password=" + password + " dbname=" + dbname + " sslmode=" + sslmode

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("skipping: real Postgres test DB not reachable at %s:%s (%v)", host, port, err)
		return nil
	}
	if err := db.Ping(); err != nil {
		t.Skipf("skipping: real Postgres test DB not reachable at %s:%s (%v)", host, port, err)
		return nil
	}

	drv := entsql.OpenDB(dialect.Postgres, db)
	client := ent.NewClient(ent.Driver(drv))

	t.Cleanup(func() {
		_ = client.Close()
	})

	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	})
	require.NoError(t, err)

	return postgres.NewClient(&postgres.EntClients{
		Writer:    client,
		Reader:    client,
		HasReader: false,
	}, log, nil)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newTestCouponRepository(t *testing.T) domainCoupon.Repository {
	t.Helper()
	client := newRealPostgresTestClient(t)
	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	})
	require.NoError(t, err)
	return NewCouponRepository(client, log, noopRedisCache{})
}

func testCouponContext() context.Context {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, types.DefaultUserID)
	return ctx
}

func newTestCoupon(name string, maxRedemptions *int) *domainCoupon.Coupon {
	ctx := testCouponContext()
	pct := decimal.NewFromInt(10)
	c := &domainCoupon.Coupon{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
		Name:           name,
		Type:           types.CouponTypePercentage,
		Cadence:        types.CouponCadenceOnce,
		PercentageOff:  &pct,
		MaxRedemptions: maxRedemptions,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	c.Status = types.StatusPublished
	return c
}

func TestIncrementRedemptions_RespectsMaxRedemptions(t *testing.T) {
	repo := newTestCouponRepository(t)
	ctx := testCouponContext()

	maxRedemptions := 1
	c := newTestCoupon("race-test", &maxRedemptions)
	require.NoError(t, repo.Create(ctx, c))

	// First increment should succeed (0 < 1)
	err1 := repo.IncrementRedemptions(ctx, c.ID)
	require.NoError(t, err1)

	// Second increment should fail — already at max
	err2 := repo.IncrementRedemptions(ctx, c.ID)
	require.Error(t, err2)
	require.True(t, ierr.IsValidation(err2), "expected a validation-class error when limit reached, got: %v", err2)

	got, err := repo.Get(ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, 1, got.TotalRedemptions, "total_redemptions must not exceed max_redemptions")
}

func TestIncrementRedemptions_ConcurrentRequestsRespectLimit(t *testing.T) {
	repo := newTestCouponRepository(t)
	ctx := testCouponContext()

	maxRedemptions := 1
	c := newTestCoupon("concurrent-race-test", &maxRedemptions)
	require.NoError(t, repo.Create(ctx, c))

	const n = 5
	var wg sync.WaitGroup
	results := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = repo.IncrementRedemptions(ctx, c.ID)
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, err := range results {
		if err == nil {
			successCount++
		}
	}
	require.Equal(t, 1, successCount, "exactly one concurrent increment should succeed under max_redemptions=1")

	got, err := repo.Get(ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, 1, got.TotalRedemptions)
}

func TestIncrementRedemptions_UnlimitedCouponAlwaysSucceeds(t *testing.T) {
	repo := newTestCouponRepository(t)
	ctx := testCouponContext()

	c := newTestCoupon("unlimited-test", nil)
	require.NoError(t, repo.Create(ctx, c))

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.IncrementRedemptions(ctx, c.ID))
	}

	got, err := repo.Get(ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, 3, got.TotalRedemptions)
}
