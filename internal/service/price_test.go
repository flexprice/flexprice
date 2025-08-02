package service

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
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
	s.priceService = NewPriceService(s.priceRepo, s.meterRepo, s.priceUnitRepo, s.logger)
}

func (s *PriceServiceSuite) TestCreatePrice() {
	req := dto.CreatePriceRequest{
		Amount:             "100",
		Currency:           "usd",
		PlanID:             "plan-1",
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
		ID:       "price-1",
		Amount:   decimal.NewFromInt(100),
		Currency: "usd",
		PlanID:   "plan-1",
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
		ID:        "price-1",
		Amount:    decimal.NewFromInt(100),
		Currency:  "usd",
		PlanID:    "plan-1",
		BaseModel: types.GetDefaultBaseModel(s.ctx),
	})
	_ = s.priceRepo.Create(s.ctx, &price.Price{
		ID:        "price-2",
		Amount:    decimal.NewFromInt(200),
		Currency:  "usd",
		PlanID:    "plan-1",
		BaseModel: types.GetDefaultBaseModel(s.ctx),
	})

	// Retrieve all prices within limit
	priceFilter := types.NewPriceFilter()
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
	resp, err = s.priceService.GetPrices(s.ctx, priceFilter)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(2, resp.Pagination.Total)
	s.Len(resp.Items, 0) // Ensure no prices are retrieved
}

func (s *PriceServiceSuite) TestUpdatePrice() {
	// Create a price
	price := &price.Price{
		ID:       "price-1",
		Amount:   decimal.NewFromInt(100),
		Currency: "usd",
		PlanID:   "plan-1",
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

	err := s.priceService.DeletePrice(s.ctx, "price-1")
	s.NoError(err)

	// Ensure the price no longer exists
	_, err = s.priceRepo.Get(s.ctx, "price-1")
	s.Error(err)
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

	// From the debug logs, the calculation is:
	// (10 * 50) = 500 (first tier)
	// (15 * 40) = 600 (second tier, not 10 as expected)
	// No third tier is used
	// Total: 500 + 600 = 1100
	expectedFinalCost = decimal.NewFromInt(1100)
	s.Equal(expectedFinalCost.Equal(result.FinalCost), true)

	expectedUnitCost = decimal.NewFromInt(1100).Div(decimal.NewFromInt(25)) // 1100/25
	s.Equal(expectedUnitCost.Equal(result.EffectiveUnitCost), true)
	s.Equal(decimal.NewFromInt(40).Equal(result.TierUnitAmount), true) // Second tier is the last one used
	s.Equal(1, result.SelectedTierIndex)                               // Index 1 (second tier)
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
