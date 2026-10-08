package export

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedRevenueFact writes one provisional fact with a fixed ComputedAt.
func seedRevenueFact(t *testing.T, ctx context.Context, store *testutil.InMemoryRevenueFactStore, id string, computedAt time.Time) {
	t.Helper()
	day := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	require.NoError(t, store.UpsertProvisional(ctx, []*revenuefact.RevenueFact{{
		ID:                id,
		CustomerID:        "cust_1",
		SubscriptionID:    "sub_1",
		SubLineItemID:     lo.ToPtr("sli_" + id),
		PriceID:           lo.ToPtr("price_" + id),
		RevenueSource:     types.RevenueSourceFixed,
		PeriodStart:       day,
		PeriodEnd:         day,
		Day:               day,
		NetAmount:         decimal.NewFromInt(30),
		DecompositionMode: types.PeriodOnly,
		Currency:          "usd",
		Status:            types.FactProvisional,
		ComputedAt:        computedAt,
	}}))
}

// TestRevenueFactsExporter_WindowAndCSV: only rows recomputed inside the task
// window export, and the CSV carries the documented columns.
func TestRevenueFactsExporter_WindowAndCSV(t *testing.T) {
	ctx := types.SetTenantID(context.Background(), "tenant_1")
	ctx = types.SetEnvironmentID(ctx, "env_1")
	store := testutil.NewInMemoryRevenueFactStore()

	windowStart := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	windowEnd := windowStart.AddDate(0, 0, 1)
	seedRevenueFact(t, ctx, store, "rf_before", windowStart.Add(-time.Hour))
	seedRevenueFact(t, ctx, store, "rf_inside", windowStart.Add(6*time.Hour))
	seedRevenueFact(t, ctx, store, "rf_after", windowEnd.Add(time.Hour))

	exporter := NewRevenueFactsExporter(store, logger.NewNoopLogger())
	data, count, err := exporter.PrepareData(ctx, &dto.ExportRequest{
		EntityType: types.ScheduledTaskEntityTypeRevenueFacts,
		TenantID:   "tenant_1",
		EnvID:      "env_1",
		StartTime:  windowStart,
		EndTime:    windowEnd,
	})
	require.NoError(t, err)
	require.Equal(t, 1, count, "only the row recomputed inside the window exports")

	csv := string(data)
	assert.Contains(t, csv, "rf_inside")
	assert.NotContains(t, csv, "rf_before")
	assert.NotContains(t, csv, "rf_after")

	header := strings.SplitN(csv, "\n", 2)[0]
	for _, col := range []string{"id", "net_amount", "revenue_source", "status", "is_revert", "computed_at", "version"} {
		assert.Contains(t, header, col, "documented column %q must be in the CSV header", col)
	}
}

// TestRevenueFactsExporter_PagesPastBatchSize: more rows than one page still
// export exactly once each.
func TestRevenueFactsExporter_PagesPastBatchSize(t *testing.T) {
	ctx := types.SetTenantID(context.Background(), "tenant_1")
	ctx = types.SetEnvironmentID(ctx, "env_1")
	store := testutil.NewInMemoryRevenueFactStore()

	windowStart := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	const rows = 1005 // one full page + a remainder
	for i := 0; i < rows; i++ {
		seedRevenueFact(t, ctx, store, fmt.Sprintf("rf_%04d", i), windowStart.Add(time.Duration(i)*time.Second))
	}

	exporter := NewRevenueFactsExporter(store, logger.NewNoopLogger())
	_, count, err := exporter.PrepareData(ctx, &dto.ExportRequest{
		TenantID:  "tenant_1",
		EnvID:     "env_1",
		StartTime: windowStart.Add(-time.Minute),
		EndTime:   windowStart.Add(time.Hour),
	})
	require.NoError(t, err)
	assert.Equal(t, rows, count, "every row must export exactly once across pages")
}
