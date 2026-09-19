package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// curveTestOpts is the config assembled by the curveXxx functional options
// below, consumed by buildTestCurveInput.
type curveTestOpts struct {
	perDay           decimal.Decimal
	entitlementLimit decimal.Decimal
	price            *price.Price
	days             int
}

type curveTestOpt func(*curveTestOpts)

// curvePerDay sets the flat quantity of usage fired on each day of the period.
func curvePerDay(qty int64) curveTestOpt {
	return func(o *curveTestOpts) { o.perDay = decimal.NewFromInt(qty) }
}

// curveEntitlementLimit sets the entitlement quantity consumed earliest-first.
func curveEntitlementLimit(qty int64) curveTestOpt {
	return func(o *curveTestOpts) { o.entitlementLimit = decimal.NewFromInt(qty) }
}

// curveFlatRate builds a USAGE, FLAT_FEE price priced at unitAmount per unit.
func curveFlatRate(unitAmount string) curveTestOpt {
	return func(o *curveTestOpts) {
		o.price = &price.Price{
			ID:           "price_curve_flat_test",
			Amount:       decimal.RequireFromString(unitAmount),
			Currency:     "usd",
			Type:         types.PRICE_TYPE_USAGE,
			BillingModel: types.BILLING_MODEL_FLAT_FEE,
		}
	}
}

// curveDays sets how many days of usage to fire and the period length.
func curveDays(n int) curveTestOpt {
	return func(o *curveTestOpts) { o.days = n }
}

// curvePrice sets an arbitrary price fixture directly (e.g. a graduated/SLAB
// price built inline by the caller, rather than via curveFlatRate).
func curvePrice(p *price.Price) curveTestOpt {
	return func(o *curveTestOpts) { o.price = p }
}

// buildTestCurveInput seeds an in-memory meter-usage store with `days` daily
// records of `perDay` quantity, and returns the usageCurveInput for the
// resulting period (BuildUsageCurve reads usage back out via
// GetCumulativeDailyUsage, exactly like the real ClickHouse-backed path).
func buildTestCurveInput(t *testing.T, ctx context.Context, store *testutil.InMemoryMeterUsageStore, opts ...curveTestOpt) usageCurveInput {
	t.Helper()

	cfg := curveTestOpts{days: 1}
	for _, opt := range opts {
		opt(&cfg)
	}
	require.NotNil(t, cfg.price, "a price option (e.g. curveFlatRate) is required")

	const meterID = "meter_curve_test"
	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	records := make([]*events.MeterUsage, 0, cfg.days)
	for d := 0; d < cfg.days; d++ {
		ts := periodStart.AddDate(0, 0, d).Add(time.Hour)
		id := types.GenerateUUIDWithPrefix("mu_curve")
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID:            id,
				TenantID:      types.GetTenantID(ctx),
				EnvironmentID: types.GetEnvironmentID(ctx),
				EventName:     "curve_test_event",
				Timestamp:     ts,
				IngestedAt:    ts,
				Properties:    map[string]interface{}{},
			},
			MeterID:    meterID,
			QtyTotal:   cfg.perDay,
			UniqueHash: id,
		})
	}
	require.NoError(t, store.BulkInsertMeterUsage(ctx, records))

	return usageCurveInput{
		Price:            cfg.price,
		MeterID:          meterID,
		PeriodStart:      periodStart,
		PeriodEnd:        periodStart.AddDate(0, 0, cfg.days),
		EntitlementLimit: cfg.entitlementLimit,
	}
}

// curveMarginal returns the per-day delta at slice index idx: got[idx] minus
// got[idx-1] (or got[0] alone when idx==0). Note idx is a slice position, not
// a 1-based day number — index 10 is the 11th day (Day fields are 0-indexed
// from period start).
func curveMarginal(got []dayCharge, idx int) decimal.Decimal {
	if idx == 0 {
		return got[0].CumulativeCharge
	}
	return got[idx].CumulativeCharge.Sub(got[idx-1].CumulativeCharge)
}

// TestBuildUsageCurve_EntitlementLimitThenFlatRate covers the primary shape:
// 2000/day usage, a 20000 entitlementLimit, a flat $0.01/unit price, over 30 days.
// Days 1-10 stay fully inside the entitlementLimit (charge == 0); day 11 is the
// first day billed usage crosses the entitlementLimit, so its marginal charge is
// 2000 * $0.01 = $20; by day 30, 20 billed days * $20 = $400 cumulative.
func TestBuildUsageCurve_EntitlementLimitThenFlatRate(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewInMemoryMeterUsageStore()
	svc := &revenueService{ServiceParams: ServiceParams{
		Logger:         logger.NewNoopLogger(),
		MeterUsageRepo: store,
		PriceRepo:      testutil.NewInMemoryPriceStore(),
		MeterRepo:      testutil.NewInMemoryMeterStore(),
		PlanRepo:       testutil.NewInMemoryPlanStore(),
		PriceUnitRepo:  testutil.NewInMemoryPriceUnitStore(),
		AddonRepo:      testutil.NewInMemoryAddonStore(),
		SubRepo:        testutil.NewInMemorySubscriptionStore(),
	}}

	curve := buildTestCurveInput(t, ctx, store,
		curvePerDay(2000),
		curveEntitlementLimit(20000),
		curveFlatRate("0.01"),
		curveDays(30),
	)

	got, err := svc.buildUsageCurve(ctx, curve)
	require.NoError(t, err)
	require.Len(t, got, 30)

	// (a) days fully inside the entitlementLimit have zero cumulative charge.
	assert.True(t, got[9].CumulativeCharge.IsZero(), "day 10 still within entitlementLimit")

	// (b) after the entitlementLimit is exhausted, charge rises at the flat rate.
	assert.Equal(t, "20", curveMarginal(got, 10).String(), "day 11 marginal charge")

	// (c) marginal deltas sum to the period charge (20 billed days * $20).
	var marginalSum decimal.Decimal
	for i := range got {
		marginalSum = marginalSum.Add(curveMarginal(got, i))
	}
	assert.Equal(t, "400", got[29].CumulativeCharge.String(), "period cumulative charge")
	assert.True(t, marginalSum.Equal(got[29].CumulativeCharge), "marginal sum must equal period charge")

	// (d) entitlement is consumed earliest-first as a function of gross usage.
	assert.Equal(t, "20000", got[9].CumulativeEntitlementQty.String(), "entitlementLimit fully consumed by day 10")
	assert.Equal(t, "20000", got[29].CumulativeEntitlementQty.String(), "entitlement consumption caps at the entitlementLimit")
	assert.Equal(t, "40000", got[29].CumulativeBillableQty.String())
	assert.Equal(t, "60000", got[29].CumulativeGrossQty.String())

	// tier1_rate is the flat $0.01 rate; flat pricing has zero tier delta.
	assert.Equal(t, "600", got[29].UsageAtListRate.String(), "gross qty x list rate")
	assert.True(t, got[29].TierDelta.IsZero(), "flat pricing has no tier delta")
}

// TestBuildUsageCurve_GraduatedPrice exercises a SLAB-tiered price (proven additive by
// TestMarginalPrefixSumEqualsPeriodCharge) with no entitlementLimit: cumulative charge
// on each day must equal CalculateCost run directly on that day's cumulative
// quantity, and TierDelta must reflect the deviation from a flat tier1 charge.
func TestBuildUsageCurve_GraduatedPrice(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewInMemoryMeterUsageStore()
	params := ServiceParams{
		Logger:         logger.NewNoopLogger(),
		MeterUsageRepo: store,
		PriceRepo:      testutil.NewInMemoryPriceStore(),
		MeterRepo:      testutil.NewInMemoryMeterStore(),
		PlanRepo:       testutil.NewInMemoryPlanStore(),
		PriceUnitRepo:  testutil.NewInMemoryPriceUnitStore(),
		AddonRepo:      testutil.NewInMemoryAddonStore(),
		SubRepo:        testutil.NewInMemorySubscriptionStore(),
	}
	svc := &revenueService{ServiceParams: params}
	priceSvc := NewPriceService(params)

	tier1 := decimal.RequireFromString("0.01")
	tier2 := decimal.RequireFromString("0.008")
	upTo := uint64(1000)
	graduated := &price.Price{
		ID:           "price_curve_graduated_test",
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers: price.JSONBTiers{
			{UpTo: &upTo, UnitAmount: tier1},
			{UpTo: nil, UnitAmount: tier2},
		},
	}

	curve := buildTestCurveInput(t, ctx, store,
		curvePerDay(500),
		curveDays(5),
		curvePrice(graduated),
	)

	got, err := svc.buildUsageCurve(ctx, curve)
	require.NoError(t, err)
	require.Len(t, got, 5)

	for i, day := range got {
		cumQty := decimal.NewFromInt(int64((i + 1) * 500))
		wantCharge := priceSvc.CalculateCost(ctx, graduated, cumQty)
		assert.True(t, day.CumulativeCharge.Equal(wantCharge),
			"day %d: got %s want %s", i, day.CumulativeCharge, wantCharge)
		assert.True(t, day.CumulativeGrossQty.Equal(cumQty))
		assert.True(t, day.CumulativeBillableQty.Equal(cumQty), "no entitlementLimit: billable == gross")
		assert.True(t, day.CumulativeEntitlementQty.IsZero())

		wantListRate := cumQty.Mul(tier1)
		assert.True(t, day.UsageAtListRate.Equal(wantListRate))
		wantTierDelta := wantCharge.Sub(cumQty.Mul(tier1))
		assert.True(t, day.TierDelta.Equal(wantTierDelta))
	}

	// Day 3 crosses the 1000-unit tier boundary (cum qty 1500): TierDelta
	// must be non-zero once usage spills into the second, cheaper tier.
	assert.False(t, got[2].TierDelta.IsZero(), "tier delta should be non-zero past the tier boundary")
}

// curveReconcileEpsilon is the tight tolerance used for decimal reconciliation
// checks below (CalculateCost performs no intermediate rounding for flat/SLAB
// pricing, so any real divergence would show up far above this magnitude).
var curveReconcileEpsilon = decimal.RequireFromString("0.0000001")

// assertDecimalClose fails unless got and want differ by at most
// curveReconcileEpsilon.
func assertDecimalClose(t *testing.T, want, got decimal.Decimal, msgAndArgs ...interface{}) {
	t.Helper()
	diff := want.Sub(got).Abs()
	assert.Truef(t, diff.LessThanOrEqual(curveReconcileEpsilon),
		"want %s, got %s (diff %s) %v", want, got, diff, msgAndArgs)
}

// TestBuildUsageCurve_GraduatedWithEntitlementLimit is the one input combination
// where the implemented decomposition (TierDelta on billable_qty,
// UsageAtListRate on gross_qty) diverges from a gross_qty-only TierDelta
// formula: a graduated/SLAB price with a non-zero entitlementLimit, where cumulative
// gross usage crosses the tier boundary. Task 8's EntitlementAmount/NetAmount
// construction depends on this reconciling exactly.
func TestBuildUsageCurve_GraduatedWithEntitlementLimit(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewInMemoryMeterUsageStore()
	params := ServiceParams{
		Logger:         logger.NewNoopLogger(),
		MeterUsageRepo: store,
		PriceRepo:      testutil.NewInMemoryPriceStore(),
		MeterRepo:      testutil.NewInMemoryMeterStore(),
		PlanRepo:       testutil.NewInMemoryPlanStore(),
		PriceUnitRepo:  testutil.NewInMemoryPriceUnitStore(),
		AddonRepo:      testutil.NewInMemoryAddonStore(),
		SubRepo:        testutil.NewInMemorySubscriptionStore(),
	}
	svc := &revenueService{ServiceParams: params}
	priceSvc := NewPriceService(params)

	tier1 := decimal.RequireFromString("0.01")
	tier2 := decimal.RequireFromString("0.008")
	upTo := uint64(1000)
	graduated := &price.Price{
		ID:           "price_curve_graduated_entitlement_test",
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers: price.JSONBTiers{
			{UpTo: &upTo, UnitAmount: tier1},
			{UpTo: nil, UnitAmount: tier2},
		},
	}

	const entitlementLimit = 500
	curve := buildTestCurveInput(t, ctx, store,
		curvePerDay(500),
		curveEntitlementLimit(entitlementLimit),
		curveDays(8),
		curvePrice(graduated),
	)

	got, err := svc.buildUsageCurve(ctx, curve)
	require.NoError(t, err)
	require.Len(t, got, 8)

	entitlementLimitDec := decimal.NewFromInt(entitlementLimit)

	var prevCharge decimal.Decimal
	for i, day := range got {
		grossQty := decimal.NewFromInt(int64((i + 1) * 500))
		billableQty := grossQty.Sub(entitlementLimitDec)
		if billableQty.IsNegative() {
			billableQty = decimal.Zero
		}
		entitlementQty := entitlementLimitDec
		if grossQty.LessThan(entitlementLimitDec) {
			entitlementQty = grossQty
		}

		assert.True(t, day.CumulativeGrossQty.Equal(grossQty), "day %d gross qty", i)
		assert.True(t, day.CumulativeBillableQty.Equal(billableQty), "day %d billable qty", i)
		assert.True(t, day.CumulativeEntitlementQty.Equal(entitlementQty), "day %d entitlement qty", i)

		// The engine charge is CalculateCost run directly on the
		// day's cumulative billable quantity.
		wantCharge := priceSvc.CalculateCost(ctx, graduated, billableQty)
		assertDecimalClose(t, wantCharge, day.CumulativeCharge, "day %d cumulative charge", i)

		// Reconciliation identity the decomposition must satisfy:
		// UsageAtListRate + TierDelta - CumulativeEntitlementQty*tier1Rate == CumulativeCharge.
		// This is exactly where a gross_qty-based TierDelta would diverge from the implemented billable_qty-based
		// one, once gross usage crosses the tier boundary post-entitlementLimit.
		reconciled := day.UsageAtListRate.Add(day.TierDelta).Sub(entitlementQty.Mul(tier1))
		assertDecimalClose(t, day.CumulativeCharge, reconciled, "day %d reconciliation identity", i)

		// Marginal (day-over-day) charge must never be negative.
		if i > 0 {
			marginal := day.CumulativeCharge.Sub(prevCharge)
			assert.False(t, marginal.IsNegative(), "day %d marginal charge must be non-negative, got %s", i, marginal)
		}
		prevCharge = day.CumulativeCharge
	}

	// Sanity: usage does cross the tier boundary on billable_qty (day 3: billable
	// qty 1000 exactly at the boundary; day 4: billable qty 1500 spills over),
	// so this test actually exercises tiering, not just a flat/zero-entitlementLimit path.
	assert.True(t, got[2].CumulativeBillableQty.Equal(decimal.NewFromInt(1000)))
	assert.True(t, got[3].CumulativeBillableQty.Equal(decimal.NewFromInt(1500)))
	assert.False(t, got[3].TierDelta.IsZero(), "tier delta should be non-zero once billable usage spills into tier 2")
}

// --- marginal-sum characterization (formerly revenue_curve_seam_test.go) ---

// testPriceServiceParams builds the minimal ServiceParams CalculateCost needs
// (it only touches s.Logger on the priceService receiver).
func testPriceServiceParams(t *testing.T) ServiceParams {
	t.Helper()
	log := logger.NewNoopLogger()
	return ServiceParams{
		Logger:        log,
		DB:            testutil.NewMockPostgresClient(log),
		PriceRepo:     testutil.NewInMemoryPriceStore(),
		MeterRepo:     testutil.NewInMemoryMeterStore(),
		PlanRepo:      testutil.NewInMemoryPlanStore(),
		PriceUnitRepo: testutil.NewInMemoryPriceUnitStore(),
		AddonRepo:     testutil.NewInMemoryAddonStore(),
		SubRepo:       testutil.NewInMemorySubscriptionStore(),
	}
}

// seamFlatPrice returns a USAGE, FLAT_FEE price priced at unitAmount per unit.
func seamFlatPrice(t *testing.T, unitAmount string) *price.Price {
	t.Helper()
	return &price.Price{
		ID:           "price_flat_seam_test",
		Amount:       decimal.RequireFromString(unitAmount),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
}

// seamGraduatedPrice returns a USAGE, TIERED/SLAB price from ordered {upTo, unitAmount}
// pairs. An upTo of "0" marks the final, open-ended tier (UpTo = nil).
func seamGraduatedPrice(t *testing.T, tiers [][2]string) *price.Price {
	t.Helper()
	priceTiers := make(price.JSONBTiers, 0, len(tiers))
	for _, tier := range tiers {
		upToStr, unitAmountStr := tier[0], tier[1]
		var upTo *uint64
		if upToStr != "0" {
			v := decimal.RequireFromString(upToStr).BigInt().Uint64()
			upTo = &v
		}
		priceTiers = append(priceTiers, price.PriceTier{
			UpTo:       upTo,
			UnitAmount: decimal.RequireFromString(unitAmountStr),
		})
	}

	return &price.Price{
		ID:           "price_graduated_seam_test",
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers:        priceTiers,
	}
}

// TestMarginalPrefixSumEqualsPeriodCharge is the go/no-go
// characterization test for daily revenue decomposition via CalculateCost
// deltas on cumulative-through-day quantities: summing the marginal
// CalculateCost deltas across a monotonically increasing cumulative-quantity
// series must equal the single full-period CalculateCost charge, for both a
// flat and a graduated (SLAB) price. If this ever fails (e.g. rounding
// drift), the marginal decomposition must fall back to looping the full
// preview with an as-of override per day.
func TestMarginalPrefixSumEqualsPeriodCharge(t *testing.T) {
	ctx := context.Background()
	ps := NewPriceService(testPriceServiceParams(t))

	cases := []struct {
		name string
		p    *price.Price
	}{
		{"flat", seamFlatPrice(t, "0.01")},
		{"graduated", seamGraduatedPrice(t, [][2]string{{"1000", "0.01"}, {"0", "0.008"}})},
	}

	cum := []string{"0", "500", "2000", "5000", "20000", "40000", "60000"}
	epsilon := decimal.RequireFromString("0.000001")

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var marginalSum decimal.Decimal
			prev := decimal.Zero
			for _, q := range cum[1:] {
				d := decimal.RequireFromString(q)
				cur := ps.CalculateCost(ctx, c.p, d)
				marginalSum = marginalSum.Add(cur.Sub(prev))
				prev = cur
			}

			period := ps.CalculateCost(ctx, c.p, decimal.RequireFromString(cum[len(cum)-1]))

			residual := marginalSum.Sub(period).Abs()
			assert.True(t, residual.LessThan(epsilon),
				"marginal prefix sum %s != period charge %s (residual %s)", marginalSum, period, residual)
		})
	}
}

// TestBuildUsageCurve_ScopesToExternalCustomers: usage from other customers on
// the same meter must not leak into a subscription's curve.
func TestBuildUsageCurve_ScopesToExternalCustomers(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewInMemoryMeterUsageStore()
	svc := &revenueService{ServiceParams: ServiceParams{
		Logger:         logger.NewNoopLogger(),
		MeterUsageRepo: store,
		PriceRepo:      testutil.NewInMemoryPriceStore(),
		MeterRepo:      testutil.NewInMemoryMeterStore(),
		PlanRepo:       testutil.NewInMemoryPlanStore(),
		PriceUnitRepo:  testutil.NewInMemoryPriceUnitStore(),
		AddonRepo:      testutil.NewInMemoryAddonStore(),
		SubRepo:        testutil.NewInMemorySubscriptionStore(),
	}}

	periodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seed := func(id, extCustomer string, qty int64) *events.MeterUsage {
		return &events.MeterUsage{
			Event: events.Event{
				ID:                 id,
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				ExternalCustomerID: extCustomer,
				Timestamp:          periodStart.Add(time.Hour),
				IngestedAt:         periodStart.Add(time.Hour),
			},
			MeterID:    "meter_curve_scope",
			QtyTotal:   decimal.NewFromInt(qty),
			UniqueHash: id,
		}
	}
	require.NoError(t, store.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		seed("mu_mine", "cust_mine", 100),
		seed("mu_other", "cust_other", 900),
	}))

	got, err := svc.buildUsageCurve(ctx, usageCurveInput{
		Price:               &price.Price{ID: "p_scope", Amount: decimal.RequireFromString("1"), Currency: "usd", Type: types.PRICE_TYPE_USAGE, BillingModel: types.BILLING_MODEL_FLAT_FEE},
		MeterID:             "meter_curve_scope",
		PeriodStart:         periodStart,
		PeriodEnd:           periodStart.AddDate(0, 0, 1),
		ExternalCustomerIDs: []string{"cust_mine"},
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "100", got[0].CumulativeGrossQty.String(), "other customers' usage must not leak into the curve")
}
