package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/product"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateProductRequest represents the request to create a product
type CreateProductRequest struct {
	Name        string `json:"name" validate:"required"`
	LookupKey   string `json:"lookup_key" validate:"required"`
	Description string `json:"description"`
}

func (r *CreateProductRequest) Validate() error {
	return validator.ValidateRequest(r)
}

func (r *CreateProductRequest) ToProduct() *product.Product {
	now := time.Now().UTC()
	return product.NewBuilder(nil).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRODUCT)).
		WithName(r.Name).
		WithDescription(r.Description).
		WithLookupKey(r.LookupKey).
		WithStatus(types.StatusPublished).
		WithCreatedAt(now).
		WithUpdatedAt(now).
		Build()
}

// UpdateProductRequest represents the request to update a product
type UpdateProductRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

func (r *UpdateProductRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// ProductResponse represents the product response
type ProductResponse struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	LookupKey   string       `json:"lookup_key"`
	Status      types.Status `json:"status"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

func NewProductResponse(p *product.Product) *ProductResponse {
	if p == nil {
		return nil
	}
	return &ProductResponse{
		ID:          p.ID(),
		Name:        p.Name(),
		Description: p.Description(),
		LookupKey:   p.LookupKey(),
		Status:      p.Status(),
		CreatedAt:   p.CreatedAt(),
		UpdatedAt:   p.UpdatedAt(),
	}
}

// ListProductsResponse represents the response for listing products
type ListProductsResponse = types.ListResponse[*ProductResponse] // @name ListProductsResponse
