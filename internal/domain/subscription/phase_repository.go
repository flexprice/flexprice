package subscription

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// SubscriprionPhaseRepository defines the interface for subscription phase operations
type SubscriptionPhaseRepository interface {
	// Create creates a new subscription phase
	Create(ctx context.Context, phase *SubscriptionPhase) error

	// CreateBulk creates multiple subscription phases in bulk
	CreateBulk(ctx context.Context, phases []*SubscriptionPhase) error

	// Get retrieves a subscription phase by ID
	Get(ctx context.Context, id string) (*SubscriptionPhase, error)

	// Update updates an existing subscription phase
	Update(ctx context.Context, phase *SubscriptionPhase) error

	// Delete deletes a subscription phase by ID
	Delete(ctx context.Context, id string) error

	// List retrieves subscription phases based on filter
	List(ctx context.Context, filter *types.SubscriptionPhaseFilter) ([]*SubscriptionPhase, error)

	// Count returns the count of subscription phases matching the filter
	Count(ctx context.Context, filter *types.SubscriptionPhaseFilter) (int, error)
}
