package revenue

import (
	"context"
	"github.com/flexprice/flexprice/internal/ee/service"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- classifier fixtures ---

func flatSum(t *testing.T) *price.Price {
	t.Helper()
	return &price.Price{
		ID:           "price_flat_sum",
		Amount:       decimal.RequireFromString("0.01"),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
}

func flat(t *testing.T) *price.Price {
	t.Helper()
	return &price.Price{
		ID:           "price_flat",
		Amount:       decimal.RequireFromString("0.01"),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
}

func volumeTiered(t *testing.T) *price.Price {
	t.Helper()
	upTo := uint64(1000)
	return &price.Price{
		ID:           "price_volume_tiered",
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_VOLUME,
		Tiers: price.JSONBTiers{
			{UpTo: &upTo, UnitAmount: decimal.RequireFromString("0.01")},
			{UpTo: nil, UnitAmount: decimal.RequireFromString("0.008")},
		},
	}
}

func graduated(t *testing.T) *price.Price {
	t.Helper()
	upTo := uint64(1000)
	return &price.Price{
		ID:           "price_graduated",
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers: price.JSONBTiers{
			{UpTo: &upTo, UnitAmount: decimal.RequireFromString("0.01")},
			{UpTo: nil, UnitAmount: decimal.RequireFromString("0.008")},
		},
	}
}

func sumMeter(t *testing.T) *meter.Meter {
	t.Helper()
	return &meter.Meter{ID: "meter_sum", Aggregation: meter.Aggregation{Type: types.AggregationSum}}
}

func countMeter(t *testing.T) *meter.Meter {
	t.Helper()
	return &meter.Meter{ID: "meter_count", Aggregation: meter.Aggregation{Type: types.AggregationCount}}
}

func latestMeter(t *testing.T) *meter.Meter {
	t.Helper()
	return &meter.Meter{ID: "meter_latest", Aggregation: meter.Aggregation{Type: types.AggregationLatest}}
}

func countUniqueMeter(t *testing.T) *meter.Meter {
	t.Helper()
	return &meter.Meter{ID: "meter_count_unique", Aggregation: meter.Aggregation{Type: types.AggregationCountUnique}}
}

func maxMeter(t *testing.T) *meter.Meter {
	t.Helper()
	return &meter.Meter{ID: "meter_max", Aggregation: meter.Aggregation{Type: types.AggregationMax}}
}

func bucketedMaxWeekly(t *testing.T) *meter.Meter {
	t.Helper()
	return &meter.Meter{
		ID: "meter_bucketed_max_weekly",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			BucketSize: types.WindowSizeWeek,
		},
	}
}

// TestDecompositionMode pins which price/meter combinations must split per
// day (marginal) vs stay whole-period (period_only).
func TestDecompositionMode(t *testing.T) {
	assert.Equal(t, types.Marginal, decompositionMode(flatSum(t), sumMeter(t)))
	assert.Equal(t, types.PeriodOnly, decompositionMode(volumeTiered(t), sumMeter(t)))
	assert.Equal(t, types.PeriodOnly, decompositionMode(flat(t), latestMeter(t)))
	assert.Equal(t, types.PeriodOnly, decompositionMode(flat(t), bucketedMaxWeekly(t)))
	assert.Equal(t, types.PeriodOnly, decompositionMode(flat(t), maxMeter(t)),
		"plain MAX is not day-additive; the SUM-based curve cannot price it")
	assert.Equal(t, types.PeriodOnly, decompositionMode(flat(t), countUniqueMeter(t)),
		"a distinct count is not a running sum: the same event across two days adds once")
	assert.Equal(t, types.Marginal, decompositionMode(graduated(t), countMeter(t)))

	// Bucketed pricing is per window: only a flat fee stays day-additive.
	bucketedSum := &meter.Meter{ID: "meter_bucketed_sum",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, BucketSize: types.WindowSizeDay}}
	pkg := &price.Price{ID: "price_pkg", Amount: decimal.NewFromInt(1),
		BillingModel:      types.BILLING_MODEL_PACKAGE,
		TransformQuantity: price.JSONBTransformQuantity{DivideBy: 10, Round: types.ROUND_UP}}
	// Windows that nest inside a day are priced per window and grouped into
	// days, whatever the pricing shape.
	assert.Equal(t, types.Marginal, decompositionMode(pkg, bucketedSum))
	assert.Equal(t, types.Marginal, decompositionMode(graduated(t), bucketedSum))
	assert.Equal(t, types.Marginal, decompositionMode(flatSum(t), bucketedSum))

	// Week and month windows span days, so their charge cannot land on one.
	weekly := &meter.Meter{ID: "meter_bucketed_week",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, BucketSize: types.WindowSizeWeek}}
	monthly := &meter.Meter{ID: "meter_bucketed_month",
		Aggregation: meter.Aggregation{Type: types.AggregationMax, BucketSize: types.WindowSizeMonth}}
	assert.Equal(t, types.PeriodOnly, decompositionMode(flatSum(t), weekly))
	assert.Equal(t, types.PeriodOnly, decompositionMode(flatSum(t), monthly))
}

// TestIsMultiPeriodCommitment covers the multi-period commitment detection
// mirrored from billing_meter_usage.go:77-92.
func TestIsMultiPeriodCommitment(t *testing.T) {
	amount := decimal.RequireFromString("500")
	overage := decimal.RequireFromString("1.5")
	monthly := types.BILLING_PERIOD_MONTHLY
	annual := types.BILLING_PERIOD_ANNUAL

	tests := []struct {
		name string
		sub  *subscription.Subscription
		want bool
	}{
		{
			name: "no commitment",
			sub:  &subscription.Subscription{BillingPeriod: monthly},
			want: false,
		},
		{
			name: "single-period commitment (same duration as billing period)",
			sub: &subscription.Subscription{
				BillingPeriod:      monthly,
				CommitmentAmount:   &amount,
				CommitmentDuration: &monthly,
				OverageFactor:      &overage,
			},
			want: false,
		},
		{
			name: "multi-period commitment (annual commitment on monthly sub)",
			sub: &subscription.Subscription{
				BillingPeriod:      monthly,
				CommitmentAmount:   &amount,
				CommitmentDuration: &annual,
				OverageFactor:      &overage,
			},
			want: true,
		},
		{
			name: "overage factor not greater than 1",
			sub: &subscription.Subscription{
				BillingPeriod:      monthly,
				CommitmentAmount:   &amount,
				CommitmentDuration: &annual,
				OverageFactor:      lo.ToPtr(decimal.RequireFromString("1")),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isMultiPeriodCommitment(tt.sub))
		})
	}
}

// --- worked example: $30 advance fixed + usage(2000/day, 20k
// included, $0.01/call) + $500 commitment, summing to $530 over 30 days ---

const workedExampleTenantID = "tenant_worked_example"
const workedExampleEnvID = "env_worked_example"
const workedExampleCustomerID = "cust_worked_example"
const workedExampleSubID = "sub_worked_example"

var workedExamplePeriodStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// day returns the calendar day n (1-indexed) of the worked example's period.
func day(n int) time.Time {
	return workedExamplePeriodStart.AddDate(0, 0, n-1)
}

func day1() time.Time { return day(1) }

// workedExamplePeriodEnd is the inclusive last calendar day of the 30-day
// worked-example period (day 30), matching revenuePeriod's End semantics —
// distinct from the half-open exclusive bound BuildUsageCurve's
// usageCurveInput uses.
var workedExamplePeriodEnd = day(30)

// workedExampleFixedLineItem is the $30/mo advance fixed charge.
func workedExampleFixedLineItem(t *testing.T) previewLineItem {
	t.Helper()
	return previewLineItem{
		TenantID:       workedExampleTenantID,
		EnvironmentID:  workedExampleEnvID,
		CustomerID:     workedExampleCustomerID,
		SubscriptionID: workedExampleSubID,
		SubLineItemID:  "sli_fixed",
		Price: &price.Price{
			ID:             "price_fixed",
			Type:           types.PRICE_TYPE_FIXED,
			BillingModel:   types.BILLING_MODEL_FLAT_FEE,
			InvoiceCadence: types.InvoiceCadenceAdvance,
			Currency:       "usd",
		},
		Currency:     "usd",
		EngineAmount: decimal.RequireFromString("30"),
		PeriodStart:  workedExamplePeriodStart,
		PeriodEnd:    workedExamplePeriodEnd,
	}
}

// workedExampleUsageLineItem is the $0.01/call usage charge with a 20000
// call entitlementLimit, seeded at 2000 calls/day for 30 days into store.
func workedExampleUsageLineItem(t *testing.T, ctx context.Context, store *testutil.InMemoryMeterUsageStore) (previewLineItem, usageCurveInput) {
	t.Helper()
	liInput := buildTestCurveInput(t, ctx, store,
		curvePerDay(2000),
		curveEntitlementLimit(20000),
		curveFlatRate("0.01"),
		curveDays(30),
	)
	liInput.Price.Type = types.PRICE_TYPE_USAGE
	liInput.Price.InvoiceCadence = types.InvoiceCadenceArrear

	li := previewLineItem{
		TenantID:       workedExampleTenantID,
		EnvironmentID:  workedExampleEnvID,
		CustomerID:     workedExampleCustomerID,
		SubscriptionID: workedExampleSubID,
		SubLineItemID:  "sli_usage",
		Price:          liInput.Price,
		Meter:          &meter.Meter{ID: liInput.MeterID, Aggregation: meter.Aggregation{Type: types.AggregationSum}},
		Currency:       "usd",
		PeriodStart:    liInput.PeriodStart,
		// liInput.PeriodEnd is BuildUsageCurve's exclusive bound (Jan 31);
		// previewLineItem.PeriodEnd is the inclusive last calendar day (Jan 30).
		PeriodEnd: liInput.PeriodEnd.AddDate(0, 0, -1),
	}
	return li, liInput
}

// workedExampleTrueupLineItem is the $500 commitment true-up: usage billed
// $400 (20 billed days * $20/day), so the true-up tops up to $500.
func workedExampleTrueupLineItem(t *testing.T) previewLineItem {
	t.Helper()
	return previewLineItem{
		TenantID:       workedExampleTenantID,
		EnvironmentID:  workedExampleEnvID,
		CustomerID:     workedExampleCustomerID,
		SubscriptionID: workedExampleSubID,
		SubLineItemID:  "sli_trueup",
		Price: &price.Price{
			ID:             "price_trueup",
			Type:           types.PRICE_TYPE_FIXED,
			BillingModel:   types.BILLING_MODEL_FLAT_FEE,
			InvoiceCadence: types.InvoiceCadenceArrear,
			Currency:       "usd",
		},
		Metadata:     types.Metadata{"is_commitment_trueup": "true"},
		Currency:     "usd",
		EngineAmount: decimal.RequireFromString("100"),
		PeriodStart:  workedExamplePeriodStart,
		PeriodEnd:    workedExamplePeriodEnd,
	}
}

// decomposeAll runs the full decomposition pipeline (classify -> decompose)
// over the worked example's three line items, mirroring what Task 10's
// rollup will do per subscription.
func decomposeAll(t *testing.T, ctx context.Context) []*revenuefact.RevenueFact {
	t.Helper()

	store := testutil.NewInMemoryMeterUsageStore()
	period := revenuePeriod{Start: workedExamplePeriodStart, End: workedExamplePeriodEnd}

	var rows []*revenuefact.RevenueFact

	fixedLI := workedExampleFixedLineItem(t)
	if f := decomposeFixed(fixedLI, period); f != nil {
		rows = append(rows, f)
	}

	usageLI, liInput := workedExampleUsageLineItem(t, ctx, store)
	switch decompositionMode(usageLI.Price, usageLI.Meter) {
	case types.Marginal:
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
		curve, err := svc.buildUsageCurve(ctx, liInput)
		require.NoError(t, err)
		rows = append(rows, decomposeUsageMarginal(usageLI, curve)...)
	default:
		rows = append(rows, decomposeUsagePeriodOnly(usageLI, period))
	}

	trueupLI := workedExampleTrueupLineItem(t)
	if f := decomposeCommitmentTrueup(trueupLI, period); f != nil {
		rows = append(rows, f)
	}

	return rows
}

// find returns the row matching the given day and revenue source, failing
// the test if none (or more than one) is found.
func find(t *testing.T, rows []*revenuefact.RevenueFact, d time.Time, source types.RevenueSource) *revenuefact.RevenueFact {
	t.Helper()
	var match *revenuefact.RevenueFact
	for _, r := range rows {
		if r.Day.Equal(d) && r.RevenueSource == source {
			require.Nil(t, match, "multiple rows matched day %s source %s", d, source)
			match = r
		}
	}
	require.NotNil(t, match, "no row matched day %s source %s", d, source)
	return match
}

func sumNet(rows []*revenuefact.RevenueFact) decimal.Decimal {
	total := decimal.Zero
	for _, r := range rows {
		total = total.Add(r.NetAmount)
	}
	return total
}

// TestDecompose_WorkedExample_530: a $30/mo
// advance fixed charge, $0.01/call usage with a 20000-call entitlementLimit at
// 2000 calls/day, and a $500 minimum commitment, over a 30-day period.
func TestDecompose_WorkedExample_530(t *testing.T) {
	ctx := context.Background()
	rows := decomposeAll(t, ctx)

	assert.Equal(t, "30", find(t, rows, day1(), types.RevenueSourceFixed).NetAmount.String())
	assert.True(t, find(t, rows, day(5), types.RevenueSourceUsage).NetAmount.IsZero())
	assert.Equal(t, "20", find(t, rows, day(11), types.RevenueSourceUsage).NetAmount.String())
	assert.Equal(t, "100", find(t, rows, day(30), types.RevenueSourceCommitmentTrueup).NetAmount.String())
	assert.Equal(t, "530", sumNet(rows).String())

	// Every usage row must satisfy the VERIFIED FORMULA reconciliation
	// identity exactly: net == usage_at_list_rate + tier_delta - entitlement_amount.
	for _, r := range rows {
		if r.RevenueSource != types.RevenueSourceUsage || r.DecompositionMode != types.Marginal {
			continue
		}
		reconciled := r.UsageAtListRate.Add(r.TierDelta).Sub(r.EntitlementAmount)
		assert.True(t, r.NetAmount.Equal(reconciled),
			"day %s: net %s != usage_at_list_rate %s + tier_delta %s - entitlement_amount %s",
			r.Day, r.NetAmount, r.UsageAtListRate, r.TierDelta, r.EntitlementAmount)
	}
}

// TestDecomposeFixed_ExcludesTrueupAndOverage asserts the exclusion rule:
// decomposeFixed returns nil for metadata-flagged true-up/overage line items.
func TestDecomposeFixed_ExcludesTrueupAndOverage(t *testing.T) {
	period := revenuePeriod{Start: workedExamplePeriodStart, End: workedExamplePeriodEnd}

	trueupLI := workedExampleTrueupLineItem(t)
	assert.Nil(t, decomposeFixed(trueupLI, period))

	overageLI := workedExampleTrueupLineItem(t)
	overageLI.Metadata = types.Metadata{"is_overage": "true"}
	assert.Nil(t, decomposeFixed(overageLI, period))

	fixedLI := workedExampleFixedLineItem(t)
	assert.NotNil(t, decomposeFixed(fixedLI, period))
}

// TestListRate_PerUnit: the list rate is per unit, whatever shape the price
// takes. A package price's Amount buys a block, so charging the whole block
// amount per unit inflated usage_at_list_rate and entitlement_amount by the
// block size (seen on dev: 1.8 entitled hours priced at $1.80 instead of $0.18).
func TestListRate_PerUnit(t *testing.T) {
	pkg := &price.Price{
		ID:                "price_pkg",
		Amount:            decimal.NewFromInt(1),
		BillingModel:      types.BILLING_MODEL_PACKAGE,
		TransformQuantity: price.JSONBTransformQuantity{DivideBy: 10, Round: types.ROUND_UP},
	}
	assert.Equal(t, "0.1", listRate(pkg).String(), "package: amount spread over the block")

	// A malformed package price must not divide by zero.
	broken := &price.Price{ID: "price_pkg0", Amount: decimal.NewFromInt(1), BillingModel: types.BILLING_MODEL_PACKAGE}
	assert.Equal(t, "1", listRate(broken).String())

	assert.Equal(t, "0.01", listRate(flatSum(t)).String(), "flat fee: the amount is already per unit")
	assert.Equal(t, "0.01", listRate(graduated(t)).String(), "tiered: the first tier's unit amount")
}

// TestDecomposeLineCommitmentRows_TrueUpAndOverageTogether: a per-bucket
// (windowed) commitment settles each bucket on its own, so one line can carry
// BOTH an overage (a bucket that ran over) and a true-up (a bucket that fell
// short) — the engine folds both into the line's amount and reports them on
// CommitmentInfo. The three parts must split back out, sum to the line, and
// leave the discounts on the usage part only.
func TestDecomposeLineCommitmentRows_TrueUpAndOverageTogether(t *testing.T) {
	ctx := context.Background()
	svc := &revenueService{ServiceParams: service.ServiceParams{Logger: logger.NewNoopLogger()}}

	p, m := flatSum(t), sumMeter(t)
	period := revenuePeriod{
		Start: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
	}
	base := previewLineItem{
		TenantID: "t", EnvironmentID: "e", CustomerID: "cust", SubscriptionID: "sub",
		SubLineItemID: "sli", Price: p, Meter: m, Currency: "usd",
		EngineAmount: decimal.NewFromInt(200),
		LineDiscount: decimal.NewFromInt(10),
		PeriodStart:  period.Start, PeriodEnd: period.End,
	}
	item := &dto.CreateInvoiceLineItemRequest{
		Amount: decimal.NewFromInt(200),
		CommitmentInfo: &types.CommitmentInfo{
			IsWindowed:            true,
			ComputedOverageAmount: decimal.NewFromInt(40),
			ComputedTrueUpAmount:  decimal.NewFromInt(25),
		},
	}

	rows, handled, err := svc.decomposeLineCommitmentRows(ctx, &subscription.Subscription{ID: "sub"},
		&rollupInputs{}, base, item, period, p, m)
	require.NoError(t, err)
	require.True(t, handled, "a line carrying commitment amounts is decomposed here")
	require.Len(t, rows, 3)

	bySource := map[types.RevenueSource]*revenuefact.RevenueFact{}
	for _, r := range rows {
		bySource[r.RevenueSource] = r
		assert.Equal(t, types.PeriodOnly, r.DecompositionMode, "windowed commitments settle per bucket, not per day")
	}
	// within = 200 - 40 overage - 25 true-up = 135, less the $10 line discount.
	require.NotNil(t, bySource[types.RevenueSourceUsage])
	assert.Equal(t, "125", bySource[types.RevenueSourceUsage].NetAmount.String())
	assert.Equal(t, "40", bySource[types.RevenueSourceOverage].NetAmount.String())
	assert.Equal(t, "25", bySource[types.RevenueSourceCommitmentTrueup].NetAmount.String())

	// Only the usage part carries the discount, so the line still reconciles.
	assert.Equal(t, "10", bySource[types.RevenueSourceUsage].LineDiscount.String())
	assert.True(t, bySource[types.RevenueSourceOverage].LineDiscount.IsZero())
	assert.True(t, bySource[types.RevenueSourceCommitmentTrueup].LineDiscount.IsZero())
	if residual, ok := reconcileLineItem(rows, item.Amount.Sub(decimal.NewFromInt(10))); !ok {
		t.Fatalf("line item must reconcile, residual %s", residual)
	}
}

// TestDecomposeLineCommitmentRows_TrueUpOnlyKeepsDiscounts: a line whose meter
// fired nothing bills its whole commitment as true-up. With no usage row to
// carry them, the line's discounts must ride on the true-up row instead of
// being dropped.
func TestDecomposeLineCommitmentRows_TrueUpOnlyKeepsDiscounts(t *testing.T) {
	ctx := context.Background()
	svc := &revenueService{ServiceParams: service.ServiceParams{Logger: logger.NewNoopLogger()}}
	p, m := flatSum(t), sumMeter(t)
	period := revenuePeriod{
		Start: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
	}
	base := previewLineItem{
		TenantID: "t", EnvironmentID: "e", CustomerID: "cust", SubscriptionID: "sub",
		SubLineItemID: "sli", Price: p, Meter: m, Currency: "usd",
		EngineAmount:    decimal.NewFromInt(50),
		InvoiceDiscount: decimal.NewFromInt(5),
		PeriodStart:     period.Start, PeriodEnd: period.End,
	}
	item := &dto.CreateInvoiceLineItemRequest{
		Amount: decimal.NewFromInt(50),
		CommitmentInfo: &types.CommitmentInfo{
			IsWindowed:           true,
			ComputedTrueUpAmount: decimal.NewFromInt(50),
		},
	}

	rows, handled, err := svc.decomposeLineCommitmentRows(ctx, &subscription.Subscription{ID: "sub"},
		&rollupInputs{}, base, item, period, p, m)
	require.NoError(t, err)
	require.True(t, handled)
	require.Len(t, rows, 1, "a silent meter books only its true-up")
	assert.Equal(t, types.RevenueSourceCommitmentTrueup, rows[0].RevenueSource)
	assert.Equal(t, "45", rows[0].NetAmount.String(), "50 true-up less the 5 invoice discount")
	assert.Equal(t, "5", rows[0].InvoiceDiscount.String())
}

// TestPeriodDays_IntraDayWindows: the engine's period end is exclusive, but
// only a midnight end excludes its own day. Subtracting a whole day from a
// mid-day end inverted any window that opened and closed on the same date —
// a same-day cancellation wrote period_start 09-23 with period_end 09-22.
func TestPeriodDays_IntraDayWindows(t *testing.T) {
	day := func(y int, m time.Month, d, h, min int) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, time.UTC)
	}

	// A whole month: the exclusive midnight end belongs to the next period.
	p := periodDaysUTC(day(2026, 9, 1, 0, 0), day(2026, 10, 1, 0, 0))
	assert.Equal(t, "2026-09-30", p.End.Format("2006-01-02"))

	// Created and cancelled the same afternoon: one day, not an inverted one.
	p = periodDaysUTC(day(2026, 9, 23, 9, 37), day(2026, 9, 23, 13, 7))
	assert.Equal(t, "2026-09-23", p.End.Format("2006-01-02"))
	assert.False(t, p.End.Before(dayOf(p.Start, time.UTC)), "a period may never end before it starts")

	// Ending part-way through a later day: that day opens the NEXT period, so
	// it is not this one's last — the curve folds its usage into 09-24.
	p = periodDaysUTC(day(2026, 9, 23, 9, 37), day(2026, 9, 25, 13, 7))
	assert.Equal(t, "2026-09-24", p.End.Format("2006-01-02"))

	// Back-to-back periods must not both claim the shared boundary date.
	first := periodDaysUTC(day(2026, 3, 31, 18, 30), day(2026, 6, 30, 18, 30))
	second := periodDaysUTC(day(2026, 6, 30, 18, 30), day(2026, 9, 30, 18, 30))
	assert.Equal(t, "2026-06-29", first.End.Format("2006-01-02"))
	assert.True(t, first.End.Before(dayOf(second.Start, time.UTC)), "periods overlap on a day")

	// Degenerate zero-length window still keys to its own day.
	p = periodDaysUTC(day(2026, 9, 23, 9, 37), day(2026, 9, 23, 9, 37))
	assert.Equal(t, "2026-09-23", p.End.Format("2006-01-02"))
	assert.False(t, p.End.Before(dayOf(p.Start, time.UTC)))
}

// periodDaysUTC keeps the UTC cases above readable.
func periodDaysUTC(start, exclusiveEnd time.Time) revenuePeriod {
	return periodDays(start, exclusiveEnd, time.UTC)
}

// TestPeriodDays_SubscriptionTimezone: day bounds must be computed in the
// subscription's zone, because buildUsageCurve splits usage into LOCAL days.
// A Berlin period ending Jun 30 00:00 local is Jun 29 22:00 UTC; the curve's
// last local day is Jun 29, so a UTC-truncated bound of Jun 28 would exclude
// a day the curve wrote and the flip would never reach it.
func TestPeriodDays_SubscriptionTimezone(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, berlin)
	exclusiveEnd := time.Date(2026, 6, 30, 0, 0, 0, 0, berlin)

	p := periodDays(start, exclusiveEnd, berlin)
	assert.Equal(t, "2026-06-01", p.End.AddDate(0, 0, -28).Format("2006-01-02"))
	assert.Equal(t, "2026-06-29", p.End.Format("2006-01-02"),
		"the last local day the curve emits must be inside the bound")

	// The same instants read in UTC land a day earlier — the bug being fixed.
	assert.Equal(t, "2026-06-28", periodDaysUTC(start, exclusiveEnd).End.Format("2006-01-02"))

	// East of UTC too: Kolkata is +05:30, so a local midnight end is 18:30 UTC
	// the previous day.
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	kStart := time.Date(2026, 4, 1, 0, 0, 0, 0, kolkata)
	kEnd := time.Date(2026, 7, 1, 0, 0, 0, 0, kolkata)
	assert.Equal(t, "2026-06-30", periodDays(kStart, kEnd, kolkata).End.Format("2006-01-02"))

	// Back-to-back local periods must not both claim the boundary date.
	next := periodDays(exclusiveEnd, time.Date(2026, 7, 30, 0, 0, 0, 0, berlin), berlin)
	assert.True(t, periodDays(start, exclusiveEnd, berlin).End.Before(dayOf(next.Start, berlin)))

	// An unknown or empty zone falls back to UTC instead of failing.
	assert.Equal(t, time.UTC, locationOf(""))
	assert.Equal(t, time.UTC, locationOf("Not/AZone"))
}

// TestChangedRows: the diff narrows the write, and the only thing that matters
// is that it never withholds a write that carries a change.
func TestChangedRows(t *testing.T) {
	row := func(day string, net string) *revenuefact.RevenueFact {
		d, err := time.Parse("2006-01-02", day)
		require.NoError(t, err)
		return &revenuefact.RevenueFact{
			PriceID:       lo.ToPtr("price_1"),
			SubLineItemID: lo.ToPtr("sli_1"),
			Day:           d,
			RevenueSource: types.RevenueSourceUsage,
			NetAmount:     decimal.RequireFromString(net),
		}
	}

	stored := []*revenuefact.RevenueFact{row("2026-05-01", "10"), row("2026-05-02", "20")}

	assert.Empty(t, changedRows([]*revenuefact.RevenueFact{row("2026-05-01", "10"), row("2026-05-02", "20")}, stored),
		"an identical recompute must write nothing")

	changed := changedRows([]*revenuefact.RevenueFact{row("2026-05-01", "10"), row("2026-05-02", "25")}, stored)
	require.Len(t, changed, 1)
	assert.Equal(t, "25", changed[0].NetAmount.String(), "only the day that moved is written")

	// A value falling back to zero MUST be written: the upsert never deletes,
	// so withholding it would leave the old non-zero row standing as revenue
	// that no reconciliation catches.
	zeroed := changedRows([]*revenuefact.RevenueFact{row("2026-05-01", "0")}, stored)
	require.Len(t, zeroed, 1, "a value returning to zero is a change, not a no-op")

	// A new day has no stored counterpart.
	assert.Len(t, changedRows([]*revenuefact.RevenueFact{row("2026-05-03", "5")}, stored), 1)

	// Nothing stored yet: everything is new.
	assert.Len(t, changedRows([]*revenuefact.RevenueFact{row("2026-05-01", "10")}, nil), 1)

	// Same grain, different non-money field still counts as a change.
	entitled := row("2026-05-01", "10")
	entitled.EntitlementQty = decimal.NewFromInt(50)
	assert.Len(t, changedRows([]*revenuefact.RevenueFact{entitled}, stored), 1,
		"entitlement movement is a change even when net is identical")
}

// TestChangedRows_EmptyRows: a row carrying nothing is written only when it
// corrects a stored row that carried something. Creating one otherwise records
// that a line item existed and delivered nothing — on production that was
// 13,464,148 of 13,465,791 rows.
func TestChangedRows_EmptyRows(t *testing.T) {
	empty := func(day string) *revenuefact.RevenueFact {
		d, err := time.Parse("2006-01-02", day)
		require.NoError(t, err)
		return &revenuefact.RevenueFact{
			PriceID: lo.ToPtr("price_1"), SubLineItemID: lo.ToPtr("sli_1"),
			Day: d, RevenueSource: types.RevenueSourceUsage,
		}
	}
	paid := func(day, net string) *revenuefact.RevenueFact {
		r := empty(day)
		r.NetAmount = decimal.RequireFromString(net)
		return r
	}

	// Nothing stored: an empty row is not worth creating.
	assert.Empty(t, changedRows([]*revenuefact.RevenueFact{empty("2026-05-01")}, nil),
		"an empty row with nothing to correct must not be written")

	// A row that previously carried value and now does not MUST be written:
	// the upsert never deletes, so skipping it strands stale revenue.
	corrected := changedRows(
		[]*revenuefact.RevenueFact{empty("2026-05-01")},
		[]*revenuefact.RevenueFact{paid("2026-05-01", "10")},
	)
	require.Len(t, corrected, 1, "a value returning to zero must still be written")

	// An already-empty stored row is unchanged, so nothing is written.
	assert.Empty(t, changedRows(
		[]*revenuefact.RevenueFact{empty("2026-05-01")},
		[]*revenuefact.RevenueFact{empty("2026-05-01")},
	))

	// Rows that carry something are unaffected.
	assert.Len(t, changedRows([]*revenuefact.RevenueFact{paid("2026-05-02", "5")}, nil), 1)

	// Free usage is not "nothing": zero net with a consumed entitlement is how
	// entitlement coverage stays visible.
	entitled := empty("2026-05-03")
	entitled.EntitlementQty = decimal.NewFromInt(100)
	assert.Len(t, changedRows([]*revenuefact.RevenueFact{entitled}, nil), 1,
		"entitlement-covered usage must still be recorded")

	// A revert posts even at zero so a void nets out.
	zeroRevert := empty("2026-05-04")
	zeroRevert.IsRevert = true
	assert.Len(t, changedRows([]*revenuefact.RevenueFact{zeroRevert}, nil), 1)
}
