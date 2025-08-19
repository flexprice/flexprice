package priceunit

import (
	"context"
)

// Repository defines the interface for price unit persistence
type Repository interface {

	// CRUD operations

	// Create creates a new price unit
	Create(ctx context.Context, unit *PriceUnit) error

	// List returns a list of pricing units based on filter
	List(ctx context.Context, filter *PriceUnitFilter) ([]*PriceUnit, error)

	// Count returns the total count of pricing units based on filter
	Count(ctx context.Context, filter *PriceUnitFilter) (int, error)

	// Update updates an existing pricing unit
	Update(ctx context.Context, unit *PriceUnit) error

	// Delete deletes a pricing unit by its ID
	Delete(ctx context.Context, id string) error

	// Get operations

	// GetByCode fetches a pricing unit by its code
	GetByCode(ctx context.Context, code string) (*PriceUnit, error)

	// Get fetches a pricing unit by its ID
	Get(ctx context.Context, id string) (*PriceUnit, error)
}
