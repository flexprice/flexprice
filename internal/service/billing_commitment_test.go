package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockPriceServiceForCommitment implements PriceService for commitment tests.
// CalculateCost returns quantity as-is (1:1 cost mapping).
type mockPriceServiceForCommitment struct{}

func (m *mockPriceServiceForCommitment) CreatePrice(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) CreateBulkPrice(ctx context.Context, req dto.CreateBulkPriceRequest) (*dto.CreateBulkPriceResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) GetPrice(ctx context.Context, id string) (*dto.PriceResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) GetPricesByPlanID(ctx context.Context, req dto.GetPricesByPlanRequest) (*dto.ListPricesResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) GetPricesBySubscriptionID(ctx context.Context, subscriptionID string) (*dto.ListPricesResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) GetPricesByAddonID(ctx context.Context, addonID string) (*dto.ListPricesResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) GetPricesByCostsheetID(ctx context.Context, costsheetID string) (*dto.ListPricesResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) GetPrices(ctx context.Context, filter *types.PriceFilter) (*dto.ListPricesResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) UpdatePrice(ctx context.Context, id string, req dto.UpdatePriceRequest) (*dto.PriceResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) DeletePrice(ctx context.Context, id string, req dto.DeletePriceRequest) error {
	return nil
}
func (m *mockPriceServiceForCommitment) CalculateCost(ctx context.Context, p *price.Price, quantity decimal.Decimal) decimal.Decimal {
	return quantity // 1:1 mapping
}
func (m *mockPriceServiceForCommitment) CalculateBucketedCost(ctx context.Context, p *price.Price, bucketedValues []decimal.Decimal) decimal.Decimal {
	return decimal.Zero
}
func (m *mockPriceServiceForCommitment) CalculateCostWithBreakup(ctx context.Context, p *price.Price, quantity decimal.Decimal, round bool) dto.CostBreakup {
	return dto.CostBreakup{}
}
func (m *mockPriceServiceForCommitment) CalculateCostSheetPrice(ctx context.Context, p *price.Price, quantity decimal.Decimal) decimal.Decimal {
	return quantity
}
func (m *mockPriceServiceForCommitment) GetByLookupKey(ctx context.Context, lookupKey string) (*dto.PriceResponse, error) {
	return nil, nil
}

func newTestCommitmentLogger() *logger.Logger {
	return &logger.Logger{SugaredLogger: zap.NewNop().Sugar()}
}

// helper to build a line item with amount-based commitment
func newAmountCommitmentLineItem(commitmentAmount float64, overageFactor float64, duration *types.BillingPeriod) *subscription.SubscriptionLineItem {
	amt := decimal.NewFromFloat(commitmentAmount)
	factor := decimal.NewFromFloat(overageFactor)
	return &subscription.SubscriptionLineItem{
		ID:                      "li_test",
		CommitmentAmount:        &amt,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &factor,
		CommitmentTrueUpEnabled: false,
		CommitmentWindowed:      false,
		CommitmentDuration:      duration,
	}
}

// helper to build a line item with quantity-based commitment
func newQuantityCommitmentLineItem(commitmentQty float64, overageFactor float64, duration *types.BillingPeriod) *subscription.SubscriptionLineItem {
	qty := decimal.NewFromFloat(commitmentQty)
	factor := decimal.NewFromFloat(overageFactor)
	return &subscription.SubscriptionLineItem{
		ID:                      "li_test",
		CommitmentQuantity:      &qty,
		CommitmentType:          types.COMMITMENT_TYPE_QUANTITY,
		CommitmentOverageFactor: &factor,
		CommitmentTrueUpEnabled: false,
		CommitmentWindowed:      false,
		CommitmentDuration:      duration,
	}
}

func newTestCommitmentCalculator() *commitmentCalculator {
	return newCommitmentCalculator(newTestCommitmentLogger(), &mockPriceServiceForCommitment{})
}

// -------------------------------------------------------------------
// isCumulativeCommitment tests
// -------------------------------------------------------------------

func TestIsCumulativeCommitment(t *testing.T) {
	monthly := types.BILLING_PERIOD_MONTHLY
	annual := types.BILLING_PERIOD_ANNUAL

	t.Run("nil duration is not cumulative", func(t *testing.T) {
		li := &subscription.SubscriptionLineItem{CommitmentDuration: nil}
		assert.False(t, isCumulativeCommitment(li, monthly))
	})

	t.Run("same duration as billing period is not cumulative", func(t *testing.T) {
		li := &subscription.SubscriptionLineItem{CommitmentDuration: &monthly}
		assert.False(t, isCumulativeCommitment(li, monthly))
	})

	t.Run("annual commitment on monthly billing is cumulative", func(t *testing.T) {
		li := &subscription.SubscriptionLineItem{CommitmentDuration: &annual}
		assert.True(t, isCumulativeCommitment(li, monthly))
	})
}

func TestIsCumulativeSubscriptionCommitment(t *testing.T) {
	monthly := types.BILLING_PERIOD_MONTHLY
	annual := types.BILLING_PERIOD_ANNUAL

	t.Run("nil duration is not cumulative", func(t *testing.T) {
		sub := &subscription.Subscription{BillingPeriod: monthly, CommitmentDuration: nil}
		assert.False(t, isCumulativeSubscriptionCommitment(sub))
	})

	t.Run("same duration is not cumulative", func(t *testing.T) {
		sub := &subscription.Subscription{BillingPeriod: monthly, CommitmentDuration: &monthly}
		assert.False(t, isCumulativeSubscriptionCommitment(sub))
	})

	t.Run("mismatched duration is cumulative", func(t *testing.T) {
		sub := &subscription.Subscription{BillingPeriod: monthly, CommitmentDuration: &annual}
		assert.True(t, isCumulativeSubscriptionCommitment(sub))
	})
}

// -------------------------------------------------------------------
// applyCumulativeCommitmentToLineItem tests
// -------------------------------------------------------------------

func TestApplyCumulativeCommitment_Case2_NoOverage(t *testing.T) {
	// Case 2: cumulative usage within commitment, no overage
	calc := newTestCommitmentCalculator()
	ctx := context.Background()
	annual := types.BILLING_PERIOD_ANNUAL
	li := newAmountCommitmentLineItem(60000, 1.5, &annual)

	// Month 1: usage=$10k, previousCumulative=$0 → no overage
	charge, info, err := calc.applyCumulativeCommitmentToLineItem(
		ctx, li,
		decimal.NewFromInt(10000), // currentPeriodUsageCost
		decimal.Zero,              // previousCumulativeUsageCost
		&price.Price{},
	)
	require.NoError(t, err)
	assert.True(t, charge.Equal(decimal.NewFromInt(10000)), "expected 10000, got %s", charge)
	assert.True(t, info.IsCumulative)
	assert.True(t, info.ComputedCommitmentUtilizedAmount.Equal(decimal.NewFromInt(10000)))
	assert.True(t, info.ComputedOverageAmount.Equal(decimal.Zero))
	assert.True(t, info.CumulativeUsageCost.Equal(decimal.NewFromInt(10000)))
	assert.True(t, info.PreviousCumulativeUsageCost.Equal(decimal.Zero))
}

func TestApplyCumulativeCommitment_Case1_AllOverage(t *testing.T) {
	// Case 1: commitment already fully consumed, entire period is overage
	calc := newTestCommitmentCalculator()
	ctx := context.Background()
	annual := types.BILLING_PERIOD_ANNUAL
	li := newAmountCommitmentLineItem(60000, 1.5, &annual)

	// Month 6: usage=$30k, previousCumulative=$75k (already past $60k commitment)
	charge, info, err := calc.applyCumulativeCommitmentToLineItem(
		ctx, li,
		decimal.NewFromInt(30000), // currentPeriodUsageCost
		decimal.NewFromInt(75000), // previousCumulativeUsageCost
		&price.Price{},
	)
	require.NoError(t, err)
	// $30k × 1.5 = $45k
	assert.True(t, charge.Equal(decimal.NewFromInt(45000)), "expected 45000, got %s", charge)
	assert.True(t, info.IsCumulative)
	assert.True(t, info.ComputedCommitmentUtilizedAmount.Equal(decimal.Zero))
	assert.True(t, info.ComputedOverageAmount.Equal(decimal.NewFromInt(45000)))
}

func TestApplyCumulativeCommitment_Case3_CrossesBoundary(t *testing.T) {
	// Case 3: crosses commitment boundary this period
	calc := newTestCommitmentCalculator()
	ctx := context.Background()
	annual := types.BILLING_PERIOD_ANNUAL
	li := newAmountCommitmentLineItem(60000, 1.5, &annual)

	// Month 5: usage=$20k, previousCumulative=$55k → $5k within + $15k overage
	charge, info, err := calc.applyCumulativeCommitmentToLineItem(
		ctx, li,
		decimal.NewFromInt(20000), // currentPeriodUsageCost
		decimal.NewFromInt(55000), // previousCumulativeUsageCost
		&price.Price{},
	)
	require.NoError(t, err)
	// withinCommitment = $60k - $55k = $5k
	// overagePortion = $20k - $5k = $15k
	// overageCharge = $15k × 1.5 = $22.5k
	// finalCharge = $5k + $22.5k = $27.5k
	expected := decimal.NewFromFloat(27500)
	assert.True(t, charge.Equal(expected), "expected 27500, got %s", charge)
	assert.True(t, info.ComputedCommitmentUtilizedAmount.Equal(decimal.NewFromInt(5000)))
	assert.True(t, info.ComputedOverageAmount.Equal(decimal.NewFromFloat(22500)))
}

func TestApplyCumulativeCommitment_FullSixMonthScenario(t *testing.T) {
	// Simulates the 6-month annual commitment scenario from requirements:
	// $60k annual commitment, 1.5x overage, monthly billing
	calc := newTestCommitmentCalculator()
	ctx := context.Background()
	annual := types.BILLING_PERIOD_ANNUAL
	li := newAmountCommitmentLineItem(60000, 1.5, &annual)

	type monthData struct {
		usage           int64
		prevCumulative  int64
		expectedCharge  string
		expectedOverage string
	}

	months := []monthData{
		{usage: 10000, prevCumulative: 0, expectedCharge: "10000", expectedOverage: "0"},
		{usage: 15000, prevCumulative: 10000, expectedCharge: "15000", expectedOverage: "0"},
		{usage: 20000, prevCumulative: 25000, expectedCharge: "20000", expectedOverage: "0"},
		{usage: 10000, prevCumulative: 45000, expectedCharge: "10000", expectedOverage: "0"},
		{usage: 20000, prevCumulative: 55000, expectedCharge: "27500", expectedOverage: "22500"}, // $5k within + $15k×1.5
		{usage: 30000, prevCumulative: 75000, expectedCharge: "45000", expectedOverage: "45000"}, // all overage $30k×1.5
	}

	for i, m := range months {
		charge, info, err := calc.applyCumulativeCommitmentToLineItem(
			ctx, li,
			decimal.NewFromInt(m.usage),
			decimal.NewFromInt(m.prevCumulative),
			&price.Price{},
		)
		require.NoError(t, err, "month %d", i+1)

		expCharge, _ := decimal.NewFromString(m.expectedCharge)
		expOverage, _ := decimal.NewFromString(m.expectedOverage)

		assert.True(t, charge.Equal(expCharge),
			"month %d: expected charge %s, got %s", i+1, m.expectedCharge, charge)
		assert.True(t, info.ComputedOverageAmount.Equal(expOverage),
			"month %d: expected overage %s, got %s", i+1, m.expectedOverage, info.ComputedOverageAmount)
		assert.True(t, info.IsCumulative, "month %d: expected IsCumulative=true", i+1)
		assert.False(t, info.TrueUpEnabled, "month %d: true-up should be disabled for cumulative", i+1)
	}
}

func TestApplyCumulativeCommitment_ZeroUsageMonth(t *testing.T) {
	calc := newTestCommitmentCalculator()
	ctx := context.Background()
	annual := types.BILLING_PERIOD_ANNUAL
	li := newAmountCommitmentLineItem(60000, 1.5, &annual)

	// Zero usage with previous cumulative within commitment
	charge, info, err := calc.applyCumulativeCommitmentToLineItem(
		ctx, li,
		decimal.Zero,             // currentPeriodUsageCost
		decimal.NewFromInt(30000), // previousCumulativeUsageCost
		&price.Price{},
	)
	require.NoError(t, err)
	assert.True(t, charge.Equal(decimal.Zero), "expected 0, got %s", charge)
	assert.True(t, info.ComputedOverageAmount.Equal(decimal.Zero))
	assert.True(t, info.ComputedCommitmentUtilizedAmount.Equal(decimal.Zero))
}

func TestApplyCumulativeCommitment_ZeroUsageMonth_PastCommitment(t *testing.T) {
	calc := newTestCommitmentCalculator()
	ctx := context.Background()
	annual := types.BILLING_PERIOD_ANNUAL
	li := newAmountCommitmentLineItem(60000, 1.5, &annual)

	// Zero usage after commitment fully consumed — overage of $0 × 1.5 = $0
	charge, info, err := calc.applyCumulativeCommitmentToLineItem(
		ctx, li,
		decimal.Zero,             // currentPeriodUsageCost
		decimal.NewFromInt(70000), // previousCumulativeUsageCost
		&price.Price{},
	)
	require.NoError(t, err)
	assert.True(t, charge.Equal(decimal.Zero), "expected 0, got %s", charge)
	assert.True(t, info.ComputedOverageAmount.Equal(decimal.Zero))
}

func TestApplyCumulativeCommitment_ExactlyAtCommitment(t *testing.T) {
	calc := newTestCommitmentCalculator()
	ctx := context.Background()
	annual := types.BILLING_PERIOD_ANNUAL
	li := newAmountCommitmentLineItem(60000, 1.5, &annual)

	// Cumulative exactly hits the commitment boundary: $50k prev + $10k current = $60k
	charge, info, err := calc.applyCumulativeCommitmentToLineItem(
		ctx, li,
		decimal.NewFromInt(10000),
		decimal.NewFromInt(50000),
		&price.Price{},
	)
	require.NoError(t, err)
	// No overage — cumulative equals commitment
	assert.True(t, charge.Equal(decimal.NewFromInt(10000)), "expected 10000, got %s", charge)
	assert.True(t, info.ComputedOverageAmount.Equal(decimal.Zero))
	assert.True(t, info.ComputedCommitmentUtilizedAmount.Equal(decimal.NewFromInt(10000)))
}

func TestApplyCumulativeCommitment_PreviousExactlyAtCommitment(t *testing.T) {
	calc := newTestCommitmentCalculator()
	ctx := context.Background()
	annual := types.BILLING_PERIOD_ANNUAL
	li := newAmountCommitmentLineItem(60000, 1.5, &annual)

	// Previous cumulative is exactly at commitment boundary → all current usage is overage
	charge, info, err := calc.applyCumulativeCommitmentToLineItem(
		ctx, li,
		decimal.NewFromInt(5000),
		decimal.NewFromInt(60000),
		&price.Price{},
	)
	require.NoError(t, err)
	// $5k × 1.5 = $7.5k
	assert.True(t, charge.Equal(decimal.NewFromFloat(7500)), "expected 7500, got %s", charge)
	assert.True(t, info.ComputedCommitmentUtilizedAmount.Equal(decimal.Zero))
	assert.True(t, info.ComputedOverageAmount.Equal(decimal.NewFromFloat(7500)))
}

func TestApplyCumulativeCommitment_QuantityBased(t *testing.T) {
	// Quantity-based commitment: 1000 units, 1:1 cost mapping → effectively $1000 commitment
	calc := newTestCommitmentCalculator()
	ctx := context.Background()
	annual := types.BILLING_PERIOD_ANNUAL
	li := newQuantityCommitmentLineItem(1000, 2.0, &annual)

	// Previous cumulative cost: $800, current: $300 → crosses boundary at $1000
	// withinCommitment = $1000 - $800 = $200
	// overagePortion = $300 - $200 = $100
	// overageCharge = $100 × 2.0 = $200
	// finalCharge = $200 + $200 = $400
	charge, info, err := calc.applyCumulativeCommitmentToLineItem(
		ctx, li,
		decimal.NewFromInt(300),
		decimal.NewFromInt(800),
		&price.Price{},
	)
	require.NoError(t, err)
	assert.True(t, charge.Equal(decimal.NewFromInt(400)), "expected 400, got %s", charge)
	assert.True(t, info.ComputedCommitmentUtilizedAmount.Equal(decimal.NewFromInt(200)))
	assert.True(t, info.ComputedOverageAmount.Equal(decimal.NewFromInt(200)))
	assert.True(t, info.IsCumulative)
}

func TestApplyCumulativeCommitment_OverageFactor1(t *testing.T) {
	// Overage factor of 1.0 means overage charged at same rate
	calc := newTestCommitmentCalculator()
	ctx := context.Background()
	annual := types.BILLING_PERIOD_ANNUAL
	li := newAmountCommitmentLineItem(10000, 1.0, &annual)

	// All overage: $5k × 1.0 = $5k
	charge, _, err := calc.applyCumulativeCommitmentToLineItem(
		ctx, li,
		decimal.NewFromInt(5000),
		decimal.NewFromInt(15000),
		&price.Price{},
	)
	require.NoError(t, err)
	assert.True(t, charge.Equal(decimal.NewFromInt(5000)), "expected 5000, got %s", charge)
}

func TestApplyCumulativeCommitment_CommitmentInfoFields(t *testing.T) {
	calc := newTestCommitmentCalculator()
	ctx := context.Background()
	annual := types.BILLING_PERIOD_ANNUAL
	li := newAmountCommitmentLineItem(60000, 1.5, &annual)

	_, info, err := calc.applyCumulativeCommitmentToLineItem(
		ctx, li,
		decimal.NewFromInt(10000),
		decimal.NewFromInt(20000),
		&price.Price{},
	)
	require.NoError(t, err)

	// Verify all CommitmentInfo fields are set correctly
	assert.Equal(t, types.COMMITMENT_TYPE_AMOUNT, info.Type)
	assert.True(t, info.Amount.Equal(decimal.NewFromInt(60000)))
	assert.Equal(t, types.BILLING_PERIOD_ANNUAL, info.Duration)
	assert.NotNil(t, info.OverageFactor)
	assert.True(t, info.OverageFactor.Equal(decimal.NewFromFloat(1.5)))
	assert.False(t, info.TrueUpEnabled)
	assert.False(t, info.IsWindowed)
	assert.True(t, info.IsCumulative)
	assert.True(t, info.CumulativeUsageCost.Equal(decimal.NewFromInt(30000)))       // 20000 + 10000
	assert.True(t, info.PreviousCumulativeUsageCost.Equal(decimal.NewFromInt(20000))) // passed in
	assert.True(t, info.ComputedTrueUpAmount.Equal(decimal.Zero))
}

// -------------------------------------------------------------------
// calculateSubscriptionCumulativeOverage tests
// -------------------------------------------------------------------

func TestSubscriptionCumulativeOverage_Case2_NoOverage(t *testing.T) {
	// previousCumulative + currentPeriod <= commitment → no overage
	adjustment, info := calculateSubscriptionCumulativeOverage(
		decimal.NewFromInt(10000),  // currentPeriodTotal
		decimal.NewFromInt(30000),  // previousCumulativeTotal
		decimal.NewFromInt(60000),  // commitmentAmount
		decimal.NewFromFloat(1.5),  // overageFactor
	)
	assert.True(t, adjustment.Equal(decimal.Zero), "expected 0, got %s", adjustment)
	assert.True(t, info.withinCommitment.Equal(decimal.NewFromInt(10000)))
	assert.True(t, info.overagePortion.Equal(decimal.Zero))
}

func TestSubscriptionCumulativeOverage_Case1_AllOverage(t *testing.T) {
	// previousCumulative >= commitment → entire current period is overage
	// adjustment = currentPeriod × (overageFactor - 1) = $30k × 0.5 = $15k
	adjustment, info := calculateSubscriptionCumulativeOverage(
		decimal.NewFromInt(30000),  // currentPeriodTotal
		decimal.NewFromInt(75000),  // previousCumulativeTotal
		decimal.NewFromInt(60000),  // commitmentAmount
		decimal.NewFromFloat(1.5),  // overageFactor
	)
	expected := decimal.NewFromInt(15000) // 30000 × (1.5 - 1)
	assert.True(t, adjustment.Equal(expected), "expected 15000, got %s", adjustment)
	assert.True(t, info.overagePortion.Equal(decimal.NewFromInt(30000)))
	assert.True(t, info.withinCommitment.Equal(decimal.Zero))
}

func TestSubscriptionCumulativeOverage_Case3_CrossesBoundary(t *testing.T) {
	// Crosses boundary: $55k prev + $20k current vs $60k commitment
	// withinCommitment = $60k - $55k = $5k
	// overagePortion = $20k - $5k = $15k
	// adjustment = $15k × (1.5 - 1) = $7.5k
	adjustment, info := calculateSubscriptionCumulativeOverage(
		decimal.NewFromInt(20000),  // currentPeriodTotal
		decimal.NewFromInt(55000),  // previousCumulativeTotal
		decimal.NewFromInt(60000),  // commitmentAmount
		decimal.NewFromFloat(1.5),  // overageFactor
	)
	expected := decimal.NewFromFloat(7500) // 15000 × 0.5
	assert.True(t, adjustment.Equal(expected), "expected 7500, got %s", adjustment)
	assert.True(t, info.withinCommitment.Equal(decimal.NewFromInt(5000)))
	assert.True(t, info.overagePortion.Equal(decimal.NewFromInt(15000)))
}

func TestSubscriptionCumulativeOverage_FullSixMonthScenario(t *testing.T) {
	// Same 6-month scenario but at subscription level.
	// The adjustment is the ADDITIONAL charge (overagePortion × (factor-1)),
	// not the total charge.
	type monthData struct {
		currentTotal       int64
		prevCumulative     int64
		expectedAdjustment string
	}

	months := []monthData{
		{currentTotal: 10000, prevCumulative: 0, expectedAdjustment: "0"},
		{currentTotal: 15000, prevCumulative: 10000, expectedAdjustment: "0"},
		{currentTotal: 20000, prevCumulative: 25000, expectedAdjustment: "0"},
		{currentTotal: 10000, prevCumulative: 45000, expectedAdjustment: "0"},
		{currentTotal: 20000, prevCumulative: 55000, expectedAdjustment: "7500"},  // $15k × 0.5
		{currentTotal: 30000, prevCumulative: 75000, expectedAdjustment: "15000"}, // $30k × 0.5
	}

	for i, m := range months {
		adjustment, _ := calculateSubscriptionCumulativeOverage(
			decimal.NewFromInt(m.currentTotal),
			decimal.NewFromInt(m.prevCumulative),
			decimal.NewFromInt(60000),
			decimal.NewFromFloat(1.5),
		)

		expAdj, _ := decimal.NewFromString(m.expectedAdjustment)
		assert.True(t, adjustment.Equal(expAdj),
			"month %d: expected adjustment %s, got %s", i+1, m.expectedAdjustment, adjustment)
	}
}

func TestSubscriptionCumulativeOverage_ExactlyAtBoundary(t *testing.T) {
	// previousCumulative + currentPeriod == commitment → no overage
	adjustment, _ := calculateSubscriptionCumulativeOverage(
		decimal.NewFromInt(10000),
		decimal.NewFromInt(50000),
		decimal.NewFromInt(60000),
		decimal.NewFromFloat(1.5),
	)
	assert.True(t, adjustment.Equal(decimal.Zero))
}

func TestSubscriptionCumulativeOverage_PreviousExactlyAtBoundary(t *testing.T) {
	// previousCumulative == commitment → all overage
	// adjustment = $5k × (1.5 - 1) = $2.5k
	adjustment, _ := calculateSubscriptionCumulativeOverage(
		decimal.NewFromInt(5000),
		decimal.NewFromInt(60000),
		decimal.NewFromInt(60000),
		decimal.NewFromFloat(1.5),
	)
	assert.True(t, adjustment.Equal(decimal.NewFromFloat(2500)), "expected 2500, got %s", adjustment)
}

// -------------------------------------------------------------------
// Existing generateBucketStarts tests
// -------------------------------------------------------------------

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
