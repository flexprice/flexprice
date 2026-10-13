package revenue

import (
	"context"
	"github.com/flexprice/flexprice/internal/ee/service"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
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
	svc := &revenueService{ServiceParams: service.ServiceParams{
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
	params := service.ServiceParams{
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
	priceSvc := service.NewPriceService(params)

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
	params := service.ServiceParams{
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
	priceSvc := service.NewPriceService(params)

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

// testPriceServiceParams builds the minimal service.ServiceParams CalculateCost needs
// (it only touches s.Logger on the priceService receiver).
func testPriceServiceParams(t *testing.T) service.ServiceParams {
	t.Helper()
	log := logger.NewNoopLogger()
	return service.ServiceParams{
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
	ps := service.NewPriceService(testPriceServiceParams(t))

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
	svc := &revenueService{ServiceParams: service.ServiceParams{
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

// TestSplitCurveAtCommitment_GraduatedQuantitySplit: the qty boundary comes
// from inverting the tier curve, not from dividing money at the first-tier
// rate. Tiers $0.01 to 1000 units then $0.008; 2500 units cost $22. A $14
// commitment covers 1500 units (1000×0.01 + 500×0.008) — a money÷rate split
// would wrongly place only (22−14)/0.01 = 800 units in overage instead of 1000.
func TestSplitCurveAtCommitment_GraduatedQuantitySplit(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewInMemoryMeterUsageStore()
	params := service.ServiceParams{
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
	priceSvc := service.NewPriceService(params)

	tier1 := decimal.RequireFromString("0.01")
	upTo := uint64(1000)
	graduated := &price.Price{
		ID:           "price_split_graduated_test",
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers: price.JSONBTiers{
			{UpTo: &upTo, UnitAmount: tier1},
			{UpTo: nil, UnitAmount: decimal.RequireFromString("0.008")},
		},
	}

	input := buildTestCurveInput(t, ctx, store,
		curvePerDay(500),
		curveDays(5),
		curvePrice(graduated),
	)
	curve, err := svc.buildUsageCurve(ctx, input)
	require.NoError(t, err)
	require.Len(t, curve, 5)

	normalAmount := decimal.NewFromInt(14)
	overageAmount := decimal.NewFromInt(8)
	totalQty := curve[len(curve)-1].CumulativeBillableQty

	normalQty := quantityAtCharge(ctx, priceSvc, graduated, normalAmount, totalQty)
	assertDecimalClose(t, decimal.NewFromInt(1500), normalQty, "charge inverse of $14")

	normal, overage := splitCurveAtCommitment(curve, normalAmount, overageAmount, tier1, normalQty)
	require.Len(t, normal, 5)
	require.Len(t, overage, 5)

	assert.True(t, normal[4].CumulativeCharge.Equal(normalAmount), "normal half pins to the engine's line amount")
	assert.True(t, overage[4].CumulativeCharge.Equal(overageAmount), "overage half pins to the engine's line amount")
	assertDecimalClose(t, decimal.NewFromInt(1000), overage[4].CumulativeBillableQty,
		"2500 − 1500 units are overage, not (22−14)/0.01 = 800")
	assertDecimalClose(t, decimal.NewFromInt(1500), normal[4].CumulativeBillableQty)

	// Days fully inside the commitment carry no overage quantity.
	assert.True(t, overage[0].CumulativeBillableQty.IsZero())
	assert.True(t, overage[1].CumulativeBillableQty.IsZero())
	// Quantity never double-counts across the halves.
	for i := range curve {
		assertDecimalClose(t, curve[i].CumulativeBillableQty,
			normal[i].CumulativeBillableQty.Add(overage[i].CumulativeBillableQty), "day", i)
	}
}

// TestBuildUsageCurve_ClampsToToday: an open period books rows only through
// today (its remaining days have not happened), a period that has not started
// books nothing at all, and a period shorter than a day still books its one
// day — the one-day fallback must survive the clamp.
func TestBuildUsageCurve_ClampsToToday(t *testing.T) {
	ctx := context.Background()
	svc := &revenueService{ServiceParams: service.ServiceParams{
		Logger:         logger.NewNoopLogger(),
		MeterUsageRepo: testutil.NewInMemoryMeterUsageStore(),
		PriceRepo:      testutil.NewInMemoryPriceStore(),
		MeterRepo:      testutil.NewInMemoryMeterStore(),
		PlanRepo:       testutil.NewInMemoryPlanStore(),
		PriceUnitRepo:  testutil.NewInMemoryPriceUnitStore(),
		AddonRepo:      testutil.NewInMemoryAddonStore(),
		SubRepo:        testutil.NewInMemorySubscriptionStore(),
	}}
	// One fixed instant drives both the expectations and the clamp, so the
	// test cannot straddle a UTC midnight between the two.
	asOf := time.Date(2026, 5, 15, 13, 0, 0, 0, time.UTC)
	today := asOf.Truncate(24 * time.Hour)
	in := func(start, end time.Time) usageCurveInput {
		return usageCurveInput{Price: flatSum(t), MeterID: "meter_clamp", PeriodStart: start, PeriodEnd: end, AsOf: asOf}
	}

	open, err := svc.buildUsageCurve(ctx, in(today.AddDate(0, 0, -4), today.AddDate(0, 0, 26)))
	require.NoError(t, err)
	require.Len(t, open, 5, "four elapsed days plus today, not the whole 30-day period")
	assert.True(t, open[len(open)-1].Day.Equal(today), "last row is today, got %s", open[len(open)-1].Day)

	future, err := svc.buildUsageCurve(ctx, in(today.AddDate(0, 0, 3), today.AddDate(0, 1, 3)))
	require.NoError(t, err)
	assert.Empty(t, future, "a period that has not started books no rows")

	past := today.AddDate(0, 0, -2)
	short, err := svc.buildUsageCurve(ctx, in(past, past.Add(6*time.Hour)))
	require.NoError(t, err)
	assert.Len(t, short, 1, "a sub-day period still books its single day")
}

// TestBuildUsageCurve_BoundsByLocalDate: usage is bucketed to local midnight,
// so bounding the shared read by exact instants drops the period's own first
// day — a period starting 09:37 would discard everything metered before 09:37
// that day. The end day is the mirror case: it belongs to this period only when
// the period ends part-way through it.
func TestBuildUsageCurve_BoundsByLocalDate(t *testing.T) {
	ctx := context.Background()
	svc := &revenueService{ServiceParams: service.ServiceParams{
		Logger:         logger.NewNoopLogger(),
		MeterUsageRepo: testutil.NewInMemoryMeterUsageStore(),
		PriceRepo:      testutil.NewInMemoryPriceStore(),
		MeterRepo:      testutil.NewInMemoryMeterStore(),
		PlanRepo:       testutil.NewInMemoryPlanStore(),
		PriceUnitRepo:  testutil.NewInMemoryPriceUnitStore(),
		AddonRepo:      testutil.NewInMemoryAddonStore(),
		SubRepo:        testutil.NewInMemorySubscriptionStore(),
	}}

	day1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	usage := []events.DailyUsagePoint{
		{Day: day1, Qty: decimal.NewFromInt(100)},
		{Day: day1.AddDate(0, 0, 1), Qty: decimal.NewFromInt(200)},
		{Day: day1.AddDate(0, 0, 2), Qty: decimal.NewFromInt(400)},
	}
	build := func(start, end time.Time) []dayCharge {
		got, err := svc.buildUsageCurve(ctx, usageCurveInput{
			Price: flatSum(t), MeterID: "meter_bounds",
			PeriodStart: start, PeriodEnd: end, Usage: usage,
			AsOf: day1.AddDate(0, 0, 3),
		})
		require.NoError(t, err)
		return got
	}

	// Starts mid-day 1: that whole day's usage still belongs to this period.
	mid := build(day1.Add(9*time.Hour+37*time.Minute), day1.AddDate(0, 0, 2))
	require.NotEmpty(t, mid)
	assert.True(t, mid[len(mid)-1].CumulativeGrossQty.Equal(decimal.NewFromInt(300)),
		"day 1's usage must not be dropped by a mid-day start, got %s", mid[len(mid)-1].CumulativeGrossQty)

	// Ends at midnight on day 3: day 3 opens the next period and must not fold
	// in, or one line item is charged for the next one's usage.
	midnight := build(day1, day1.AddDate(0, 0, 2))
	assert.True(t, midnight[len(midnight)-1].CumulativeGrossQty.Equal(decimal.NewFromInt(300)),
		"a midnight end must not absorb its own day, got %s", midnight[len(midnight)-1].CumulativeGrossQty)

	// Ends mid-day 3: that day's usage up to the end instant does belong here,
	// and the walk folds it into the last emitted day.
	partial := build(day1, day1.AddDate(0, 0, 2).Add(13*time.Hour))
	assert.True(t, partial[len(partial)-1].CumulativeGrossQty.Equal(decimal.NewFromInt(700)),
		"a mid-day end must fold its own day back in, got %s", partial[len(partial)-1].CumulativeGrossQty)
}

// TestCumulativeUsageByDay_PartialBoundaryDays: a grant window opens at
// QuotaCrossedAt, an event instant, so a mid-day boundary is the norm. Slicing
// whole-day quantities by date would put that day's pre-crossing usage on the
// billed side of the boundary and drop a mid-day end's usage entirely. Only a
// boundary that is not local midnight costs an exact read.
func TestCumulativeUsageByDay_PartialBoundaryDays(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewInMemoryMeterUsageStore()
	svc := &revenueService{ServiceParams: service.ServiceParams{
		Logger:         logger.NewNoopLogger(),
		MeterUsageRepo: store,
		PriceRepo:      testutil.NewInMemoryPriceStore(),
		MeterRepo:      testutil.NewInMemoryMeterStore(),
		PlanRepo:       testutil.NewInMemoryPlanStore(),
		PriceUnitRepo:  testutil.NewInMemoryPriceUnitStore(),
		AddonRepo:      testutil.NewInMemoryAddonStore(),
		SubRepo:        testutil.NewInMemorySubscriptionStore(),
	}}

	day1 := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	// 10 units at 06:00 and 90 at 18:00 on day 1; 50 on day 2.
	var records []*events.MeterUsage
	for _, seed := range []struct {
		at  time.Time
		qty int64
	}{{day1.Add(6 * time.Hour), 10}, {day1.Add(18 * time.Hour), 90}, {day1.AddDate(0, 0, 1).Add(9 * time.Hour), 50}} {
		id := types.GenerateUUIDWithPrefix("mu_bnd")
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID: id, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
				EventName: "bnd", Timestamp: seed.at, IngestedAt: seed.at,
				Properties: map[string]interface{}{},
			},
			MeterID: "meter_bnd", QtyTotal: decimal.NewFromInt(seed.qty), UniqueHash: id,
		})
	}
	require.NoError(t, store.BulkInsertMeterUsage(ctx, records))

	in := grantCurveInput{
		Meter: &meter.Meter{ID: "meter_bnd"},
		Usage: []events.DailyUsagePoint{
			{Day: day1, Qty: decimal.NewFromInt(100)},
			{Day: day1.AddDate(0, 0, 1), Qty: decimal.NewFromInt(50)},
		},
		PeriodStart: day1,
		PeriodEnd:   day1.AddDate(0, 0, 2),
	}

	// Window opens at noon on day 1: only the 90 units after noon belong to it.
	got, err := svc.cumulativeUsageByDay(ctx, in, day1.Add(12*time.Hour), day1.AddDate(0, 0, 2))
	require.NoError(t, err)
	assert.Equal(t, "90", got[day1.Format(dayKeyLayout)].String(),
		"the pre-crossing part of the opening day must not count as billed")
	assert.Equal(t, "140", got[day1.AddDate(0, 0, 1).Format(dayKeyLayout)].String())

	// Window ends at noon on day 2: that day's morning usage still belongs to it.
	got, err = svc.cumulativeUsageByDay(ctx, in, day1, day1.AddDate(0, 0, 1).Add(12*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, "150", got[day1.AddDate(0, 0, 1).Format(dayKeyLayout)].String(),
		"a mid-day end must keep that morning's usage, not drop the day")

	// Midnight-aligned window: served entirely from the pre-read.
	got, err = svc.cumulativeUsageByDay(ctx, in, day1, day1.AddDate(0, 0, 1))
	require.NoError(t, err)
	assert.Equal(t, "100", got[day1.Format(dayKeyLayout)].String())
	assert.NotContains(t, got, day1.AddDate(0, 0, 1).Format(dayKeyLayout),
		"a midnight end owns none of its own day")
}
