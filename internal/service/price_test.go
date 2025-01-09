package service

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type PriceServiceSuite struct {
	suite.Suite
	ctx          context.Context
	priceService *priceService
	priceRepo    *testutil.InMemoryPriceStore
}

func TestPriceService(t *testing.T) {
	suite.Run(t, new(PriceServiceSuite))
}

func (s *PriceServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.priceRepo = testutil.NewInMemoryPriceStore()

	s.priceService = &priceService{
		repo: s.priceRepo,
	}
}

func (s *PriceServiceSuite) TestCreatePrice() {
	req := dto.CreatePriceRequest{
		Amount:             "100",
		Currency:           "USD",
		PlanID:             "plan-1",
		Type:               types.PRICE_TYPE_USAGE,
		MeterID:            "meter-1",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		TierMode:           types.BILLING_TIER_SLAB,
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
		Currency: "USD",
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
	_ = s.priceRepo.Create(s.ctx, &price.Price{ID: "price-1", Amount: decimal.NewFromInt(100), Currency: "USD", PlanID: "plan-1"})
	_ = s.priceRepo.Create(s.ctx, &price.Price{ID: "price-2", Amount: decimal.NewFromInt(200), Currency: "USD", PlanID: "plan-1"})

	// Retrieve all prices within limit
	priceFilter := types.NewPriceFilter()
	priceFilter.QueryFilter.Offset = lo.ToPtr(0)
	priceFilter.QueryFilter.Limit = lo.ToPtr(10)
	resp, err := s.priceService.GetPrices(s.ctx, priceFilter)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(2, resp.Total) // Ensure all prices are retrieved
	s.Len(resp.Prices, 2)

	// Sort the response prices by ID
	sort.Slice(resp.Prices, func(i, j int) bool {
		return resp.Prices[i].ID < resp.Prices[j].ID
	})

	s.Equal("price-1", resp.Prices[0].ID)
	s.Equal("price-2", resp.Prices[1].ID)

	// Retrieve with offset exceeding available records
	priceFilter.QueryFilter.Offset = lo.ToPtr(10)
	priceFilter.QueryFilter.Limit = lo.ToPtr(10)
	resp, err = s.priceService.GetPrices(s.ctx, priceFilter)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(0, resp.Total) // Ensure no prices are retrieved
	s.Len(resp.Prices, 0)
}

func (s *PriceServiceSuite) TestUpdatePrice() {
	// Create a price
	price := &price.Price{
		ID:       "price-1",
		Amount:   decimal.NewFromInt(100),
		Currency: "USD",
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
	price := &price.Price{ID: "price-1", Amount: decimal.NewFromInt(100), Currency: "USD"}
	_ = s.priceRepo.Create(s.ctx, price)

	err := s.priceService.DeletePrice(s.ctx, "price-1")
	s.NoError(err)

	// Ensure the price no longer exists
	_, err = s.priceRepo.Get(s.ctx, "price-1")
	s.Error(err)
}
