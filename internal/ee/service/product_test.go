package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type ProductServiceSuite struct {
	suite.Suite
	ctx            context.Context
	productService ProductService
	priceService   *priceService
	priceRepo      *testutil.InMemoryPriceStore
}

func TestProductService(t *testing.T) {
	suite.Run(t, new(ProductServiceSuite))
}

func (s *ProductServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.priceRepo = testutil.NewInMemoryPriceStore()
	log := logger.NewNoopLogger()
	params := ServiceParams{
		ProductRepo:  testutil.NewInMemoryProductStore(),
		PriceRepo:    s.priceRepo,
		SettingsRepo: testutil.NewInMemorySettingsStore(),
		Logger:       log,
		DB:           testutil.NewMockPostgresClient(log),
	}
	s.productService = NewProductService(params)
	s.priceService = NewPriceService(params).(*priceService)
}

func (s *ProductServiceSuite) createProduct(lookupKey string) *dto.ProductResponse {
	p, err := s.productService.CreateProduct(s.ctx, dto.CreateProductRequest{Name: "API calls", LookupKey: lookupKey})
	s.Require().NoError(err)
	return p
}

func (s *ProductServiceSuite) createPrice(productID string) *price.Price {
	p := &price.Price{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:        decimal.NewFromInt(10),
		Currency:      "usd",
		Type:          types.PRICE_TYPE_FIXED,
		BillingModel:  types.BILLING_MODEL_FLAT_FEE,
		PriceUnitType: types.PRICE_UNIT_TYPE_FIAT,
		ProductID:     productID,
		BaseModel:     types.GetDefaultBaseModel(s.ctx),
	}
	s.Require().NoError(s.priceRepo.Create(s.ctx, p))
	return p
}

func (s *ProductServiceSuite) TestCreateAndGet() {
	created := s.createProduct("api_calls")

	got, err := s.productService.GetProduct(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal("api_calls", got.LookupKey)
	s.Equal("API calls", got.Name)
	s.Equal(types.StatusPublished, got.Status)
}

func (s *ProductServiceSuite) TestCreateRequiresNameAndLookupKey() {
	_, err := s.productService.CreateProduct(s.ctx, dto.CreateProductRequest{Name: "API calls"})
	s.True(ierr.IsValidation(err))
}

func (s *ProductServiceSuite) TestUpdateKeepsLookupKey() {
	created := s.createProduct("api_calls")

	updated, err := s.productService.UpdateProduct(s.ctx, created.ID, dto.UpdateProductRequest{
		Name:        lo.ToPtr("Requests"),
		Description: lo.ToPtr("Metered requests"),
	})
	s.Require().NoError(err)
	s.Equal("Requests", updated.Name)
	s.Equal("Metered requests", updated.Description)
	s.Equal("api_calls", updated.LookupKey)
}

func (s *ProductServiceSuite) TestDelete() {
	tests := []struct {
		name         string
		withPrice    bool
		wantErr      bool
		wantArchived bool
	}{
		{name: "archives an unused product", wantArchived: true},
		{name: "blocked while a published price references it", withPrice: true, wantErr: true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			created := s.createProduct(tt.name)
			if tt.withPrice {
				s.createPrice(created.ID)
			}

			err := s.productService.DeleteProduct(s.ctx, created.ID)
			if tt.wantErr {
				s.True(ierr.IsValidation(err))
				return
			}
			s.Require().NoError(err)

			got, err := s.productService.GetProduct(s.ctx, created.ID)
			s.Require().NoError(err)
			s.Equal(types.StatusArchived, got.Status)

			list, err := s.productService.GetProducts(s.ctx, nil)
			s.Require().NoError(err)
			s.Empty(lo.Filter(list.Items, func(p *dto.ProductResponse, _ int) bool { return p.ID == created.ID }))
		})
	}
}

func (s *ProductServiceSuite) TestPriceProductValidation() {
	published := s.createProduct("published")
	archived := s.createProduct("archived")
	s.Require().NoError(s.productService.DeleteProduct(s.ctx, archived.ID))

	tests := []struct {
		name      string
		productID string
		check     func(error) bool
	}{
		{name: "empty is allowed", productID: "", check: func(err error) bool { return err == nil }},
		{name: "published product", productID: published.ID, check: func(err error) bool { return err == nil }},
		{name: "unknown product", productID: "product_missing", check: ierr.IsNotFound},
		{name: "archived product", productID: archived.ID, check: ierr.IsValidation},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.True(tt.check(s.priceService.validateProduct(s.ctx, tt.productID)))
		})
	}
}

func (s *ProductServiceSuite) TestPriceProductIsSetOnce() {
	first := s.createProduct("first")
	second := s.createProduct("second")
	p := s.createPrice("")

	resp, err := s.priceService.UpdatePrice(s.ctx, p.ID, dto.UpdatePriceRequest{ProductID: first.ID})
	s.Require().NoError(err)
	s.Equal(first.ID, resp.ProductID)

	_, err = s.priceService.UpdatePrice(s.ctx, p.ID, dto.UpdatePriceRequest{ProductID: first.ID})
	s.Require().NoError(err, "re-sending the same product is a no-op")

	_, err = s.priceService.UpdatePrice(s.ctx, p.ID, dto.UpdatePriceRequest{ProductID: second.ID})
	s.True(ierr.IsValidation(err))

	stored, err := s.priceRepo.Get(s.ctx, p.ID)
	s.Require().NoError(err)
	s.Equal(first.ID, stored.ProductID)
}
