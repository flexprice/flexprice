package selfbilling

import (
	"context"
	"fmt"
	"testing"
	"time"

	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 14, 10, 12, 0, 0, time.UTC)

func TestUsageWindow(t *testing.T) {
	tests := []struct {
		name     string
		lookback time.Duration
		wantFrom time.Time
	}{
		{"3h", 3 * time.Hour, time.Date(2026, 10, 14, 7, 0, 0, 0, time.UTC)},
		{"12h", 12 * time.Hour, time.Date(2026, 10, 13, 22, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to := usageWindow(now, tt.lookback)
			assert.Equal(t, time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC), to, "the open hour 10:00-11:00 is never sent")
			assert.Equal(t, tt.wantFrom, from)
		})
	}
}

func TestPushUsage_BuildsDeterministicEvents(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	hour := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	ps, pe := hour.AddDate(0, -1, 0), hour
	d.store.AddCount(&domainsb.TenantEnvHourCount{TenantID: "t1", EnvironmentID: "env_1", Hour: hour, Count: 42})
	d.store.AddInvoice(&domainsb.FinalizedInvoice{
		ID: "inv_1", TenantID: "t1", EnvironmentID: "env_1", SubscriptionID: "sub_9",
		InvoiceType: "SUBSCRIPTION", BillingReason: "SUBSCRIPTION_CYCLE", Currency: "inr",
		Subtotal: decimal.RequireFromString("1200"), TotalDiscount: decimal.RequireFromString("100"),
		TotalPrepaidCreditsApplied: decimal.RequireFromString("150"), TotalTax: decimal.RequireFromString("50.50"),
		Total: decimal.RequireFromString("1000.50"), AmountDue: decimal.RequireFromString("1000.50"), AmountPaid: decimal.Zero,
		PeriodStart: &ps, PeriodEnd: &pe, FinalizedAt: hour.Add(5 * time.Minute),
	})

	require.NoError(t, s.pushUsage(context.Background(), now))
	require.Len(t, fc.ingested, 1)
	evs := fc.ingested[0]
	require.Len(t, evs, 2)

	assert.Equal(t, "evt:us:t1:env_1:2026101408", evs[0].id)
	assert.Equal(t, "events_ingested", evs[0].name)
	assert.Equal(t, "us:t1", evs[0].extID)
	assert.Equal(t, hour, evs[0].timestamp, "timestamp is the hour start so re-sends dedupe on (timestamp, id)")
	assert.Equal(t, map[string]string{"count": "42", "environment_id": "env_1", "region": "us"}, evs[0].properties)

	assert.Equal(t, "inv_1", evs[1].id)
	assert.Equal(t, "invoice_value", evs[1].name)
	assert.Equal(t, hour.Add(5*time.Minute), evs[1].timestamp)
	assert.Equal(t, map[string]string{
		"amount": "1000.5", "total": "1000.5", "subtotal": "1200", "total_discount": "100",
		"total_prepaid_credits_applied": "150", "total_tax": "50.5", "amount_due": "1000.5", "amount_paid": "0",
		"currency": "INR", "environment_id": "env_1", "invoice_type": "SUBSCRIPTION", "billing_reason": "SUBSCRIPTION_CYCLE",
		"subscription_id": "sub_9", "period_start": ps.Format(time.RFC3339), "period_end": pe.Format(time.RFC3339),
	}, evs[1].properties)
}

func TestPushUsage_OneEventPerTenantEnvHour(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	h := now.Truncate(time.Hour).Add(-time.Hour)
	d.store.AddCount(&domainsb.TenantEnvHourCount{TenantID: "t1", EnvironmentID: "env_sandbox", Hour: h, Count: 5})
	d.store.AddCount(&domainsb.TenantEnvHourCount{TenantID: "t1", EnvironmentID: "env_prod", Hour: h, Count: 7})
	require.NoError(t, s.pushUsage(context.Background(), now))
	ids := lo.Map(fc.ingested[0], func(e usageEvent, _ int) string { return e.id })
	assert.ElementsMatch(t, []string{"evt:us:t1:env_sandbox:2026101409", "evt:us:t1:env_prod:2026101409"}, ids)
}

func TestPushUsage_RerunSendsSameIDsAndTimestamps(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	d.store.AddCount(&domainsb.TenantEnvHourCount{TenantID: "t1", EnvironmentID: "e1", Hour: now.Truncate(time.Hour).Add(-time.Hour), Count: 1})
	require.NoError(t, s.pushUsage(context.Background(), now))
	require.NoError(t, s.pushUsage(context.Background(), now.Add(time.Hour)))
	assert.Equal(t, fc.ingested[0][0].id, fc.ingested[1][0].id)
	assert.Equal(t, fc.ingested[0][0].timestamp, fc.ingested[1][0].timestamp)
}

func TestPushUsage_HonoursLookback(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	s.Config.Billing.UsageLookbackHours = 2
	base := now.Truncate(time.Hour)
	d.store.AddCount(&domainsb.TenantEnvHourCount{TenantID: "t1", EnvironmentID: "e1", Hour: base.Add(-2 * time.Hour), Count: 1}) // in
	d.store.AddCount(&domainsb.TenantEnvHourCount{TenantID: "t1", EnvironmentID: "e1", Hour: base.Add(-3 * time.Hour), Count: 1}) // out
	d.store.AddCount(&domainsb.TenantEnvHourCount{TenantID: "t1", EnvironmentID: "e1", Hour: base, Count: 1})                     // open hour, out
	require.NoError(t, s.pushUsage(context.Background(), now))
	require.Len(t, fc.ingested[0], 1)
	assert.Equal(t, base.Add(-2*time.Hour), fc.ingested[0][0].timestamp)
}

// A re-sent event lands in a later ingest hour and is counted there too: an accepted v1 overcount.
func TestPushUsage_ReingestedDuplicateCountsInLaterHour(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	base := now.Truncate(time.Hour)
	d.store.AddCount(&domainsb.TenantEnvHourCount{TenantID: "t1", EnvironmentID: "e1", Hour: base.Add(-2 * time.Hour), Count: 1})
	d.store.AddCount(&domainsb.TenantEnvHourCount{TenantID: "t1", EnvironmentID: "e1", Hour: base, Count: 1}) // same event id re-sent in the then-open hour

	require.NoError(t, s.pushUsage(context.Background(), now))
	require.NoError(t, s.pushUsage(context.Background(), now.Add(time.Hour)))
	require.Len(t, fc.ingested, 2)

	first := lo.Map(fc.ingested[0], func(e usageEvent, _ int) string { return e.id })
	assert.Equal(t, []string{"evt:us:t1:e1:2026101408"}, first, "the open hour is held back on the first run")

	second := lo.Map(fc.ingested[1], func(e usageEvent, _ int) string { return e.id })
	assert.ElementsMatch(t, []string{"evt:us:t1:e1:2026101408", "evt:us:t1:e1:2026101410"}, second)
	for _, e := range fc.ingested[1] {
		assert.Equal(t, "1", e.properties["count"])
	}
}

func TestPushUsage_ChunksAt1000(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	for i := 0; i < 2500; i++ {
		d.store.AddCount(&domainsb.TenantEnvHourCount{TenantID: fmt.Sprintf("t%d", i), EnvironmentID: "e1", Hour: now.Truncate(time.Hour).Add(-time.Hour), Count: 1})
	}
	require.NoError(t, s.pushUsage(context.Background(), now))
	require.Len(t, fc.ingested, 3)
	assert.Len(t, fc.ingested[0], 1000)
	assert.Len(t, fc.ingested[2], 500)
}

func TestPushUsage_SkipsBillingTenant(t *testing.T) {
	fc := newFakeClient()
	s, d := newTestService(t, fc)
	s.Config.Billing.TenantID = "t_billing"
	h := now.Truncate(time.Hour).Add(-time.Hour)
	d.store.AddCount(&domainsb.TenantEnvHourCount{TenantID: "t_billing", EnvironmentID: "e1", Hour: h, Count: 9})
	d.store.AddInvoice(&domainsb.FinalizedInvoice{ID: "inv_b", TenantID: "t_billing", Total: decimal.NewFromInt(5), Currency: "usd", FinalizedAt: h})
	d.store.AddCount(&domainsb.TenantEnvHourCount{TenantID: "t1", EnvironmentID: "e1", Hour: h, Count: 1})

	require.NoError(t, s.pushUsage(context.Background(), now))
	require.Len(t, fc.ingested, 1)
	require.Len(t, fc.ingested[0], 1)
	assert.Equal(t, "us:t1", fc.ingested[0][0].extID)
}

func TestPushUsage_NothingToSend(t *testing.T) {
	fc := newFakeClient()
	s, _ := newTestService(t, fc)
	require.NoError(t, s.pushUsage(context.Background(), now))
	assert.Empty(t, fc.ingested)
}
