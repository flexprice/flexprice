/*
Package costsheet provides repository interfaces for managing costsheet data access.
This package defines the contract for data persistence operations on costsheet entities.
*/
package costsheet

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the contract for costsheet data access operations.
// It provides methods for CRUD operations and querying costsheet records.
type Repository interface {
	// Create creates a new costsheet record
	Create(ctx context.Context, costsheet *Costsheet) error

	// GetByID retrieves a costsheet record by its ID
	GetByID(ctx context.Context, id string) (*Costsheet, error)

	// GetByTenantAndEnvironment retrieves costsheet records for a specific tenant and environment
	GetByTenantAndEnvironment(ctx context.Context, tenantID, environmentID string) ([]*Costsheet, error)

	// List retrieves costsheet records based on the provided filter
	List(ctx context.Context, filter *Filter) ([]*Costsheet, error)

	// Count returns the total number of costsheet records matching the filter
	Count(ctx context.Context, filter *Filter) (int64, error)

	// Update updates an existing costsheet record
	Update(ctx context.Context, costsheet *Costsheet) error

	// Delete soft deletes a costsheet record by setting its status to deleted
	Delete(ctx context.Context, id string) error

	// GetByName retrieves a costsheet record by name within a tenant and environment
	GetByName(ctx context.Context, tenantID, environmentID, name string) (*Costsheet, error)

	// ListByStatus retrieves costsheet records filtered by status
	ListByStatus(ctx context.Context, tenantID, environmentID string, status types.Status) ([]*Costsheet, error)

	// GetByLookupKey retrieves a costsheet record by lookup key within a tenant and environment
	GetByLookupKey(ctx context.Context, tenantID, environmentID, lookupKey string) (*Costsheet, error)
}
