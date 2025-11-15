package priceunit

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for price unit persistence
type Repository interface {
	// CRUD operations

	// Create creates a new price unit and returns the created price unit
	Create(ctx context.Context, priceUnit *PriceUnit) (*PriceUnit, error)

	// List returns a list of pricing units based on filter
	List(ctx context.Context, filter *types.PriceUnitFilter) ([]*PriceUnit, error)

	// Count returns the total count of pricing units based on filter
	Count(ctx context.Context, filter *types.PriceUnitFilter) (int, error)

	// Update updates an existing pricing unit and returns the updated price unit
	Update(ctx context.Context, priceUnit *PriceUnit) (*PriceUnit, error)

	// Delete deletes a pricing unit
	Delete(ctx context.Context, priceUnit *PriceUnit) error

	// Get operations

	// Get fetches a pricing unit by its ID
	Get(ctx context.Context, id string) (*PriceUnit, error)

	GetByCode(ctx context.Context, code string) (*PriceUnit, error)
}
