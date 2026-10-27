package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/product"
	ierr "github.com/flexprice/flexprice/internal/errors"
	productRepo "github.com/flexprice/flexprice/internal/repository/product"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// ProductService owns product validation and the delete guard.
type ProductService interface {
	CreateProduct(ctx context.Context, req dto.CreateProductRequest) (*dto.ProductResponse, error)
	GetProduct(ctx context.Context, id string) (*dto.ProductResponse, error)
	GetProducts(ctx context.Context, filter *productRepo.Filter) (*dto.ListProductsResponse, error)
	UpdateProduct(ctx context.Context, id string, req dto.UpdateProductRequest) (*dto.ProductResponse, error)
	DeleteProduct(ctx context.Context, id string) error
}

type productService struct {
	ServiceParams
}

func NewProductService(params ServiceParams) ProductService {
	return &productService{ServiceParams: params}
}

func (s *productService) CreateProduct(ctx context.Context, req dto.CreateProductRequest) (*dto.ProductResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	p := req.ToProduct()
	if err := s.ProductRepo.Create(ctx, p); err != nil {
		return nil, err
	}
	return dto.NewProductResponse(p), nil
}

func (s *productService) GetProduct(ctx context.Context, id string) (*dto.ProductResponse, error) {
	p, err := s.ProductRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return dto.NewProductResponse(p), nil
}

func (s *productService) GetProducts(ctx context.Context, filter *productRepo.Filter) (*dto.ListProductsResponse, error) {
	if filter == nil {
		filter = productRepo.NewFilter()
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	products, err := s.ProductRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count := len(products)
	if !filter.IsUnlimited() {
		count, err = s.ProductRepo.Count(ctx, filter)
		if err != nil {
			return nil, err
		}
	}

	return &dto.ListProductsResponse{
		Items:      lo.Map(products, func(p *product.Product, _ int) *dto.ProductResponse { return dto.NewProductResponse(p) }),
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

func (s *productService) UpdateProduct(ctx context.Context, id string, req dto.UpdateProductRequest) (*dto.ProductResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	existing, err := s.ProductRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	b := product.NewBuilder(existing).WithUpdatedAt(time.Now().UTC())
	if req.Name != nil {
		b.WithName(*req.Name)
	}
	if req.Description != nil {
		b.WithDescription(*req.Description)
	}
	updated := b.Build()

	if err := s.ProductRepo.Update(ctx, updated); err != nil {
		return nil, err
	}
	return dto.NewProductResponse(updated), nil
}

// DeleteProduct archives a product that no published price references.
func (s *productService) DeleteProduct(ctx context.Context, id string) error {
	if _, err := s.ProductRepo.GetByID(ctx, id); err != nil {
		return err
	}

	filter := types.NewNoLimitPriceFilter()
	filter.ProductIDs = []string{id}
	filter.AllowExpiredPrices = true
	count, err := s.PriceRepo.Count(ctx, filter)
	if err != nil {
		return err
	}
	if count > 0 {
		return ierr.NewError("product is referenced by prices").
			WithHint("Delete every price that references this product before deleting it").
			WithReportableDetails(map[string]interface{}{
				"product_id":  id,
				"price_count": count,
			}).
			Mark(ierr.ErrValidation)
	}

	return s.ProductRepo.Delete(ctx, id)
}
