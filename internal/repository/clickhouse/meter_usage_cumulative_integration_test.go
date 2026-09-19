package clickhouse

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// newRealClickHouseTestStore builds a real *clickhouse.ClickHouseStore backed
// by an actual ClickHouse instance. Configuration comes from
// FLEXPRICE_TEST_CLICKHOUSE_* env vars (defaults match the docker-compose dev
// stack), mirroring internal/repository/ent/revenue_fact_test.go's harness.
//
// Skips (instead of failing) when no reachable ClickHouse is configured, so it
// does not break `make test` / CI runs without a live database.
func newRealClickHouseTestStore(t *testing.T) *clickhouse.ClickHouseStore {
	t.Helper()

	address := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_ADDRESS", "localhost:9000")
	username := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_USERNAME", "flexprice")
	password := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_PASSWORD", "flexprice123")
	database := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_DATABASE", "flexprice")

	cfg := &config.Configuration{
		ClickHouse: config.ClickHouseConfig{
			Address:        address,
			Username:       username,
			Password:       password,
			Database:       database,
			MaxMemoryUsage: 1,
		},
	}

	store, err := clickhouse.NewClickHouseStore(cfg, nil)
	if err != nil {
		t.Skipf("skipping: real ClickHouse test instance not reachable at %s (%v)", address, err)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := store.GetConn().Ping(ctx); err != nil {
		t.Skipf("skipping: real ClickHouse test instance not reachable at %s (%v)", address, err)
		return nil
	}

	t.Cleanup(func() {
		_ = store.Close()
	})

	return store
}

func chEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// TestGetCumulativeDailyUsage_Integration proves GetCumulativeDailyUsage runs
// against a real ClickHouse instance and returns a monotonically
// non-decreasing running total. Skips automatically when ClickHouse isn't
// reachable (see newRealClickHouseTestStore) — this test's job is to compile
// and exercise real wiring, not to assert on seeded data.
func TestGetCumulativeDailyUsage_Integration(t *testing.T) {
	store := newRealClickHouseTestStore(t)

	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	})
	require.NoError(t, err)

	repo := NewMeterUsageRepository(store, log)

	params := &events.CumulativeDailyUsageParams{
		TenantID:      "test_tenant_" + uuid.NewString(),
		EnvironmentID: "test_env_" + uuid.NewString(),
		MeterID:       "test_meter_" + uuid.NewString(),
		StartTime:     time.Now().UTC().AddDate(0, 0, -30),
		EndTime:       time.Now().UTC(),
		UseFinal:      true,
	}

	points, err := repo.GetCumulativeDailyUsage(context.Background(), params)
	require.NoError(t, err)

	var running float64
	for _, p := range points {
		v, _ := p.CumulativeQty.Float64()
		require.GreaterOrEqual(t, v, running-1e-9, "cumulative quantity must not decrease day over day")
		running = v
	}
}
