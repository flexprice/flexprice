package service

import (
	"context"
	"testing"
	"time"

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
	assert.Equal(t, types.Marginal, decompositionMode(graduated(t), countMeter(t)))
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
