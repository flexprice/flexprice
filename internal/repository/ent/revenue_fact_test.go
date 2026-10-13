package ent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRevenueFactRepository(t *testing.T) revenuefact.Repository {
	t.Helper()
	client := newRealPostgresTestClient(t)
	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	})
	require.NoError(t, err)
	return NewRevenueFactRepository(client, log)
}

func revenueFactTestContext(tenantID, environmentID string) context.Context {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)
	return ctx
}

func newTestRevenueFact(subscriptionID, priceID string, day time.Time, source types.RevenueSource) *revenuefact.RevenueFact {
	return &revenuefact.RevenueFact{
		ID:                types.GenerateUUIDWithPrefix("rf"),
		CustomerID:        "cust_" + subscriptionID,
		SubscriptionID:    subscriptionID,
		SubLineItemID:     lo.ToPtr("sli_" + priceID),
		PriceID:           &priceID,
		RevenueSource:     source,
		PeriodStart:       day,
		PeriodEnd:         day.AddDate(0, 0, 1),
		Day:               day,
		NetAmount:         decimal.NewFromInt(100),
		DecompositionMode: types.Marginal,
		Currency:          "usd",
		Status:            types.FactProvisional,
	}
}

func TestRevenueFactRepository_UpsertProvisionalIsIdempotentPerGrain(t *testing.T) {
	repo := newTestRevenueFactRepository(t)
	runID := types.GenerateUUID()
	ctx := revenueFactTestContext("tenant_"+runID, "env_"+runID)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	f1 := newTestRevenueFact("sub_"+runID, "price_"+runID, day, types.RevenueSourceUsage)
	require.NoError(t, repo.UpsertProvisional(ctx, []*revenuefact.RevenueFact{f1}))

	f2 := newTestRevenueFact("sub_"+runID, "price_"+runID, day, types.RevenueSourceUsage)
	require.NoError(t, repo.UpsertProvisional(ctx, []*revenuefact.RevenueFact{f2}))

	got, err := repo.ListBySubscriptionPeriod(ctx, "sub_"+runID, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1), types.FactProvisional)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.EqualValues(t, 2, got[0].Version)
	require.Equal(t, "tenant_"+runID, got[0].TenantID)
	require.Equal(t, "env_"+runID, got[0].EnvironmentID)
}

func TestRevenueFactRepository_FlipToFinal(t *testing.T) {
	repo := newTestRevenueFactRepository(t)
	runID := types.GenerateUUID()
	ctx := revenueFactTestContext("tenant_"+runID, "env_"+runID)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	f := newTestRevenueFact("sub_"+runID, "price_"+runID, day, types.RevenueSourceUsage)
	require.NoError(t, repo.UpsertProvisional(ctx, []*revenuefact.RevenueFact{f}))

	n, err := repo.FlipToFinal(ctx, "sub_"+runID, "price_"+runID, "sli_price_"+runID, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1), "inv_"+runID, "inv_li_"+runID)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	finalFacts, err := repo.ListBySubscriptionPeriod(ctx, "sub_"+runID, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1), types.FactFinal)
	require.NoError(t, err)
	require.Len(t, finalFacts, 1)
	require.NotNil(t, finalFacts[0].InvoiceID)
	require.Equal(t, "inv_"+runID, *finalFacts[0].InvoiceID)

	stillProvisional, err := repo.ListBySubscriptionPeriod(ctx, "sub_"+runID, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1), types.FactProvisional)
	require.NoError(t, err)
	require.Empty(t, stillProvisional)
}

func TestRevenueFactRepository_ScopedByTenantAndEnvironment(t *testing.T) {
	repo := newTestRevenueFactRepository(t)
	runID := types.GenerateUUID()
	day := time.Now().UTC().Truncate(24 * time.Hour)

	ctxTenant1 := revenueFactTestContext("tenant_a_"+runID, "env_"+runID)
	ctxTenant2 := revenueFactTestContext("tenant_b_"+runID, "env_"+runID)

	f := newTestRevenueFact("sub_"+runID, "price_"+runID, day, types.RevenueSourceUsage)
	require.NoError(t, repo.UpsertProvisional(ctxTenant1, []*revenuefact.RevenueFact{f}))

	// A different tenant querying the same subscription/day must see nothing.
	got, err := repo.ListBySubscriptionPeriod(ctxTenant2, "sub_"+runID, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1), types.FactProvisional)
	require.NoError(t, err)
	require.Empty(t, got)

	n, err := repo.FlipToFinal(ctxTenant2, "sub_"+runID, "price_"+runID, "sli_price_"+runID, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1), "inv_x", "inv_li_x")
	require.NoError(t, err)
	require.Equal(t, 0, n, "flipping from a different tenant must not affect another tenant's rows")
}

// nilClient builds a postgres.IClient whose backing ent clients are nil. It
// exercises the guards that reject before any DB access (importing testutil's
// MockPostgresClient here would form an import cycle through the ent package).
func nilClient(t *testing.T, log *logger.Logger) postgres.IClient {
	t.Helper()
	return postgres.NewClient(&postgres.EntClients{HasReader: false}, log, nil)
}

// TestRevenueFactRepository_UpsertProvisionalRejectsEmptyPriceID runs without a
// real Postgres: it uses a nil-backed client to prove the empty-price_id guard
// in UpsertProvisional runs BEFORE any DB access. If the guard were missing or
// placed after the Writer(ctx) call, this test would panic on a nil client
// instead of returning a validation error.
func TestRevenueFactRepository_UpsertProvisionalRejectsEmptyPriceID(t *testing.T) {
	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	})
	require.NoError(t, err)

	repo := NewRevenueFactRepository(nilClient(t, log), log)
	ctx := revenueFactTestContext("tenant_1", "env_1")
	day := time.Now().UTC().Truncate(24 * time.Hour)

	f := newTestRevenueFact("sub_1", "price_1", day, types.RevenueSourceUsage)
	f.PriceID = nil

	err = repo.UpsertProvisional(ctx, []*revenuefact.RevenueFact{f})
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestRevenueFactRepository_UpsertProvisionalRejectsBlankPriceID(t *testing.T) {
	log, err := logger.NewLogger(&config.Configuration{
		Logging: config.LoggingConfig{Level: types.LogLevelInfo},
	})
	require.NoError(t, err)

	repo := NewRevenueFactRepository(nilClient(t, log), log)
	ctx := revenueFactTestContext("tenant_1", "env_1")
	day := time.Now().UTC().Truncate(24 * time.Hour)

	blank := ""
	f := newTestRevenueFact("sub_1", "price_1", day, types.RevenueSourceUsage)
	f.PriceID = &blank

	err = repo.UpsertProvisional(ctx, []*revenuefact.RevenueFact{f})
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

// TestRevenueFactRepository_UpsertProvisionalChunksLargeBatches proves a batch
// whose bind-parameter count exceeds Postgres' 65535-per-statement cap still
// upserts completely: 2100 facts x 33 columns would be 69300 params unchunked.
func TestRevenueFactRepository_UpsertProvisionalChunksLargeBatches(t *testing.T) {
	repo := newTestRevenueFactRepository(t)
	runID := types.GenerateUUID()
	ctx := revenueFactTestContext("tenant_"+runID, "env_"+runID)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	const n = 2100
	facts := make([]*revenuefact.RevenueFact, 0, n)
	for i := 0; i < n; i++ {
		// Distinct price per row keeps every row on its own provisional grain.
		facts = append(facts, newTestRevenueFact("sub_"+runID, fmt.Sprintf("price_%s_%d", runID, i), day, types.RevenueSourceUsage))
	}
	require.NoError(t, repo.UpsertProvisional(ctx, facts))

	got, err := repo.ListBySubscriptionPeriod(ctx, "sub_"+runID, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1), types.FactProvisional)
	require.NoError(t, err)
	require.Len(t, got, n)
}

// Two line items can share one price on one day (e.g. a mid-period price
// version change) — sub_line_item_id keeps them on separate grains.
func TestRevenueFactRepository_TwoLineItemsSharePriceAndDay(t *testing.T) {
	repo := newTestRevenueFactRepository(t)
	runID := types.GenerateUUID()
	ctx := revenueFactTestContext("tenant_"+runID, "env_"+runID)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	a := newTestRevenueFact("sub_"+runID, "price_"+runID, day, types.RevenueSourceFixed)
	a.SubLineItemID = lo.ToPtr("sli_a_" + runID)
	b := newTestRevenueFact("sub_"+runID, "price_"+runID, day, types.RevenueSourceFixed)
	b.SubLineItemID = lo.ToPtr("sli_b_" + runID)

	require.NoError(t, repo.UpsertProvisional(ctx, []*revenuefact.RevenueFact{a, b}))

	got, err := repo.ListBySubscriptionPeriod(ctx, "sub_"+runID, day.AddDate(0, 0, -1), day.AddDate(0, 0, 1), types.FactProvisional)
	require.NoError(t, err)
	require.Len(t, got, 2, "distinct line items must keep distinct rows")
}

// A flip for one line item must never stamp a sibling line item's rows, even
// when both share the same price and day.
func TestRevenueFactRepository_FlipStampsOnlyItsOwnLineItem(t *testing.T) {
	repo := newTestRevenueFactRepository(t)
	runID := types.GenerateUUID()
	ctx := revenueFactTestContext("tenant_"+runID, "env_"+runID)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	a := newTestRevenueFact("sub_"+runID, "price_"+runID, day, types.RevenueSourceFixed)
	a.SubLineItemID = lo.ToPtr("sli_a_" + runID)
	b := newTestRevenueFact("sub_"+runID, "price_"+runID, day, types.RevenueSourceFixed)
	b.SubLineItemID = lo.ToPtr("sli_b_" + runID)
	require.NoError(t, repo.UpsertProvisional(ctx, []*revenuefact.RevenueFact{a, b}))

	n, err := repo.FlipToFinal(ctx, "sub_"+runID, "price_"+runID, "sli_a_"+runID, day, day, "inv_"+runID, "inv_li_a_"+runID)
	require.NoError(t, err)
	require.Equal(t, 1, n, "only line item A's row may flip")

	final, err := repo.ListBySubscriptionPeriod(ctx, "sub_"+runID, day, day, types.FactFinal)
	require.NoError(t, err)
	require.Len(t, final, 1)
	require.Equal(t, "sli_a_"+runID, lo.FromPtr(final[0].SubLineItemID))
	require.Equal(t, "inv_li_a_"+runID, lo.FromPtr(final[0].InvoiceLineItemID))
}
