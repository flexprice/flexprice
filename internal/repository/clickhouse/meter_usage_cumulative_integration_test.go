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

// TestGetDailyUsageByMeter_Integration proves GetDailyUsageByMeter runs against
// a real ClickHouse instance and keys its per-day rows by meter. Skips
// automatically when ClickHouse isn't reachable (see
// newRealClickHouseTestStore) — this test's job is to compile and exercise real
// wiring, not to assert on seeded data.
func TestGetDailyUsageByMeter_Integration(t *testing.T) {
	store := newRealClickHouseTestStore(t)

	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	})
	require.NoError(t, err)

	repo := NewMeterUsageRepository(store, log)

	meterA := "test_meter_" + uuid.NewString()
	meterB := "test_meter_" + uuid.NewString()
	params := &events.DailyUsageParams{
		TenantID:      "test_tenant_" + uuid.NewString(),
		EnvironmentID: "test_env_" + uuid.NewString(),
		MeterIDs:      []string{meterA, meterB},
		StartTime:     time.Now().UTC().AddDate(0, 0, -30),
		EndTime:       time.Now().UTC(),
		UseFinal:      true,
	}

	byMeter, err := repo.GetDailyUsageByMeter(context.Background(), params)
	require.NoError(t, err)

	// Whatever comes back must be attributed to a meter we asked for, and the
	// days within a meter must be ordered — the caller accumulates in order.
	for meterID, points := range byMeter {
		require.Contains(t, params.MeterIDs, meterID, "a meter we did not ask for came back")
		for i := 1; i < len(points); i++ {
			require.False(t, points[i].Day.Before(points[i-1].Day), "days must be ordered within a meter")
		}
	}

	// An empty meter list must not issue a query at all.
	empty, err := repo.GetDailyUsageByMeter(context.Background(), &events.DailyUsageParams{
		TenantID: params.TenantID, EnvironmentID: params.EnvironmentID,
	})
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestGetUsageMultiMeterBillingGroups_Integration(t *testing.T) {
	store := newRealClickHouseTestStore(t)
	repo := &MeterUsageRepository{
		store: store,
		qb:    NewMeterUsageQueryBuilder(),
	}
	params := &events.MeterUsageQueryParams{
		TenantID:            "batch_tenant",
		EnvironmentID:       "batch_env",
		ExternalCustomerIDs: []string{"batch_customer"},
		MeterIDs:            []string{"meter_a", "meter_b"},
		AggregationType:     types.AggregationMax,
		WindowSize:          types.WindowSizeDay,
		GroupBy:             []string{"properties.instance"},
		StartTime:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:             time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
	}
	// A CTE fixture exercises the real query and scanner without writing shared tables.
	fixture := `WITH meter_usage AS (
		SELECT row.1 AS meter_id, row.2 AS id, toJSONString(map('instance', row.3)) AS properties, toDecimal128(row.4, 8) AS qty_total,
			toDateTime('2026-01-01 12:00:00', 'UTC') AS timestamp,
			'batch_tenant' AS tenant_id, row.5 AS environment_id, row.6 AS external_customer_id
		FROM (SELECT arrayJoin([
			('meter_a', 'a1', 'x', 60, 'batch_env', 'batch_customer'),
			('meter_a', 'a2', 'x', 120, 'batch_env', 'batch_customer'),
			('meter_a', 'a3', 'y', 180, 'batch_env', 'batch_customer'),
			('meter_b', 'b1', 'x', 70, 'batch_env', 'batch_customer'),
			('meter_a', 'other_env', 'x', 9999, 'other_env', 'batch_customer'),
			('meter_a', 'other_customer', 'x', 9999, 'batch_env', 'other_customer')
		]) AS row)
	) `
	query := fixture + BuildMultiMeterQuery("MAX(qty_total)", "COUNT(DISTINCT id)", params, repo.qb)
	_, args := repo.qb.BuildWhereClause(params)
	results, err := repo.executeMultiMeterWindowedQuery(context.Background(), query, args, params)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, result := range results {
		switch result.MeterID {
		case "meter_a":
			require.Equal(t, "300", result.TotalValue.String())
			require.Len(t, result.Points, 2)
			require.Equal(t, "120", result.Points[0].Value.String())
			require.Equal(t, "180", result.Points[1].Value.String())
		case "meter_b":
			require.Equal(t, "70", result.TotalValue.String())
			require.Len(t, result.Points, 1)
		default:
			t.Fatalf("unexpected meter %s", result.MeterID)
		}
	}
}
