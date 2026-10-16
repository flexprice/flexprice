package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportsRepository_RunsBillingQuery(t *testing.T) {
	address := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_ADDRESS", "localhost:9000")
	username := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_USERNAME", "flexprice")
	password := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_PASSWORD", "flexprice123")
	db := fmt.Sprintf("reports_it_%d", time.Now().UnixNano())

	admin := newRealClickHouseTestStore(t)
	ctx := context.Background()
	require.NoError(t, admin.GetConn().Exec(ctx, "CREATE DATABASE "+db))
	t.Cleanup(func() { _ = admin.GetConn().Exec(context.Background(), "DROP DATABASE IF EXISTS "+db) })
	require.NoError(t, admin.GetConn().Exec(ctx, `CREATE TABLE `+db+`.invoices (
		id String, tenant_id String, environment_id String, status String,
		invoice_status String, invoice_type String, customer_id String, currency String,
		total Decimal(20, 9), finalized_at DateTime64(6),
		_peerdb_is_deleted UInt8, _peerdb_version Int64
	) ENGINE = ReplacingMergeTree(_peerdb_version) ORDER BY id`))
	ins := `INSERT INTO ` + db + `.invoices VALUES `
	require.NoError(t, admin.GetConn().Exec(ctx, ins+
		`('i1','t1','e1','published','FINALIZED','SUBSCRIPTION','c1','usd',10,'2026-09-05 00:00:00',0,1),`+
		`('i1','t1','e1','published','FINALIZED','SUBSCRIPTION','c1','usd',10,'2026-09-05 00:00:00',0,2),`+
		`('i2','t1','e1','published','FINALIZED','SUBSCRIPTION','c2','inr',5,'2026-09-06 00:00:00',0,1),`+
		`('i3','t1','e1','published','DRAFT','SUBSCRIPTION','c1','usd',99,'2026-09-06 00:00:00',0,1),`+
		`('i4','t2','e1','published','FINALIZED','SUBSCRIPTION','c9','usd',77,'2026-09-06 00:00:00',0,1),`+
		`('i5','t1','e1','published','FINALIZED','SUBSCRIPTION','c1','usd',33,'1970-01-01 00:00:00',0,1),`+
		`('i6','t1','e1','published','FINALIZED','SUBSCRIPTION','c1','usd',44,'2026-09-07 00:00:00',1,1),`+
		`('i7','t1','e1','published','FINALIZED','SUBSCRIPTION','c3','usd',70,'2026-08-10 00:00:00',0,1),`+
		`('i7','t1','e1','published','VOIDED','SUBSCRIPTION','c3','usd',70,'2026-08-10 00:00:00',0,2),`+
		`('i8','t1','e1','published','FINALIZED','SUBSCRIPTION','c1','usd',2,'2026-08-20 00:00:00',0,1),`+
		`('i9','t1','e1','published','FINALIZED','SUBSCRIPTION','c4','usd',4,'2026-08-15 00:00:00',0,1)`))

	cfg := &config.Configuration{
		ClickHouse: config.ClickHouseConfig{Address: address, Username: username, Password: password, Database: "default", MaxMemoryUsage: 1},
		Reports:    config.ReportsConfig{Database: db, Username: username, Password: password, MaxResultRows: 100},
	}
	repo, err := NewReportsRepository(cfg, nil)
	require.NoError(t, err)
	require.True(t, repo.Enabled())

	var filters []*analytics.Filter
	run := func(shape types.Shape, grain types.Grain, dims ...string) [][]string {
		rv := &analytics.ResolvedView{
			Family:     types.FamilyBilling,
			Shape:      shape,
			Metrics:    []types.Metric{types.MetricInvoiceTotal},
			Dimensions: dims,
			Filters:    filters,
			Time: analytics.TimeSpec{
				From:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
				To:    time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
				Grain: grain,
			},
		}
		res, err := repo.RunReport(ctx, rv, "t1", "e1")
		require.NoError(t, err)
		return res.Rows
	}

	// Dedup, status, tenant, epoch filters.
	assert.Equal(t, [][]string{{"inr", "5"}, {"usd", "16"}}, run(types.ShapeScalar, ""))

	assert.Equal(t, [][]string{
		{"2026-08-01T00:00:00Z", "usd", "6"},
		{"2026-09-01T00:00:00Z", "inr", "5"},
		{"2026-09-01T00:00:00Z", "usd", "10"},
	}, run(types.ShapeTimeseries, types.GrainMonth))

	assert.Equal(t, [][]string{
		{"c1", "usd", "12"},
		{"c2", "inr", "5"},
		{"c4", "usd", "4"},
	}, run(types.ShapeBreakdown, "", "customer_id"))

	filters = []*analytics.Filter{{Field: "customer_id", Op: types.IN, Value: []string{"c2"}}}
	assert.Equal(t, [][]string{{"inr", "5"}}, run(types.ShapeScalar, ""))
	filters = nil

	weekly := run(types.ShapeTimeseries, types.GrainWeek)
	require.NotEmpty(t, weekly)
	for _, r := range weekly {
		assert.True(t, strings.HasSuffix(r[0], "T00:00:00Z"), r[0])
		bucket, err := time.Parse(time.RFC3339, r[0])
		require.NoError(t, err)
		assert.Equal(t, time.Monday, bucket.Weekday())
	}

	runCmp := func(shape types.Shape, grain types.Grain, compare string, dims ...string) [][]string {
		rv := &analytics.ResolvedView{
			Family: types.FamilyBilling, Shape: shape, Metrics: []types.Metric{types.MetricInvoiceTotal}, Dimensions: dims,
			Time:    analytics.TimeSpec{From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Grain: grain},
			Compare: compare,
		}
		res, err := repo.RunReport(ctx, rv, "t1", "e1")
		require.NoError(t, err)
		return res.Rows
	}

	assert.Equal(t, [][]string{{"inr", "5", "0"}, {"usd", "10", "6"}}, runCmp(types.ShapeScalar, "", "previous_period"))
	assert.Equal(t, [][]string{
		{"2026-09-01T00:00:00Z", "inr", "5", "0"},
		{"2026-09-01T00:00:00Z", "usd", "10", "6"},
	}, runCmp(types.ShapeTimeseries, types.GrainMonth, "previous_period"))
	assert.Contains(t, runCmp(types.ShapeTimeseries, types.GrainDay, "previous_period"), []string{"2026-09-20T00:00:00Z", "usd", "0", "2"})
	byCustomer := runCmp(types.ShapeBreakdown, "", "previous_period", "customer_id")
	assert.Contains(t, byCustomer, []string{"c1", "usd", "10", "2"})
	assert.Contains(t, byCustomer, []string{"c4", "usd", "0", "4"})
}
