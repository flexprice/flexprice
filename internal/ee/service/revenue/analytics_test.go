package revenue

import (
	"context"
	"fmt"
	"github.com/flexprice/flexprice/internal/ee/service"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// revenueAnalyticsFixture seeds one month of facts covering every shape the
// API must aggregate: daily marginal usage, a whole-period fixed fee, a
// commitment true-up, and a revert pair from a voided invoice.
func revenueAnalyticsFixture(t *testing.T) (context.Context, Service, *testutil.InMemoryRevenueFactStore, revenuePeriod) {
	t.Helper()
	ctx := types.SetTenantID(context.Background(), "tenant_ra")
	ctx = types.SetEnvironmentID(ctx, "env_ra")

	store := testutil.NewInMemoryRevenueFactStore()
	settingsStore := testutil.NewInMemorySettingsStore()
	require.NoError(t, settingsStore.Create(ctx, &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix("setting"),
		Key:           types.SettingKeyRevenueAnalyticsConfig,
		Value:         map[string]interface{}{"enabled": true},
		EnvironmentID: "env_ra",
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))

	periodStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	period := revenuePeriod{Start: periodStart, End: periodStart.AddDate(0, 0, 29)}

	mk := func(id string, source types.RevenueSource, day time.Time, net int64, mode types.DecompositionMode) *revenuefact.RevenueFact {
		return &revenuefact.RevenueFact{
			ID: id, CustomerID: "cust_ra", SubscriptionID: "sub_ra",
			SubLineItemID: lo.ToPtr("sli_" + id), PriceID: lo.ToPtr("price_" + id),
			RevenueSource: source, PeriodStart: period.Start, PeriodEnd: period.End, Day: day,
			NetAmount: decimal.NewFromInt(net), DecompositionMode: mode,
			Currency: "usd", Status: types.FactProvisional,
		}
	}

	var facts []*revenuefact.RevenueFact
	// 30 marginal usage days at $10/day.
	for d := 0; d < 30; d++ {
		facts = append(facts, mk(fmt.Sprintf("u%02d", d), types.RevenueSourceUsage, period.Start.AddDate(0, 0, d), 10, types.Marginal))
	}
	// $30 fixed fee booked whole on period start; $100 true-up on period end.
	facts = append(facts,
		mk("fixed", types.RevenueSourceFixed, period.Start, 30, types.PeriodOnly),
		mk("trueup", types.RevenueSourceCommitmentTrueup, period.End, 100, types.PeriodOnly),
	)
	require.NoError(t, store.UpsertProvisional(ctx, facts))

	svc := New(service.ServiceParams{
		Logger:          logger.NewNoopLogger(),
		RevenueFactRepo: store,
		SettingsRepo:    settingsStore,
	})
	return ctx, svc, store, period
}

func revenueAnalyticsRequest(period revenuePeriod) *dto.RevenueAnalyticsRequest {
	return &dto.RevenueAnalyticsRequest{
		StartTime: period.Start,
		EndTime:   period.exclusiveEnd(),
		Status:    types.FactProvisional,
	}
}

// TestGetRevenueAnalytics_SourceGrouping: default folds true-up into usage;
// include_adjustments breaks it out as a labeled row. Totals identical.
func TestGetRevenueAnalytics_SourceGrouping(t *testing.T) {
	ctx, svc, _, period := revenueAnalyticsFixture(t)

	req := revenueAnalyticsRequest(period)
	req.Granularity = types.RevenueGranularityTotal
	req.GroupBy = []string{"revenue_source"}

	res, err := svc.GetRevenueAnalytics(ctx, req)
	require.NoError(t, err)
	require.Len(t, res.Rows, 2, "default view shows plain usage/fixed only")
	bySource := map[string]decimal.Decimal{}
	for _, r := range res.Rows {
		assert.Empty(t, r.AdjustmentType)
		bySource[r.Group["revenue_source"]] = r.NetAmount
	}
	assert.Equal(t, "400", bySource["usage"].String(), "usage absorbs the true-up by default (300 + 100)")
	assert.Equal(t, "30", bySource["fixed"].String())

	req.IncludeAdjustments = true
	res, err = svc.GetRevenueAnalytics(ctx, req)
	require.NoError(t, err)
	require.Len(t, res.Rows, 3, "breakout adds the true-up as its own row")
	total := decimal.Zero
	adjustments := 0
	for _, r := range res.Rows {
		total = total.Add(r.NetAmount)
		if r.AdjustmentType != "" {
			adjustments++
			assert.Equal(t, "commitment_trueup", r.AdjustmentType)
			assert.Equal(t, "commitment_trueup", r.Group["revenue_source"])
			assert.Equal(t, "100", r.NetAmount.String())
		}
	}
	assert.Equal(t, 1, adjustments)
	assert.Equal(t, "430", total.String(), "totals never change with the breakout")
}

// TestGetRevenueAnalytics_AllocationPolicies: billed keeps the fixed fee as a
// spike on its booked day; amortized spreads it across the period. Totals equal.
func TestGetRevenueAnalytics_AllocationPolicies(t *testing.T) {
	ctx, svc, _, period := revenueAnalyticsFixture(t)

	req := revenueAnalyticsRequest(period)
	req.Granularity = types.RevenueGranularityDay

	res, err := svc.GetRevenueAnalytics(ctx, req)
	require.NoError(t, err)
	assert.True(t, res.ContainsAllocated, "day view carries period-shaped charges")
	// revenue_source is always a dimension, so a day can carry several rows.
	byDay := map[string]decimal.Decimal{}
	total := decimal.Zero
	for _, r := range res.Rows {
		key := r.Day.Format("2006-01-02")
		byDay[key] = byDay[key].Add(r.NetAmount)
		total = total.Add(r.NetAmount)
	}
	assert.Equal(t, "40", byDay[period.Start.Format("2006-01-02")].String(), "billed: day 1 = $10 usage + $30 fixed spike")
	assert.Equal(t, "430", total.String())

	req.AllocationPolicy = types.RevenueAllocationAmortized
	res, err = svc.GetRevenueAnalytics(ctx, req)
	require.NoError(t, err)
	amortizedTotal := decimal.Zero
	for _, r := range res.Rows {
		amortizedTotal = amortizedTotal.Add(r.NetAmount)
	}
	assert.True(t, amortizedTotal.Equal(total), "amortized must preserve the total exactly")
	firstDay := decimal.Zero
	for _, r := range res.Rows {
		if r.Day.Equal(period.Start) {
			firstDay = firstDay.Add(r.NetAmount)
		}
	}
	// Day 1 = $10 usage + $30/30 fixed + $100/30 true-up = 14.3333…
	expectedFirstDay := decimal.RequireFromString("14.333333333333333")
	assert.True(t, firstDay.Sub(expectedFirstDay).Abs().LessThan(decimal.RequireFromString("0.0001")),
		"amortized day 1 must spread fixed and true-up, got %s", firstDay)
}

// TestGetRevenueAnalytics_PeriodGranularity: period buckets are exact — every
// row shape aggregates back to its own billing period, never a neighbor's.
func TestGetRevenueAnalytics_PeriodGranularity(t *testing.T) {
	ctx, svc, store, period := revenueAnalyticsFixture(t)

	// Second billing period right after the first: 10 marginal usage days at
	// $5/day plus a $25 whole-period fixed fee.
	next := revenuePeriod{Start: period.exclusiveEnd(), End: period.exclusiveEnd().AddDate(0, 0, 9)}
	var facts []*revenuefact.RevenueFact
	for d := 0; d < 10; d++ {
		facts = append(facts, &revenuefact.RevenueFact{
			ID: fmt.Sprintf("p2u%02d", d), CustomerID: "cust_ra", SubscriptionID: "sub_ra",
			SubLineItemID: lo.ToPtr(fmt.Sprintf("sli_p2u%02d", d)), PriceID: lo.ToPtr(fmt.Sprintf("price_p2u%02d", d)),
			RevenueSource: types.RevenueSourceUsage, PeriodStart: next.Start, PeriodEnd: next.End,
			Day: next.Start.AddDate(0, 0, d), NetAmount: decimal.NewFromInt(5),
			DecompositionMode: types.Marginal, Currency: "usd", Status: types.FactProvisional,
		})
	}
	facts = append(facts, &revenuefact.RevenueFact{
		ID: "p2fixed", CustomerID: "cust_ra", SubscriptionID: "sub_ra",
		SubLineItemID: lo.ToPtr("sli_p2fixed"), PriceID: lo.ToPtr("price_p2fixed"),
		RevenueSource: types.RevenueSourceFixed, PeriodStart: next.Start, PeriodEnd: next.End,
		Day: next.Start, NetAmount: decimal.NewFromInt(25),
		DecompositionMode: types.PeriodOnly, Currency: "usd", Status: types.FactProvisional,
	})
	require.NoError(t, store.UpsertProvisional(ctx, facts))

	req := revenueAnalyticsRequest(period)
	req.EndTime = next.exclusiveEnd()
	req.Granularity = types.RevenueGranularityPeriod

	res, err := svc.GetRevenueAnalytics(ctx, req)
	require.NoError(t, err)
	require.Len(t, res.Rows, 4, "each period splits into its usage and fixed rows")
	byStart := map[string]decimal.Decimal{}
	for _, r := range res.Rows {
		require.NotNil(t, r.PeriodStart)
		require.NotNil(t, r.PeriodEnd)
		byStart[r.PeriodStart.Format("2006-01-02")] = byStart[r.PeriodStart.Format("2006-01-02")].Add(r.NetAmount)
		if r.PeriodStart.Equal(period.Start) {
			assert.True(t, r.PeriodEnd.Equal(period.End))
		} else {
			assert.True(t, r.PeriodEnd.Equal(next.End))
		}
	}
	assert.Equal(t, "430", byStart[period.Start.Format("2006-01-02")].String())
	assert.Equal(t, "75", byStart[next.Start.Format("2006-01-02")].String(), "second period = 10×$5 usage + $25 fixed")
	assert.False(t, res.ContainsAllocated, "period view allocates nothing")
}

// TestGetRevenueAnalytics_DeniedWithoutOptIn: the surface is gated on the
// tenant setting, like the export.
func TestGetRevenueAnalytics_DeniedWithoutOptIn(t *testing.T) {
	ctx, _, _, period := revenueAnalyticsFixture(t)
	_ = ctx

	deniedCtx := types.SetTenantID(context.Background(), "tenant_other")
	deniedCtx = types.SetEnvironmentID(deniedCtx, "env_other")
	svc := New(service.ServiceParams{
		Logger:          logger.NewNoopLogger(),
		RevenueFactRepo: testutil.NewInMemoryRevenueFactStore(),
		SettingsRepo:    testutil.NewInMemorySettingsStore(),
	})
	_, err := svc.GetRevenueAnalytics(deniedCtx, revenueAnalyticsRequest(period))
	require.Error(t, err)
}

// TestGetRevenueAnalytics_DropsEmptyBuckets: a line item rolled over a day it
// saw nothing (before a commitment is crossed, or an idle meter) books a row
// of zeros — noise in a response. A day of fully entitled usage nets zero too
// but is NOT empty: its quantity columns say what the entitlement absorbed.
func TestGetRevenueAnalytics_DropsEmptyBuckets(t *testing.T) {
	ctx, svc, store, period := revenueAnalyticsFixture(t)

	mk := func(id string, src types.RevenueSource, net, entQty, entAmt int64) *revenuefact.RevenueFact {
		return &revenuefact.RevenueFact{
			ID: id, CustomerID: "cust_ra", SubscriptionID: "sub_ra",
			SubLineItemID: lo.ToPtr("sli_" + id), PriceID: lo.ToPtr("price_" + id),
			RevenueSource: src, PeriodStart: period.Start, PeriodEnd: period.End,
			Day: period.Start, NetAmount: decimal.NewFromInt(net),
			EntitlementQty: decimal.NewFromInt(entQty), EntitlementAmount: decimal.NewFromInt(entAmt),
			DecompositionMode: types.Marginal, Currency: "usd", Status: types.FactProvisional,
		}
	}
	require.NoError(t, store.UpsertProvisional(ctx, []*revenuefact.RevenueFact{
		mk("zero_overage", types.RevenueSourceOverage, 0, 0, 0),
		mk("entitled", types.RevenueSourceUsage, 0, 40, 4),
	}))

	req := revenueAnalyticsRequest(period)
	req.Granularity = types.RevenueGranularityTotal
	req.GroupBy = []string{"price_id"}
	req.IncludeAdjustments = true

	res, err := svc.GetRevenueAnalytics(ctx, req)
	require.NoError(t, err)

	byPrice := map[string]*dto.RevenueAnalyticsRow{}
	for _, r := range res.Rows {
		byPrice[r.Group["price_id"]] = r
	}
	assert.NotContains(t, byPrice, "price_zero_overage", "a bucket with nothing in it is dropped")
	entitled := byPrice["price_entitled"]
	require.NotNil(t, entitled, "fully entitled usage must survive: it reports what was absorbed")
	assert.True(t, entitled.NetAmount.IsZero())
	assert.Equal(t, "40", entitled.EntitlementQty.String())
}

// TestGetRevenueAnalytics_StatusAndCurrencyAreRowIdentity: an omitted status
// returns booked and in-progress rows side by side, each labelled, rather than
// defaulting to one and silently hiding the other. Currency is always a
// dimension too, because adding amounts across currencies is not a number.
func TestGetRevenueAnalytics_StatusAndCurrencyAreRowIdentity(t *testing.T) {
	ctx, svc, store, period := revenueAnalyticsFixture(t)

	mk := func(id string, status types.FactStatus, currency string, net int64) *revenuefact.RevenueFact {
		return &revenuefact.RevenueFact{
			ID: id, CustomerID: "cust_ra", SubscriptionID: "sub_ra",
			SubLineItemID: lo.ToPtr("sli_" + id), PriceID: lo.ToPtr("price_" + id),
			RevenueSource: types.RevenueSourceUsage, PeriodStart: period.Start, PeriodEnd: period.End,
			Day: period.Start, NetAmount: decimal.NewFromInt(net),
			DecompositionMode: types.Marginal, Currency: currency, Status: status,
		}
	}
	require.NoError(t, store.UpsertProvisional(ctx, []*revenuefact.RevenueFact{
		mk("booked", types.FactProvisional, "usd", 70),
		mk("eur", types.FactProvisional, "eur", 11),
	}))
	// The store stamps PROVISIONAL like Postgres does, so book one row the
	// way finalization does.
	flipped, err := store.FlipToFinal(ctx, "sub_ra", "price_booked", "sli_booked",
		period.Start, period.End, "inv_ra", "ili_ra")
	require.NoError(t, err)
	require.Equal(t, 1, flipped)

	req := &dto.RevenueAnalyticsRequest{
		StartTime:   period.Start,
		EndTime:     period.exclusiveEnd(),
		Granularity: types.RevenueGranularityTotal,
	}
	res, err := svc.GetRevenueAnalytics(ctx, req)
	require.NoError(t, err)

	byStatusCurrency := map[string]decimal.Decimal{}
	for _, r := range res.Rows {
		assert.NotEmpty(t, r.Status, "every row says which status it is")
		key := string(r.Status) + "/" + r.Group["currency"]
		byStatusCurrency[key] = byStatusCurrency[key].Add(r.NetAmount)
	}
	assert.Equal(t, "70", byStatusCurrency["FINAL/usd"].String(), "booked rows come back unasked")
	assert.Equal(t, "11", byStatusCurrency["PROVISIONAL/eur"].String(), "a second currency never merges into the first")
	assert.True(t, byStatusCurrency["PROVISIONAL/usd"].IsPositive(), "the in-progress period is still there")

	// Naming a status narrows to it.
	req.Status = types.FactFinal
	res, err = svc.GetRevenueAnalytics(ctx, req)
	require.NoError(t, err)
	require.NotEmpty(t, res.Rows)
	for _, r := range res.Rows {
		assert.Equal(t, types.FactFinal, r.Status)
	}
	assert.Equal(t, types.FactFinal, res.Query.Status, "the echo reports what was applied")
}
