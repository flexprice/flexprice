package service

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/priceunit"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type PriceServiceSuite struct {
	suite.Suite
	ctx           context.Context
	priceService  PriceService
	priceRepo     *testutil.InMemoryPriceStore
	meterRepo     *testutil.InMemoryMeterStore
	priceUnitRepo *testutil.InMemoryPriceUnitStore
	logger        *logger.Logger
}

func TestPriceService(t *testing.T) {
	suite.Run(t, new(PriceServiceSuite))
}

func (s *PriceServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.priceRepo = testutil.NewInMemoryPriceStore()
	s.meterRepo = testutil.NewInMemoryMeterStore()
	s.priceUnitRepo = testutil.NewInMemoryPriceUnitStore()
	s.logger = logger.GetLogger()

	serviceParams := ServiceParams{
		PriceRepo:     s.priceRepo,
		MeterRepo:     s.meterRepo,
		PriceUnitRepo: s.priceUnitRepo,
		PlanRepo:      testutil.NewInMemoryPlanStore(),
		Logger:        s.logger,
	}
	s.priceService = NewPriceService(serviceParams)
}

func (s *PriceServiceSuite) TestCreatePrice() {
	// Create a plan first so that the price can reference it
	plan := &plan.Plan{
		ID:          "plan-1",
		Name:        "Test Plan",
		Description: "A test plan",
		BaseModel:   types.GetDefaultBaseModel(s.ctx),
	}
	_ = s.priceService.(*priceService).ServiceParams.PlanRepo.Create(s.ctx, plan)

	req := dto.CreatePriceRequest{
		Amount:             "100",
		Currency:           "usd",
		PlanID:             "plan-1",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           "plan-1",
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            "meter-1",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		TierMode:           types.BILLING_TIER_SLAB,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		Description:        "Test Price",
		Metadata:           map[string]string{"key": "value"},
		Tiers: []dto.CreatePriceTier{
			{
				UpTo:       lo.ToPtr(uint64(10)),
				UnitAmount: "50",
				FlatAmount: lo.ToPtr("10"),
			},
			{
				UpTo:       lo.ToPtr(uint64(20)),
				UnitAmount: "40",
				FlatAmount: lo.ToPtr("5"),
			},
		},
	}

	resp, err := s.priceService.CreatePrice(s.ctx, req)
	s.NoError(err)
	s.NotNil(resp)

	// Convert expected amount to decimal.Decimal for comparison
	expectedAmount, _ := decimal.NewFromString(req.Amount)
	s.Equal(expectedAmount, resp.Price.Amount) // Compare decimal.Decimal

	// Normalize currency to lowercase for comparison
	s.Equal(strings.ToLower(req.Currency), resp.Price.Currency)
}

func (s *PriceServiceSuite) TestGetPrice() {
	// Create a price
	price := &price.Price{
		ID:         "price-1",
		Amount:     decimal.NewFromInt(100),
		Currency:   "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:   "plan-1",
	}
	_ = s.priceRepo.Create(s.ctx, price)

	resp, err := s.priceService.GetPrice(s.ctx, "price-1")
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(price.Amount, resp.Price.Amount)

	// Non-existent price
	resp, err = s.priceService.GetPrice(s.ctx, "nonexistent-id")
	s.Error(err)
	s.Nil(resp)
}

func (s *PriceServiceSuite) TestGetPrices() {
	// Prepopulate the repository with prices associated with a plan_id
	_ = s.priceRepo.Create(s.ctx, &price.Price{
		ID:         "price-1",
		Amount:     decimal.NewFromInt(100),
		Currency:   "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:   "plan-1",
		BaseModel:  types.GetDefaultBaseModel(s.ctx),
	})
	_ = s.priceRepo.Create(s.ctx, &price.Price{
		ID:         "price-2",
		Amount:     decimal.NewFromInt(200),
		Currency:   "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:   "plan-1",
		BaseModel:  types.GetDefaultBaseModel(s.ctx),
	})

	// Retrieve all prices within limit
	priceFilter := types.NewPriceFilter()
	priceFilter.EntityType = lo.ToPtr(types.PRICE_ENTITY_TYPE_PLAN)
	priceFilter.QueryFilter.Offset = lo.ToPtr(0)
	priceFilter.QueryFilter.Limit = lo.ToPtr(10)
	resp, err := s.priceService.GetPrices(s.ctx, priceFilter)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(2, resp.Pagination.Total) // Ensure all prices are retrieved
	s.Len(resp.Items, 2)

	// Sort the response prices by ID
	sort.Slice(resp.Items, func(i, j int) bool {
		return resp.Items[i].ID < resp.Items[j].ID
	})

	s.Equal("price-1", resp.Items[0].ID)
	s.Equal("price-2", resp.Items[1].ID)

	// Retrieve with offset exceeding available records
	priceFilter.QueryFilter.Offset = lo.ToPtr(10)
	priceFilter.QueryFilter.Limit = lo.ToPtr(10)
	priceFilter.EntityType = lo.ToPtr(types.PRICE_ENTITY_TYPE_PLAN)
	resp, err = s.priceService.GetPrices(s.ctx, priceFilter)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(2, resp.Pagination.Total)
	s.Len(resp.Items, 0) // Ensure no prices are retrieved
}

func (s *PriceServiceSuite) TestUpdatePrice() {
	// Create a price
	price := &price.Price{
		ID:         "price-1",
		Amount:     decimal.NewFromInt(100),
		Currency:   "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:   "plan-1",
	}
	_ = s.priceRepo.Create(s.ctx, price)

	req := dto.UpdatePriceRequest{
		Description: "Updated Description",
		Metadata:    map[string]string{"updated": "true"},
	}

	resp, err := s.priceService.UpdatePrice(s.ctx, "price-1", req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.Description, resp.Price.Description)
	s.Equal(req.Metadata, map[string]string(resp.Price.Metadata)) // Convert Metadata for comparison
}

func (s *PriceServiceSuite) TestDeletePrice() {
	// Create a price
	price := &price.Price{ID: "price-1", Amount: decimal.NewFromInt(100), Currency: "usd"}
	_ = s.priceRepo.Create(s.ctx, price)

	req := dto.DeletePriceRequest{
		EndDate: lo.ToPtr(time.Now().UTC().AddDate(0, 0, 1)),
	}

	err := s.priceService.DeletePrice(s.ctx, "price-1", req)
	s.NoError(err)

	// Ensure the price still exists but has an end date set
	updatedPrice, err := s.priceRepo.Get(s.ctx, "price-1")
	s.NoError(err)
	s.NotNil(updatedPrice.EndDate)
	s.Equal(req.EndDate.Unix(), updatedPrice.EndDate.Unix())
}

func (s *PriceServiceSuite) TestCalculateCostWithBreakup_FlatFee() {
	price := &price.Price{
		ID:           "price-1",
		Amount:       decimal.NewFromInt(100),
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}

	quantity := decimal.NewFromInt(5)
	result := s.priceService.CalculateCostWithBreakup(s.ctx, price, quantity, false)

	s.Equal(decimal.NewFromInt(500).Equal(result.FinalCost), true, "Final cost is %v", result.FinalCost)
	s.Equal(decimal.NewFromInt(100).Equal(result.EffectiveUnitCost), true)
	s.Equal(decimal.NewFromInt(100).Equal(result.TierUnitAmount), true)
	s.Equal(-1, result.SelectedTierIndex)
}

func (s *PriceServiceSuite) TestCalculateCostWithBreakup_Package() {
	price := &price.Price{
		ID:           "price-2",
		Amount:       decimal.NewFromInt(50),
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_PACKAGE,
		TransformQuantity: price.JSONBTransformQuantity{
			DivideBy: 10,
			Round:    types.ROUND_UP,
		},
	}

	quantity := decimal.NewFromInt(25)
	result := s.priceService.CalculateCostWithBreakup(s.ctx, price, quantity, false)

	s.Equal(decimal.NewFromInt(150).Equal(result.FinalCost), true) // 25/10 = 2.5, rounded up to 3, 3*50 = 150

	// Effective unit cost should be final cost divided by actual quantity (150/25 = 6)
	expectedUnitCost := decimal.NewFromInt(150).Div(decimal.NewFromInt(25))
	s.Equal(expectedUnitCost.Equal(result.EffectiveUnitCost), true,
		"Expected effective unit cost %s but got %s",
		expectedUnitCost.String(),
		result.EffectiveUnitCost.String())

	// Tier unit amount should be package price divided by package size (50/10 = 5)
	expectedTierAmount := decimal.NewFromInt(50).Div(decimal.NewFromInt(10))
	s.Equal(expectedTierAmount.Equal(result.TierUnitAmount), true,
		"Expected tier unit amount %s but got %s",
		expectedTierAmount.String(),
		result.TierUnitAmount.String())

	s.Equal(-1, result.SelectedTierIndex)
}

func (s *PriceServiceSuite) TestCalculateCostWithBreakup_TieredVolume() {
	upTo10 := uint64(10)
	upTo20 := uint64(20)
	price := &price.Price{
		ID:           "price-3",
		Amount:       decimal.NewFromInt(100),
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_VOLUME,
		Tiers: []price.PriceTier{
			{
				UpTo:       &upTo10,
				UnitAmount: decimal.NewFromInt(50),
			},
			{
				UpTo:       &upTo20,
				UnitAmount: decimal.NewFromInt(40),
			},
			{
				UnitAmount: decimal.NewFromInt(30),
			},
		},
	}

	// Test within first tier
	quantity := decimal.NewFromInt(5)
	result := s.priceService.CalculateCostWithBreakup(s.ctx, price, quantity, false)
	s.Equal(decimal.NewFromInt(250).Equal(result.FinalCost), true) // 5 * 50 = 250
	s.Equal(decimal.NewFromInt(50).Equal(result.EffectiveUnitCost), true)
	s.Equal(decimal.NewFromInt(50).Equal(result.TierUnitAmount), true)
	s.Equal(0, result.SelectedTierIndex)

	// Test within second tier
	quantity = decimal.NewFromInt(15)
	result = s.priceService.CalculateCostWithBreakup(s.ctx, price, quantity, false)
	s.Equal(decimal.NewFromInt(600).Equal(result.FinalCost), true) // 15 * 40 = 600
	s.Equal(decimal.NewFromInt(40).Equal(result.EffectiveUnitCost), true)
	s.Equal(decimal.NewFromInt(40).Equal(result.TierUnitAmount), true)
	s.Equal(1, result.SelectedTierIndex)

	// Test within third tier (unlimited)
	quantity = decimal.NewFromInt(25)
	result = s.priceService.CalculateCostWithBreakup(s.ctx, price, quantity, false)
	s.Equal(decimal.NewFromInt(750).Equal(result.FinalCost), true) // 25 * 30 = 750
	s.Equal(decimal.NewFromInt(30).Equal(result.EffectiveUnitCost), true)
	s.Equal(decimal.NewFromInt(30).Equal(result.TierUnitAmount), true)
	s.Equal(2, result.SelectedTierIndex)
}

func (s *PriceServiceSuite) TestCalculateCostWithBreakup_TieredSlab() {
	upTo10 := uint64(10)
	upTo20 := uint64(20)
	price := &price.Price{
		ID:           "price-4",
		Amount:       decimal.NewFromInt(100),
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers: []price.PriceTier{
			{
				UpTo:       &upTo10,
				UnitAmount: decimal.NewFromInt(50),
			},
			{
				UpTo:       &upTo20,
				UnitAmount: decimal.NewFromInt(40),
			},
			{
				UnitAmount: decimal.NewFromInt(30),
			},
		},
	}

	// Test within first tier
	quantity := decimal.NewFromInt(5)
	result := s.priceService.CalculateCostWithBreakup(s.ctx, price, quantity, false)
	s.Equal(decimal.NewFromInt(250).Equal(result.FinalCost), true) // 5 * 50 = 250
	s.Equal(decimal.NewFromInt(50).Equal(result.TierUnitAmount), true)
	s.Equal(0, result.SelectedTierIndex)

	// Test spanning first and second tiers
	quantity = decimal.NewFromInt(15)
	result = s.priceService.CalculateCostWithBreakup(s.ctx, price, quantity, false)

	// The implementation calculates: (10 * 50) + (5 * 40) = 500 + 200 = 700
	expectedFinalCost := decimal.NewFromInt(700)
	s.Equal(expectedFinalCost.Equal(result.FinalCost), true)

	expectedUnitCost := decimal.NewFromInt(700).Div(decimal.NewFromInt(15)) // 700/15
	s.Equal(expectedUnitCost.Equal(result.EffectiveUnitCost), true)
	s.Equal(decimal.NewFromInt(40).Equal(result.TierUnitAmount), true)
	s.Equal(1, result.SelectedTierIndex)

	// Test spanning all three tiers
	quantity = decimal.NewFromInt(25)
	result = s.priceService.CalculateCostWithBreakup(s.ctx, price, quantity, false)

	// Corrected calculation:
	// (10 * 50) = 500 (first tier: 0-10)
	// (10 * 40) = 400 (second tier: 10-20)
	// (5 * 30) = 150 (third tier: 20+)
	// Total: 500 + 400 + 150 = 1050
	expectedFinalCost = decimal.NewFromInt(1050)
	s.Equal(expectedFinalCost.Equal(result.FinalCost), true)

	expectedUnitCost = decimal.NewFromInt(1050).Div(decimal.NewFromInt(25)) // 1050/25
	s.Equal(expectedUnitCost.Equal(result.EffectiveUnitCost), true)
	s.Equal(decimal.NewFromInt(30).Equal(result.TierUnitAmount), true) // Third tier is the last one used
	s.Equal(2, result.SelectedTierIndex)                               // Index 2 (third tier)
}

func (s *PriceServiceSuite) TestCalculateCostWithBreakup_ZeroQuantity() {
	price := &price.Price{
		ID:           "price-5",
		Amount:       decimal.NewFromInt(100),
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}

	quantity := decimal.Zero
	result := s.priceService.CalculateCostWithBreakup(s.ctx, price, quantity, false)

	s.Equal(decimal.Zero, result.FinalCost)
	s.Equal(decimal.Zero, result.EffectiveUnitCost)
	s.Equal(decimal.Zero, result.TierUnitAmount)
	s.Equal(-1, result.SelectedTierIndex)
}

func (s *PriceServiceSuite) TestCalculateCostWithBreakup_PackageScenarios() {
	// Tests the main package pricing scenarios
	// Base case: 100 units for $1.00 package
	// Test cases include:
	// 2 units → $1.00 (1 package)
	// 100 units → $1.00 (1 package, exact)
	// 150 units → $2.00 (2 packages)
	// 300 units → $3.00 (3 packages)
	// 0 units → $0.00 (edge case)
	// 99 units → $1.00 (partial package)
	// 101 units → $2.00 (just over one package)

	// Define a standard package price: 100 units for $1.00
	price := &price.Price{
		ID:           "price-package",
		Amount:       decimal.NewFromInt(1), // $1.00 per package
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_PACKAGE,
		TransformQuantity: price.JSONBTransformQuantity{
			DivideBy: 100,            // Package size of 100 units
			Round:    types.ROUND_UP, // Always round up to next package
		},
	}

	// Test cases
	testCases := []struct {
		name          string
		quantity      decimal.Decimal
		expectedCost  decimal.Decimal
		expectedUnits int64
	}{
		{
			name:          "Small quantity (2 units) should charge one full package",
			quantity:      decimal.NewFromInt(2),
			expectedCost:  decimal.NewFromInt(1), // $1.00 (1 package)
			expectedUnits: 1,
		},
		{
			name:          "Quantity at package boundary (100 units) should charge one package",
			quantity:      decimal.NewFromInt(100),
			expectedCost:  decimal.NewFromInt(1), // $1.00 (1 package)
			expectedUnits: 1,
		},
		{
			name:          "Mid-range quantity (150 units) should charge two packages",
			quantity:      decimal.NewFromInt(150),
			expectedCost:  decimal.NewFromInt(2), // $2.00 (2 packages)
			expectedUnits: 2,
		},
		{
			name:          "Large quantity (300 units) should charge three packages",
			quantity:      decimal.NewFromInt(300),
			expectedCost:  decimal.NewFromInt(3), // $3.00 (3 packages)
			expectedUnits: 3,
		},
		{
			name:          "Zero quantity should result in zero cost",
			quantity:      decimal.Zero,
			expectedCost:  decimal.Zero,
			expectedUnits: 0,
		},
		{
			name:          "Partial package (99 units) should charge one full package",
			quantity:      decimal.NewFromInt(99),
			expectedCost:  decimal.NewFromInt(1), // $1.00 (1 package)
			expectedUnits: 1,
		},
		{
			name:          "Just over one package (101 units) should charge two packages",
			quantity:      decimal.NewFromInt(101),
			expectedCost:  decimal.NewFromInt(2), // $2.00 (2 packages)
			expectedUnits: 2,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result := s.priceService.CalculateCostWithBreakup(s.ctx, price, tc.quantity, false)

			// Verify final cost
			s.True(tc.expectedCost.Equal(result.FinalCost),
				"Expected cost %s but got %s for quantity %s",
				tc.expectedCost.String(),
				result.FinalCost.String(),
				tc.quantity.String())

			// Verify effective unit cost (if quantity is not zero)
			if !tc.quantity.IsZero() {
				expectedUnitCost := tc.expectedCost.Div(tc.quantity)
				s.True(expectedUnitCost.Equal(result.EffectiveUnitCost),
					"Expected unit cost %s but got %s for quantity %s",
					expectedUnitCost.String(),
					result.EffectiveUnitCost.String(),
					tc.quantity.String())
			}

			// Verify tier unit amount (should be price per unit in a full package)
			expectedTierUnitAmount := decimal.NewFromInt(1).Div(decimal.NewFromInt(100))
			s.True(expectedTierUnitAmount.Equal(result.TierUnitAmount),
				"Expected tier unit amount %s but got %s",
				expectedTierUnitAmount.String(),
				result.TierUnitAmount.String())
		})
	}
}

func (s *PriceServiceSuite) TestCreatePriceWithCustomUnitTiered() {
	// Create a plan first
	plan := &plan.Plan{
		ID:          "plan-tiered-1",
		Name:        "Test Plan",
		Description: "A test plan",
		BaseModel:   types.GetDefaultBaseModel(s.ctx),
	}
	_ = s.priceService.(*priceService).ServiceParams.PlanRepo.Create(s.ctx, plan)

	// Create a price unit first
	priceUnit := &priceunit.PriceUnit{
		ID:             "pu-1",
		Code:           "btc",
		Symbol:         "₿",
		ConversionRate: decimal.NewFromFloat(50000.00), // 1 BTC = 50000 USD
		Precision:      8,
		BaseCurrency:   "usd",
		BaseModel:      types.GetDefaultBaseModel(s.ctx),
	}
	_ = s.priceUnitRepo.Create(s.ctx, priceUnit)

	req := dto.CreatePriceRequest{
		Currency:           "usd",
		PlanID:             "plan-tiered-1",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           "plan-tiered-1",
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            "meter-1",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		TierMode:           types.BILLING_TIER_VOLUME,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		Description:        "Test Price with Custom Unit Tiers",
		PriceUnitType:      types.PRICE_UNIT_TYPE_CUSTOM,
		PriceUnitConfig: &dto.PriceUnitConfig{
			PriceUnit: "btc",
			PriceUnitTiers: []dto.CreatePriceTier{
				{
					UpTo:       lo.ToPtr(uint64(1000)),
					UnitAmount: "0.001",
					FlatAmount: lo.ToPtr("0.0001"),
				},
				{
					UnitAmount: "0.0005",
				},
			},
		},
	}

	resp, err := s.priceService.CreatePrice(s.ctx, req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(types.BILLING_MODEL_TIERED, resp.BillingModel)
	s.Equal("btc", resp.PriceUnit)
	s.Len(resp.Tiers, 2)
	s.Equal(decimal.NewFromFloat(50), resp.Tiers[0].UnitAmount) // 0.001 BTC * 50000 USD/BTC = 50 USD
	s.Equal(decimal.NewFromFloat(25), resp.Tiers[1].UnitAmount) // 0.0005 BTC * 50000 USD/BTC = 25 USD
}

func (s *PriceServiceSuite) TestCalculateCostWithBreakup_PackageRoundingModes() {
	// Tests different rounding behaviors
	// Tests both ROUND_UP and ROUND_DOWN modes
	// Test cases include:
	// 50 units with round up → $1.00
	// 50 units with round down → $0.00
	// 250 units with round up → $3.00
	// 250 units with round down → $2.00

	basePrice := &price.Price{
		ID:           "price-package-rounding",
		Amount:       decimal.NewFromInt(1), // $1.00 per package
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_PACKAGE,
		TransformQuantity: price.JSONBTransformQuantity{
			DivideBy: 100, // Package size of 100 units
		},
	}

	testCases := []struct {
		name         string
		roundingMode string
		quantity     decimal.Decimal
		expectedCost decimal.Decimal
	}{
		{
			name:         "Round up mode with partial package",
			roundingMode: types.ROUND_UP,
			quantity:     decimal.NewFromInt(50),
			expectedCost: decimal.NewFromInt(1), // Rounds up to 1 package
		},
		{
			name:         "Round down mode with partial package",
			roundingMode: types.ROUND_DOWN,
			quantity:     decimal.NewFromInt(50),
			expectedCost: decimal.Zero, // Rounds down to 0 packages
		},
		{
			name:         "Round up mode with multiple packages",
			roundingMode: types.ROUND_UP,
			quantity:     decimal.NewFromInt(250),
			expectedCost: decimal.NewFromInt(3), // Rounds up to 3 packages
		},
		{
			name:         "Round down mode with multiple packages",
			roundingMode: types.ROUND_DOWN,
			quantity:     decimal.NewFromInt(250),
			expectedCost: decimal.NewFromInt(2), // Rounds down to 2 packages
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Create a copy of base price with specific rounding mode
			testPrice := *basePrice
			testPrice.TransformQuantity.Round = tc.roundingMode

			result := s.priceService.CalculateCostWithBreakup(s.ctx, &testPrice, tc.quantity, false)

			s.True(tc.expectedCost.Equal(result.FinalCost),
				"Expected cost %s but got %s for quantity %s with %s rounding",
				tc.expectedCost.String(),
				result.FinalCost.String(),
				tc.quantity.String(),
				tc.roundingMode)
		})
	}
}

func (s *PriceServiceSuite) TestCalculateBucketedCost_FlatFee() {
	price := &price.Price{
		ID:           "price-bucketed-flat",
		Amount:       decimal.NewFromFloat(0.10), // $0.10 per unit
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}

	// Test with multiple bucket values representing max values per time bucket
	bucketedValues := []decimal.Decimal{
		decimal.NewFromInt(9),  // Bucket 1: max(2,5,6,9) = 9
		decimal.NewFromInt(10), // Bucket 2: max(10) = 10
	}

	result := s.priceService.CalculateBucketedCost(s.ctx, price, bucketedValues)

	// Expected: (9 * 0.10) + (10 * 0.10) = 0.90 + 1.00 = 1.90
	expected := decimal.NewFromFloat(1.9)
	s.True(expected.Equal(result),
		"Expected cost %s but got %s for bucketed values %v",
		expected.String(), result.String(), bucketedValues)
}

func (s *PriceServiceSuite) TestCalculateBucketedCost_Package() {
	price := &price.Price{
		ID:           "price-bucketed-package",
		Amount:       decimal.NewFromInt(1), // $1 per package
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_PACKAGE,
		TransformQuantity: price.JSONBTransformQuantity{
			DivideBy: 10,             // 10 units per package
			Round:    types.ROUND_UP, // Round up
		},
	}

	// Test with bucket values that require different package allocations
	bucketedValues := []decimal.Decimal{
		decimal.NewFromInt(9),  // Bucket 1: max(2,5,6,9) = 9 → ceil(9/10) = 1 package
		decimal.NewFromInt(10), // Bucket 2: max(10) = 10 → ceil(10/10) = 1 package
	}

	result := s.priceService.CalculateBucketedCost(s.ctx, price, bucketedValues)

	// Expected: 1 package + 1 package = $2
	expected := decimal.NewFromInt(2)
	s.True(expected.Equal(result),
		"Expected cost %s but got %s for bucketed values %v",
		expected.String(), result.String(), bucketedValues)
}

func (s *PriceServiceSuite) TestCalculateBucketedCost_TieredSlab() {
	upTo10 := uint64(10)
	upTo20 := uint64(20)
	price := &price.Price{
		ID:           "price-bucketed-tiered-slab",
		Amount:       decimal.Zero,
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers: []price.PriceTier{
			{UpTo: &upTo10, UnitAmount: decimal.NewFromFloat(0.10)}, // 0-10: $0.10/unit
			{UpTo: &upTo20, UnitAmount: decimal.NewFromFloat(0.05)}, // 11-20: $0.05/unit
			{UpTo: nil, UnitAmount: decimal.NewFromFloat(0.02)},     // 21+: $0.02/unit
		},
	}

	// Test with bucket values that span different tiers
	bucketedValues := []decimal.Decimal{
		decimal.NewFromInt(9),  // Bucket 1: max(2,5,6,9) = 9 → 9*$0.10 = $0.90
		decimal.NewFromInt(15), // Bucket 2: max(10,15) = 15 → 10*$0.10 + 5*$0.05 = $1.25
	}

	result := s.priceService.CalculateBucketedCost(s.ctx, price, bucketedValues)

	// Expected: $0.90 + $1.25 = $2.15
	expected := decimal.NewFromFloat(2.15)
	s.True(expected.Equal(result),
		"Expected cost %s but got %s for bucketed values %v",
		expected.String(), result.String(), bucketedValues)
}

func (s *PriceServiceSuite) TestCalculateBucketedCost_TieredVolume() {
	upTo10 := uint64(10)
	upTo20 := uint64(20)
	price := &price.Price{
		ID:           "price-bucketed-tiered-volume",
		Amount:       decimal.Zero,
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_VOLUME,
		Tiers: []price.PriceTier{
			{UpTo: &upTo10, UnitAmount: decimal.NewFromFloat(0.10)}, // 0-10: $0.10/unit
			{UpTo: &upTo20, UnitAmount: decimal.NewFromFloat(0.05)}, // 11-20: $0.05/unit
			{UpTo: nil, UnitAmount: decimal.NewFromFloat(0.02)},     // 21+: $0.02/unit
		},
	}

	// Test with bucket values that fall into different volume tiers
	bucketedValues := []decimal.Decimal{
		decimal.NewFromInt(9),  // Bucket 1: max(2,5,6,9) = 9 → tier 1 → 9*$0.10 = $0.90
		decimal.NewFromInt(15), // Bucket 2: max(10,15) = 15 → tier 2 → 15*$0.05 = $0.75
	}

	result := s.priceService.CalculateBucketedCost(s.ctx, price, bucketedValues)

	// Expected: $0.90 + $0.75 = $1.65
	expected := decimal.NewFromFloat(1.65)
	s.True(expected.Equal(result),
		"Expected cost %s but got %s for bucketed values %v",
		expected.String(), result.String(), bucketedValues)
}

func (s *PriceServiceSuite) TestCalculateBucketedCost_EmptyBuckets() {
	price := &price.Price{
		ID:           "price-bucketed-empty",
		Amount:       decimal.NewFromFloat(0.10),
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}

	// Test with empty bucket array
	bucketedValues := []decimal.Decimal{}

	result := s.priceService.CalculateBucketedCost(s.ctx, price, bucketedValues)

	// Expected: $0.00 (no buckets to process)
	expected := decimal.Zero
	s.True(expected.Equal(result),
		"Expected cost %s but got %s for empty bucketed values",
		expected.String(), result.String())
}

func (s *PriceServiceSuite) TestCalculateBucketedCost_ZeroValues() {
	price := &price.Price{
		ID:           "price-bucketed-zero",
		Amount:       decimal.NewFromFloat(0.10),
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_FLAT_FEE,
	}

	// Test with zero values in buckets
	bucketedValues := []decimal.Decimal{
		decimal.Zero,          // Bucket 1: no usage
		decimal.NewFromInt(5), // Bucket 2: max = 5
		decimal.Zero,          // Bucket 3: no usage
	}

	result := s.priceService.CalculateBucketedCost(s.ctx, price, bucketedValues)

	// Expected: (0 * 0.10) + (5 * 0.10) + (0 * 0.10) = $0.50
	expected := decimal.NewFromFloat(0.50)
	s.True(expected.Equal(result),
		"Expected cost %s but got %s for bucketed values with zeros %v",
		expected.String(), result.String(), bucketedValues)
}

func (s *PriceServiceSuite) TestCalculateBucketedCost_ComplexScenario() {
	// Test a complex scenario with package billing and multiple buckets
	price := &price.Price{
		ID:           "price-bucketed-complex",
		Amount:       decimal.NewFromInt(5), // $5 per package
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_PACKAGE,
		TransformQuantity: price.JSONBTransformQuantity{
			DivideBy: 100,            // 100 units per package
			Round:    types.ROUND_UP, // Round up
		},
	}

	// Test with various bucket values
	bucketedValues := []decimal.Decimal{
		decimal.NewFromInt(50),  // Bucket 1: max = 50 → ceil(50/100) = 1 package → $5
		decimal.NewFromInt(150), // Bucket 2: max = 150 → ceil(150/100) = 2 packages → $10
		decimal.NewFromInt(200), // Bucket 3: max = 200 → ceil(200/100) = 2 packages → $10
		decimal.NewFromInt(99),  // Bucket 4: max = 99 → ceil(99/100) = 1 package → $5
	}

	result := s.priceService.CalculateBucketedCost(s.ctx, price, bucketedValues)

	// Expected: $5 + $10 + $10 + $5 = $30
	expected := decimal.NewFromInt(30)
	s.True(expected.Equal(result),
		"Expected cost %s but got %s for complex bucketed scenario %v",
		expected.String(), result.String(), bucketedValues)
}

func (s *PriceServiceSuite) TestCalculateCostWithBreakup_TieredSlabCorrected() {
	// Test the corrected slab tier calculation logic
	// Pricing: 0-5 = $0/unit, 5-10 = $2/unit, 10+ = $3/unit
	upTo5 := uint64(5)
	upTo10 := uint64(10)
	price := &price.Price{
		ID:           "price-slab-corrected",
		Amount:       decimal.Zero,
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers: []price.PriceTier{
			{
				UpTo:       &upTo5,
				UnitAmount: decimal.Zero, // $0/unit for 0-5
			},
			{
				UpTo:       &upTo10,
				UnitAmount: decimal.NewFromInt(2), // $2/unit for 5-10
			},
			{
				UnitAmount: decimal.NewFromInt(3), // $3/unit for 10+
			},
		},
	}

	testCases := []struct {
		name         string
		quantity     decimal.Decimal
		expectedCost decimal.Decimal
		description  string
	}{
		{
			name:         "Quantity 3 - only first tier",
			quantity:     decimal.NewFromInt(3),
			expectedCost: decimal.Zero, // 3 * $0 = $0
			description:  "3 units at $0/unit = $0",
		},
		{
			name:         "Quantity 5 - boundary of first tier",
			quantity:     decimal.NewFromInt(5),
			expectedCost: decimal.Zero, // 5 * $0 = $0
			description:  "5 units at $0/unit = $0",
		},
		{
			name:         "Quantity 7 - spans first and second tiers",
			quantity:     decimal.NewFromInt(7),
			expectedCost: decimal.NewFromInt(4), // 5 * $0 + 2 * $2 = $4
			description:  "5 units at $0/unit + 2 units at $2/unit = $4",
		},
		{
			name:         "Quantity 10 - spans first two tiers exactly",
			quantity:     decimal.NewFromInt(10),
			expectedCost: decimal.NewFromInt(10), // 5 * $0 + 5 * $2 = $10
			description:  "5 units at $0/unit + 5 units at $2/unit = $10",
		},
		{
			name:         "Quantity 11 - the example from the problem",
			quantity:     decimal.NewFromInt(11),
			expectedCost: decimal.NewFromInt(13), // 5 * $0 + 5 * $2 + 1 * $3 = $13
			description:  "5 units at $0/unit + 5 units at $2/unit + 1 unit at $3/unit = $13",
		},
		{
			name:         "Quantity 15 - more units in third tier",
			quantity:     decimal.NewFromInt(15),
			expectedCost: decimal.NewFromInt(25), // 5 * $0 + 5 * $2 + 5 * $3 = $25
			description:  "5 units at $0/unit + 5 units at $2/unit + 5 units at $3/unit = $25",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result := s.priceService.CalculateCostWithBreakup(s.ctx, price, tc.quantity, false)
			s.True(tc.expectedCost.Equal(result.FinalCost),
				"Expected cost %s but got %s for quantity %s. %s",
				tc.expectedCost.String(),
				result.FinalCost.String(),
				tc.quantity.String(),
				tc.description)

			// Also test the regular CalculateCost method
			regularResult := s.priceService.CalculateCost(s.ctx, price, tc.quantity)
			s.True(tc.expectedCost.Equal(regularResult),
				"Regular CalculateCost: Expected cost %s but got %s for quantity %s. %s",
				tc.expectedCost.String(),
				regularResult.String(),
				tc.quantity.String(),
				tc.description)
		})
	}
}

func (s *PriceServiceSuite) TestCalculateCostWithBreakup_TieredSlabWithFlatAmount() {
	// Test slab tiers with flat amounts
	// Pricing: 0-5 = $1 flat + $0/unit, 5-10 = $2 flat + $1/unit, 10+ = $0 flat + $2/unit
	upTo5 := uint64(5)
	upTo10 := uint64(10)
	flatAmount1 := decimal.NewFromInt(1)
	flatAmount2 := decimal.NewFromInt(2)
	flatAmount3 := decimal.Zero

	price := &price.Price{
		ID:           "price-slab-flat",
		Amount:       decimal.Zero,
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers: []price.PriceTier{
			{
				UpTo:       &upTo5,
				UnitAmount: decimal.Zero,
				FlatAmount: &flatAmount1, // $1 flat + $0/unit for 0-5
			},
			{
				UpTo:       &upTo10,
				UnitAmount: decimal.NewFromInt(1),
				FlatAmount: &flatAmount2, // $2 flat + $1/unit for 5-10
			},
			{
				UnitAmount: decimal.NewFromInt(2),
				FlatAmount: &flatAmount3, // $0 flat + $2/unit for 10+
			},
		},
	}

	testCases := []struct {
		name         string
		quantity     decimal.Decimal
		expectedCost decimal.Decimal
		description  string
	}{
		{
			name:         "Quantity 3 - only first tier",
			quantity:     decimal.NewFromInt(3),
			expectedCost: decimal.NewFromInt(1), // $1 flat + 3 * $0 = $1
			description:  "$1 flat + 3 units at $0/unit = $1",
		},
		{
			name:         "Quantity 7 - spans first and second tiers",
			quantity:     decimal.NewFromInt(7),
			expectedCost: decimal.NewFromInt(5), // $1 flat + 5*$0 + $2 flat + 2*$1 = $5
			description:  "First tier: $1 flat + 5*$0, Second tier: $2 flat + 2*$1 = $5",
		},
		{
			name:         "Quantity 12 - spans all three tiers",
			quantity:     decimal.NewFromInt(12),
			expectedCost: decimal.NewFromInt(12), // $1 + 5*$0 + $2 + 5*$1 + $0 + 2*$2 = $12
			description:  "All tiers: ($1 + 0) + ($2 + $5) + ($0 + $4) = $12",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result := s.priceService.CalculateCostWithBreakup(s.ctx, price, tc.quantity, false)
			s.True(tc.expectedCost.Equal(result.FinalCost),
				"Expected cost %s but got %s for quantity %s. %s",
				tc.expectedCost.String(),
				result.FinalCost.String(),
				tc.quantity.String(),
				tc.description)
		})
	}
}

func (s *PriceServiceSuite) TestCalculateCostWithBreakup_TieredSlabEdgeCases() {
	// Test edge cases for slab tier calculation
	upTo1 := uint64(1)
	upTo2 := uint64(2)
	price := &price.Price{
		ID:           "price-slab-edge",
		Amount:       decimal.Zero,
		Currency:     "usd",
		BillingModel: types.BILLING_MODEL_TIERED,
		TierMode:     types.BILLING_TIER_SLAB,
		Tiers: []price.PriceTier{
			{
				UpTo:       &upTo1,
				UnitAmount: decimal.NewFromInt(10), // $10/unit for 0-1
			},
			{
				UpTo:       &upTo2,
				UnitAmount: decimal.NewFromInt(20), // $20/unit for 1-2
			},
			{
				UnitAmount: decimal.NewFromInt(30), // $30/unit for 2+
			},
		},
	}

	testCases := []struct {
		name         string
		quantity     decimal.Decimal
		expectedCost decimal.Decimal
		description  string
	}{
		{
			name:         "Quantity 0 - zero usage",
			quantity:     decimal.Zero,
			expectedCost: decimal.Zero,
			description:  "Zero quantity should result in zero cost",
		},
		{
			name:         "Quantity 1 - exactly at first tier boundary",
			quantity:     decimal.NewFromInt(1),
			expectedCost: decimal.NewFromInt(10), // 1 * $10 = $10
			description:  "1 unit at $10/unit = $10",
		},
		{
			name:         "Quantity 2 - exactly at second tier boundary",
			quantity:     decimal.NewFromInt(2),
			expectedCost: decimal.NewFromInt(30), // 1 * $10 + 1 * $20 = $30
			description:  "1 unit at $10/unit + 1 unit at $20/unit = $30",
		},
		{
			name:         "Quantity 3 - into third tier",
			quantity:     decimal.NewFromInt(3),
			expectedCost: decimal.NewFromInt(60), // 1 * $10 + 1 * $20 + 1 * $30 = $60
			description:  "1 unit at $10/unit + 1 unit at $20/unit + 1 unit at $30/unit = $60",
		},
		{
			name:         "Decimal quantity 2.5",
			quantity:     decimal.NewFromFloat(2.5),
			expectedCost: decimal.NewFromInt(45), // 1 * $10 + 1 * $20 + 0.5 * $30 = $45
			description:  "1 unit at $10 + 1 unit at $20 + 0.5 unit at $30 = $45",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result := s.priceService.CalculateCostWithBreakup(s.ctx, price, tc.quantity, false)
			s.True(tc.expectedCost.Equal(result.FinalCost),
				"Expected cost %s but got %s for quantity %s. %s",
				tc.expectedCost.String(),
				result.FinalCost.String(),
				tc.quantity.String(),
				tc.description)
		})
	}
}

func (s *PriceServiceSuite) TestDeletePrice_Comprehensive() {
	// Test data setup for comprehensive delete price tests
	s.Run("TC-DEL-001_Missing_Price_ID", func() {
		// This test would be handled at the HTTP layer, not service layer
		// Service layer expects valid price ID
		s.T().Skip("Test handled at HTTP layer")
	})

	s.Run("TC-DEL-002_Invalid_Price_ID_Format", func() {
		// This test would be handled at the HTTP layer, not service layer
		// Service layer expects valid price ID
		s.T().Skip("Test handled at HTTP layer")
	})

	s.Run("TC-DEL-003_Non_Existent_Price_ID", func() {
		req := dto.DeletePriceRequest{
			EndDate: lo.ToPtr(time.Now().UTC().AddDate(0, 0, 1)),
		}

		err := s.priceService.DeletePrice(s.ctx, "non-existent-id", req)
		s.Error(err)
		s.Contains(err.Error(), "not found")
	})

	s.Run("TC-DEL-004_No_End_Date_Provided", func() {
		// Create a price first
		price := &price.Price{
			ID:         "price-no-end-date",
			Amount:     decimal.NewFromInt(100),
			Currency:   "usd",
			EntityType: types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:   "plan-1",
		}
		_ = s.priceRepo.Create(s.ctx, price)

		// Delete without end date
		req := dto.DeletePriceRequest{}
		err := s.priceService.DeletePrice(s.ctx, price.ID, req)
		s.NoError(err)

		// Verify price still exists but has end date set
		updatedPrice, err := s.priceRepo.Get(s.ctx, price.ID)
		s.NoError(err)
		s.NotNil(updatedPrice.EndDate)

		// End date should be set to current time (within 1 second tolerance)
		timeDiff := time.Since(*updatedPrice.EndDate)
		s.True(timeDiff < time.Second, "End date should be set to current time")
	})

	s.Run("TC-DEL-005_Past_End_Date_Provided", func() {
		// Create a price first
		price := &price.Price{
			ID:         "price-past-end-date",
			Amount:     decimal.NewFromInt(100),
			Currency:   "usd",
			EntityType: types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:   "plan-1",
		}
		_ = s.priceRepo.Create(s.ctx, price)

		// Try to delete with past end date
		pastDate := time.Now().UTC().AddDate(0, 0, -1) // 1 day ago
		req := dto.DeletePriceRequest{
			EndDate: lo.ToPtr(pastDate),
		}

		err := s.priceService.DeletePrice(s.ctx, price.ID, req)
		s.Error(err)
		s.Contains(err.Error(), "end date must be in the future")
	})

	s.Run("TC-DEL-006_Future_End_Date_Provided", func() {
		// Create a price first
		price := &price.Price{
			ID:         "price-future-end-date",
			Amount:     decimal.NewFromInt(100),
			Currency:   "usd",
			EntityType: types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:   "plan-1",
		}
		_ = s.priceRepo.Create(s.ctx, price)

		// Delete with future end date
		futureDate := time.Now().UTC().AddDate(0, 0, 7) // 7 days from now
		req := dto.DeletePriceRequest{
			EndDate: lo.ToPtr(futureDate),
		}

		err := s.priceService.DeletePrice(s.ctx, price.ID, req)
		s.NoError(err)

		// Verify price end date is set to specified future date
		updatedPrice, err := s.priceRepo.Get(s.ctx, price.ID)
		s.NoError(err)
		s.NotNil(updatedPrice.EndDate)
		s.Equal(futureDate.Unix(), updatedPrice.EndDate.Unix())
	})

	s.Run("TC-DEL-007_Current_Time_As_End_Date", func() {
		// Create a price first
		price := &price.Price{
			ID:         "price-current-end-date",
			Amount:     decimal.NewFromInt(100),
			Currency:   "usd",
			EntityType: types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:   "plan-1",
		}
		_ = s.priceRepo.Create(s.ctx, price)

		// Delete with current time as end date (must be in future)
		currentTime := time.Now().UTC().Add(time.Second) // Add 1 second to ensure it's in the future
		req := dto.DeletePriceRequest{
			EndDate: lo.ToPtr(currentTime),
		}

		err := s.priceService.DeletePrice(s.ctx, price.ID, req)
		s.NoError(err)

		// Verify price end date is set to current time
		updatedPrice, err := s.priceRepo.Get(s.ctx, price.ID)
		s.NoError(err)
		s.NotNil(updatedPrice.EndDate)
		s.Equal(currentTime.Unix(), updatedPrice.EndDate.Unix())
	})

	s.Run("TC-DEL-008_Very_Far_Future_End_Date", func() {
		// Create a price first
		price := &price.Price{
			ID:         "price-far-future-end-date",
			Amount:     decimal.NewFromInt(100),
			Currency:   "usd",
			EntityType: types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:   "plan-1",
		}
		_ = s.priceRepo.Create(s.ctx, price)

		// Delete with very far future end date
		farFutureDate := time.Now().UTC().AddDate(10, 0, 0) // 10 years from now
		req := dto.DeletePriceRequest{
			EndDate: lo.ToPtr(farFutureDate),
		}

		err := s.priceService.DeletePrice(s.ctx, price.ID, req)
		s.NoError(err)

		// Verify price end date is set to specified far future date
		updatedPrice, err := s.priceRepo.Get(s.ctx, price.ID)
		s.NoError(err)
		s.NotNil(updatedPrice.EndDate)
		s.Equal(farFutureDate.Unix(), updatedPrice.EndDate.Unix())
	})

	s.Run("TC-DEL-009_Already_Deleted_Terminated_Price", func() {
		// Create a price with existing end date
		existingEndDate := time.Now().UTC().AddDate(0, 0, 5) // 5 days from now
		price := &price.Price{
			ID:         "price-already-ended",
			Amount:     decimal.NewFromInt(100),
			Currency:   "usd",
			EntityType: types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:   "plan-1",
			EndDate:    lo.ToPtr(existingEndDate),
		}
		_ = s.priceRepo.Create(s.ctx, price)

		// Try to update with new end date - should fail because price is already terminated
		newEndDate := time.Now().UTC().AddDate(0, 0, 10) // 10 days from now
		req := dto.DeletePriceRequest{
			EndDate: lo.ToPtr(newEndDate),
		}

		err := s.priceService.DeletePrice(s.ctx, price.ID, req)
		s.Error(err)
		s.Contains(err.Error(), "price is already terminated")

		// Verify price end date remains unchanged
		updatedPrice, err := s.priceRepo.Get(s.ctx, price.ID)
		s.NoError(err)
		s.NotNil(updatedPrice.EndDate)
		s.Equal(existingEndDate.Unix(), updatedPrice.EndDate.Unix())
	})

	s.Run("TC-DEL-010_Price_With_Different_Statuses", func() {
		// Test with published price (normal case)
		publishedPrice := &price.Price{
			ID:         "price-published",
			Amount:     decimal.NewFromInt(100),
			Currency:   "usd",
			EntityType: types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:   "plan-1",
		}
		_ = s.priceRepo.Create(s.ctx, publishedPrice)

		req := dto.DeletePriceRequest{
			EndDate: lo.ToPtr(time.Now().UTC().AddDate(0, 0, 1)),
		}

		err := s.priceService.DeletePrice(s.ctx, publishedPrice.ID, req)
		s.NoError(err)

		// Verify published price can be terminated
		updatedPrice, err := s.priceRepo.Get(s.ctx, publishedPrice.ID)
		s.NoError(err)
		s.NotNil(updatedPrice.EndDate)
	})

	s.Run("TC-DEL-011_Price_Used_In_Active_Subscriptions", func() {
		// This test requires subscription context which is better tested in subscription_test.go
		s.T().Skip("Test better suited for subscription_test.go")
	})

	s.Run("TC-DEL-012_Price_With_Active_Line_Items", func() {
		// This test requires subscription context which is better tested in subscription_test.go
		s.T().Skip("Test better suited for subscription_test.go")
	})

	s.Run("TC-DEL-013_Price_With_Override_Prices", func() {
		// This test requires subscription context which is better tested in subscription_test.go
		s.T().Skip("Test better suited for subscription_test.go")
	})

	s.Run("TC-DEL-014_Price_In_Different_Environments_Tenants", func() {
		// This test requires multi-tenant context which is better tested in integration tests
		s.T().Skip("Test better suited for integration tests")
	})
}

func (s *PriceServiceSuite) TestDeletePrice_EdgeCases() {
	s.Run("TC-DEL-015_Price_With_Complex_Tiers", func() {
		// Create a price with complex tier structure
		upTo10 := uint64(10)
		upTo20 := uint64(20)
		complexPrice := &price.Price{
			ID:           "price-complex-tiers",
			Amount:       decimal.Zero,
			Currency:     "usd",
			EntityType:   types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:     "plan-1",
			BillingModel: types.BILLING_MODEL_TIERED,
			TierMode:     types.BILLING_TIER_SLAB,
			Tiers: []price.PriceTier{
				{UpTo: &upTo10, UnitAmount: decimal.NewFromFloat(0.10)},
				{UpTo: &upTo20, UnitAmount: decimal.NewFromFloat(0.05)},
				{UpTo: nil, UnitAmount: decimal.NewFromFloat(0.02)},
			},
		}
		_ = s.priceRepo.Create(s.ctx, complexPrice)

		// Terminate the complex price
		req := dto.DeletePriceRequest{
			EndDate: lo.ToPtr(time.Now().UTC().AddDate(0, 0, 1)),
		}

		err := s.priceService.DeletePrice(s.ctx, complexPrice.ID, req)
		s.NoError(err)

		// Verify complex price is terminated
		updatedPrice, err := s.priceRepo.Get(s.ctx, complexPrice.ID)
		s.NoError(err)
		s.NotNil(updatedPrice.EndDate)
		s.Len(updatedPrice.Tiers, 3) // Tiers should be preserved
	})

	s.Run("TC-DEL-016_Price_With_Metadata", func() {
		// Create a price with metadata
		metadataPrice := &price.Price{
			ID:         "price-with-metadata",
			Amount:     decimal.NewFromInt(100),
			Currency:   "usd",
			EntityType: types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:   "plan-1",
			Metadata: price.JSONBMetadata{
				"description": "Test price with metadata",
				"category":    "test",
				"version":     "1.0",
			},
		}
		_ = s.priceRepo.Create(s.ctx, metadataPrice)

		// Terminate the price with metadata
		req := dto.DeletePriceRequest{
			EndDate: lo.ToPtr(time.Now().UTC().AddDate(0, 0, 1)),
		}

		err := s.priceService.DeletePrice(s.ctx, metadataPrice.ID, req)
		s.NoError(err)

		// Verify price is terminated and metadata is preserved
		updatedPrice, err := s.priceRepo.Get(s.ctx, metadataPrice.ID)
		s.NoError(err)
		s.NotNil(updatedPrice.EndDate)
		s.Equal("Test price with metadata", updatedPrice.Metadata["description"])
		s.Equal("test", updatedPrice.Metadata["category"])
		s.Equal("1.0", updatedPrice.Metadata["version"])
	})

	s.Run("TC-DEL-017_Price_With_Transform_Quantity", func() {
		// Create a price with transform quantity
		transformPrice := &price.Price{
			ID:           "price-transform",
			Amount:       decimal.NewFromInt(1),
			Currency:     "usd",
			EntityType:   types.PRICE_ENTITY_TYPE_PLAN,
			EntityID:     "plan-1",
			BillingModel: types.BILLING_MODEL_PACKAGE,
			TransformQuantity: price.JSONBTransformQuantity{
				DivideBy: 100,
				Round:    types.ROUND_UP,
			},
		}
		_ = s.priceRepo.Create(s.ctx, transformPrice)

		// Terminate the transform price
		req := dto.DeletePriceRequest{
			EndDate: lo.ToPtr(time.Now().UTC().AddDate(0, 0, 1)),
		}

		err := s.priceService.DeletePrice(s.ctx, transformPrice.ID, req)
		s.NoError(err)

		// Verify price is terminated and transform quantity is preserved
		updatedPrice, err := s.priceRepo.Get(s.ctx, transformPrice.ID)
		s.NoError(err)
		s.NotNil(updatedPrice.EndDate)
		s.Equal(100, updatedPrice.TransformQuantity.DivideBy)
		s.Equal(types.ROUND_UP, updatedPrice.TransformQuantity.Round)
	})
}
