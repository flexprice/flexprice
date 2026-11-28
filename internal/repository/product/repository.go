package product

import (
	"context"

	domainProduct "github.com/flexprice/flexprice/internal/domain/product"
)

// Repository persists products.
type Repository interface {
	Create(ctx context.Context, p *domainProduct.Product) error
	GetByID(ctx context.Context, id string) (*domainProduct.Product, error)
	Update(ctx context.Context, p *domainProduct.Product) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *Filter) ([]*domainProduct.Product, error)
	Count(ctx context.Context, filter *Filter) (int, error)
}
