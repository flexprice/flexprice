package service

import (
	"testing"
	"time"

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
	log := logger.GetLogger()
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

// TestApplyWindowCommitment_TimeBuckets_FilterOutOfBucketWindow exercises the
// invoice/billing pipeline contract: windows whose start hour falls outside the
// configured CommitmentTimeBuckets are billed at base usage rate, with no
// commitment credit, no overage premium, and no true-up.
func TestApplyWindowCommitment_TimeBuckets_FilterOutOfBucketWindow(t *testing.T) {
	ctx := testutil.SetupContext()
	calc := newCommitmentCalculatorForTest(t)

	commitmentAmount := decimal.NewFromInt(10) // $10 commitment per window
	overageFactor := decimal.NewFromInt(2)
	lineItem := &subscription.SubscriptionLineItem{
		ID:                      "sub_li_tb_test",
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        &commitmentAmount,
		CommitmentOverageFactor: &overageFactor,
		CommitmentTrueUpEnabled: true,
		CommitmentWindowed:      true,
		// Business hours window: only 09:00-17:00 UTC gets commitment treatment.
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{Start: types.Bucket{Hour: 9, Minute: 0}, End: types.Bucket{Hour: 17, Minute: 0}},
		},
	}

	unitAmount := decimal.NewFromInt(2) // $2/unit → 5 units == commitment
	p := flatFeePrice(unitAmount)

	// Three windows: two inside the business-hours bucket, one outside.
	day := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)
	bucketStarts := []time.Time{
		day.Add(10 * time.Hour), // 10:00 UTC — in-bucket, under-utilized (true-up fires)
		day.Add(18 * time.Hour), // 18:00 UTC — OUT-of-bucket (charge base rate only)
		day.Add(12 * time.Hour), // 12:00 UTC — in-bucket, over-utilized (overage fires)
	}
	bucketedValues := []decimal.Decimal{
		decimal.NewFromInt(2), // cost $4  → < commitment $10 → true-up to $10
		decimal.NewFromInt(2), // cost $4  → out-of-bucket → bill $4 (no true-up)
		decimal.NewFromInt(8), // cost $16 → > commitment $10 → $10 + ($6 * 2) = $22
	}

	total, info, err := calc.applyWindowCommitmentToLineItem(ctx, lineItem, bucketedValues, bucketStarts, p)
	require.NoError(t, err)
	require.NotNil(t, info)

	// $10 + $4 + $22 = $36. Without time-bucket filtering the out-of-bucket
	// window would also true-up to $10, giving $42 — so a $36 result proves
	// the filter is honored end-to-end.
	assert.True(t, decimal.NewFromInt(36).Equal(total),
		"expected total $36 (in-bucket true-up + out-of-bucket base + in-bucket overage), got %s", total)
	assert.True(t, info.IsWindowed)

	// Computed breakdown should reflect only the two in-bucket windows:
	//   commitment utilized = $4 (under-utilized: tracks windowCost) + $10 (over: tracks commitment) = $14
	//   overage             = $6 * 2 = $12
	//   true-up             = $10 - $4 = $6
	assert.True(t, decimal.NewFromInt(14).Equal(info.ComputedCommitmentUtilizedAmount),
		"expected commitment utilized $14, got %s", info.ComputedCommitmentUtilizedAmount)
	assert.True(t, decimal.NewFromInt(12).Equal(info.ComputedOverageAmount),
		"expected overage $12, got %s", info.ComputedOverageAmount)
	assert.True(t, decimal.NewFromInt(6).Equal(info.ComputedTrueUpAmount),
		"expected true-up $6, got %s", info.ComputedTrueUpAmount)
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
