package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFillBucketedUsageForWindowCommitment_EmptyResults_ReturnsExpectedWindowsWithZeros(t *testing.T) {
	// When there is no usage, fill should return one value per expected window (all zeros)
	// so that commitment/true-up is applied per window
	periodStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC) // 3 full days

	filled := FillBucketedUsageForWindowCommitment(periodStart, periodEnd, types.WindowSizeDay, nil, nil)
	require.NotNil(t, filled, "fill should return non-nil when period and bucket size are valid")
	assert.Len(t, filled, 3, "should have 3 windows (Jan 1, 2, 3)")
	for i, v := range filled {
		assert.True(t, v.IsZero(), "window %d should be zero when no usage", i)
	}
}

func TestFillBucketedUsageForWindowCommitment_PartialResults_MergesCorrectly(t *testing.T) {
	periodStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)

	// Only day 2 has usage (sparse result from GetUsageByMeter)
	day2Start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	results := []events.UsageResult{
		{WindowSize: day2Start, Value: decimal.NewFromInt(100)},
	}

	filled := FillBucketedUsageForWindowCommitment(periodStart, periodEnd, types.WindowSizeDay, nil, results)
	require.NotNil(t, filled)
	require.Len(t, filled, 3)
	assert.True(t, filled[0].IsZero(), "day 1 should be 0")
	assert.True(t, filled[1].Equal(decimal.NewFromInt(100)), "day 2 should be 100")
	assert.True(t, filled[2].IsZero(), "day 3 should be 0")
}

func TestFillBucketedUsageForWindowCommitment_EmptyPeriod_ReturnsNil(t *testing.T) {
	periodStart := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	filled := FillBucketedUsageForWindowCommitment(periodStart, periodEnd, types.WindowSizeDay, nil, nil)
	assert.Nil(t, filled)
}

func TestApplyWindowCommitmentToLineItem_NoUsage_TrueUp_ChargesCommitmentPerWindow(t *testing.T) {
	// When there is no usage in any window but true-up is enabled,
	// customer should be charged commitment amount per window (full true-up)
	suite := &billingCommitmentTestSuite{}
	suite.SetupTest()

	ctx := context.Background()
	priceSvc := &mockPriceServiceForCommitment{costPerUnit: decimal.NewFromFloat(0.1)}
	calc := newCommitmentCalculator(suite.logger, priceSvc)

	lineItem := &subscription.SubscriptionLineItem{
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        loPtr(decimal.NewFromFloat(10)),
		CommitmentOverageFactor: loPtr(decimal.NewFromFloat(1.5)),
		CommitmentTrueUpEnabled: true,
	}
	price := &price.Price{ID: "price_1", Type: types.PRICE_TYPE_USAGE}

	// 3 windows, all zero usage (e.g. from FillBucketedUsageForWindowCommitment when no events)
	bucketedValues := []decimal.Decimal{decimal.Zero, decimal.Zero, decimal.Zero}

	charge, info, err := calc.applyWindowCommitmentToLineItem(ctx, lineItem, bucketedValues, price)
	require.NoError(t, err)
	require.NotNil(t, info)

	// Each window: usage 0 < commitment 10, true-up -> charge 10. Total = 30
	expectedTotal := decimal.NewFromInt(30)
	assert.True(t, charge.Equal(expectedTotal), "expected charge 30 (10 per window), got %s", charge.String())
	assert.True(t, info.ComputedTrueUpAmount.Equal(expectedTotal), "true-up amount should be 30")
}

// billingCommitmentTestSuite provides logger for commitment unit tests without full service deps.
type billingCommitmentTestSuite struct {
	logger *logger.Logger
}

func (s *billingCommitmentTestSuite) SetupTest() {
	cfg := &config.Configuration{}
	cfg.Logging.FluentdEnabled = false
	var err error
	s.logger, err = logger.NewLogger(cfg)
	if err != nil {
		panic("failed to create test logger: " + err.Error())
	}
}

func loPtr(d decimal.Decimal) *decimal.Decimal { return &d }

// mockPriceServiceForCommitment is a minimal PriceService for commitment unit tests.
// Only CalculateCost is used by the commitment calculator; other methods are no-op stubs.
type mockPriceServiceForCommitment struct {
	costPerUnit decimal.Decimal
}

func (m *mockPriceServiceForCommitment) CalculateCost(ctx context.Context, p *price.Price, quantity decimal.Decimal) decimal.Decimal {
	return quantity.Mul(m.costPerUnit)
}

func (m *mockPriceServiceForCommitment) CalculateBucketedCost(ctx context.Context, p *price.Price, bucketedValues []decimal.Decimal) decimal.Decimal {
	return decimal.Zero
}
func (m *mockPriceServiceForCommitment) CalculateCostWithBreakup(ctx context.Context, p *price.Price, quantity decimal.Decimal, round bool) dto.CostBreakup {
	return dto.CostBreakup{}
}
func (m *mockPriceServiceForCommitment) CalculateCostSheetPrice(ctx context.Context, p *price.Price, quantity decimal.Decimal) decimal.Decimal {
	return decimal.Zero
}
func (m *mockPriceServiceForCommitment) CreatePrice(context.Context, dto.CreatePriceRequest) (*dto.PriceResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) CreateBulkPrice(context.Context, dto.CreateBulkPriceRequest) (*dto.CreateBulkPriceResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) GetPrice(context.Context, string) (*dto.PriceResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) GetPricesByPlanID(context.Context, dto.GetPricesByPlanRequest) (*dto.ListPricesResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) GetPricesBySubscriptionID(context.Context, string) (*dto.ListPricesResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) GetPricesByAddonID(context.Context, string) (*dto.ListPricesResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) GetPricesByCostsheetID(context.Context, string) (*dto.ListPricesResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) GetPrices(context.Context, *types.PriceFilter) (*dto.ListPricesResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) UpdatePrice(context.Context, string, dto.UpdatePriceRequest) (*dto.PriceResponse, error) {
	return nil, nil
}
func (m *mockPriceServiceForCommitment) DeletePrice(context.Context, string, dto.DeletePriceRequest) error {
	return nil
}
func (m *mockPriceServiceForCommitment) GetByLookupKey(context.Context, string) (*dto.PriceResponse, error) {
	return nil, nil
}
