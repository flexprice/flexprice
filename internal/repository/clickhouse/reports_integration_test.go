package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	chstore "github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Reports skip FINAL; read merged parts.
func mergeTables(t *testing.T, admin *chstore.ClickHouseStore, db string, tables ...string) {
	t.Helper()
	for _, table := range tables {
		require.NoError(t, admin.GetConn().Exec(context.Background(), "OPTIMIZE TABLE "+db+"."+table+" FINAL"))
	}
}

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
		period_start DateTime64(6) DEFAULT finalized_at, issue_date DateTime64(6) DEFAULT toDateTime64(0, 6),
		_peerdb_is_deleted UInt8, _peerdb_version Int64
	) ENGINE = ReplacingMergeTree(_peerdb_version) ORDER BY id`))
	baseCols := `(id, tenant_id, environment_id, status, invoice_status, invoice_type, customer_id, currency, total, finalized_at, _peerdb_is_deleted, _peerdb_version)`
	ins := `INSERT INTO ` + db + `.invoices ` + baseCols + ` VALUES `
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
	// Finalized later than the period it bills for.
	require.NoError(t, admin.GetConn().Exec(ctx, `INSERT INTO `+db+`.invoices
		(id, tenant_id, environment_id, status, invoice_status, invoice_type, customer_id, currency, total, finalized_at, period_start, issue_date, _peerdb_is_deleted, _peerdb_version) VALUES
		('i10','t1','e1','published','FINALIZED','SUBSCRIPTION','c5','usd',100,'2026-10-02 00:00:00','2026-09-15 00:00:00','2026-09-15 00:00:00',0,1),
		('i11','t1','e1','published','FINALIZED','ONE_OFF','c6','usd',7,'2026-10-03 00:00:00','1970-01-01 00:00:00','2026-08-25 00:00:00',0,1),
		('i12','t1','e1','published','FINALIZED','SUBSCRIPTION','c7','usd',50,'2026-09-10 00:00:00','2026-07-01 00:00:00','2026-07-01 00:00:00',0,1)`))
	mergeTables(t, admin, db, "invoices")

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

	// Dedup, status, tenant, and epoch filters; finalized_at attribution.
	assert.Equal(t, [][]string{{"inr", "5"}, {"usd", "66"}}, run(types.ShapeScalar, ""))

	// Finalized invoices outside the selected window are excluded.
	assert.Equal(t, [][]string{
		{"2026-08-01T00:00:00Z", "usd", "6"},
		{"2026-09-01T00:00:00Z", "inr", "5"},
		{"2026-09-01T00:00:00Z", "usd", "60"},
	}, run(types.ShapeTimeseries, types.GrainMonth))

	assert.Equal(t, [][]string{
		{"c7", "usd", "50"},
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

	assert.Equal(t, [][]string{{"inr", "5", "0"}, {"usd", "110", "13"}}, runCmp(types.ShapeScalar, "", "previous_period"))
	assert.Equal(t, [][]string{
		{"2026-09-01T00:00:00Z", "inr", "5", "0"},
		{"2026-09-01T00:00:00Z", "usd", "110", "13"},
	}, runCmp(types.ShapeTimeseries, types.GrainMonth, "previous_period"))
	assert.Contains(t, runCmp(types.ShapeTimeseries, types.GrainDay, "previous_period"), []string{"2026-09-20T00:00:00Z", "usd", "0", "2"})
	byCustomer := runCmp(types.ShapeBreakdown, "", "previous_period", "customer_id")
	assert.Contains(t, byCustomer, []string{"c1", "usd", "10", "2"})
	assert.Contains(t, byCustomer, []string{"c4", "usd", "0", "4"})

	arpuCmp := func(shape types.Shape) [][]string {
		rv := &analytics.ResolvedView{
			Family: types.FamilyBilling, Shape: shape, Metrics: []types.Metric{types.MetricARPU},
			Time:    analytics.TimeSpec{From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Grain: types.GrainMonth},
			Compare: "previous_period",
		}
		res, err := repo.RunReport(ctx, rv, "t1", "e1")
		require.NoError(t, err)
		return res.Rows
	}
	// Sep 50/1, Aug 6/3; inr no prior.
	rows := arpuCmp(types.ShapeScalar)
	require.Len(t, rows, 2)
	assert.Equal(t, []string{"inr", "5", ""}, rows[0])
	assert.Equal(t, "usd", rows[1][0])
	assert.Equal(t, "50", rows[1][1])
	assert.Equal(t, "3", rows[1][2])
}

func TestReportsRepository_GenericReports(t *testing.T) {
	address := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_ADDRESS", "localhost:9000")
	username := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_USERNAME", "flexprice")
	password := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_PASSWORD", "flexprice123")
	db := fmt.Sprintf("reports_gen_%d", time.Now().UnixNano())

	admin := newRealClickHouseTestStore(t)
	ctx := context.Background()
	require.NoError(t, admin.GetConn().Exec(ctx, "CREATE DATABASE "+db))
	t.Cleanup(func() { _ = admin.GetConn().Exec(context.Background(), "DROP DATABASE IF EXISTS "+db) })
	require.NoError(t, admin.GetConn().Exec(ctx, `CREATE TABLE `+db+`.invoices (
		id String, tenant_id String, environment_id String, status String,
		invoice_status String, payment_status String, customer_id String, currency String,
		amount_remaining Decimal(20, 9), amount_paid Decimal(20, 9),
		finalized_at DateTime64(6), due_date DateTime64(6), issue_date DateTime64(6), paid_at DateTime64(6),
		total Decimal(20, 9), period_start DateTime64(6),
		_peerdb_is_deleted UInt8, _peerdb_version Int64
	) ENGINE = ReplacingMergeTree(_peerdb_version) ORDER BY id`))
	require.NoError(t, admin.GetConn().Exec(ctx, `CREATE TABLE `+db+`.wallet_transactions (
		id String, tenant_id String, environment_id String, status String,
		type String, transaction_status String, transaction_reason String,
		customer_id String, currency String, amount Decimal(20, 9), credit_amount Decimal(20, 9), conversion_rate Nullable(Decimal(10, 5)), created_at DateTime64(6), consumption_breakdown Nullable(String),
		_peerdb_is_deleted UInt8, _peerdb_version Int64
	) ENGINE = ReplacingMergeTree(_peerdb_version) ORDER BY id`))

	epoch := "1970-01-01 00:00:00"
	future := time.Now().UTC().AddDate(0, 0, 10).Format("2006-01-02 15:04:05")
	d10 := time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02 15:04:05")
	d100 := time.Now().UTC().AddDate(0, 0, -100).Format("2006-01-02 15:04:05")
	inv := func(id, invStatus, pay, cust string, remaining, paid int, due, issue, paidAt string) string {
		return fmt.Sprintf("('%s','t1','e1','published','%s','%s','%s','usd',%d,%d,'2026-09-01 00:00:00','%s','%s','%s',%d,'%s',0,1)",
			id, invStatus, pay, cust, remaining, paid, due, issue, paidAt, remaining+paid, epoch)
	}
	unpaid := func(id, invStatus, cust string, total int, period string) string {
		return fmt.Sprintf("('%s','t1','e1','published','%s','PENDING','%s','usd',0,0,'2026-09-01 00:00:00','%s','%s','%s',%d,'%s',0,1)",
			id, invStatus, cust, epoch, epoch, epoch, total, period)
	}
	require.NoError(t, admin.GetConn().Exec(ctx, `INSERT INTO `+db+`.invoices VALUES `+strings.Join([]string{
		inv("a1", "FINALIZED", "PENDING", "c1", 30, 0, epoch, epoch, epoch),
		inv("a2", "FINALIZED", "PENDING", "c1", 70, 0, future, epoch, epoch),
		inv("a3", "FINALIZED", "SUCCEEDED", "c1", 0, 5, d10, epoch, epoch),
		inv("a4", "DRAFT", "PENDING", "c1", 500, 0, d10, epoch, epoch),
		inv("a5", "FINALIZED", "PENDING", "c2", 11, 0, d10, epoch, epoch),
		inv("a6", "FINALIZED", "PENDING", "c2", 13, 0, d100, epoch, epoch),
		inv("p1", "FINALIZED", "SUCCEEDED", "c1", 0, 100, epoch, "2026-09-01 00:00:00", "2026-09-11 00:00:00"),
		inv("p2", "FINALIZED", "SUCCEEDED", "c1", 0, 50, epoch, "2026-09-05 00:00:00", "2026-09-25 00:00:00"),
		inv("p3", "FINALIZED", "OVERPAID", "c2", 0, 30, epoch, epoch, "2026-09-13 00:00:00"),
		unpaid("b1", "FINALIZED", "c3", 200, "2026-09-15 00:00:00"),
		unpaid("b2", "FINALIZED", "c3", 100, "2026-09-16 00:00:00"),
		unpaid("b3", "FINALIZED", "c4", 60, "2026-09-17 00:00:00"),
		unpaid("b4", "DRAFT", "c5", 999, "2026-09-18 00:00:00"),
	}, ",")))

	tx := func(id, typ, st, reason string, amount int) string {
		return fmt.Sprintf("('%s','t1','e1','published','%s','%s','%s','c1','usd',%d,%d,1,'2026-09-10 00:00:00','[]',0,1)", id, typ, st, reason, amount, amount)
	}
	require.NoError(t, admin.GetConn().Exec(ctx, `INSERT INTO `+db+`.wallet_transactions VALUES `+strings.Join([]string{
		tx("w1", "credit", "completed", "PURCHASED_CREDIT_DIRECT", 50),
		tx("w2", "credit", "completed", "FREE_CREDIT_GRANT", 10),
		tx("w3", "credit", "failed", "PURCHASED_CREDIT_INVOICED", 99),
		tx("w4", "credit", "completed", "CREDIT_NOTE", 7),
		tx("w6", "debit", "completed", "CREDIT_EXPIRED", 5),
		`('w5','t1','e1','published','debit','completed','INVOICE_PAYMENT','c1','usd',12,12,1,'2026-09-10 00:00:00','[{"credit_transaction_id":"w1","credits":8},{"credit_transaction_id":"w2","credits":4}]',0,1)`,
	}, ",")))
	mergeTables(t, admin, db, "invoices", "wallet_transactions")

	cfg := &config.Configuration{
		ClickHouse: config.ClickHouseConfig{Address: address, Username: username, Password: password, Database: "default", MaxMemoryUsage: 1},
		Reports:    config.ReportsConfig{Database: db, Username: username, Password: password, MaxResultRows: 100},
	}
	repo, err := NewReportsRepository(cfg, nil)
	require.NoError(t, err)

	run := func(metric types.Metric, shape types.Shape, dims ...string) [][]string {
		rv := &analytics.ResolvedView{
			Family: types.FamilyBilling, Shape: shape, Metrics: []types.Metric{metric}, Dimensions: dims,
			Time: analytics.TimeSpec{
				From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
				To:   time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			},
		}
		res, err := repo.RunReport(ctx, rv, "t1", "e1")
		require.NoError(t, err)
		return res.Rows
	}

	// Snapshot ignores the window.
	assert.Equal(t, [][]string{{"usd", "124"}}, run(types.MetricTotalReceivables, types.ShapeScalar))

	aging := map[string]string{}
	for _, r := range run(types.MetricReceivablesAging, types.ShapeBreakdown, "aging_bucket") {
		aging[r[0]] = r[2]
	}
	assert.Equal(t, map[string]string{"No due date": "30", "Current": "70", "1-30": "11", "90+": "13"}, aging)

	assert.Equal(t, [][]string{{"usd", "15"}}, run(types.MetricDaysToPay, types.ShapeScalar))

	assert.ElementsMatch(t, [][]string{
		{"FREE_CREDIT_GRANT", "usd", "10"},
		{"PURCHASED_CREDIT_DIRECT", "usd", "50"},
	}, run(types.MetricCreditTopups, types.ShapeBreakdown, "transaction_reason"))

	assert.ElementsMatch(t, [][]string{{"free", "usd", "4"}, {"paid", "usd", "8"}}, run(types.MetricCreditConsumption, types.ShapeScalar))

	assert.Equal(t, [][]string{{"usd", "90"}}, run(types.MetricARPUPaid, types.ShapeScalar))

	// p1, p2 (c1), b1, b2 (c3), b3 (c4): 510 over 3 customers.
	assert.Equal(t, [][]string{{"usd", "170"}}, run(types.MetricARPU, types.ShapeScalar))
	monthly := &analytics.ResolvedView{
		Family: types.FamilyBilling, Shape: types.ShapeTimeseries, Metrics: []types.Metric{types.MetricARPU},
		Time: analytics.TimeSpec{
			From:  time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			To:    time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			Grain: types.GrainMonth,
		},
	}
	mres, err := repo.RunReport(ctx, monthly, "t1", "e1")
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"2026-09-01T00:00:00Z", "usd", "170"}}, mres.Rows)
}

func TestReportsRepository_CustomReports(t *testing.T) {
	address := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_ADDRESS", "localhost:9000")
	username := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_USERNAME", "flexprice")
	password := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_PASSWORD", "flexprice123")
	db := fmt.Sprintf("reports_cus_%d", time.Now().UnixNano())

	admin := newRealClickHouseTestStore(t)
	ctx := context.Background()
	require.NoError(t, admin.GetConn().Exec(ctx, "CREATE DATABASE "+db))
	t.Cleanup(func() { _ = admin.GetConn().Exec(context.Background(), "DROP DATABASE IF EXISTS "+db) })
	require.NoError(t, admin.GetConn().Exec(ctx, `CREATE TABLE `+db+`.invoices (
		id String, tenant_id String, environment_id String, status String,
		invoice_status String, currency String, total Decimal(20, 9), finalized_at DateTime64(6),
		period_start DateTime64(6), issue_date DateTime64(6),
		_peerdb_is_deleted UInt8, _peerdb_version Int64
	) ENGINE = ReplacingMergeTree(_peerdb_version) ORDER BY id`))
	require.NoError(t, admin.GetConn().Exec(ctx, `CREATE TABLE `+db+`.credit_notes (
		id String, tenant_id String, environment_id String, status String,
		credit_note_status String, currency String, total_amount Decimal(20, 8),
		created_at DateTime64(6), finalized_at DateTime64(6),
		_peerdb_is_deleted UInt8, _peerdb_version Int64
	) ENGINE = ReplacingMergeTree(_peerdb_version) ORDER BY id`))
	require.NoError(t, admin.GetConn().Exec(ctx, `CREATE TABLE `+db+`.subscriptions (
		id String, tenant_id String, environment_id String, status String,
		customer_id String, currency String, subscription_status String,
		start_date DateTime64(6), end_date DateTime64(6), cancelled_at DateTime64(6),
		_peerdb_is_deleted UInt8, _peerdb_version Int64
	) ENGINE = ReplacingMergeTree(_peerdb_version) ORDER BY id`))

	require.NoError(t, admin.GetConn().Exec(ctx, `INSERT INTO `+db+`.invoices VALUES
		('i1','t1','e1','published','FINALIZED','usd',100,'2026-09-10 00:00:00','2026-09-10 00:00:00','2026-09-10 00:00:00',0,1),
		('i2','t1','e1','published','DRAFT','usd',500,'2026-09-10 00:00:00','2026-09-10 00:00:00','2026-09-10 00:00:00',0,1),
		('i3','t1','e1','published','FINALIZED','usd',30,'2026-10-05 00:00:00','2026-09-20 00:00:00','2026-09-20 00:00:00',0,1),
		('i4','t1','e1','published','FINALIZED','usd',8,'2026-09-02 00:00:00','1970-01-01 00:00:00','2026-10-08 00:00:00',0,1),
		('i5','t1','e1','published','FINALIZED','usd',40,'2026-10-05 00:00:00','2026-09-25 00:00:00','2026-10-05 00:00:00',0,1)`))
	require.NoError(t, admin.GetConn().Exec(ctx, `INSERT INTO `+db+`.credit_notes VALUES
		('n1','t1','e1','published','FINALIZED','usd',20,'2026-09-12 00:00:00','2026-09-12 00:00:00',0,1),
		('n2','t1','e1','published','FINALIZED','usd',15,'2026-10-03 00:00:00','2026-10-03 00:00:00',0,1),
		('n3','t1','e1','published','DRAFT','usd',99,'2026-09-12 00:00:00','1970-01-01 00:00:00',0,1)`))

	epoch := "1970-01-01 00:00:00"
	sub := func(id, cust, st, start, end, cancelled string) string {
		return fmt.Sprintf("('%s','t1','e1','published','%s','usd','%s','%s','%s','%s',0,1)", id, cust, st, start, end, cancelled)
	}
	require.NoError(t, admin.GetConn().Exec(ctx, `INSERT INTO `+db+`.subscriptions VALUES `+strings.Join([]string{
		sub("s1", "c1", "active", "2026-01-01 00:00:00", epoch, epoch),
		sub("s2", "c1", "active", "2026-08-15 00:00:00", epoch, epoch),
		sub("s3", "c2", "trialing", "2026-08-20 00:00:00", epoch, epoch),
		sub("s4", "c3", "cancelled", "2026-01-01 00:00:00", epoch, "2026-09-15 00:00:00"),
		sub("s5", "c4", "active", "2026-10-05 00:00:00", epoch, epoch),
		sub("s6", "c5", "cancelled", "2026-01-01 00:00:00", "2026-09-20 00:00:00", epoch),
		sub("s7", "c6", "cancelled", "2026-01-01 00:00:00", epoch, epoch),
		sub("s8", "c7", "cancelled", "2026-01-01 00:00:00", epoch, "2026-07-15 00:00:00"),
	}, ",")))
	mergeTables(t, admin, db, "invoices", "credit_notes", "subscriptions")

	cfg := &config.Configuration{
		ClickHouse: config.ClickHouseConfig{Address: address, Username: username, Password: password, Database: "default", MaxMemoryUsage: 1},
		Reports:    config.ReportsConfig{Database: db, Username: username, Password: password, MaxResultRows: 100},
	}
	repo, err := NewReportsRepository(cfg, nil)
	require.NoError(t, err)

	run := func(metric types.Metric, shape types.Shape, from, to time.Time, compare ...string) [][]string {
		rv := &analytics.ResolvedView{
			Family: types.FamilyBilling, Shape: shape, Metrics: []types.Metric{metric},
			Time: analytics.TimeSpec{From: from, To: to, Grain: types.GrainMonth},
		}
		if len(compare) > 0 {
			rv.Compare = compare[0]
		}
		res, err := repo.RunReport(ctx, rv, "t1", "e1")
		require.NoError(t, err)
		return res.Rows
	}
	sep := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	oct := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	assert.Equal(t, [][]string{
		{"2026-09-01T00:00:00Z", "usd", "88"},
		{"2026-10-01T00:00:00Z", "usd", "55"},
	}, run(types.MetricNetBilling, types.ShapeTimeseries, sep, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)))

	// i5: period in Sep, issued in Oct.
	assert.Equal(t, [][]string{
		{"2026-09-01T00:00:00Z", "usd", "88"},
		{"2026-10-01T00:00:00Z", "usd", "55"},
	}, run(types.MetricNetBillingIssued, types.ShapeTimeseries, sep, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, [][]string{
		{"2026-09-01T00:00:00Z", "usd", "108"},
		{"2026-10-01T00:00:00Z", "usd", "70"},
	}, run(types.MetricInvoiceTotal, types.ShapeTimeseries, sep, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, [][]string{
		{"2026-09-01T00:00:00Z", "usd", "108"},
		{"2026-10-01T00:00:00Z", "usd", "70"},
	}, run(types.MetricInvoiceTotalIssued, types.ShapeTimeseries, sep, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)))
	assert.Equal(t, [][]string{{"usd", "70", "108"}},
		run(types.MetricInvoiceTotalIssued, types.ShapeScalar, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), "previous_period"))

	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	// c1, c2 open; c3 cancelled_at, c5 end_date mid-window.
	assert.Equal(t, [][]string{{"usd", "4"}}, run(types.MetricActiveCustomers, types.ShapeScalar, sep, oct))
	// Cancelled and ended subs drop out; c4 starts.
	assert.Equal(t, [][]string{{"usd", "3"}}, run(types.MetricActiveCustomers, types.ShapeScalar, oct, nov))
	// Both bounded intervals ended before window.
	assert.Equal(t, [][]string{{"usd", "2"}}, run(types.MetricActiveCustomers, types.ShapeScalar, late, oct))
}

func TestReportsRepository_EventsVolume(t *testing.T) {
	address := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_ADDRESS", "localhost:9000")
	username := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_USERNAME", "flexprice")
	password := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_PASSWORD", "flexprice123")
	db := fmt.Sprintf("reports_evt_%d", time.Now().UnixNano())

	admin := newRealClickHouseTestStore(t)
	ctx := context.Background()
	require.NoError(t, admin.GetConn().Exec(ctx, "CREATE DATABASE "+db))
	t.Cleanup(func() { _ = admin.GetConn().Exec(context.Background(), "DROP DATABASE IF EXISTS "+db) })
	require.NoError(t, admin.GetConn().Exec(ctx, `CREATE TABLE `+db+`.meter_usage (
		id String, tenant_id String, environment_id String, external_customer_id String,
		meter_id String, event_name String, timestamp DateTime, qty_total Decimal(18, 8)
	) ENGINE = MergeTree PARTITION BY toYYYYMMDD(timestamp)
	ORDER BY (tenant_id, environment_id, external_customer_id, meter_id, timestamp, id)`))

	n := 0
	row := func(tenant, env, meter, ts string) string {
		n++
		return fmt.Sprintf("('e%d','%s','%s','c1','%s','evt','%s',1)", n, tenant, env, meter, ts)
	}
	require.NoError(t, admin.GetConn().Exec(ctx, `INSERT INTO `+db+`.meter_usage VALUES `+strings.Join([]string{
		row("t1", "e1", "m1", "2026-09-01 00:00:00"),
		row("t1", "e1", "m1", "2026-09-01 12:00:00"),
		row("t1", "e1", "m2", "2026-09-01 23:59:59"),
		row("t1", "e1", "m1", "2026-09-02 08:00:00"),
		row("t1", "e1", "m1", "2026-09-08 00:00:00"),
		row("t1", "e1", "m2", "2026-09-30 23:59:59"),
		row("t1", "e1", "m1", "2026-10-01 00:00:00"),
		row("t1", "e1", "m1", "2026-08-31 23:59:59"),
		row("t1", "e2", "m1", "2026-09-01 10:00:00"),
		row("t2", "e1", "m1", "2026-09-01 10:00:00"),
	}, ",")))

	cfg := &config.Configuration{
		ClickHouse: config.ClickHouseConfig{Address: address, Username: username, Password: password, Database: "default", MaxMemoryUsage: 1},
		Reports:    config.ReportsConfig{Database: db, Username: username, Password: password, MaxResultRows: 100},
	}
	repo, err := NewReportsRepository(cfg, nil)
	require.NoError(t, err)

	run := func(tenant string, shape types.Shape, grain types.Grain, limit int, filters []*analytics.Filter, dims ...string) [][]string {
		rv := &analytics.ResolvedView{
			Family: types.FamilyBilling, Shape: shape, Metrics: []types.Metric{types.MetricEventsVolume},
			Dimensions: dims, Filters: filters, Limit: limit,
			Time: analytics.TimeSpec{
				From:  time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
				To:    time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
				Grain: grain,
			},
		}
		res, err := repo.RunReport(ctx, rv, tenant, "e1")
		require.NoError(t, err)
		return res.Rows
	}

	assert.Equal(t, [][]string{
		{"2026-09-01T00:00:00Z", "3"},
		{"2026-09-02T00:00:00Z", "1"},
		{"2026-09-08T00:00:00Z", "1"},
		{"2026-09-30T00:00:00Z", "1"},
	}, run("t1", types.ShapeTimeseries, types.GrainDay, 0, nil))
	assert.Equal(t, [][]string{
		{"2026-08-31T00:00:00Z", "4"},
		{"2026-09-07T00:00:00Z", "1"},
		{"2026-09-28T00:00:00Z", "1"},
	}, run("t1", types.ShapeTimeseries, types.GrainWeek, 0, nil))
	assert.Equal(t, [][]string{{"2026-09-01T00:00:00Z", "6"}}, run("t1", types.ShapeTimeseries, types.GrainMonth, 0, nil))
	assert.Equal(t, [][]string{
		{"2026-09-01T00:00:00Z", "m1", "2"},
		{"2026-09-01T00:00:00Z", "m2", "1"},
		{"2026-09-02T00:00:00Z", "m1", "1"},
		{"2026-09-08T00:00:00Z", "m1", "1"},
		{"2026-09-30T00:00:00Z", "m2", "1"},
	}, run("t1", types.ShapeTimeseries, types.GrainDay, 0, nil, "meter_id"))

	assert.Equal(t, [][]string{{"m1", "4"}, {"m2", "2"}}, run("t1", types.ShapeBreakdown, "", 0, nil, "meter_id"))
	assert.Equal(t, [][]string{{"m1", "4"}}, run("t1", types.ShapeBreakdown, "", 1, nil, "meter_id"))
	only2 := []*analytics.Filter{{Field: "meter_id", Op: types.IN, Value: []string{"m2", "nope"}}}
	assert.Equal(t, [][]string{{"m2", "2"}}, run("t1", types.ShapeBreakdown, "", 0, only2, "meter_id"))

	assert.Equal(t, [][]string{{"m1", "1"}}, run("t2", types.ShapeBreakdown, "", 0, nil, "meter_id"))
	assert.Empty(t, run("t3", types.ShapeBreakdown, "", 0, nil, "meter_id"))
}
