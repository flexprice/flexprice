//go:build pgintegration

package ent

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/planpricesync"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// Run with:
//
//	go test -tags pgintegration ./internal/repository/ent -run TestTerminatePlanPricesLineItemsV2
func TestTerminatePlanPricesLineItemsV2(t *testing.T) {
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	client := postgres.NewClient(clients, log, nil)

	runID := types.GenerateUUIDWithPrefix("pps")
	ctx := types.SetUserID(types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_"+runID), "user_test")
	tenantID, envID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	planID := "plan_" + runID
	db := client.Writer(ctx)
	exec := func(q string, args ...any) {
		_, err := db.ExecContext(ctx, q, args...)
		require.NoError(t, err)
	}

	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	oldEnd := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	newEnd := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)

	// Ended long ago (seq 5), ended recently (seq 12), still live (seq 11).
	for _, p := range []struct {
		id  string
		seq int64
		end *time.Time
	}{{"old", 5, &oldEnd}, {"new", 12, &newEnd}, {"live", 11, nil}} {
		exec(`INSERT INTO prices (id, tenant_id, environment_id, status, created_at, updated_at, amount, currency, type,
		        billing_period, billing_period_count, billing_model, entity_type, entity_id, start_date, end_date, sequence)
		      VALUES ($1, $2, $3, 'published', now(), now(), 1, 'usd', 'USAGE', 'MONTHLY', 1, 'FLAT_FEE', 'PLAN', $4, $5, $6, $7)`,
			"price_"+p.id+runID, tenantID, envID, planID, start, p.end, p.seq)
	}

	// subA was last stamped at 10, so it already saw "old" ended; subB (at 3) saw neither.
	for _, s := range []struct {
		id  string
		seq int64
	}{{"a", 10}, {"b", 3}} {
		exec(`INSERT INTO subscriptions (id, tenant_id, environment_id, status, created_at, updated_at, customer_id, plan_id,
		        currency, billing_anchor, start_date, current_period_start, current_period_end, billing_cadence,
		        billing_period, synced_price_sequence)
		      VALUES ($1, $2, $3, 'published', now(), now(), 'cust_x', $4, 'usd', $5, $5, $5, $5, 'RECURRING', 'MONTHLY', $6)`,
			"subs_"+s.id+runID, tenantID, envID, planID, start, s.seq)
		for _, p := range []string{"old", "new", "live"} {
			exec(`INSERT INTO subscription_line_items (id, tenant_id, environment_id, status, created_at, updated_at,
			        customer_id, entity_id, entity_type, price_id, quantity, currency, billing_period, start_date, subscription_id)
			      VALUES ($1, $2, $3, 'published', now(), now(), 'cust_x', $4, 'plan', $5, 0, 'usd', 'MONTHLY', $6, $7)`,
				"subsli_"+s.id+p+runID, tenantID, envID, planID, "price_"+p+runID, start, "subs_"+s.id+runID)
		}
	}

	repo := NewPlanPriceSyncRepository(client, log)
	n, err := repo.TerminatePlanPricesLineItemsV2(ctx, planpricesync.TerminatePlanPricesLineItemsV2Params{
		PlanID: planID,
		SubIDs: []string{"subs_a" + runID, "subs_b" + runID},
	})
	require.NoError(t, err)
	require.Equal(t, 3, n)

	endDates := map[string]*time.Time{}
	rows, err := db.QueryContext(ctx, `SELECT id, end_date FROM subscription_line_items WHERE environment_id = $1`, envID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id string
		var end *time.Time
		require.NoError(t, rows.Scan(&id, &end))
		endDates[id] = end
	}
	require.NoError(t, rows.Err())

	ended := func(id string) *time.Time { return endDates["subsli_"+id+runID] }
	require.Nil(t, ended("aold"), "price ended before subA's synced sequence is not re-applied")
	require.WithinDuration(t, newEnd, *ended("anew"), 0)
	require.Nil(t, ended("alive"))
	require.WithinDuration(t, oldEnd, *ended("bold"), 0)
	require.WithinDuration(t, newEnd, *ended("bnew"), 0)
	require.Nil(t, ended("blive"))
}
