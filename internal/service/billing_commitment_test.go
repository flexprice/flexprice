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

// TestApplyWindowCommitment_PerBucket_PerWindow verifies that commitment is
// applied PER WINDOW: each window inside a bucket uses that bucket's price +
// commitment independently, and out-of-bucket windows use the line item's own
// commitment.
//
//	bucket [09:00,12:00): $2/unit, amount commitment $5/window, overage 2x, no true-up
//	line item: $1/unit, amount commitment $10/window, overage 2x, true-up ON
//	  09:00 in-bucket, usage 10 → 10×$2=$20 ≥ $5 → $5 + ($15×2) = $35
//	  10:00 in-bucket, usage  1 →  1×$2=$2  < $5, no true-up → $2
//	  18:00 out-of-bucket, usage 3 → 3×$1=$3 < $10, line-item true-up → $10
//	total = $47 (utilized $10 + overage $30 + true-up $7)
func TestApplyWindowCommitment_PerBucket_PerWindow(t *testing.T) {
	ctx := testutil.SetupContext()
	calc, priceStore := newCommitmentCalculatorWithPriceStore(t)

	bucketPriceID := "price_bkt_2"
	require.NoError(t, priceStore.Create(ctx, &price.Price{
		ID: bucketPriceID, Amount: decimal.NewFromInt(2), Currency: "usd",
		Type: types.PRICE_TYPE_USAGE, BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}))

	overage := decimal.NewFromInt(2)
	liCommit := decimal.NewFromInt(10)
	lineItem := &subscription.SubscriptionLineItem{
		ID:                      "li_per_window",
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        &liCommit,
		CommitmentOverageFactor: &overage,
		CommitmentTrueUpEnabled: true,
		CommitmentWindowed:      true,
		CommitmentTimeBuckets: types.TimeOfDayBuckets{{
			ID: "b1", Start: types.Bucket{Hour: 9}, End: types.Bucket{Hour: 12},
			PriceID: bucketPriceID, CommitmentType: types.COMMITMENT_TYPE_AMOUNT,
			CommitmentValue: decimal.NewFromInt(5), OverageFactor: &overage, TrueUpEnabled: false,
		}},
	}
	lineItemPrice := flatFeePrice(decimal.NewFromInt(1))

	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	starts := []time.Time{day.Add(9 * time.Hour), day.Add(10 * time.Hour), day.Add(18 * time.Hour)}
	values := []decimal.Decimal{decimal.NewFromInt(10), decimal.NewFromInt(1), decimal.NewFromInt(3)}

	total, info, err := calc.applyWindowCommitmentToLineItem(ctx, lineItem, values, starts, lineItemPrice)
	require.NoError(t, err)
	require.NotNil(t, info)

	assert.True(t, decimal.NewFromInt(47).Equal(total), "expected $47 got %s", total)
	assert.True(t, info.IsWindowed)
	assert.True(t, decimal.NewFromInt(10).Equal(info.ComputedCommitmentUtilizedAmount), "utilized %s", info.ComputedCommitmentUtilizedAmount)
	assert.True(t, decimal.NewFromInt(30).Equal(info.ComputedOverageAmount), "overage %s", info.ComputedOverageAmount)
	assert.True(t, decimal.NewFromInt(7).Equal(info.ComputedTrueUpAmount), "true-up %s", info.ComputedTrueUpAmount)
	// Sum invariant: charge == utilized + overage + true-up.
	assert.True(t, total.Equal(info.ComputedCommitmentUtilizedAmount.Add(info.ComputedOverageAmount).Add(info.ComputedTrueUpAmount)))
}

// TestApplyWindowCommitment_PerBucket_QuantityAndBaseRate verifies a QUANTITY-typed
// bucket commitment and the out-of-bucket base-rate fallback when the line item
// carries no commitment.
//
//	bucket [00:00,06:00): $1/unit, quantity commitment 4 units, overage 2x
//	line item: $3/unit, NO commitment
//	  02:00 in-bucket, usage 10 → 10×$1=$10, commit 4×$1=$4 → $4 + ($6×2) = $16
//	  10:00 out-of-bucket, usage 5 → 5×$3=$15 base (no commitment)
//	total = $31
func TestApplyWindowCommitment_PerBucket_QuantityAndBaseRate(t *testing.T) {
	ctx := testutil.SetupContext()
	calc, priceStore := newCommitmentCalculatorWithPriceStore(t)

	bucketPriceID := "price_bkt_q"
	require.NoError(t, priceStore.Create(ctx, &price.Price{
		ID: bucketPriceID, Amount: decimal.NewFromInt(1), Currency: "usd",
		Type: types.PRICE_TYPE_USAGE, BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}))

	overage := decimal.NewFromInt(2)
	lineItem := &subscription.SubscriptionLineItem{
		ID:                 "li_qty_base",
		CommitmentWindowed: true, // buckets bill even without a top-level commitment
		CommitmentTimeBuckets: types.TimeOfDayBuckets{{
			ID: "bq", Start: types.Bucket{Hour: 0}, End: types.Bucket{Hour: 6},
			PriceID: bucketPriceID, CommitmentType: types.COMMITMENT_TYPE_QUANTITY,
			CommitmentValue: decimal.NewFromInt(4), OverageFactor: &overage,
		}},
	}
	lineItemPrice := flatFeePrice(decimal.NewFromInt(3))

	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	starts := []time.Time{day.Add(2 * time.Hour), day.Add(10 * time.Hour)}
	values := []decimal.Decimal{decimal.NewFromInt(10), decimal.NewFromInt(5)}

	total, info, err := calc.applyWindowCommitmentToLineItem(ctx, lineItem, values, starts, lineItemPrice)
	require.NoError(t, err)
	require.NotNil(t, info)

	assert.True(t, decimal.NewFromInt(31).Equal(total), "expected $31 got %s", total)
	// utilized: $4 (bucket commit) + $15 (out-of-bucket base) = $19; overage: $12; true-up: 0
	assert.True(t, decimal.NewFromInt(19).Equal(info.ComputedCommitmentUtilizedAmount), "utilized %s", info.ComputedCommitmentUtilizedAmount)
	assert.True(t, decimal.NewFromInt(12).Equal(info.ComputedOverageAmount), "overage %s", info.ComputedOverageAmount)
	assert.True(t, info.ComputedTrueUpAmount.IsZero(), "true-up %s", info.ComputedTrueUpAmount)
}

// TestApplyWindowCommitment_PerBucket_PriceOverrideFromWindowSize proves that
// the bucket's SUBSCRIPTION-scoped price OVERRIDES the line item price for
// in-bucket windows, with the window grid generated from a window-size input
// (types.WindowSizeHour) through the real fillBucketedValuesForWindowedCommitment
// path — not hand-built slices.
//
//	period:    2026-01-01 → 2026-01-02 (24 hourly windows, zero-filled)
//	bucket:    [09:00,12:00), $5/u (override), amount commit $50/window, overage 1x
//	line item: $1/u, no commitment (true-up flag on only to engage the fill grid)
//	usage:     09:00=60u, 10:00=40u (in-bucket); 00:00=30u (out-of-bucket)
//
//	09:00 → 60×$5=$300 ≥ $50 → $50+($250×1) = $300
//	10:00 → 40×$5=$200 ≥ $50 → $200
//	00:00 → 30×$1 = $30 (line item base rate)
//	21 empty windows → $0
//
// Total = $530. Had the override NOT applied, in-bucket would be 100u×$1=$100
// and the total $130 — so $530 is unambiguous proof.
func TestApplyWindowCommitment_PerBucket_PriceOverrideFromWindowSize(t *testing.T) {
	ctx := testutil.SetupContext()
	calc, priceStore := newCommitmentCalculatorWithPriceStore(t)

	bucketPriceID := "price_override_5"
	require.NoError(t, priceStore.Create(ctx, &price.Price{
		ID: bucketPriceID, Amount: decimal.NewFromInt(5), Currency: "usd",
		Type: types.PRICE_TYPE_USAGE, BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}))

	overageOne := decimal.NewFromInt(1)
	lineItem := &subscription.SubscriptionLineItem{
		ID:                 "li_override_ws",
		CommitmentWindowed: true,
		// Top-level true-up flag with NO top-level commitment: nothing can be trued
		// up, so the fill emits only the windows with real usage (the bucket here
		// has no true-up either). The override still applies per usage window.
		CommitmentTrueUpEnabled: true,
		CommitmentTimeBuckets: types.TimeOfDayBuckets{{
			ID: "bkt_morning", Start: types.Bucket{Hour: 9}, End: types.Bucket{Hour: 12},
			PriceID: bucketPriceID, CommitmentType: types.COMMITMENT_TYPE_AMOUNT,
			CommitmentValue: decimal.NewFromInt(50), OverageFactor: &overageOne,
		}},
	}
	lineItemPrice := flatFeePrice(decimal.NewFromInt(1))

	periodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 0, 1)

	usageResult := &events.AggregationResult{
		Type: types.AggregationSum,
		Results: []events.UsageResult{
			{WindowSize: periodStart.Add(9 * time.Hour), Value: decimal.NewFromInt(60)},
			{WindowSize: periodStart.Add(10 * time.Hour), Value: decimal.NewFromInt(40)},
			{WindowSize: periodStart, Value: decimal.NewFromInt(30)},
		},
	}

	// Drive the grid from the window size input (HOUR). Only the three windows with
	// real usage are emitted — no empty window here can true-up.
	bs := &billingService{}
	windowValues, windowStarts := bs.fillBucketedValuesForWindowedCommitment(
		lineItem, usageResult, periodStart, periodEnd, types.WindowSizeHour, nil, types.AggregationSum)
	require.Len(t, windowStarts, 3, "only the windows with real usage are emitted")

	total, info, err := calc.applyWindowCommitmentToLineItem(ctx, lineItem, windowValues, windowStarts, lineItemPrice)
	require.NoError(t, err)
	require.NotNil(t, info)

	assert.True(t, decimal.NewFromInt(530).Equal(total),
		"expected $530 (bucket price override applied per window); got %s — $130 would mean the override was ignored", total)
	assert.True(t, decimal.NewFromInt(400).Equal(info.ComputedOverageAmount),
		"expected overage $400 from the $5 override base, got %s", info.ComputedOverageAmount)
	assert.True(t, decimal.NewFromInt(130).Equal(info.ComputedCommitmentUtilizedAmount),
		"expected utilized $130, got %s", info.ComputedCommitmentUtilizedAmount)
	assert.True(t, info.ComputedTrueUpAmount.IsZero(),
		"expected zero true-up, got %s", info.ComputedTrueUpAmount)
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

// TestFillBucketedValuesForWindowedCommitment verifies which windows the
// invoice-billing fill emits: every window with real usage, plus EMPTY windows
// only where commitment can actually true-up (inside a true-up bucket, or
// out-of-bucket only when the line item's own commitment has true-up). Empty
// windows that cannot charge are not synthesized.
//
// All cases use a 24-hour period (24 hourly windows), bucket [09:00,12:00), and a
// single usage event at 10:00 (in-bucket).
func TestFillBucketedValuesForWindowedCommitment(t *testing.T) {
	overage2x := decimal.NewFromInt(2)
	periodStart := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)
	hourTrueUpCommit := decimal.NewFromInt(10)

	usage := &events.AggregationResult{
		Type: types.AggregationSum,
		Results: []events.UsageResult{
			{WindowSize: time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), Value: decimal.NewFromInt(5)},
		},
	}
	bucket := func(trueUp bool) types.TimeOfDayBuckets {
		return types.TimeOfDayBuckets{{
			ID: "b1", Start: types.Bucket{Hour: 9}, End: types.Bucket{Hour: 12},
			CommitmentType: types.COMMITMENT_TYPE_AMOUNT, CommitmentValue: decimal.NewFromInt(5),
			OverageFactor: &overage2x, TrueUpEnabled: trueUp,
		}}
	}

	tests := []struct {
		name      string
		item      *subscription.SubscriptionLineItem
		wantHours []int // expected window start hours, in order
	}{
		{
			// Only in-bucket windows are filled: 09 (empty), 10 (usage), 11 (empty).
			// Out-of-bucket empties bill $0 and must NOT be synthesized.
			name: "bucket-only true-up fills only in-bucket windows",
			item: &subscription.SubscriptionLineItem{
				ID: "li_bucket_trueup", CommitmentWindowed: true, CommitmentTrueUpEnabled: false,
				CommitmentTimeBuckets: bucket(true),
			},
			wantHours: []int{9, 10, 11},
		},
		{
			// Top-level commitment + true-up commits 24/7 → every empty window fills.
			name: "top-level true-up fills every window",
			item: &subscription.SubscriptionLineItem{
				ID: "li_top_trueup", CommitmentWindowed: true,
				CommitmentType: types.COMMITMENT_TYPE_AMOUNT, CommitmentAmount: &hourTrueUpCommit,
				CommitmentOverageFactor: &overage2x, CommitmentTrueUpEnabled: true,
			},
			wantHours: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23},
		},
		{
			// No true-up anywhere → early return, only the used window.
			name: "no true-up returns only used windows",
			item: &subscription.SubscriptionLineItem{
				ID: "li_no_trueup", CommitmentWindowed: true, CommitmentTimeBuckets: bucket(false),
			},
			wantHours: []int{10},
		},
	}

	s := &billingService{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			values, starts := s.fillBucketedValuesForWindowedCommitment(
				tc.item, usage, periodStart, periodEnd, types.WindowSizeHour, &periodStart, types.AggregationSum)

			require.Len(t, values, len(tc.wantHours))
			require.Len(t, starts, len(tc.wantHours))
			gotHours := make([]int, len(starts))
			for i, st := range starts {
				gotHours[i] = st.Hour()
			}
			require.Equal(t, tc.wantHours, gotHours, "filled window hours")
		})
	}
}
