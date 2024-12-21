package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type PriceServiceSuite struct {
	suite.Suite
	ctx          context.Context
	priceService *priceService
	priceRepo    *testutil.InMemoryPriceRepository
}

func TestPriceService(t *testing.T) {
	suite.Run(t, new(PriceServiceSuite))
}

func (s *PriceServiceSuite) SetupTest() {
	s.ctx = context.Background()
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
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		Description:        "Test Price",
		Metadata:           map[string]string{"key": "value"},
	}

	resp, err := s.priceService.CreatePrice(s.ctx, req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.Amount, resp.Price.Amount)
	s.Equal(req.Currency, resp.Price.Currency)
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
	// Prepopulate the repository with prices
	_ = s.priceRepo.Create(s.ctx, &price.Price{ID: "price-1", Amount: decimal.NewFromInt(100), Currency: "USD"})
	_ = s.priceRepo.Create(s.ctx, &price.Price{ID: "price-2", Amount: decimal.NewFromInt(200), Currency: "USD"})

	resp, err := s.priceService.GetPrices(s.ctx, types.Filter{Offset: 0, Limit: 10})
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(2, resp.Total)

	resp, err = s.priceService.GetPrices(s.ctx, types.Filter{Offset: 10, Limit: 10})
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(0, resp.Total)
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
	s.Equal(req.Metadata, resp.Price.Metadata)
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
