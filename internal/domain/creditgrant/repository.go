package creditgrant

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for credit grant repository
type Repository interface {
	// Create creates a new credit grant
	Create(ctx context.Context, creditGrant *CreditGrant) (*CreditGrant, error)

	// Get retrieves a credit grant by ID
	Get(ctx context.Context, id string) (*CreditGrant, error)

	// List retrieves credit grants based on filter
	List(ctx context.Context, filter *types.CreditGrantFilter) ([]*CreditGrant, error)

	// ListAll retrieves all credit grants
	ListAll(ctx context.Context, filter *types.CreditGrantFilter) ([]*CreditGrant, error)

	// Count counts credit grants based on filter
	Count(ctx context.Context, filter *types.CreditGrantFilter) (int, error)

	// Update updates an existing credit grant
	Update(ctx context.Context, creditGrant *CreditGrant) (*CreditGrant, error)

	// Delete deletes a credit grant by ID
	Delete(ctx context.Context, id string) error

	// GetByPlan retrieves credit grants for a specific plan
	GetByPlan(ctx context.Context, planID string) ([]*CreditGrant, error)

	// GetBySubscription retrieves credit grants for a specific subscription
	GetBySubscription(ctx context.Context, subscriptionID string) ([]*CreditGrant, error)

	// FindAllActiveRecurringGrants finds all active recurring credit grants
	FindAllActiveRecurringGrants(ctx context.Context) ([]*CreditGrant, error)
}
