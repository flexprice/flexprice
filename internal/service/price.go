package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/types"
)

type PriceService interface {
	CreatePrice(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error)
	GetPrice(ctx context.Context, id string) (*dto.PriceResponse, error)
	GetPrices(ctx context.Context, filter types.Filter) (*dto.ListPricesResponse, error)
	UpdatePrice(ctx context.Context, id string, req dto.UpdatePriceRequest) (*dto.PriceResponse, error)
	DeletePrice(ctx context.Context, id string) error
}

type priceService struct {
	repo price.Repository
}

func NewPriceService(repo price.Repository) PriceService {
	return &priceService{repo: repo}
}

func (s *priceService) CreatePrice(ctx context.Context, req dto.CreatePriceRequest) (*dto.PriceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	if req.PlanID == "" {
		return nil, fmt.Errorf("plan_id is required")
	}

	price := req.ToPrice(ctx)

	if err := s.repo.Create(ctx, price); err != nil {
		return nil, fmt.Errorf("failed to create price: %w", err)
	}

	return &dto.PriceResponse{Price: price}, nil
}

func (s *priceService) GetPrice(ctx context.Context, id string) (*dto.PriceResponse, error) {
	price, err := s.repo.Get(ctx, id)
	if err != nil {

		return nil, fmt.Errorf("failed to get price: %w", err)
	}

	return &dto.PriceResponse{Price: price}, nil
}

func (s *priceService) GetPrices(ctx context.Context, filter types.Filter) (*dto.ListPricesResponse, error) {
	prices, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list prices: %w", err)
	}

	response := &dto.ListPricesResponse{
		Prices: make([]dto.PriceResponse, len(prices)),
	}

	for i, p := range prices {
		response.Prices[i] = dto.PriceResponse{Price: p}
	}

	response.Total = len(prices)
	response.Offset = filter.Offset
	response.Limit = filter.Limit

	return response, nil
}

func (s *priceService) UpdatePrice(ctx context.Context, id string, req dto.UpdatePriceRequest) (*dto.PriceResponse, error) {
	price, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get price: %w", err)
	}

	price.Description = req.Description
	price.Metadata = req.Metadata
	price.LookupKey = req.LookupKey

	if err := s.repo.Update(ctx, price); err != nil {
		return nil, fmt.Errorf("failed to update price: %w", err)
	}

	return &dto.PriceResponse{Price: price}, nil
}

func (s *priceService) DeletePrice(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete price: %w", err)
	}
	return nil
}
