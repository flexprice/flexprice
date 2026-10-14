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

func TestReportsRepository_Datasets(t *testing.T) {
	address := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_ADDRESS", "localhost:9000")
	username := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_USERNAME", "flexprice")
	password := chEnvOrDefault("FLEXPRICE_TEST_CLICKHOUSE_PASSWORD", "flexprice123")
	suffix := time.Now().UnixNano()
	db := fmt.Sprintf("reports_ds_%d", suffix)
	usageDB := fmt.Sprintf("reports_dsu_%d", suffix)

	admin := newRealClickHouseTestStore(t)
	ctx := context.Background()
	for _, d := range []string{db, usageDB} {
		require.NoError(t, admin.GetConn().Exec(ctx, "CREATE DATABASE "+d))
		d := d
		t.Cleanup(func() { _ = admin.GetConn().Exec(context.Background(), "DROP DATABASE IF EXISTS "+d) })
	}

	const dec = "Decimal(20, 9)"
	const ts = "DateTime64(6)"
	scope := "id String, tenant_id String, environment_id String, status String, _peerdb_is_deleted UInt8, _peerdb_version Int64"
	mirrored := func(table, cols string) {
		require.NoError(t, admin.GetConn().Exec(ctx, "CREATE TABLE "+db+"."+table+" ("+scope+", "+cols+
			") ENGINE = ReplacingMergeTree(_peerdb_version) ORDER BY id"))
	}
	mirrored("invoices", `customer_id String, subscription_id String, currency String, invoice_status String,
		payment_status String, invoice_type String, billing_reason String,
		period_start `+ts+`, issue_date `+ts+`, finalized_at `+ts+`, due_date `+ts+`, paid_at `+ts+`,
		total `+dec+`, amount_due `+dec+`, amount_paid `+dec+`, amount_remaining `+dec+`,
		total_tax `+dec+`, total_discount `+dec+`, total_credits_applied `+dec)
	mirrored("customers", `name String, external_id String, address_country String, billing_currency String,
		tax_treatment String, created_at `+ts)
	mirrored("subscriptions", `customer_id String, currency String, subscription_status String, billing_period String,
		billing_cadence String, collection_method String, plan_id String,
		start_date `+ts+`, created_at `+ts+`, cancelled_at `+ts+`, commitment_amount `+dec)
	mirrored("invoice_line_items", `invoice_id String, customer_id String, meter_id String, currency String,
		price_type String, plan_display_name String, display_name String, entity_type String,
		period_start `+ts+`, amount `+dec+`, quantity `+dec+`, credits_applied `+dec+`, line_item_discount `+dec)
	mirrored("meters", `name String`)
	mirrored("wallets", `wallet_type String`)
	mirrored("wallet_transactions", `wallet_id String, customer_id String, currency String, type String,
		transaction_reason String, transaction_status String,
		created_at `+ts+`, credit_amount `+dec+`, amount `+dec)
	mirrored("credit_notes", `customer_id String, currency String, credit_note_status String, credit_note_type String,
		reason String, refund_status String, finalized_at `+ts+`, created_at `+ts+`, total_amount `+dec)
	require.NoError(t, admin.GetConn().Exec(ctx, `CREATE TABLE `+usageDB+`.meter_usage (
		id String, tenant_id String, environment_id String, external_customer_id String,
		meter_id String, source String, timestamp DateTime, qty_total Decimal(18, 8)
	) ENGINE = MergeTree ORDER BY (tenant_id, environment_id, external_customer_id, meter_id, timestamp, id)`))

	q := func(s string) string { return "'" + s + "'" }
	// Omitted columns take type defaults (epoch times, zero amounts).
	seed := func(dbName, table, cols string, rows ...string) {
		require.NoError(t, admin.GetConn().Exec(ctx, "INSERT INTO "+dbName+"."+table+" ("+cols+") VALUES "+strings.Join(rows, ",")))
	}
	const sc = "id, tenant_id, environment_id, status, _peerdb_is_deleted, _peerdb_version, "
	row := func(id, tenant, status string, deleted, version int, extra ...string) string {
		return fmt.Sprintf("(%s,%s,'e1',%s,%d,%d,%s)", q(id), q(tenant), q(status), deleted, version, strings.Join(extra, ","))
	}
	pub := "published"
	sep := func(day int) string { return q(fmt.Sprintf("2026-09-%02d 00:00:00", day)) }
	aug := func(day int) string { return q(fmt.Sprintf("2026-08-%02d 00:00:00", day)) }

	seed(db, "customers", sc+"name, external_id, address_country, created_at",
		row("c1", "t1", pub, 0, 1, q("Old"), q("x1"), q("US"), sep(1)),
		row("c1", "t1", pub, 0, 2, q("Alice"), q("x1"), q("US"), sep(1)),
		row("c2", "t1", pub, 0, 1, q("Bob"), q("x2"), q("IN"), sep(2)),
		row("c6", "t1", pub, 0, 1, q("Cara"), q("x6"), q("US"), sep(3)),
		row("c3", "t1", pub, 1, 1, q("Gone"), q("x3"), q("US"), sep(4)),
		row("c4", "t2", pub, 0, 1, q("Other"), q("x4"), q("US"), sep(4)),
		row("c5", "t1", "archived", 0, 1, q("Dup"), q("x1"), q("US"), sep(5)))

	seed(db, "invoices", sc+"customer_id, currency, invoice_status, total, period_start",
		row("i1", "t1", pub, 0, 1, q("c1"), q("usd"), q("FINALIZED"), "1", sep(5)),
		row("i1", "t1", pub, 0, 2, q("c1"), q("usd"), q("FINALIZED"), "10", sep(5)),
		row("i2", "t1", pub, 0, 1, q("c1"), q("usd"), q("FINALIZED"), "20", sep(6)),
		row("i3", "t1", pub, 0, 1, q("c2"), q("inr"), q("FINALIZED"), "5", sep(7)),
		row("i4", "t1", pub, 1, 1, q("c1"), q("usd"), q("FINALIZED"), "99", sep(8)),
		row("i5", "t2", pub, 0, 1, q("c4"), q("usd"), q("FINALIZED"), "77", sep(8)),
		row("i7", "t1", pub, 0, 1, q("c2"), q("usd"), q("FINALIZED"), "40", aug(10)),
		row("i8", "t1", pub, 0, 1, q("c2"), q("usd"), q("DRAFT"), "1000", aug(12)))

	seed(db, "meters", sc+"name",
		row("mt1", "t1", pub, 0, 1, q("Meter One")),
		row("mt2", "t1", pub, 0, 1, q("Meter Two")))

	seed(db, "invoice_line_items", sc+"invoice_id, customer_id, meter_id, currency, amount, period_start",
		row("l1", "t1", pub, 0, 1, q("i1"), q("c1"), q("mt1"), q("usd"), "4", sep(5)),
		row("l2", "t1", pub, 0, 1, q("i2"), q("c1"), q("mt1"), q("usd"), "6", sep(6)),
		row("l3", "t1", pub, 0, 1, q("i3"), q("c2"), q("mt2"), q("inr"), "5", sep(7)),
		row("l4", "t1", pub, 1, 1, q("i3"), q("c2"), q("mt2"), q("inr"), "50", sep(7)),
		row("l5", "t2", pub, 0, 1, q("i5"), q("c4"), q("mt1"), q("usd"), "70", sep(8)))

	seed(db, "wallets", sc+"wallet_type",
		row("wl1", "t1", pub, 0, 1, q("OLD")),
		row("wl1", "t1", pub, 0, 2, q("PRE_PAID")))
	seed(db, "wallet_transactions", sc+"wallet_id, customer_id, currency, type, amount, created_at",
		row("w1", "t1", pub, 0, 1, q("wl1"), q("c1"), q("usd"), q("credit"), "50", sep(10)),
		row("w2", "t1", pub, 0, 1, q("wl1"), q("c1"), q("usd"), q("debit"), "12", sep(11)),
		row("w3", "t1", pub, 1, 1, q("wl1"), q("c1"), q("usd"), q("credit"), "99", sep(12)),
		row("w4", "t2", pub, 0, 1, q("wl1"), q("c4"), q("usd"), q("credit"), "77", sep(12)))

	seed(db, "credit_notes", sc+"customer_id, currency, credit_note_status, total_amount, finalized_at",
		row("n1", "t1", pub, 0, 1, q("c1"), q("usd"), q("FINALIZED"), "20", sep(12)),
		row("n2", "t1", pub, 0, 1, q("c2"), q("inr"), q("FINALIZED"), "8", sep(13)),
		row("n3", "t1", pub, 1, 1, q("c1"), q("usd"), q("FINALIZED"), "99", sep(12)),
		row("n4", "t2", pub, 0, 1, q("c4"), q("usd"), q("FINALIZED"), "77", sep(12)))

	seed(db, "subscriptions", sc+"customer_id, currency, subscription_status, commitment_amount, start_date",
		row("s1", "t1", pub, 0, 1, q("c1"), q("usd"), q("active"), "100", sep(2)),
		row("s2", "t1", pub, 0, 1, q("c1"), q("usd"), q("active"), "50", sep(3)),
		row("s3", "t1", pub, 0, 1, q("c2"), q("inr"), q("cancelled"), "30", sep(4)),
		row("s4", "t1", pub, 1, 1, q("c2"), q("inr"), q("active"), "999", sep(4)),
		row("s5", "t2", pub, 0, 1, q("c4"), q("usd"), q("active"), "888", sep(4)))

	usage := func(id, tenant, ext, meter, at string, qty int) string {
		return fmt.Sprintf("(%s,%s,'e1',%s,%s,'api',%s,%d)", q(id), q(tenant), q(ext), q(meter), at, qty)
	}
	seed(usageDB, "meter_usage", "id, tenant_id, environment_id, external_customer_id, meter_id, source, timestamp, qty_total",
		usage("u1", "t1", "x1", "mt1", sep(2), 3),
		usage("u2", "t1", "x1", "mt1", sep(3), 4),
		usage("u3", "t1", "x2", "mt2", sep(4), 5),
		usage("u4", "t2", "x1", "mt1", sep(4), 100))

	cfg := &config.Configuration{
		ClickHouse: config.ClickHouseConfig{Address: address, Username: username, Password: password, Database: "default", MaxMemoryUsage: 1},
		Reports:    config.ReportsConfig{Database: db, UsageDatabase: usageDB, Username: username, Password: password, MaxResultRows: 100},
	}
	repo, err := NewReportsRepository(cfg, nil)
	require.NoError(t, err)

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	run := func(def *analytics.ViewDefinition, from, to time.Time, compare string) [][]string {
		res, err := repo.RunDataset(ctx, &analytics.DatasetQuery{Def: def, From: from, To: to, Compare: compare}, "t1", "e1")
		require.NoError(t, err)
		return res.Rows
	}
	count := &analytics.Measure{Agg: "count"}
	sum := func(f string) *analytics.Measure { return &analytics.Measure{Agg: "sum", Field: f} }

	cases := []struct {
		name     string
		def      analytics.ViewDefinition
		from, to time.Time
		want     [][]string
	}{
		{
			name: "invoices",
			def: analytics.ViewDefinition{Dataset: "invoices", Measures: []*analytics.Measure{count, sum("total")},
				Dimensions: []string{"customer"}, Time: analytics.TimeSpecRaw{Field: "billing_period"}},
			from: from, to: to,
			want: [][]string{{"c1", "Alice", "usd", "2", "30"}, {"c2", "Bob", "inr", "1", "5"}},
		},
		{
			name: "invoice_lines",
			def: analytics.ViewDefinition{Dataset: "invoice_lines", Measures: []*analytics.Measure{count, sum("amount")},
				Dimensions: []string{"meter"}, Time: analytics.TimeSpecRaw{Field: "billing_period"}},
			from: from, to: to,
			want: [][]string{{"mt1", "Meter One", "usd", "2", "10"}, {"mt2", "Meter Two", "inr", "1", "5"}},
		},
		{
			name: "wallet_transactions",
			def: analytics.ViewDefinition{Dataset: "wallet_transactions", Measures: []*analytics.Measure{count, sum("amount")},
				Dimensions: []string{"wallet_type"}, Time: analytics.TimeSpecRaw{Field: "created_at"}},
			from: from, to: to,
			want: [][]string{{"PRE_PAID", "usd", "2", "62"}},
		},
		{
			name: "credit_notes",
			def: analytics.ViewDefinition{Dataset: "credit_notes", Measures: []*analytics.Measure{count, sum("total_amount")},
				Dimensions: []string{"credit_note_status"}, Time: analytics.TimeSpecRaw{Field: "finalized_at"}},
			from: from, to: to,
			want: [][]string{{"FINALIZED", "usd", "1", "20"}, {"FINALIZED", "inr", "1", "8"}},
		},
		{
			name: "subscriptions",
			def: analytics.ViewDefinition{Dataset: "subscriptions", Measures: []*analytics.Measure{count, sum("commitment_amount")},
				Dimensions: []string{"subscription_status"}, Time: analytics.TimeSpecRaw{Field: "start_date"}},
			from: from, to: to,
			want: [][]string{{"active", "usd", "2", "150"}, {"cancelled", "inr", "1", "30"}},
		},
		{
			name: "customers",
			def: analytics.ViewDefinition{Dataset: "customers", Measures: []*analytics.Measure{count},
				Dimensions: []string{"address_country"}, Time: analytics.TimeSpecRaw{Field: "created_at"}},
			from: from, to: to,
			want: [][]string{{"US", "2"}, {"IN", "1"}},
		},
		{
			// Archived duplicate external id must not double usage.
			name: "usage",
			def: analytics.ViewDefinition{Dataset: "usage", Measures: []*analytics.Measure{count, sum("quantity")},
				Dimensions: []string{"customer"}, Time: analytics.TimeSpecRaw{Field: "timestamp"}},
			from: from, to: from.AddDate(0, 0, 7),
			want: [][]string{{"c1", "Alice", "2", "7"}, {"c2", "Bob", "1", "5"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := tc.def
			assert.ElementsMatch(t, tc.want, run(&def, tc.from, tc.to, ""))
		})
	}

	finalized := []*analytics.Filter{{Field: "invoice_status", Op: types.IN, Value: []string{"FINALIZED"}}}
	t.Run("cross_check_invoice_total", func(t *testing.T) {
		wide := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
		got := run(&analytics.ViewDefinition{Dataset: "invoices", Measures: []*analytics.Measure{sum("total")},
			Filters: finalized, Time: analytics.TimeSpecRaw{Field: "billing_period", Grain: types.GrainMonth}}, wide, to, "")
		res, err := repo.RunReport(ctx, &analytics.ResolvedView{
			Family: types.FamilyBilling, Shape: types.ShapeTimeseries, Metrics: []types.Metric{types.MetricInvoiceTotal},
			Time: analytics.TimeSpec{From: wide, To: to, Grain: types.GrainMonth},
		}, "t1", "e1")
		require.NoError(t, err)
		require.NotEmpty(t, got)
		assert.Equal(t, res.Rows, got)
	})

	t.Run("compare_previous_period", func(t *testing.T) {
		got := run(&analytics.ViewDefinition{Dataset: "invoices", Measures: []*analytics.Measure{sum("total")},
			Filters: finalized, Time: analytics.TimeSpecRaw{Field: "billing_period"}}, from, to, "previous_period")
		assert.Equal(t, [][]string{{"inr", "5", "0"}, {"usd", "30", "40"}}, got)
	})

	t.Run("injection_filter", func(t *testing.T) {
		got := run(&analytics.ViewDefinition{Dataset: "invoices", Measures: []*analytics.Measure{count},
			Filters: []*analytics.Filter{{Field: "currency", Op: types.EQUAL, Value: []string{"' OR 1=1 --"}}},
			Time:    analytics.TimeSpecRaw{Field: "billing_period"}}, from, to, "")
		assert.Equal(t, [][]string{{"0"}}, got)
	})
}
