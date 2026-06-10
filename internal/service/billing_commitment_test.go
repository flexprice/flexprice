package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateBucketStarts_EmptyRange(t *testing.T) {
	start := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeDay, nil)
	assert.Nil(t, out)

	end2 := time.Date(2024, 1, 9, 0, 0, 0, 0, time.UTC)
	out2 := generateBucketStarts(start, end2, types.WindowSizeDay, nil)
	assert.Nil(t, out2)
}

func TestGenerateBucketStarts_Day(t *testing.T) {
	start := time.Date(2024, 1, 10, 12, 30, 0, 0, time.UTC)
	end := time.Date(2024, 1, 13, 0, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeDay, nil)
	require.Len(t, out, 3)
	assert.True(t, out[0].Equal(time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[1].Equal(time.Date(2024, 1, 11, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[2].Equal(time.Date(2024, 1, 12, 0, 0, 0, 0, time.UTC)))
}

func TestGenerateBucketStarts_Hour(t *testing.T) {
	start := time.Date(2024, 1, 10, 1, 30, 0, 0, time.UTC)
	end := time.Date(2024, 1, 10, 5, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeHour, nil)
	require.Len(t, out, 4)
	assert.True(t, out[0].Equal(time.Date(2024, 1, 10, 1, 0, 0, 0, time.UTC)))
	assert.True(t, out[3].Equal(time.Date(2024, 1, 10, 4, 0, 0, 0, time.UTC)))
}

func TestGenerateBucketStarts_Month_NoAnchor(t *testing.T) {
	// No anchor: buckets align to period start (e.g. subscription created 15 Jan → 15 Jan, 15 Feb, 15 Mar)
	start := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeMonth, nil)
	require.Len(t, out, 3)
	assert.True(t, out[0].Equal(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[1].Equal(time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[2].Equal(time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)))
}

func TestGenerateBucketStarts_Month_NoAnchor_SubscriptionCreated5Feb(t *testing.T) {
	// Calendar subscription created 5 Feb: period 5 Feb - 5 Mar; buckets align to period start (5th)
	start := time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeMonth, nil)
	require.Len(t, out, 1)
	assert.True(t, out[0].Equal(time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC)))
}

func TestGenerateBucketStarts_Month_WithAnchor(t *testing.T) {
	// Anchor 5th: periods are 5th - 5th
	anchor := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	start := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC) // in period Jan 5 - Feb 5
	end := time.Date(2024, 3, 10, 0, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeMonth, &anchor)
	require.Len(t, out, 3)
	assert.True(t, out[0].Equal(time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[1].Equal(time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[2].Equal(time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)))
}

func TestGenerateBucketStarts_Month_WithAnchor_StartBeforeAnchorDay(t *testing.T) {
	// Start is Jan 3; anchor 5th -> period containing Jan 3 is Dec 5 - Jan 5
	anchor := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	start := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 6, 0, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeMonth, &anchor)
	require.Len(t, out, 3)
	assert.True(t, out[0].Equal(time.Date(2023, 12, 5, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[1].Equal(time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[2].Equal(time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC)))
}

// newCommitmentCalculatorForTest builds a calculator with a real PriceService
// backed by in-memory stores so flat-fee CalculateCost works deterministically.
func newCommitmentCalculatorForTest(t *testing.T) *commitmentCalculator {
	t.Helper()
	log := logger.NewNoopLogger()
	params := ServiceParams{
		Logger:        log,
		DB:            testutil.NewMockPostgresClient(log),
		PriceRepo:     testutil.NewInMemoryPriceStore(),
		MeterRepo:     testutil.NewInMemoryMeterStore(),
		PlanRepo:      testutil.NewInMemoryPlanStore(),
		PriceUnitRepo: testutil.NewInMemoryPriceUnitStore(),
		AddonRepo:     testutil.NewInMemoryAddonStore(),
		SubRepo:       testutil.NewInMemorySubscriptionStore(),
	}
	return newCommitmentCalculator(log, NewPriceService(params))
}

// flatFeePrice returns a flat-fee USAGE price priced at unitAmount per unit.
// Used by windowed-commitment tests where CalculateCost must be linear.
func flatFeePrice(unitAmount decimal.Decimal) *price.Price {
	amt := unitAmount
	return &price.Price{
		ID:           "price_flat_test",
		Amount:       amt,
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
}

// TestApplyWindowCommitment_TimeBuckets_NilBucketStarts confirms that when the
// caller passes nil bucketStarts (single-bucket / period-aggregate path), the
// time-bucket filter is skipped — commitment applies 24/7 as if no buckets
// were configured. Documented behavior in applyWindowCommitmentToLineItem.
func TestApplyWindowCommitment_TimeBuckets_NilBucketStarts(t *testing.T) {
	ctx := testutil.SetupContext()
	calc := newCommitmentCalculatorForTest(t)

	commitmentAmount := decimal.NewFromInt(10)
	overageFactor := decimal.NewFromInt(2)
	lineItem := &subscription.SubscriptionLineItem{
		ID:                      "sub_li_tb_nil_starts",
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        &commitmentAmount,
		CommitmentOverageFactor: &overageFactor,
		CommitmentTrueUpEnabled: true,
		CommitmentWindowed:      true,
		// Buckets configured, but the caller signals "no per-window timestamps".
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{Start: types.Bucket{Hour: 9, Minute: 0}, End: types.Bucket{Hour: 17, Minute: 0}},
		},
	}

	p := flatFeePrice(decimal.NewFromInt(2)) // $2/unit
	bucketedValues := []decimal.Decimal{
		decimal.NewFromInt(2), // cost $4 → true-up to $10
		decimal.NewFromInt(8), // cost $16 → $10 + $12 = $22
	}

	total, _, err := calc.applyWindowCommitmentToLineItem(ctx, lineItem, bucketedValues, nil, p)
	require.NoError(t, err)

	// Both windows get commitment treatment (no filtering): $10 + $22 = $32.
	assert.True(t, decimal.NewFromInt(32).Equal(total),
		"expected total $32 with nil bucketStarts (filter skipped), got %s", total)
}

// TestApplyWindowCommitment_TimeBuckets_NoBucketsConfigured guards against
// regressions where an empty TimeOfDayBuckets accidentally filters everything.
// Empty TimeBuckets must be the "no restriction" sentinel — every window
// goes through the normal commitment path.
func TestApplyWindowCommitment_TimeBuckets_NoBucketsConfigured(t *testing.T) {
	ctx := testutil.SetupContext()
	calc := newCommitmentCalculatorForTest(t)

	commitmentAmount := decimal.NewFromInt(10)
	overageFactor := decimal.NewFromInt(2)
	lineItem := &subscription.SubscriptionLineItem{
		ID:                      "sub_li_no_buckets",
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        &commitmentAmount,
		CommitmentOverageFactor: &overageFactor,
		CommitmentTrueUpEnabled: true,
		CommitmentWindowed:      true,
		// No TimeBuckets configured.
	}

	p := flatFeePrice(decimal.NewFromInt(2))

	// All hours of the day should get commitment treatment.
	day := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)
	bucketStarts := []time.Time{day.Add(3 * time.Hour), day.Add(20 * time.Hour)}
	bucketedValues := []decimal.Decimal{decimal.NewFromInt(2), decimal.NewFromInt(2)}

	total, _, err := calc.applyWindowCommitmentToLineItem(ctx, lineItem, bucketedValues, bucketStarts, p)
	require.NoError(t, err)
	// Both windows true-up to $10 → $20 total.
	assert.True(t, decimal.NewFromInt(20).Equal(total),
		"expected $20 total when no time buckets configured, got %s", total)
}

func TestChargeAtQty_FlatFee(t *testing.T) {
	ctx := testutil.SetupContext()
	calc := newCommitmentCalculatorForTest(t)

	p := flatFeePrice(decimal.NewFromInt(2))
	got := calc.chargeAtQty(ctx, p, decimal.NewFromInt(5))
	assert.True(t, got.Equal(decimal.NewFromInt(10)), "expected 10 got %s", got.String())
}

func TestBucketIndexAtWindowStart(t *testing.T) {
	buckets := types.TimeOfDayBuckets{
		{Start: types.Bucket{Hour: 9}, End: types.Bucket{Hour: 17}},
		{Start: types.Bucket{Hour: 22}, End: types.Bucket{Hour: 6}}, // midnight wrap
	}
	at := func(h, m int) time.Time {
		return time.Date(2026, 1, 1, h, m, 0, 0, time.UTC)
	}
	assert.Equal(t, 0, bucketIndexAtWindowStart(buckets, at(10, 0)))
	assert.Equal(t, 1, bucketIndexAtWindowStart(buckets, at(23, 0)))
	assert.Equal(t, 1, bucketIndexAtWindowStart(buckets, at(2, 0)))
	assert.Equal(t, -1, bucketIndexAtWindowStart(buckets, at(7, 0)))
}

// newCommitmentCalculatorWithPriceStore is like newCommitmentCalculatorForTest but
// also returns the in-memory price store so tests can pre-populate bucket prices.
func newCommitmentCalculatorWithPriceStore(t *testing.T) (*commitmentCalculator, *testutil.InMemoryPriceStore) {
	t.Helper()
	log := logger.NewNoopLogger()
	priceStore := testutil.NewInMemoryPriceStore()
	params := ServiceParams{
		Logger:        log,
		DB:            testutil.NewMockPostgresClient(log),
		PriceRepo:     priceStore,
		MeterRepo:     testutil.NewInMemoryMeterStore(),
		PlanRepo:      testutil.NewInMemoryPlanStore(),
		PriceUnitRepo: testutil.NewInMemoryPriceUnitStore(),
		AddonRepo:     testutil.NewInMemoryAddonStore(),
		SubRepo:       testutil.NewInMemorySubscriptionStore(),
	}
	return newCommitmentCalculator(log, NewPriceService(params)), priceStore
}

// TestApplyWindowCommitment_PerBucketDispatch_FlatFee verifies that when a bucket
// has per-bucket pricing (PriceID + HasCommitment()), windows are grouped by bucket,
// aggregated across the cycle, and the bucket's own commitment math is applied.
//
// Setup:
//
//	line item:  top-level commitment=0 (no out-of-bucket commitment)
//	bucket:     [09:00, 17:00), FLAT_FEE $2/unit, commitment_type=amount,
//	            commitment_value=$100, true_up_enabled=true, overage_factor=1
//	24 hourly windows:
//	  - hours 9..16 (8 windows) in-bucket, usage=10 each  → 80 units in-bucket total
//	  - remaining 16 windows out-of-bucket, usage=5 each  → 80 units out-of-bucket total
//
// In-bucket:
//
//	80 units × $2/unit = $160 actual > $100 commitment
//	overage = ($160 − $100) × 1 = $60
//	charge  = $100 + $60 = $160
//
// Out-of-bucket:
//
//	line item has 0 commitment → actual charge only = 80 × $1 (line item flat $1) = $80
//
// Total = $160 + $80 = $240
func TestApplyWindowCommitment_PerBucketDispatch_FlatFee(t *testing.T) {
	ctx := testutil.SetupContext()
	calc, priceStore := newCommitmentCalculatorWithPriceStore(t)

	// Bucket price: $2/unit flat-fee
	bucketPriceID := "price_bucket_2_per_unit"
	bucketPriceObj := &price.Price{
		ID:           bucketPriceID,
		Amount:       decimal.NewFromInt(2),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
	require.NoError(t, priceStore.Create(ctx, bucketPriceObj))

	commitmentVal := decimal.NewFromInt(100)
	overageFactorBucket := decimal.NewFromFloat(1.0)

	// Line item: out-of-bucket uses $1/unit flat (no commitment at line-item level).
	liCommitmentAmount := decimal.Zero
	liOverageFactor := decimal.NewFromFloat(1.0)
	lineItem := &subscription.SubscriptionLineItem{
		ID:                      "li_per_bucket_flat",
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        &liCommitmentAmount,
		CommitmentOverageFactor: &liOverageFactor,
		CommitmentTrueUpEnabled: false,
		CommitmentWindowed:      true,
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 17, Minute: 0},
				PriceID:         bucketPriceID,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: commitmentVal,
				OverageFactor:   &overageFactorBucket,
				TrueUpEnabled:   true,
			},
		},
	}

	// Line item price: $1/unit (used for out-of-bucket windows)
	lineItemPrice := flatFeePrice(decimal.NewFromInt(1))

	// Build 24 hourly windows: hours 0..23, usage=10 in-bucket (9..16), usage=5 outside.
	day := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	bucketStarts := make([]time.Time, 24)
	bucketedValues := make([]decimal.Decimal, 24)
	for h := 0; h < 24; h++ {
		bucketStarts[h] = day.Add(time.Duration(h) * time.Hour)
		if h >= 9 && h < 17 {
			bucketedValues[h] = decimal.NewFromInt(10) // in-bucket: 8 windows × 10 = 80 units
		} else {
			bucketedValues[h] = decimal.NewFromInt(5) // out-of-bucket: 16 windows × 5 = 80 units
		}
	}

	total, info, err := calc.applyWindowCommitmentToLineItem(ctx, lineItem, bucketedValues, bucketStarts, lineItemPrice)
	require.NoError(t, err)
	require.NotNil(t, info)

	// In-bucket: 80 units × $2 = $160 > $100 → charge = $100 + ($60 × 1) = $160
	// Out-of-bucket: 80 units × $1 = $80, commitment=0 → charge = $80 (actual ≥ 0)
	// Total = $240
	assert.True(t, decimal.NewFromInt(240).Equal(total),
		"expected $240 total (per-bucket dispatch flat fee), got %s", total)
	assert.True(t, info.IsWindowed)

	// In-bucket overage: $60; out-of-bucket overage: 0 (since commitment=0, entire charge is "committed")
	assert.True(t, decimal.NewFromInt(60).Equal(info.ComputedOverageAmount),
		"expected in-bucket overage $60, got %s", info.ComputedOverageAmount)

	// No true-up should fire: in-bucket exceeded commitment; out-of-bucket has no true-up.
	assert.True(t, info.ComputedTrueUpAmount.Equal(decimal.Zero),
		"expected zero true-up, got %s", info.ComputedTrueUpAmount)
}

// TestApplyWindowCommitment_PerBucket_PriceOverrideFromWindowSize proves that the
// SUBSCRIPTION-scoped price carried on a bucket actually OVERRIDES the line item
// price for in-bucket windows — and that the whole thing is driven by a window
// size provided as input (not hand-built window grids).
//
// The window grid is generated from types.WindowSizeHour via the real
// fillBucketedValuesForWindowedCommitment path, then fed to the commitment
// calculator. Because the bucket commitment uses overage_factor=1, the in-bucket
// charge collapses to baseCharge = usage × <bucket price>, so the total directly
// reflects which price was used.
//
// Setup:
//
//	period:        2026-01-01 00:00 → 2026-01-02 00:00 UTC (24 hourly windows)
//	window size:   HOUR (drives the grid)
//	bucket:        [09:00, 12:00), FLAT_FEE $5/unit (override), amount commitment
//	               $50, overage_factor=1, true_up=false
//	line item:     FLAT_FEE $1/unit, top-level commitment=0
//	usage:         hour 09 = 60u, hour 10 = 40u (in-bucket, 100u total)
//	               hour 00 = 30u (out-of-bucket)
//
// In-bucket:  100u × $5 (override) = $500 base ≥ $50 commitment
//
//	overage = ($500 − $50) × 1 = $450 → charge = $50 + $450 = $500
//
// Out-of-bucket: 30u × $1 (line item) = $30, no commitment → $30
//
// Total = $530. (Had the override NOT been applied, in-bucket would be
// 100u × $1 = $100 and the total would be $130 — so $530 is unambiguous proof.)
func TestApplyWindowCommitment_PerBucket_PriceOverrideFromWindowSize(t *testing.T) {
	ctx := testutil.SetupContext()
	calc, priceStore := newCommitmentCalculatorWithPriceStore(t)

	// Bucket price override: $5/unit flat-fee.
	bucketPriceID := "price_bucket_override_5"
	require.NoError(t, priceStore.Create(ctx, &price.Price{
		ID:           bucketPriceID,
		Amount:       decimal.NewFromInt(5),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}))

	bucketCommitment := decimal.NewFromInt(50)
	overageFactor := decimal.NewFromInt(1)
	liCommitment := decimal.Zero

	lineItem := &subscription.SubscriptionLineItem{
		ID:                      "li_price_override",
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        &liCommitment, // out-of-bucket: no commitment
		CommitmentOverageFactor: &overageFactor,
		CommitmentWindowed:      true,
		// True-up at the line-item level only makes the fill path generate the
		// full hourly grid (zero-filling empty windows); out-of-bucket commitment
		// is zero, so no true-up actually fires there.
		CommitmentTrueUpEnabled: true,
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{
				ID:              "bucket_morning",
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 12, Minute: 0},
				PriceID:         bucketPriceID,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: bucketCommitment,
				OverageFactor:   &overageFactor,
				TrueUpEnabled:   false,
			},
		},
	}

	// Line item price: $1/unit — must NOT be used for in-bucket windows.
	lineItemPrice := flatFeePrice(decimal.NewFromInt(1))

	periodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 0, 1)

	// Raw windowed usage, keyed by the hour-window start. The window starts here
	// must line up with the HOUR grid that fill will generate.
	usageResult := &events.AggregationResult{
		Type: types.AggregationSum,
		Results: []events.UsageResult{
			{WindowSize: periodStart.Add(9 * time.Hour), Value: decimal.NewFromInt(60)},  // in-bucket
			{WindowSize: periodStart.Add(10 * time.Hour), Value: decimal.NewFromInt(40)}, // in-bucket
			{WindowSize: periodStart.Add(0 * time.Hour), Value: decimal.NewFromInt(30)},  // out-of-bucket
		},
	}

	// Drive the grid from the window size input (HOUR).
	bs := &billingService{}
	bucketedValues, bucketStarts := bs.fillBucketedValuesForWindowedCommitment(
		lineItem, usageResult, periodStart, periodEnd, types.WindowSizeHour, nil, types.AggregationSum)

	// 24 hourly windows generated from the window size, zero-filled where idle.
	require.Len(t, bucketStarts, 24, "HOUR window size over a day must yield 24 windows")
	require.Len(t, bucketedValues, 24)

	total, info, err := calc.applyWindowCommitmentToLineItem(ctx, lineItem, bucketedValues, bucketStarts, lineItemPrice)
	require.NoError(t, err)
	require.NotNil(t, info)

	// $500 in-bucket (override applied) + $30 out-of-bucket = $530.
	assert.True(t, decimal.NewFromInt(530).Equal(total),
		"expected $530 (bucket price override applied); got %s — $130 would mean the override was ignored", total)

	// In-bucket overage = ($500 − $50) × 1 = $450, proving the $5 override drove the base charge.
	assert.True(t, decimal.NewFromInt(450).Equal(info.ComputedOverageAmount),
		"expected in-bucket overage $450 from the $5 override base, got %s", info.ComputedOverageAmount)

	// Utilized = $50 bucket commitment + $30 out-of-bucket actual = $80.
	assert.True(t, decimal.NewFromInt(80).Equal(info.ComputedCommitmentUtilizedAmount),
		"expected utilized $80, got %s", info.ComputedCommitmentUtilizedAmount)

	assert.True(t, info.ComputedTrueUpAmount.Equal(decimal.Zero),
		"expected zero true-up, got %s", info.ComputedTrueUpAmount)
	assert.True(t, info.IsWindowed)
}

// TestApplyWindowCommitment_PerBucketDispatch_QuantityTrueUp verifies the
// QUANTITY-typed commitment path in applyWindowCommitmentPerBucket.
//
// Setup:
//
//	bucket: [09:00, 17:00), FLAT_FEE $0.80/unit,
//	        commitment_type=quantity, commitment_value=1000, true_up_enabled=true, overage_factor=1
//	actual in-bucket usage: 600 units
//
// Math:
//
//	usage charge   = 600 × $0.80 = $480
//	commit charge  = 1000 × $0.80 = $800
//	usage < commit → true-up = $800 − $480 = $320
//	total charge   = $800 (commit_charge, which includes true-up)
func TestApplyWindowCommitment_PerBucketDispatch_QuantityTrueUp(t *testing.T) {
	ctx := testutil.SetupContext()
	calc, priceStore := newCommitmentCalculatorWithPriceStore(t)

	// Bucket price: $0.80/unit flat-fee
	bucketPriceID := "price_bucket_080"
	bucketPriceObj := &price.Price{
		ID:           bucketPriceID,
		Amount:       decimal.NewFromFloat(0.80),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
	require.NoError(t, priceStore.Create(ctx, bucketPriceObj))

	commitmentQty := decimal.NewFromInt(1000)
	overageFactor := decimal.NewFromFloat(1.0)

	liCommitmentAmount := decimal.Zero
	liOverageFactor := decimal.NewFromFloat(1.0)
	lineItem := &subscription.SubscriptionLineItem{
		ID:                      "li_per_bucket_qty_trueup",
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        &liCommitmentAmount,
		CommitmentOverageFactor: &liOverageFactor,
		CommitmentTrueUpEnabled: false,
		CommitmentWindowed:      true,
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 17, Minute: 0},
				PriceID:         bucketPriceID,
				CommitmentType:  types.COMMITMENT_TYPE_QUANTITY,
				CommitmentValue: commitmentQty,
				OverageFactor:   &overageFactor,
				TrueUpEnabled:   true,
			},
		},
	}

	// Line item price: doesn't matter for this test (no out-of-bucket usage)
	lineItemPrice := flatFeePrice(decimal.NewFromInt(1))

	// 8 in-bucket hourly windows (hours 9..16), 75 units each → 600 total
	day := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	bucketStarts := make([]time.Time, 8)
	bucketedValues := make([]decimal.Decimal, 8)
	for i := 0; i < 8; i++ {
		bucketStarts[i] = day.Add(time.Duration(9+i) * time.Hour)
		bucketedValues[i] = decimal.NewFromInt(75) // 8 × 75 = 600 in-bucket units
	}

	total, info, err := calc.applyWindowCommitmentToLineItem(ctx, lineItem, bucketedValues, bucketStarts, lineItemPrice)
	require.NoError(t, err)
	require.NotNil(t, info)

	// usage charge = 600 × $0.80 = $480
	// commit charge = 1000 × $0.80 = $800
	// true-up = $800 − $480 = $320
	// total charge = $800
	expectedTotal := decimal.NewFromInt(800)
	assert.True(t, expectedTotal.Equal(total),
		"expected $800 total (quantity commitment true-up), got %s", total)

	expectedTrueUp := decimal.NewFromInt(320)
	assert.True(t, expectedTrueUp.Equal(info.ComputedTrueUpAmount),
		"expected true-up $320, got %s", info.ComputedTrueUpAmount)

	assert.True(t, info.ComputedOverageAmount.Equal(decimal.Zero),
		"expected zero overage, got %s", info.ComputedOverageAmount)

	assert.True(t, info.IsWindowed)
}

// TestApplyWindowCommitment_PerBucket_SumInvariant pins the invariant:
//
//	totalCharge == sum(per_bucket_subcharge) + out_of_bucket_subcharge
//	where each subcharge == utilized + overage + true_up
//
// Two buckets are configured in the same line item plus out-of-bucket usage.
// Bucket A (09:00–12:00, $2/unit, commitment=$10) has overage: 3 windows × 5
// units = 15 units × $2 = $30 > $10.
// Bucket B (18:00–22:00, $1/unit, commitment=$20, true-up) has under-usage:
// 4 windows × 2 units = 8 units × $1 = $8 < $20 → true-up fires.
// Out-of-bucket windows pay the line-item base rate ($1/unit) with no commitment.
//
// The invariant must hold by construction; this test locks it in to guard
// future refactors against sum-drift.
func TestApplyWindowCommitment_PerBucket_SumInvariant(t *testing.T) {
	ctx := testutil.SetupContext()
	calc, priceStore := newCommitmentCalculatorWithPriceStore(t)

	// Bucket A price: $2/unit flat-fee
	bucketPriceA := &price.Price{
		ID:           "price_buc_a_2_per_unit",
		Amount:       decimal.NewFromInt(2),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
	require.NoError(t, priceStore.Create(ctx, bucketPriceA))

	// Bucket B price: $1/unit flat-fee
	bucketPriceB := &price.Price{
		ID:           "price_buc_b_1_per_unit",
		Amount:       decimal.NewFromInt(1),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
	require.NoError(t, priceStore.Create(ctx, bucketPriceB))

	overageFactor := decimal.NewFromInt(1)

	// Line item: zero top-level commitment so out-of-bucket pays only base rate.
	liCommitmentAmount := decimal.Zero
	liOverageFactor := decimal.NewFromFloat(1.0)
	lineItem := &subscription.SubscriptionLineItem{
		ID:                      "li_sum",
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        &liCommitmentAmount,
		CommitmentOverageFactor: &liOverageFactor,
		CommitmentTrueUpEnabled: false,
		CommitmentWindowed:      true,
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{
				ID:              "buc_a",
				Start:           types.Bucket{Hour: 9},
				End:             types.Bucket{Hour: 12},
				PriceID:         bucketPriceA.ID,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(10), // overage: actual $30 > $10
				OverageFactor:   &overageFactor,
				TrueUpEnabled:   false,
			},
			{
				ID:              "buc_b",
				Start:           types.Bucket{Hour: 18},
				End:             types.Bucket{Hour: 22},
				PriceID:         bucketPriceB.ID,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(20), // true-up: actual $8 < $20
				OverageFactor:   &overageFactor,
				TrueUpEnabled:   true,
			},
		},
	}

	// Line item price: $1/unit used for out-of-bucket windows.
	lineItemPrice := flatFeePrice(decimal.NewFromInt(1))

	// 24 hourly windows; usage chosen to push bucket A over its commitment
	// (overage) and bucket B under its commitment (true-up).
	at := func(h int) time.Time { return time.Date(2026, 1, 1, h, 0, 0, 0, time.UTC) }
	starts := make([]time.Time, 24)
	values := make([]decimal.Decimal, 24)
	for h := 0; h < 24; h++ {
		starts[h] = at(h)
		switch {
		case h >= 9 && h < 12: // bucket A: 3 windows × 5 units = 15 units × $2 = $30 > $10 ⇒ overage
			values[h] = decimal.NewFromInt(5)
		case h >= 18 && h < 22: // bucket B: 4 windows × 2 units = 8 units × $1 = $8 < $20 ⇒ true-up
			values[h] = decimal.NewFromInt(2)
		default: // out-of-bucket: 17 windows × 1 unit × $1/unit = $17
			values[h] = decimal.NewFromInt(1)
		}
	}

	total, info, err := calc.applyWindowCommitmentToLineItem(ctx, lineItem, values, starts, lineItemPrice)
	require.NoError(t, err)
	require.NotNil(t, info)

	// In applyWindowCommitmentPerBucket, out-of-bucket usage with zero line-item
	// commitment is charged at base rate and added to totalUtilized (not a separate
	// bucket). Therefore, the full invariant is:
	//
	//   total == ComputedCommitmentUtilizedAmount + ComputedOverageAmount + ComputedTrueUpAmount
	//
	// Arithmetic:
	//   Bucket A: 15 units × $2 = $30 > $10 → charge=$30, util=$10, overage=$20, trueUp=0
	//   Bucket B:  8 units × $1 = $8  < $20 → charge=$20, util=$8,  overage=0,  trueUp=$12
	//   Out-of-bucket: 17 units × $1 = $17  → charge=$17, util=$17 (zero commitment path)
	//   total = $30 + $20 + $17 = $67
	//   sum   = ($10+$8+$17) + $20 + $12 = $35 + $20 + $12 = $67 ✓
	expectedSum := info.ComputedCommitmentUtilizedAmount.
		Add(info.ComputedOverageAmount).
		Add(info.ComputedTrueUpAmount)

	assert.True(t,
		total.Equal(expectedSum),
		"sum invariant violated: total=%s expected (utilized+overage+trueup)=%s "+
			"(utilized=%s overage=%s trueup=%s)",
		total.String(), expectedSum.String(),
		info.ComputedCommitmentUtilizedAmount.String(),
		info.ComputedOverageAmount.String(),
		info.ComputedTrueUpAmount.String(),
	)
}

// TestBilling_BucketCommitmentEndToEnd exercises the full commitment math flow:
//
//  1. A line item with one bucket (09:00–17:00) carries a flat-fee $0.80/unit
//     price and a QUANTITY commitment = 1000 with true-up enabled.
//  2. 24 hourly windows simulate a billing period:
//     - hours 9–16 (8 windows): 75 units each → 600 in-bucket units
//     - all other hours (16 windows): 5 units each → 80 out-of-bucket units
//  3. Out-of-bucket windows are billed at the line-item price ($0.50/unit).
//
// Expected totals:
//
//	In-bucket:
//	  actual charge = 600 × $0.80 = $480
//	  commit charge = 1000 × $0.80 = $800
//	  true-up       = $800 − $480  = $320  (usage < commitment)
//	  bucket charge = $800
//	Out-of-bucket:
//	  80 × $0.50 = $40
//	Grand total = $840
//
// NOTE: A future testcontainers-backed variant should additionally exercise
// persistence + ClickHouse aggregation. The in-memory test covers the service
// path end-to-end.
func TestBilling_BucketCommitmentEndToEnd(t *testing.T) {
	ctx := testutil.SetupContext()
	calc, priceStore := newCommitmentCalculatorWithPriceStore(t)

	// Bucket price: $0.80/unit flat-fee (registered in the in-memory store so
	// the calculator can resolve it when dispatching per-bucket commitment math).
	bucketPrice := &price.Price{
		ID:           "prc_peak",
		Amount:       decimal.NewFromFloat(0.80),
		Currency:     "usd",
		Type:         types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}
	require.NoError(t, priceStore.Create(ctx, bucketPrice))

	// Line-item price: $0.50/unit (used for out-of-bucket windows).
	lineItemPrice := flatFeePrice(decimal.NewFromFloat(0.50))
	lineItemPrice.ID = "prc_offpeak"

	overage := decimal.NewFromFloat(1.0)
	liCommitment := decimal.Zero
	lineItem := &subscription.SubscriptionLineItem{
		ID:                      "li_e2e",
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        &liCommitment,
		CommitmentOverageFactor: &overage,
		CommitmentTrueUpEnabled: false,
		CommitmentWindowed:      true,
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{
				ID:              "buc_peak",
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 17, Minute: 0},
				PriceID:         bucketPrice.ID,
				CommitmentType:  types.COMMITMENT_TYPE_QUANTITY,
				CommitmentValue: decimal.NewFromInt(1000),
				OverageFactor:   &overage,
				TrueUpEnabled:   true,
			},
		},
	}

	// Build 24 hourly windows for 2026-01-01.
	at := func(h int) time.Time { return time.Date(2026, 1, 1, h, 0, 0, 0, time.UTC) }
	starts := make([]time.Time, 24)
	values := make([]decimal.Decimal, 24)
	for h := 0; h < 24; h++ {
		starts[h] = at(h)
		if h >= 9 && h < 17 {
			// 8 in-bucket windows × 75 units = 600 total in-bucket units
			values[h] = decimal.NewFromInt(75)
		} else {
			// 16 out-of-bucket windows × 5 units = 80 total out-of-bucket units
			values[h] = decimal.NewFromInt(5)
		}
	}

	total, info, err := calc.applyWindowCommitmentToLineItem(ctx, lineItem, values, starts, lineItemPrice)
	require.NoError(t, err)
	require.NotNil(t, info)

	// Grand total: $800 (bucket, incl. true-up) + $40 (out-of-bucket) = $840.
	expected := decimal.NewFromInt(840)
	assert.True(t, expected.Equal(total),
		"expected grand total $840 got %s", total.String())

	// True-up = commit_charge($800) - usage_charge($480) = $320.
	expectedTrueUp := decimal.NewFromInt(320)
	assert.True(t, expectedTrueUp.Equal(info.ComputedTrueUpAmount),
		"expected true-up $320 got %s", info.ComputedTrueUpAmount.String())

	// No windows exceeded the quantity commitment, so no overage premium.
	assert.True(t, info.ComputedOverageAmount.IsZero(),
		"expected zero overage got %s", info.ComputedOverageAmount.String())

	// Sum invariant: total == utilized + overage + true_up
	// (out-of-bucket charge is folded into ComputedCommitmentUtilizedAmount
	// for zero-commitment line items, mirroring applyWindowCommitmentPerBucket).
	sumInfo := info.ComputedCommitmentUtilizedAmount.
		Add(info.ComputedOverageAmount).
		Add(info.ComputedTrueUpAmount)
	assert.True(t, sumInfo.Equal(total),
		"sum invariant violated: sum(utilized+overage+true_up)=%s total=%s",
		sumInfo.String(), total.String())

	assert.True(t, info.IsWindowed)
}

// TestApplyWindowCommitment_TimeBuckets_LengthMismatch verifies the defensive
// guard rejects misaligned slices instead of producing wrong charges silently.
func TestApplyWindowCommitment_TimeBuckets_LengthMismatch(t *testing.T) {
	ctx := testutil.SetupContext()
	calc := newCommitmentCalculatorForTest(t)

	commitmentAmount := decimal.NewFromInt(10)
	overageFactor := decimal.NewFromInt(2)
	lineItem := &subscription.SubscriptionLineItem{
		ID:                      "sub_li_mismatch",
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        &commitmentAmount,
		CommitmentOverageFactor: &overageFactor,
		CommitmentTrueUpEnabled: true,
		CommitmentWindowed:      true,
	}

	p := flatFeePrice(decimal.NewFromInt(2))
	bucketedValues := []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(2)}
	bucketStarts := []time.Time{time.Now().UTC()} // length 1 vs values length 2

	_, _, err := calc.applyWindowCommitmentToLineItem(ctx, lineItem, bucketedValues, bucketStarts, p)
	require.Error(t, err, "mismatched bucketStarts/bucketedValues must be rejected")
}
