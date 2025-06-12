package costsheet

import (
	"context"
)

// Repository defines the interface for costsheet data persistence operations.
// It provides methods for creating, reading, updating, and querying costsheets.
type Repository interface {
	// Create stores a new costsheet record
	Create(ctx context.Context, costsheet *Costsheet) error

	// Get retrieves a costsheet by its ID
	Get(ctx context.Context, id string) (*Costsheet, error)

	// Update modifies an existing costsheet record
	Update(ctx context.Context, costsheet *Costsheet) error

	// Delete removes a costsheet record
	Delete(ctx context.Context, id string) error

	// List retrieves costsheets based on the provided filter
	List(ctx context.Context, filter *Filter) ([]*Costsheet, error)

	// GetByMeterAndPrice retrieves a costsheet by meter ID and price ID
	GetByMeterAndPrice(ctx context.Context, meterID, priceID string) (*Costsheet, error)
}
